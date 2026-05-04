package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/stretchr/testify/require"
)

// TestMain sets up appResources for tests.
func TestMain(m *testing.M) {
	// Wire a minimal Resources (web-files only) into appResources.
	appResources = resources.NewResources(nil, nil)

	code := m.Run()
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
