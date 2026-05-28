# 1. Opaque `Entry[K]` handle for policy bookkeeping

- Status: Accepted
- Date: 2026-05-27

## Context

A cache shell owns storage and value bookkeeping; a `Policy` owns eviction
ordering and the eviction choice. The two must agree on which node a key maps
to. An earlier shape had the policy keep its own `map[K]*node` alongside the
shell's `map[K]V`. That created two problems:

1. **Duplicated key bookkeeping.** Every key lived in two maps, doubling the
   per-entry memory and the hashing/allocation cost of a `Put`.
2. **A cross-structure invariant.** The shell's map and the policy's map had to
   be kept consistent under concurrency and across every operation, and the
   policy had to be *told which key was evicted* so it could delete from its
   own map — leaking an ordering contract (`Evict → Remove → Add`) into the
   shell.

## Decision

The policy mints an opaque per-entry handle, `Entry[K]` (with a single
`Key() K` method), at admission time. The shell stores exactly one handle per
live key in its own map (`record{handle, value}`) and hands that same handle
back to the policy on `Touch` and `Remove`. The policy embeds whatever it needs
(typically an intrusive list/heap node plus the key) inside its `Entry`
implementation.

Eviction is folded into a single `Admit(key) (admitted, evicted Entry[K], ok)`
call: a new-key insertion is the only thing that can push the cache past
capacity, so it is the only operation that can evict. When `ok` is true the
shell recovers the victim's value by looking up `evicted.Key()` in its own map,
deletes it, fires `OnEvict`, and then stores the admitted handle.

## Consequences

- **One bookkeeping structure per key.** The `Entry` *is* the node the policy
  needs; there is no parallel key map and no cross-structure invariant.
- **Order-free, atomic policy contract.** The three-call `Evict → Remove → Add`
  sequence collapses into one `Admit`. The policy never needs to be told which
  key was evicted — it returns the victim's handle directly.
- **The shell never inspects the handle.** It only calls `Key()` (to recover a
  victim's value) and passes the handle back verbatim, so policies are free to
  choose their internal node representation.
- **Trust boundary.** If a policy returns a victim handle whose key the shell
  does not hold, that violates the policy contract; the shell drops it silently
  rather than firing `OnEvict` with a zero value.
