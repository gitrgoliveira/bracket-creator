package excel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleError(t *testing.T) {
	// Test with nil error
	err := handleError("test", nil)
	assert.NoError(t, err)

	// Test with non-nil error
	originalErr := errors.New("test error")
	err = handleError("TestOperation", originalErr)
	assert.Error(t, err)

	// Check that the error message is as expected
	expectedErrMsg := "excel TestOperation operation failed: test error"
	assert.Equal(t, expectedErrMsg, err.Error())

	// Check that the original error is wrapped
	assert.ErrorIs(t, err, originalErr)
}
