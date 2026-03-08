package version

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

var regexp = fmt.Sprintf(`^bracket-creator - .*

Git Commit: .*
Build date: [0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} .*
Go version: go[0-9]{1}.[0-9]+.*
OS / Arch : %s %s
`, runtime.GOOS, runtime.GOARCH)

func TestGenerateOutput(t *testing.T) {
	assert.Regexp(t, regexp, generateOutput())
}

func TestPrint(t *testing.T) {
	// Save the original stdout
	originalStdout := os.Stdout
	defer func() { os.Stdout = originalStdout }()

	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Error creating pipe: %v", err)
	}

	// Set stdout to the pipe writer
	os.Stdout = w

	// Call the function
	Print()

	// Close the writer to avoid deadlock and flush all data
	err = w.Close()
	if err != nil {
		t.Fatalf("Error closing writer: %v", err)
	}

	// Read the output from the pipe
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Error reading from pipe: %v", err)
	}

	// Close the reader
	err = r.Close()
	if err != nil {
		t.Fatalf("Error closing reader: %v", err)
	}

	// Assert the output
	assert.Regexp(t, regexp, string(out))
}

func TestGetVersion(t *testing.T) {
	v := GetVersion()
	assert.NotEmpty(t, v, "Version should not be empty")
}

func TestGetGitCommit(t *testing.T) {
	commit := GetGitCommit()
	assert.NotEmpty(t, commit, "Git commit should not be empty")
}

func TestGetBuildDate(t *testing.T) {
	date := GetBuildDate()
	assert.NotEmpty(t, date, "Build date should not be empty")
}

func TestGetGoVersion(t *testing.T) {
	goVer := GetGoVersion()
	assert.NotEmpty(t, goVer, "Go version should not be empty")
	assert.Contains(t, goVer, "go", "Go version should contain 'go'")
}

func TestGetOsArch(t *testing.T) {
	osArch := GetOsArch()
	assert.NotEmpty(t, osArch, "OS/Arch should not be empty")
	assert.Contains(t, osArch, runtime.GOOS, "Should contain OS")
	assert.Contains(t, osArch, runtime.GOARCH, "Should contain architecture")
}
