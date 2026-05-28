package cachex_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/diegosperes/cachex"
)

// TestPolicyContract verifies the generated mock satisfies the Policy contract
// and that the package's sentinel error is well formed. The compile-time
// assignment is the load-bearing check; the asserts guard against accidental
// edits to the sentinel.
func TestPolicyContract(t *testing.T) {
	t.Parallel()

	t.Run("mock implements Policy", func(t *testing.T) {
		t.Parallel()

		var _ cachex.Policy[string] = NewMockPolicy[string](gomock.NewController(t))
	})

	t.Run("ErrInvalidCapacity is a stable sentinel", func(t *testing.T) {
		t.Parallel()

		assert.Error(t, cachex.ErrInvalidCapacity)
		assert.True(t, errors.Is(cachex.ErrInvalidCapacity, cachex.ErrInvalidCapacity))
		assert.EqualError(t, cachex.ErrInvalidCapacity, "cachex: capacity must be positive")
	})
}
