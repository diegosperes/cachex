package cachex

// config holds the construction-time settings for a cache shell. Its
// zero value is valid: no eviction callback is registered.
type config[K comparable, V any] struct {
	onEvict func(K, V)
}

// Option configures a cache at construction.
type Option[K comparable, V any] func(*config[K, V])

// WithOnEvict registers a callback invoked synchronously when an entry is
// evicted by the policy. It does not fire on explicit Delete or Clear. The
// callback runs while the cache lock is held; do not call back into the cache
// from it, or the cache will deadlock.
func WithOnEvict[K comparable, V any](fn func(K, V)) Option[K, V] {
	return func(c *config[K, V]) {
		c.onEvict = fn
	}
}
