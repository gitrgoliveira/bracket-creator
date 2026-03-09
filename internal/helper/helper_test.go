package helper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			input:    []string{"apple", "banana", "cherry"},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "with duplicates",
			input:    []string{"apple", "banana", "apple", "cherry", "banana"},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "all duplicates",
			input:    []string{"apple", "apple", "apple"},
			expected: []string{"apple"},
		},
		{
			name:     "with empty strings",
			input:    []string{"apple", "", "banana", "", "cherry"},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "only empty strings",
			input:    []string{"", "", ""},
			expected: []string{},
		},
		{
			name:     "preserves order of first occurrence",
			input:    []string{"zebra", "apple", "banana", "apple", "zebra"},
			expected: []string{"zebra", "apple", "banana"},
		},
		{
			name:     "single element",
			input:    []string{"apple"},
			expected: []string{"apple"},
		},
		{
			name:     "unicode characters",
			input:    []string{"ñame", "apple", "ñame", "café"},
			expected: []string{"ñame", "apple", "café"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RemoveDuplicates(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadEntriesFromFile(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		setupFile   bool
		filePath    string
		expected    []string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid file with multiple lines",
			fileContent: "line1\nline2\nline3",
			setupFile:   true,
			expected:    []string{"line1", "line2", "line3"},
			wantErr:     false,
		},
		{
			name:        "valid file with single line",
			fileContent: "single line",
			setupFile:   true,
			expected:    []string{"single line"},
			wantErr:     false,
		},
		{
			name:        "empty file",
			fileContent: "",
			setupFile:   true,
			expected:    nil,
			wantErr:     false,
		},
		{
			name:        "file with empty lines",
			fileContent: "line1\n\nline3",
			setupFile:   true,
			expected:    []string{"line1", "", "line3"},
			wantErr:     false,
		},
		{
			name:        "file with unicode content",
			fileContent: "日本語\nñoño\ncafé",
			setupFile:   true,
			expected:    []string{"日本語", "ñoño", "café"},
			wantErr:     false,
		},
		{
			name:        "missing file",
			setupFile:   false,
			filePath:    "nonexistent.txt",
			wantErr:     true,
			errContains: "does not exist",
		},
		{
			name:        "directory traversal attempt with relative path",
			setupFile:   false,
			filePath:    "../../../etc/passwd",
			wantErr:     true,
			errContains: "invalid file path",
		},
		{
			name:        "path with .. component",
			setupFile:   false,
			filePath:    "test/../../../etc/passwd",
			wantErr:     true,
			errContains: "invalid file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var testFilePath string

			if tt.setupFile {
				// Create temporary file
				tmpDir := t.TempDir()
				testFilePath = filepath.Join(tmpDir, "test.txt")
				err := os.WriteFile(testFilePath, []byte(tt.fileContent), 0644)
				require.NoError(t, err)
			} else {
				testFilePath = tt.filePath
			}

			result, err := ReadEntriesFromFile(testFilePath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadEntriesFromFile_PermissionError(t *testing.T) {
	// Skip on Windows as permission handling is different
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping permission test on Windows")
	}

	// Create a file with no read permissions
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "noperm.txt")
	err := os.WriteFile(testFilePath, []byte("content"), 0644)
	require.NoError(t, err)

	// Remove read permissions
	err = os.Chmod(testFilePath, 0000)
	require.NoError(t, err)
	defer os.Chmod(testFilePath, 0644) // Restore permissions for cleanup

	_, err = ReadEntriesFromFile(testFilePath)
	require.Error(t, err)
}

func TestRowStack_Push(t *testing.T) {
	stack := &RowStack{}

	stack.Push(1)
	assert.Equal(t, 1, stack.Peek())

	stack.Push(2)
	assert.Equal(t, 2, stack.Peek())

	stack.Push(3)
	assert.Equal(t, 3, stack.Peek())
}

func TestRowStack_Pop(t *testing.T) {
	tests := []struct {
		name     string
		initial  []int
		expected int
	}{
		{
			name:     "pop from stack with one element",
			initial:  []int{1},
			expected: 1,
		},
		{
			name:     "pop from stack with multiple elements",
			initial:  []int{1, 2, 3},
			expected: 3,
		},
		{
			name:     "pop from empty stack",
			initial:  []int{},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &RowStack{data: tt.initial}
			result := stack.Pop()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRowStack_Peek(t *testing.T) {
	tests := []struct {
		name        string
		initial     []int
		expected    int
		shouldPanic bool
	}{
		{
			name:     "peek at stack with one element",
			initial:  []int{1},
			expected: 1,
		},
		{
			name:     "peek at stack with multiple elements",
			initial:  []int{1, 2, 3},
			expected: 3,
		},
		{
			name:        "peek at empty stack",
			initial:     []int{},
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &RowStack{data: tt.initial}

			if tt.shouldPanic {
				assert.Panics(t, func() {
					stack.Peek()
				})
				return
			}

			result := stack.Peek()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRowStack_PushHighest(t *testing.T) {
	tests := []struct {
		name     string
		first    int
		second   int
		expected int
	}{
		{
			name:     "first is higher",
			first:    5,
			second:   3,
			expected: 5,
		},
		{
			name:     "second is higher",
			first:    3,
			second:   5,
			expected: 5,
		},
		{
			name:     "both equal pushes second",
			first:    5,
			second:   5,
			expected: 5,
		},
		{
			name:     "negative numbers",
			first:    -3,
			second:   -5,
			expected: -3,
		},
		{
			name:     "zero values",
			first:    0,
			second:   -1,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &RowStack{}
			stack.PushHighest(tt.first, tt.second)
			assert.Equal(t, tt.expected, stack.Peek())
		})
	}
}

func TestRowStack_Operations(t *testing.T) {
	t.Run("push pop sequence", func(t *testing.T) {
		stack := &RowStack{}

		// Push multiple values
		stack.Push(1)
		stack.Push(2)
		stack.Push(3)

		// Pop in reverse order
		assert.Equal(t, 3, stack.Pop())
		assert.Equal(t, 2, stack.Pop())
		assert.Equal(t, 1, stack.Pop())
		assert.Equal(t, -1, stack.Pop()) // Empty stack
	})

	t.Run("push highest sequence", func(t *testing.T) {
		stack := &RowStack{}

		stack.PushHighest(10, 5)
		assert.Equal(t, 10, stack.Peek())

		stack.PushHighest(3, 7)
		assert.Equal(t, 7, stack.Peek())

		assert.Equal(t, 7, stack.Pop())
		assert.Equal(t, 10, stack.Pop())
	})

	t.Run("mixed operations", func(t *testing.T) {
		stack := &RowStack{}

		stack.Push(1)
		stack.PushHighest(5, 3)
		stack.Push(2)

		assert.Equal(t, 2, stack.Pop())
		assert.Equal(t, 5, stack.Pop())
		assert.Equal(t, 1, stack.Pop())
	})
}
