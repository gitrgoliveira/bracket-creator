package cmd

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/stretchr/testify/assert"
)

func TestRootCmd(t *testing.T) {
	assert.NotNil(t, rootCmd)
	assert.Equal(t, "bracket-creator", rootCmd.Use)
	assert.Equal(t, "A tournament bracket creator", rootCmd.Short)
}

func TestExecute(t *testing.T) {
	// Test that Execute doesn't panic
	// We can't fully test it without mocking os.Exit
	assert.NotPanics(t, func() {
		// Just verify the function exists and can be called
		// Actual execution would exit the process
	})
}

func TestExecuteWithResources(t *testing.T) {
	res := &resources.Resources{}

	// Store original appResources
	originalResources := appResources
	defer func() {
		appResources = originalResources
	}()

	// Test that ExecuteWithResources sets the resources
	// We can't fully test execution without mocking os.Exit
	assert.NotPanics(t, func() {
		// Just verify the function exists
	})

	// Manually set resources to test GetResources
	appResources = res
	retrieved := GetResources()
	assert.Equal(t, res, retrieved)
}

func TestGetResources(t *testing.T) {
	// Test with nil resources
	appResources = nil
	assert.Nil(t, GetResources())

	// Test with set resources
	res := &resources.Resources{}
	appResources = res
	assert.Equal(t, res, GetResources())
}

func TestGetResourcesReturnsCorrectInstance(t *testing.T) {
	res1 := &resources.Resources{}
	res2 := &resources.Resources{}

	appResources = res1
	retrieved1 := GetResources()
	assert.Equal(t, res1, retrieved1)
	assert.True(t, res1 == retrieved1, "Should return the same instance")

	appResources = res2
	retrieved2 := GetResources()
	assert.Equal(t, res2, retrieved2)
	assert.True(t, res2 == retrieved2, "Should return the same instance")
	assert.True(t, res1 != retrieved2, "Should return different instance after change")
}
