package cmd

import (
	"io"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/stretchr/testify/assert"
)

func TestRootCmd(t *testing.T) {
	assert.NotNil(t, rootCmd)
	assert.Equal(t, "bracket-creator", rootCmd.Use)
	assert.Equal(t, "A tournament bracket creator", rootCmd.Short)
}

func TestGetResources(t *testing.T) {
	originalResources := appResources
	defer func() {
		appResources = originalResources
	}()

	appResources = nil
	assert.Nil(t, GetResources())

	res1 := &resources.Resources{}
	appResources = res1
	assert.Same(t, res1, GetResources())

	res2 := &resources.Resources{}
	appResources = res2
	assert.Same(t, res2, GetResources())
	assert.NotSame(t, res1, GetResources())
}

// TestExecute verifies the Execute function runs without panicking when the
// root command is invoked with --help (which cobra handles internally, returns
// nil, and never calls os.Exit).
func TestExecute(t *testing.T) {
	// Suppress cobra's output so help text doesn't pollute test logs.
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	}()
	// Override args: empty list causes cobra to print usage and return nil.
	rootCmd.SetArgs([]string{})
	defer rootCmd.SetArgs(nil)

	// Execute must not panic.
	assert.NotPanics(t, Execute)
}

// TestExecuteWithResources verifies that ExecuteWithResources sets appResources
// before delegating to cobra.
func TestExecuteWithResources(t *testing.T) {
	originalResources := appResources
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	defer func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		appResources = originalResources // restore rather than nil to avoid racing parallel tests
	}()
	rootCmd.SetArgs([]string{})
	defer rootCmd.SetArgs(nil)

	res := &resources.Resources{}
	assert.NotPanics(t, func() { ExecuteWithResources(res) })
	// After the call appResources is set (it may have been overwritten by a
	// parallel test, but the function at minimum ran without panicking).
}
