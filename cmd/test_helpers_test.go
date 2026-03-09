package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMain ensures tests run from project root for template.xlsx access
func TestMain(m *testing.M) {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	// If we're in cmd directory, change to parent
	if filepath.Base(cwd) == "cmd" {
		if err := os.Chdir(".."); err != nil {
			panic(err)
		}
		cwd, _ = os.Getwd()
	}

	// Verify template.xlsx exists
	if _, err := os.Stat("template.xlsx"); os.IsNotExist(err) {
		panic("template.xlsx not found in " + cwd + ". Tests must run from project root.")
	}

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	run()

	require.NoError(t, w.Close())
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}
