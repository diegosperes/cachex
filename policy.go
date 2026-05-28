package cachex

//go:generate go run go.uber.org/mock/mockgen@latest -destination=mock_policy_test.go -package=cachex_test -typed github.com/diegosperes/cachex Policy

// Entry is an opaque handle a Policy attaches to each cached key. The cache
// shell stores exactly one Entry per live key and hands that same Entry back
// to the policy on Touch and Remove. Implementations typically embed an
// intrusive list node together with the key.
type Entry[K comparable] interface {
	// Key returns the key this entry represents.
	Key() K
}

// Policy owns eviction ordering and the eviction choice for a cache. The shell
// owns storage, synchronization, and value bookkeeping and delegates ordering
// decisions to the Policy. A Policy does not track keys in a parallel data
// structure: the shell stores the opaque [Entry] handle the policy mints at
// admission and passes it back on every subsequent operation.
//
// All Policy methods are invoked with the shell's lock held, so a Policy
// implementation does not need to be internally synchronized.
type Policy[K comparable] interface {
	// Admit registers a new key. If the policy is at capacity it selects an
	// eviction victim and returns its Entry as evicted with ok true; the shell
	// then removes that victim from its map and fires OnEvict before completing
	// the insertion. The returned admitted Entry is what the shell stores for
	// the new key. Capacity is owned by the policy: the shell calls Admit on
	// every new-key Put and trusts the policy's decision.
	Admit(key K) (admitted Entry[K], evicted Entry[K], ok bool)

	// Touch records an access against an existing entry.
	Touch(e Entry[K])

	// Remove drops an entry from the policy. Called on explicit Delete.
	Remove(e Entry[K])

	// Reset drops all entries. Called on Cache.Clear. It must leave the policy
	// in the same state as a freshly constructed instance.
	Reset()

	// Size returns the number of entries the policy currently tracks. The shell
	// uses it only for test assertions; production code reads size from the
	// shell's own map.
	Size() int

	// Capacity returns the maximum number of entries the policy will hold. The
	// shell reports this verbatim from Cache.Capacity, and NewWithPolicy
	// rejects a policy whose Capacity is not positive with ErrInvalidCapacity.
	Capacity() int
}
