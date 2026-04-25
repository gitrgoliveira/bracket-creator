package tests

import (
	"os"
	"testing"
)

// TestMain is the entry point for the integration test suite.
// No template setup is needed — the Excel file is now built from code.
func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
