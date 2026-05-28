package cachex

import "errors"

// ErrInvalidCapacity is returned by constructors when the configured capacity
// is not positive. Runtime cache operations never return errors: presence is
// reported via the (value, bool) pattern and capacity overflow is handled by
// eviction.
var ErrInvalidCapacity = errors.New("cachex: capacity must be positive")
