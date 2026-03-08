package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionCmd(t *testing.T) {
	assert.NotNil(t, versionCmd)
	assert.Equal(t, "version", versionCmd.Use)
	assert.Equal(t, "Print the application version.", versionCmd.Short)
	assert.Equal(t, "Print the application version.", versionCmd.Long)
}

func TestVersionCmdRun(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the version command
	versionCmd.Run(versionCmd, []string{})

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output contains version information
	// The actual version values depend on build-time variables
	assert.NotEmpty(t, output)
}

func TestVersionCmdWithArgs(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the version command with args (should be ignored)
	versionCmd.Run(versionCmd, []string{"arg1", "arg2"})

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should still print version info
	assert.NotEmpty(t, output)
}

// Made with Bob
