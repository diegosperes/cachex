// Package cachex provides generic, policy-driven in-memory caches for Go.
//
// The root package defines the core contracts — [Cache], [Entry], and
// [Policy] — together with a single generic cache shell ([NewWithPolicy])
// that owns storage, synchronization, and value bookkeeping while delegating
// eviction ordering to a pluggable [Policy]. The published library depends
// only on the standard library.
package cachex

// Cache is the contract every cache implementation in cachex satisfies. It is
// intentionally minimal; behavior that can be layered on top lives in
// extension packages. All methods are safe for concurrent use by multiple
// goroutines.
type Cache[K comparable, V any] interface {
	// Get returns the value associated with key and true if present, or the
	// zero value and false otherwise. A hit counts as an access for recency-
	// and frequency-based policies.
	Get(key K) (V, bool)

	// Peek returns the value associated with key and true if present, without
	// recording an access. It does not perturb the eviction order.
	Peek(key K) (V, bool)

	// Put inserts or updates the entry for key. If key is new and the cache is
	// at capacity, the policy's eviction victim is removed first and the
	// OnEvict callback (if configured) fires for that victim.
	Put(key K, value V)

	// Delete removes key and returns true if it was present. OnEvict does not
	// fire for explicit deletes.
	Delete(key K) bool

	// Size returns the current number of entries.
	Size() int

	// Capacity returns the configured maximum number of entries.
	Capacity() int

	// Clear removes all entries. OnEvict does not fire.
	Clear()
}
