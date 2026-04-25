package helper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDuplicateEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{name: "no duplicates", input: []string{"a", "b", "c"}, expected: []string{}},
		{name: "single duplicate", input: []string{"a", "b", "a"}, expected: []string{"a"}},
		{name: "multiple duplicates", input: []string{"a", "b", "a", "c", "b"}, expected: []string{"a", "b"}},
		{name: "triple of one entry reported once", input: []string{"a", "a", "a"}, expected: []string{"a"}},
		{name: "ignores empty strings", input: []string{"", "a", "", "b", ""}, expected: []string{}},
		{name: "case sensitive", input: []string{"Alice", "alice"}, expected: []string{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckDuplicateEntries(tt.input)
			if len(tt.expected) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestValidateCourts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		n       int
		wantErr string
	}{
		{name: "zero rejected", n: 0, wantErr: "courts must be >= 1"},
		{name: "negative rejected", n: -1, wantErr: "courts must be >= 1"},
		{name: "min accepted", n: 1},
		{name: "two accepted", n: 2},
		{name: "max accepted", n: MaxCourts},
		{name: "above max rejected", n: MaxCourts + 1, wantErr: "courts must be <= 26"},
		{name: "way above max rejected", n: 100, wantErr: "courts must be <= 26"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCourts(tt.n)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

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
	v, err := stack.Peek()
	require.NoError(t, err)
	assert.Equal(t, 1, v)

	stack.Push(2)
	v, err = stack.Peek()
	require.NoError(t, err)
	assert.Equal(t, 2, v)

	stack.Push(3)
	v, err = stack.Peek()
	require.NoError(t, err)
	assert.Equal(t, 3, v)
}

func TestRowStack_Pop(t *testing.T) {
	tests := []struct {
		name        string
		initial     []int
		expected    int
		expectError bool
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
			name:        "pop from empty stack returns error",
			initial:     []int{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &RowStack{data: tt.initial}
			result, err := stack.Pop()
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestRowStack_Peek(t *testing.T) {
	tests := []struct {
		name        string
		initial     []int
		expected    int
		expectError bool
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
			name:        "peek at empty stack returns error",
			initial:     []int{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := &RowStack{data: tt.initial}
			result, err := stack.Peek()
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
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
			v, err := stack.Peek()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, v)
		})
	}
}

func TestRowStack_Operations(t *testing.T) {
	mustPop := func(t *testing.T, s *RowStack) int {
		t.Helper()
		v, err := s.Pop()
		require.NoError(t, err)
		return v
	}
	mustPeek := func(t *testing.T, s *RowStack) int {
		t.Helper()
		v, err := s.Peek()
		require.NoError(t, err)
		return v
	}

	t.Run("push pop sequence", func(t *testing.T) {
		stack := &RowStack{}

		stack.Push(1)
		stack.Push(2)
		stack.Push(3)

		// Pop in reverse order
		assert.Equal(t, 3, mustPop(t, stack))
		assert.Equal(t, 2, mustPop(t, stack))
		assert.Equal(t, 1, mustPop(t, stack))
		_, err := stack.Pop() // Empty stack returns error
		require.Error(t, err)
	})

	t.Run("push highest sequence", func(t *testing.T) {
		stack := &RowStack{}

		stack.PushHighest(10, 5)
		assert.Equal(t, 10, mustPeek(t, stack))

		stack.PushHighest(3, 7)
		assert.Equal(t, 7, mustPeek(t, stack))

		assert.Equal(t, 7, mustPop(t, stack))
		assert.Equal(t, 10, mustPop(t, stack))
	})

	t.Run("mixed operations", func(t *testing.T) {
		stack := &RowStack{}

		stack.Push(1)
		stack.PushHighest(5, 3)
		stack.Push(2)

		assert.Equal(t, 2, mustPop(t, stack))
		assert.Equal(t, 5, mustPop(t, stack))
		assert.Equal(t, 1, mustPop(t, stack))
	})
}

func TestAssignPoolsToCourts(t *testing.T) {
	tests := []struct {
		name      string
		numPools  int
		numCourts int
		expected  []int
		wantErr   bool
	}{
		{
			name:      "even split 4 pools 2 courts",
			numPools:  4,
			numCourts: 2,
			expected:  []int{0, 0, 1, 1},
		},
		{
			name:      "uneven split 7 pools 2 courts",
			numPools:  7,
			numCourts: 2,
			expected:  []int{0, 0, 0, 0, 1, 1, 1},
		},
		{
			name:      "uneven split 7 pools 3 courts",
			numPools:  7,
			numCourts: 3,
			expected:  []int{0, 0, 0, 1, 1, 2, 2},
		},
		{
			name:      "single court",
			numPools:  5,
			numCourts: 1,
			expected:  []int{0, 0, 0, 0, 0},
		},
		{
			name:      "one pool per court",
			numPools:  3,
			numCourts: 3,
			expected:  []int{0, 1, 2},
		},
		{
			name:      "zero pools returns empty",
			numPools:  0,
			numCourts: 1,
			expected:  []int{},
		},
		{
			name:      "courts exceed pools returns valid distribution",
			numPools:  2,
			numCourts: 3,
			expected:  []int{0, 1},
			wantErr:   false,
		},
		{
			name:      "zero courts defaults to one",
			numPools:  4,
			numCourts: 0,
			expected:  []int{0, 0, 0, 0},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AssignPoolsToCourts(tt.numPools, tt.numCourts)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReorderPoolsForCourts(t *testing.T) {
	makePools := func(sizes ...int) []Pool {
		pools := make([]Pool, len(sizes))
		for i, s := range sizes {
			char := string(rune('A' + i%26))
			if i > 25 {
				char = char + char
			}
			pools[i].PoolName = "Pool " + char
			pools[i].Players = make([]Player, s)
		}
		return pools
	}

	tests := []struct {
		name          string
		pools         []Pool
		numCourts     int
		expectedSizes []int
		expectedNames []string
	}{
		{
			name:          "single court no reorder",
			pools:         makePools(3, 3, 2),
			numCourts:     1,
			expectedSizes: []int{3, 3, 2},
			expectedNames: []string{"Pool A", "Pool B", "Pool C"},
		},
		{
			name:          "2 courts 4 equal pools",
			pools:         makePools(3, 3, 3, 3),
			numCourts:     2,
			expectedSizes: []int{3, 3, 3, 3},
			expectedNames: []string{"Pool A", "Pool B", "Pool C", "Pool D"},
		},
		{
			name:          "2 courts small pools at end spread across blocks",
			pools:         makePools(3, 3, 3, 3, 2, 2),
			numCourts:     2,
			expectedSizes: []int{3, 3, 2, 3, 3, 2}, // even→block0, odd→block1
			expectedNames: []string{"Pool A", "Pool B", "Pool C", "Pool D", "Pool E", "Pool F"},
		},
		{
			name:          "2 courts 5 pools uneven blocks",
			pools:         makePools(3, 3, 3, 3, 2),
			numCourts:     2,
			expectedSizes: []int{3, 3, 2, 3, 3}, // group0: p0,p2,p4; group1: p1,p3
			expectedNames: []string{"Pool A", "Pool B", "Pool C", "Pool D", "Pool E"},
		},
		{
			name:          "3 courts 7 pools",
			pools:         makePools(3, 3, 3, 3, 3, 2, 2),
			numCourts:     3,
			expectedSizes: []int{3, 3, 2, 3, 3, 3, 2}, // g0:[p0,p3,p6] g1:[p1,p4] g2:[p2,p5]
			expectedNames: []string{"Pool A", "Pool B", "Pool C", "Pool D", "Pool E", "Pool F", "Pool G"},
		},
		{
			name:          "fewer pools than courts returns unchanged",
			pools:         makePools(3, 2),
			numCourts:     3,
			expectedSizes: []int{3, 2},
			expectedNames: []string{"Pool A", "Pool B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReorderPoolsForCourts(tt.pools, tt.numCourts)
			sizes := make([]int, len(result))
			names := make([]string, len(result))
			for i, p := range result {
				sizes[i] = len(p.Players)
				names[i] = p.PoolName
			}
			assert.Equal(t, tt.expectedSizes, sizes, "pool sizes after reorder")
			assert.Equal(t, tt.expectedNames, names, "pool names after reorder")
		})
	}
}

func TestIsDigit(t *testing.T) {
	tests := []struct {
		name     string
		ch       byte
		expected bool
	}{
		{"digit 0", '0', true},
		{"digit 5", '5', true},
		{"digit 9", '9', true},
		{"letter a", 'a', false},
		{"letter Z", 'Z', false},
		{"special char", '-', false},
		{"space", ' ', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDigit(tt.ch))
		})
	}
}

func TestExtractPrefixAndSuffix(t *testing.T) {
	tests := []struct {
		name           string
		str            string
		expectedPrefix string
		expectedSuffix string
	}{
		{"only letters", "abc", "abc", ""},
		{"only digits", "123", "", "123"},
		{"letters and digits", "pool12", "pool", "12"},
		{"mixed digits inside", "p1ool12", "p1ool", "12"},
		{"empty string", "", "", ""},
		{"letters then digits then letters", "abc12xyz", "abc12xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, suffix := extractPrefixAndSuffix(tt.str)
			assert.Equal(t, tt.expectedPrefix, prefix)
			assert.Equal(t, tt.expectedSuffix, suffix)
		})
	}
}

func TestOrderStringsAlphabetically(t *testing.T) {
	tests := []struct {
		name     string
		input    []*Node
		expected []*Node
	}{
		{
			name: "numerical suffix ordering",
			input: []*Node{
				{LeafVal: "Pool 10"},
				{LeafVal: "Pool 2"},
				{LeafVal: "Pool 1"},
			},
			expected: []*Node{
				{LeafVal: "Pool 1"},
				{LeafVal: "Pool 2"},
				{LeafVal: "Pool 10"},
			},
		},
		{
			name: "alphabetical prefix ordering",
			input: []*Node{
				{LeafVal: "Z Pool 1"},
				{LeafVal: "A Pool 2"},
				{LeafVal: "B Pool 1"},
			},
			expected: []*Node{
				{LeafVal: "A Pool 2"},
				{LeafVal: "B Pool 1"},
				{LeafVal: "Z Pool 1"},
			},
		},
		{
			name: "no numerical suffix",
			input: []*Node{
				{LeafVal: "Charlie"},
				{LeafVal: "Alpha"},
				{LeafVal: "Bravo"},
			},
			expected: []*Node{
				{LeafVal: "Alpha"},
				{LeafVal: "Bravo"},
				{LeafVal: "Charlie"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OrderStringsAlphabetically(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
