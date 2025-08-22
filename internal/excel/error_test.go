package excel

import (
	"errors"
	"testing"
)

func TestHandleError(t *testing.T) {
	// Test with nil error
	err := handleError("test", nil)
	if err != nil {
		t.Errorf("Expected nil error, got: %v", err)
	}

	// Test with non-nil error
	originalErr := errors.New("test error")
	err = handleError("TestOperation", originalErr)

	if err == nil {
		t.Error("Expected error to not be nil")
	}

	// Check that the error message is as expected
	expectedErrMsg := "excel TestOperation operation failed: test error"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}

	// Check that the original error is wrapped
	if !errors.Is(err, originalErr) {
		t.Error("Expected original error to be wrapped")
	}
}
