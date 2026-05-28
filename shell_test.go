package cachex_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/diegosperes/cachex"
)

// fakeEntry is a minimal Entry[K] handle used to drive the mock policy. The
// shell treats Entry as opaque, so a struct carrying only the key suffices.
type fakeEntry[K comparable] struct {
	key K
}

func (e fakeEntry[K]) Key() K { return e.key }

func entry[K comparable](k K) cachex.Entry[K] { return fakeEntry[K]{key: k} }

// newCache constructs a shell over a mock policy whose Capacity returns cap.
// Capacity() is allowed any number of times because NewWithPolicy and
// Cache.Capacity both read it.
func newCache[V any](
	t *testing.T,
	capacity int,
	opts ...cachex.Option[string, V],
) (cachex.Cache[string, V], *MockPolicy[string]) {
	t.Helper()

	ctrl := gomock.NewController(t)
	policy := NewMockPolicy[string](ctrl)
	policy.EXPECT().Capacity().Return(capacity).AnyTimes()

	c, err := cachex.NewWithPolicy[string, V](policy, opts...)
	require.NoError(t, err)
	require.NotNil(t, c)

	return c, policy
}

func TestNewWithPolicy(t *testing.T) {
	t.Parallel()

	t.Run("rejects non-positive capacity", func(t *testing.T) {
		t.Parallel()

		for _, capacity := range []int{0, -1, -1024} {
			ctrl := gomock.NewController(t)
			policy := NewMockPolicy[string](ctrl)
			policy.EXPECT().Capacity().Return(capacity).AnyTimes()

			c, err := cachex.NewWithPolicy[string, int](policy)

			assert.Nil(t, c)
			assert.ErrorIs(t, err, cachex.ErrInvalidCapacity)
		}
	})

	t.Run("accepts positive capacity", func(t *testing.T) {
		t.Parallel()

		c, _ := newCache[int](t, 8)
		assert.Equal(t, 8, c.Capacity())
		assert.Equal(t, 0, c.Size())
	})

	t.Run("panics on nil policy", func(t *testing.T) {
		t.Parallel()

		assert.PanicsWithValue(t, "cachex: NewWithPolicy requires a non-nil Policy", func() {
			_, _ = cachex.NewWithPolicy[string, int](nil)
		})
	})
}

func TestCallSequence(t *testing.T) {
	t.Parallel()

	t.Run("Put new key calls Admit and stores the admitted handle", func(t *testing.T) {
		t.Parallel()

		c, policy := newCache[int](t, 4)
		h := entry("a")
		policy.EXPECT().Admit("a").Return(h, nil, false)

		c.Put("a", 1)

		assert.Equal(t, 1, c.Size())
		v, ok := c.Peek("a") // Peek must not call the policy.
		assert.True(t, ok)
		assert.Equal(t, 1, v)
	})

	t.Run("Put existing key updates value and calls Touch", func(t *testing.T) {
		t.Parallel()

		c, policy := newCache[int](t, 4)
		h := entry("a")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(h, nil, false),
			policy.EXPECT().Touch(h),
		)

		c.Put("a", 1)
		c.Put("a", 2)

		assert.Equal(t, 1, c.Size())
		v, ok := c.Peek("a")
		assert.True(t, ok)
		assert.Equal(t, 2, v)
	})

	t.Run("Get hit calls Touch and returns the value", func(t *testing.T) {
		t.Parallel()

		c, policy := newCache[int](t, 4)
		h := entry("a")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(h, nil, false),
			policy.EXPECT().Touch(h),
		)

		c.Put("a", 7)
		v, ok := c.Get("a")

		assert.True(t, ok)
		assert.Equal(t, 7, v)
	})

	t.Run("Get miss returns zero value and calls no policy method", func(t *testing.T) {
		t.Parallel()

		c, _ := newCache[int](t, 4) // no Touch/Admit expectation => any call fails.

		v, ok := c.Get("missing")
		assert.False(t, ok)
		assert.Zero(t, v)
	})

	t.Run("Peek hit returns the value without any policy call", func(t *testing.T) {
		t.Parallel()

		c, policy := newCache[int](t, 4)
		h := entry("a")
		policy.EXPECT().Admit("a").Return(h, nil, false)

		c.Put("a", 9)
		v, ok := c.Peek("a")

		assert.True(t, ok)
		assert.Equal(t, 9, v)
	})

	t.Run("Peek miss returns zero value and calls no policy method", func(t *testing.T) {
		t.Parallel()

		c, _ := newCache[int](t, 4)

		v, ok := c.Peek("missing")
		assert.False(t, ok)
		assert.Zero(t, v)
	})

	t.Run("Delete present calls Remove and reports true", func(t *testing.T) {
		t.Parallel()

		evicted := false
		c, policy := newCache[int](t, 4, cachex.WithOnEvict[string, int](func(string, int) {
			evicted = true
		}))
		h := entry("a")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(h, nil, false),
			policy.EXPECT().Remove(h),
		)

		c.Put("a", 1)
		ok := c.Delete("a")

		assert.True(t, ok)
		assert.Equal(t, 0, c.Size())
		assert.False(t, evicted, "OnEvict must not fire on explicit Delete")
		_, found := c.Peek("a")
		assert.False(t, found)
	})

	t.Run("Delete absent reports false and calls no policy method", func(t *testing.T) {
		t.Parallel()

		c, _ := newCache[int](t, 4)
		assert.False(t, c.Delete("missing"))
	})

	t.Run("Clear calls Reset and drops all entries without OnEvict", func(t *testing.T) {
		t.Parallel()

		evicted := false
		c, policy := newCache[int](t, 4, cachex.WithOnEvict[string, int](func(string, int) {
			evicted = true
		}))
		ha, hb := entry("a"), entry("b")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(ha, nil, false),
			policy.EXPECT().Admit("b").Return(hb, nil, false),
			policy.EXPECT().Reset(),
		)

		c.Put("a", 1)
		c.Put("b", 2)
		c.Clear()

		assert.Equal(t, 0, c.Size())
		assert.False(t, evicted, "OnEvict must not fire on Clear")
		_, ok := c.Peek("a")
		assert.False(t, ok)
	})
}

func TestEviction(t *testing.T) {
	t.Parallel()

	t.Run("Admit eviction recovers victim value, fires OnEvict, then stores admittee", func(t *testing.T) {
		t.Parallel()

		var gotKey string
		var gotVal int
		onEvictCalls := 0
		c, policy := newCache[int](t, 1, cachex.WithOnEvict[string, int](func(k string, v int) {
			gotKey, gotVal = k, v
			onEvictCalls++
		}))

		hA := entry("a")
		hB := entry("b")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(hA, nil, false),
			// Admitting "b" evicts "a": the shell must recover a's value (1)
			// and fire OnEvict for it before completing b's insertion.
			policy.EXPECT().Admit("b").DoAndReturn(
				func(string) (cachex.Entry[string], cachex.Entry[string], bool) {
					assert.Equal(t, 0, onEvictCalls, "OnEvict must fire after Admit returns, not during")
					return hB, hA, true
				},
			),
		)

		c.Put("a", 1)
		c.Put("b", 2)

		assert.Equal(t, 1, onEvictCalls)
		assert.Equal(t, "a", gotKey)
		assert.Equal(t, 1, gotVal, "OnEvict must receive the victim's stored value")
		assert.Equal(t, 1, c.Size())

		_, ok := c.Peek("a")
		assert.False(t, ok, "victim must be removed from the map")
		v, ok := c.Peek("b")
		assert.True(t, ok)
		assert.Equal(t, 2, v)
	})

	t.Run("eviction without OnEvict configured still removes the victim", func(t *testing.T) {
		t.Parallel()

		c, policy := newCache[int](t, 1)
		hA, hB := entry("a"), entry("b")
		gomock.InOrder(
			policy.EXPECT().Admit("a").Return(hA, nil, false),
			policy.EXPECT().Admit("b").Return(hB, hA, true),
		)

		c.Put("a", 1)
		c.Put("b", 2)

		assert.Equal(t, 1, c.Size())
		_, ok := c.Peek("a")
		assert.False(t, ok)
	})
}

func TestSizeAndCapacity(t *testing.T) {
	t.Parallel()

	c, policy := newCache[int](t, 16)
	policy.EXPECT().Admit(gomock.Any()).DoAndReturn(
		func(k string) (cachex.Entry[string], cachex.Entry[string], bool) {
			return entry(k), nil, false
		},
	).Times(3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	assert.Equal(t, 3, c.Size(), "Size reads the shell map, not policy.Size")
	assert.Equal(t, 16, c.Capacity(), "Capacity is reported verbatim from the policy")
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()

	// A large capacity avoids eviction so the policy contract stays simple
	// under concurrency; the goal is to exercise the shell's single mutex.
	const capacity = 1 << 16
	c, policy := newCache[int](t, capacity)
	policy.EXPECT().Admit(gomock.Any()).DoAndReturn(
		func(k string) (cachex.Entry[string], cachex.Entry[string], bool) {
			return entry(k), nil, false
		},
	).AnyTimes()
	policy.EXPECT().Touch(gomock.Any()).AnyTimes()
	policy.EXPECT().Remove(gomock.Any()).AnyTimes()

	const goroutines = 16
	const opsPerGoroutine = 256

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				k := fmt.Sprintf("g%d-k%d", g, i)
				c.Put(k, i)
				c.Get(k)
				c.Peek(k)
				c.Delete(k)
				_ = c.Size()
			}
		}(g)
	}
	wg.Wait()

	assert.Equal(t, 0, c.Size())
}

func BenchmarkHotPath(b *testing.B) {
	ctrl := gomock.NewController(b)
	policy := NewMockPolicy[string](ctrl)
	policy.EXPECT().Capacity().Return(1024).AnyTimes()
	policy.EXPECT().Admit(gomock.Any()).DoAndReturn(
		func(k string) (cachex.Entry[string], cachex.Entry[string], bool) {
			return entry(k), nil, false
		},
	).AnyTimes()
	policy.EXPECT().Touch(gomock.Any()).AnyTimes()

	c, err := cachex.NewWithPolicy[string, int](policy)
	require.NoError(b, err)
	c.Put("hot", 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Get("hot")
	}
}
