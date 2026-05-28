# 2. "Library locks" concurrency model: a single `sync.Mutex`

- Status: Accepted
- Date: 2026-05-27

## Context

`Cache` must be safe for concurrent use by multiple goroutines (spec §1). Two
broad models were available:

- **Caller locks** (the `container/list` / `map` stdlib convention): the type
  is not synchronized; callers serialize access themselves.
- **Library locks**: the cache synchronizes internally and presents a
  goroutine-safe API.

The intrusive O(1) LRU/LFU data structures that policies build mutate shared
links on nearly every operation — including reads, because `Get` records an
access via `Policy.Touch`, which reorders recency/frequency state. A
lock-free, `sync.Map`-style design cannot preserve the worst-case O(1)
eviction guarantee without substantial complexity.

## Decision

The shell uses **library locks**: a single `sync.Mutex` guards every method.
All policy callbacks (`Admit`, `Touch`, `Remove`, `Reset`) and the `OnEvict`
callback run with that lock held. Consequences:

- A `Policy` implementation needs no internal synchronization.
- `OnEvict` must not call back into the cache, or it self-deadlocks; this is
  documented on `WithOnEvict`.

`sync.RWMutex` was rejected: `Get` mutates recency via `Touch`, so reads are
not read-only. An `RWMutex` would add overhead (a more expensive lock and the
read/write-lock decision) without ever enabling genuinely shared access on the
hot path.

A `nil` policy panics at construction (`NewWithPolicy`) rather than returning a
sentinel error. A nil policy can only be a programming defect — unlike a
non-positive capacity, which may legitimately arrive from a config file or
remote source and therefore returns `ErrInvalidCapacity`. Fail fast on the
defect; return an error on the recoverable input.

## Consequences

- Single-goroutine callers pay one uncontended mutex acquisition per
  operation, which is cheap relative to the map access it guards.
- High-contention workloads serialize on the single mutex. When that becomes a
  bottleneck, the planned `cachex/sharded` package (N-way sharding) is the
  intended remedy; the core stays simple.
- The model is deterministic and easy to reason about: there is exactly one
  lock, and every state mutation — shell map, policy, and eviction callback —
  happens under it.
