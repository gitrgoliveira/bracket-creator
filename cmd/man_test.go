package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManCmd(t *testing.T) {
	assert.NotNil(t, manCmd)
	assert.Equal(t, "man", manCmd.Use)
	assert.Equal(t, "Generates bracket-creator's command line manpages", manCmd.Short)
	assert.True(t, manCmd.Hidden)
}

func TestManCmdRun(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the man command
	manCmd.Run(manCmd, []string{})

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify output contains expected man page content
	assert.Contains(t, output, "bracket-creator")
	assert.Contains(t, output, "COPYRIGHT")
	assert.Contains(t, output, "oliveira")
}

func TestManCmdNoArgs(t *testing.T) {
	// Verify the command accepts no arguments
	err := manCmd.Args(manCmd, []string{})
	assert.NoError(t, err)
}

func TestManCmdWithArgs(t *testing.T) {
	// Verify the command rejects arguments
	err := manCmd.Args(manCmd, []string{"arg1"})
	assert.Error(t, err)
}

func TestManCmdCopyright(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the man command
	manCmd.Run(manCmd, []string{})

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify copyright section is present
	assert.Contains(t, output, "COPYRIGHT")
	assert.Contains(t, output, "2023")
	assert.Contains(t, output, "oliveira")
}

// Made with Bob
