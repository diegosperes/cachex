package cachex

import "sync"

// record is the shell's single per-key bookkeeping unit: the opaque policy
// handle minted at admission alongside the stored value. Keeping the handle
// here means the policy never maintains a parallel key map.
type record[K comparable, V any] struct {
	handle Entry[K]
	value  V
}

// shell is the generic, policy-driven cache. It owns storage,
// synchronization, and value bookkeeping; the Policy owns eviction ordering
// and the eviction choice. Fields are ordered largest-to-smallest to
// minimize struct padding.
type shell[K comparable, V any] struct {
	mu      sync.Mutex
	policy  Policy[K]
	entries map[K]record[K, V]
	onEvict func(K, V)
}

// NewWithPolicy builds a Cache that delegates eviction decisions to policy.
// Capacity is owned by the policy and reported verbatim via Cache.Capacity;
// NewWithPolicy returns ErrInvalidCapacity when policy.Capacity() is not
// positive.
//
// A nil policy is a programming error (there is no meaningful cache without
// one) and panics rather than returning an error: unlike a caller-supplied
// capacity read from config, a nil policy can only originate from a code
// defect, so failing fast at construction is the safer contract.
func NewWithPolicy[K comparable, V any](
	policy Policy[K],
	opts ...Option[K, V],
) (Cache[K, V], error) {
	if policy == nil {
		panic("cachex: NewWithPolicy requires a non-nil Policy")
	}

	if policy.Capacity() <= 0 {
		return nil, ErrInvalidCapacity
	}

	var cfg config[K, V]
	for _, opt := range opts {
		opt(&cfg)
	}

	return &shell[K, V]{
		policy:  policy,
		entries: make(map[K]record[K, V]),
		onEvict: cfg.onEvict,
	}, nil
}

// Get returns the value for key and true if present, recording an access via
// the policy's Touch. A miss makes no policy call.
func (s *shell[K, V]) Get(key K) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.entries[key]
	if !ok {
		var zero V
		return zero, false
	}

	s.policy.Touch(rec.handle)
	return rec.value, true
}

// Peek returns the value for key and true if present without recording an
// access. It never calls the policy.
func (s *shell[K, V]) Peek(key K) (V, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.entries[key]
	if !ok {
		var zero V
		return zero, false
	}

	return rec.value, true
}

// Put inserts or updates key. For an existing key it updates the value and
// calls Touch. For a new key it calls Admit; if the policy returns an eviction
// victim, the shell recovers the victim's value, removes it, fires OnEvict
// while the lock is held, and then stores the admitted handle.
func (s *shell[K, V]) Put(key K, value V) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, ok := s.entries[key]; ok {
		rec.value = value
		s.entries[key] = rec
		s.policy.Touch(rec.handle)
		return
	}

	admitted, evicted, ok := s.policy.Admit(key)
	if ok {
		s.evict(evicted)
	}

	s.entries[key] = record[K, V]{handle: admitted, value: value}
}

// evict removes the policy-selected victim from the map and fires OnEvict for
// it. It runs with the lock held. The map lookup recovers the victim's value
// so the shell need not track it elsewhere.
func (s *shell[K, V]) evict(victim Entry[K]) {
	key := victim.Key()
	rec, ok := s.entries[key]
	if !ok {
		// The policy reported a victim the shell does not hold. This violates
		// the policy contract; drop it silently rather than fire OnEvict with
		// a zero value.
		return
	}

	delete(s.entries, key)
	if s.onEvict != nil {
		s.onEvict(key, rec.value)
	}
}

// Delete removes key and reports whether it was present, calling Remove on the
// policy when present. OnEvict does not fire for explicit deletes.
func (s *shell[K, V]) Delete(key K) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.entries[key]
	if !ok {
		return false
	}

	s.policy.Remove(rec.handle)
	delete(s.entries, key)
	return true
}

// Size returns the current number of entries, read from the shell's own map.
func (s *shell[K, V]) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.entries)
}

// Capacity returns the configured maximum number of entries, reported verbatim
// from the policy.
func (s *shell[K, V]) Capacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.policy.Capacity()
}

// Clear removes all entries and resets the policy. OnEvict does not fire.
func (s *shell[K, V]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.policy.Reset()
	s.entries = make(map[K]record[K, V])
}
