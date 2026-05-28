# Public Interface

> **Status: design specification. No code exists yet.** This document is the source of truth for the API shape that will be implemented.

`cachex` targets Go 1.18+ for generics support. The published library depends only on the standard library; its test suite uses Testify and GoMock (see [`.claude/rules/testing.md`](../.claude/rules/testing.md)).

---

## 1. Core: `Cache`

`Cache` is the contract every cache implementation in `cachex` satisfies. It is intentionally minimal — anything that can be layered on top lives in extension packages (see §4).

```go
package cachex

type Cache[K comparable, V any] interface {
    // Get returns the value associated with key and true if present,
    // or the zero value and false otherwise. Counts as an access for
    // recency- and frequency-based policies.
    Get(key K) (V, bool)

    // Peek returns the value without recording an access. Useful for
    // diagnostics, admission filters, and tests that must not perturb
    // the eviction order.
    Peek(key K) (V, bool)

    // Put inserts or updates the entry for key. If the cache is at
    // capacity and key is new, the policy's eviction victim is removed
    // first; the OnEvict callback (if configured) fires for that victim.
    Put(key K, value V)

    // Delete removes key and returns true if it was present. OnEvict
    // does not fire for explicit deletes.
    Delete(key K) bool

    // Size returns the current number of entries.
    Size() int

    // Capacity returns the configured maximum number of entries.
    Capacity() int

    // Clear removes all entries. OnEvict does not fire.
    Clear()
}
```

### Concurrency

All `Cache` methods are safe for concurrent use by multiple goroutines.

This is a deliberate departure from the stdlib `container/list` / `map` convention of "caller locks." The intrusive data structures inside an O(1) LRU/LFU make a `sync.Map`, style lock-free design impractical without losing the worst-case guarantee. Single-goroutine callers pay one uncontended mutex acquisition per op, which is cheap relative to map access.

### Iteration, batch ops, range scans

Intentionally absent from the core interface. They are either ill-defined for an LRU/LFU under concurrent mutation, or trivially layerable. See §4.3 for the planned extension package.

---

## 2. Built-in Implementations

Both built-ins use functional options for configuration. Each `New` returns a concrete struct type (not the `Cache` interface) so that implementation-specific helpers can be added later without breaking callers; the returned type *satisfies* `cachex.Cache[K, V]`.

### 2.1 LRU

```go
import "github.com/diegosperes/cachex/lru"

cache, err := lru.New[string, []byte](1024)
if err != nil { /* ... */ }

cache.Put("k", value)
v, ok := cache.Get("k")
```

`Get`, `Peek`, `Put`, and `Delete` are strict O(1). Eviction removes the entry whose last access is furthest in the past.

**Signature**

```go
package lru

// New returns an LRU cache with the given capacity.
// Returns ErrInvalidCapacity if capacity <= 0.
func New[K comparable, V any](capacity int, opts ...Option[K, V]) (*Cache[K, V], error)

// Option configures an LRU cache at construction.
type Option[K comparable, V any] func(*config[K, V])

// WithOnEvict registers a callback invoked synchronously when an entry is
// evicted by the policy (not on explicit Delete or Clear). The callback
// runs while the cache lock is held; do not call back into the cache from it.
func WithOnEvict[K comparable, V any](fn func(K, V)) Option[K, V]
```

### 2.2 LFU

```go
import "github.com/diegosperes/cachex/lfu"

cache, err := lfu.New[string, []byte](1024)
```

`Get`, `Peek`, `Put`, and `Delete` are strict O(1) via the bucket-list O(1) LFU construction. Eviction removes the entry with the lowest hit count; ties are broken by LRU order within the frequency bucket.

**Signature**

```go
package lfu

func New[K comparable, V any](capacity int, opts ...Option[K, V]) (*Cache[K, V], error)

type Option[K comparable, V any] func(*config[K, V])

func WithOnEvict[K comparable, V any](fn func(K, V)) Option[K, V]
```

---

## 3. Errors and Sentinel Values

The package exposes a small set of sentinel errors. Runtime cache operations (`Get`, `Put`, `Delete`, `Peek`) never return errors — presence is reported via the `(value, bool)` pattern, and capacity overflow is handled by eviction.

```go
package cachex

// ErrInvalidCapacity is returned by constructors when capacity <= 0.
var ErrInvalidCapacity = errors.New("cachex: capacity must be positive")
```

Constructors return errors rather than panicking. Panicking on caller-supplied integers is unfriendly to library consumers who validate capacity from config files or remote sources.

---

## 4. Extensibility

### 4.1 Custom Policies: `Policy`

Custom eviction strategies plug into a generic cache shell via the `Policy` interface. The shell owns storage, synchronization, and value bookkeeping; the policy owns *ordering* and the *eviction choice*.

The key design point: **the policy does not track keys in a parallel data structure.** Instead, the shell stores an opaque per-entry handle (`Entry[K]`) returned by the policy at admission time. On every subsequent operation, the shell hands that same handle back to the policy. This eliminates duplicated key bookkeeping, removes the need for the policy to be told which key was evicted, and collapses the previous three-call `Evict → Remove → Add` sequence into one call.

```go
package cachex

// Entry is an opaque handle a Policy attaches to each cached key.
// The shell stores one Entry per live key and passes it back to the
// policy on Touch / Remove. Implementations typically embed an
// intrusive list node and the key itself.
type Entry[K comparable] interface {
    Key() K
}

type Policy[K comparable] interface {
    // Admit registers a new key. If the policy is at capacity, it
    // selects and returns an eviction victim (evicted = its Entry,
    // ok = true). The shell removes that victim from its map and
    // fires OnEvict before completing the insertion. The returned
    // admitted Entry is what the shell stores for the new key.
    //
    // Capacity is owned by the policy: the shell calls Admit on every
    // new-key Put and trusts the policy's decision.
    Admit(key K) (admitted Entry[K], evicted Entry[K], ok bool)

    // Touch records an access against an existing entry.
    Touch(e Entry[K])

    // Remove drops an entry from the policy. Called on explicit Delete.
    Remove(e Entry[K])

    // Reset drops all entries. Called on Cache.Clear. Must leave the
    // policy in the same state as a freshly constructed instance.
    Reset()

    // Size returns the number of entries the policy currently tracks.
    // Used by the shell only for assertions in tests; production code
    // reads Size from the shell's own map.
    Size() int

    // Capacity returns the maximum number of entries the policy will hold.
    // The shell reports this verbatim from Cache.Capacity. NewWithPolicy
    // rejects a policy whose Capacity is <= 0 with ErrInvalidCapacity.
    Capacity() int
}

// NewWithPolicy builds a Cache that delegates eviction decisions to policy.
// Capacity is determined by the policy and is reported via Cache.Capacity.
func NewWithPolicy[K comparable, V any](
    policy Policy[K],
    opts ...Option[K, V],
) (Cache[K, V], error)

type Option[K comparable, V any] func(*config[K, V])
func WithOnEvict[K comparable, V any](fn func(K, V)) Option[K, V]
```

**Why the handle?** The previous interface required the policy to maintain its own `map[K]*node` alongside the shell's `map[K]V`, doubling memory and creating a synchronization invariant between two structures. With `Entry[K]`, the shell's map stores `Entry[K]` directly; the entry *is* the node the policy needs. There is exactly one bookkeeping structure per key.

**Why is `Admit` the only mutator that can evict?** Eviction can only be triggered by an insertion that pushes the cache past capacity. Folding the eviction decision into `Admit` removes the ordering contract the shell used to leak (`Evict → Remove → Add`) and makes the policy's contract atomic and order-free.

#### Call sequence

| Cache operation       | Policy call(s)        | Notes                                          |
| --------------------- | --------------------- | ---------------------------------------------- |
| `Put` (new key)       | `Admit(key)`          | Policy may return an evicted Entry; OnEvict fires for it. |
| `Put` (existing key)  | `Touch(entry)`        | Shell updates the stored value.                |
| `Get` (hit)           | `Touch(entry)`        |                                                |
| `Peek` (hit)          | none                  |                                                |
| `Get` / `Peek` (miss) | none                  |                                                |
| `Delete` (present)    | `Remove(entry)`       |                                                |
| `Delete` (absent)     | none                  |                                                |
| `Clear`               | `Reset()`             | Required; no longer implementation-defined.    |

All policy callbacks run with the shell's lock held. A `Policy` implementation therefore does not need to be internally synchronized.

The built-in LRU and LFU are themselves expressible as `Policy` implementations and live in `github.com/diegosperes/cachex/policy/lru` and `github.com/diegosperes/cachex/policy/lfu` for reuse by custom shells.

### 4.3 Planned Extensions (out of scope for v1.0)

These belong in separate sub-packages once the core stabilizes:

- `cachex/ttl` — TTL wrapper that decorates any `Cache`.
- `cachex/sharded` — N-way sharded cache for high-contention workloads.

Keeping them out of the core preserves the minimal interface and avoids forcing dependencies (e.g. on `time`) onto users who don't need them.
