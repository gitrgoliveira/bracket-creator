package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmd(t *testing.T) {
	assert.NotNil(t, versionCmd)
	assert.Equal(t, "version", versionCmd.Use)
	assert.Equal(t, "Print the application version.", versionCmd.Short)
	assert.Equal(t, "Print the application version.", versionCmd.Long)
}

func TestVersionCmdRun(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "no args",
			args: []string{},
		},
		{
			name: "with args",
			args: []string{"arg1", "arg2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStdout(t, func() {
				versionCmd.Run(versionCmd, tt.args)
			})

			require.NotEmpty(t, output)
			assert.Contains(t, output, "bracket-creator -")
			assert.Contains(t, output, "Git Commit:")
			assert.Contains(t, output, "Build date:")
			assert.Contains(t, output, "Go version:")
			assert.Contains(t, output, "OS / Arch")
		})
	}
}
