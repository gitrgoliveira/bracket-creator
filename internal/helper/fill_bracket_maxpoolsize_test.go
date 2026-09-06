package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildPoolPhaseFillBracketTreeAware_RefusesMinSizeAtMaxPoolSize pins
// bc-drwx item 13: this entry point built its base target-size row
// directly, without ever routing through poolTargetSizes (the function that
// carries the upper MaxPoolSize guard every OTHER pool-formation path
// enforces), so an operator-supplied minSize at or above MaxPoolSize used
// to sail straight through to buildQualifierSkeleton, which allocates a
// []Player of that size per pool.
func TestBuildPoolPhaseFillBracketTreeAware_RefusesMinSizeAtMaxPoolSize(t *testing.T) {
	players := makeUniquePlayers(6)
	_, _, err := BuildPoolPhaseFillBracketTreeAware(players, MaxPoolSize, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool size must be less than")
}
