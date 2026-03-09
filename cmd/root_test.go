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
