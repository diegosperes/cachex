# cachex

Generic, policy-driven in-memory caches for Go (1.22+).

The root `cachex` package defines the core contracts — `Cache`, `Entry`, and
`Policy` — together with a single generic cache **shell** that owns storage,
synchronization, and value bookkeeping while delegating eviction *ordering* to
a pluggable `Policy`. The published library depends only on the standard
library.

```sh
go get github.com/diegosperes/cachex
```

## Concepts

- **`Cache[K, V]`** — the minimal contract every cache satisfies: `Get`,
  `Peek`, `Put`, `Delete`, `Size`, `Capacity`, `Clear`. All methods are safe
  for concurrent use.
- **`Policy[K]`** — owns eviction ordering and the eviction choice. The shell
  calls it under a lock, so a policy needs no internal synchronization.
- **`Entry[K]`** — an opaque handle the policy mints at admission. The shell
  stores exactly one `Entry` per live key and hands it back on `Touch` /
  `Remove`, so the policy never keeps a parallel key map. See
  [docs/adr/0001-entry-handle.md](docs/adr/0001-entry-handle.md).

`Get`, `Peek`, `Put`, and `Delete` carry no inherent allocation overhead in the
shell; cost is dominated by the policy and one uncontended mutex acquisition
per call. The "library locks" concurrency model is explained in
[docs/adr/0002-concurrency-model.md](docs/adr/0002-concurrency-model.md).

## Building a cache with a custom policy

`NewWithPolicy` wraps any `Policy` in the shell. Capacity is owned by the
policy and reported verbatim via `Cache.Capacity`; a policy whose `Capacity()`
is not positive is rejected with `ErrInvalidCapacity`. A `nil` policy is a
programming error and panics.

```go
package main

import (
	"container/list"
	"fmt"

	"github.com/diegosperes/cachex"
)

// fifoEntry is the opaque handle the policy attaches to each key. It embeds
// the intrusive list element so Remove/Touch are O(1).
type fifoEntry struct {
	elem *list.Element
	key  string
}

func (e *fifoEntry) Key() string { return e.key }

// fifo is a first-in-first-out eviction policy: the oldest admitted key is the
// victim. It implements cachex.Policy[string]. The shell calls every method
// under its lock, so no internal synchronization is needed.
type fifo struct {
	order    *list.List // front = oldest, back = newest
	capacity int
}

func newFIFO(capacity int) *fifo {
	return &fifo{order: list.New(), capacity: capacity}
}

func (f *fifo) Admit(key string) (admitted, evicted cachex.Entry[string], ok bool) {
	if f.order.Len() >= f.capacity {
		oldest := f.order.Front()
		victim := oldest.Value.(*fifoEntry)
		f.order.Remove(oldest)
		evicted, ok = victim, true
	}
	e := &fifoEntry{key: key}
	e.elem = f.order.PushBack(e)
	return e, evicted, ok
}

func (f *fifo) Touch(cachex.Entry[string]) {} // FIFO ignores accesses.

func (f *fifo) Remove(e cachex.Entry[string]) {
	f.order.Remove(e.(*fifoEntry).elem)
}

func (f *fifo) Reset()        { f.order.Init() }
func (f *fifo) Size() int     { return f.order.Len() }
func (f *fifo) Capacity() int { return f.capacity }

func main() {
	cache, err := cachex.NewWithPolicy[string, int](
		newFIFO(2),
		cachex.WithOnEvict(func(k string, v int) {
			fmt.Printf("evicted %s=%d\n", k, v)
		}),
	)
	if err != nil {
		panic(err)
	}

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3) // evicts "a" (oldest): prints "evicted a=1"

	if v, ok := cache.Get("b"); ok {
		fmt.Println("b =", v) // b = 2
	}
	fmt.Println("size =", cache.Size()) // size = 2
}
```

### Operation → policy call sequence

The shell maps each `Cache` operation onto policy calls exactly as follows:

| Operation              | Policy call | Notes                                                        |
| ---------------------- | ----------- | ------------------------------------------------------------ |
| `Put` (new key)        | `Admit`     | On evict: victim value recovered, `OnEvict` fired under lock |
| `Put` (existing key)   | `Touch`     | Value updated                                                |
| `Get` (hit)            | `Touch`     |                                                              |
| `Peek` (hit) / any miss| none        |                                                              |
| `Delete` (present)     | `Remove`    | `OnEvict` does **not** fire                                  |
| `Clear`                | `Reset`     | `OnEvict` does **not** fire                                  |

`WithOnEvict` fires only on policy eviction (not on `Delete` or `Clear`) and
runs while the cache lock is held — **do not call back into the cache from it**,
or the cache will deadlock.

## Development

```sh
go mod download                   # fetch test dependencies
go build ./...                    # build
go test ./...                     # unit tests
go test -race ./...               # tests under the race detector
go test -bench=. -benchmem ./...  # benchmarks
go vet ./...                      # static checks
go generate ./...                 # regenerate the Policy mock
```

## License

See [LICENSE](LICENSE).
