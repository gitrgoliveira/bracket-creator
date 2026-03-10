package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/require"
)

// TestMain ensures helper.TemplateFile is populated for tests
func TestMain(m *testing.M) {
	// Load template.xlsx from project root (relative to cmd package)
	templatePath := filepath.Join("..", "template.xlsx")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		panic("failed to read template.xlsx: " + err.Error())
	}

	// Populate helper.TemplateFile with in-memory FS
	helper.TemplateFile = fstest.MapFS{
		"template.xlsx": &fstest.MapFile{
			Data:    templateData,
			Mode:    0644,
			ModTime: time.Now(),
		},
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
