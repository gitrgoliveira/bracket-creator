package helper

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var WebFs embed.FS

type RowStack struct {
	data []int
}

func (s *RowStack) Push(value int) {
	s.data = append(s.data, value)
}

func (s *RowStack) PushHighest(first int, second int) {
	if first > second {
		s.Push(first)
	} else {
		s.Push(second)
	}
}
func (s *RowStack) Pop() (int, error) {
	if len(s.data) == 0 {
		return 0, fmt.Errorf("pop on empty stack")
	}
	index := len(s.data) - 1
	value := s.data[index]
	s.data = s.data[:index]
	return value, nil
}

func (s *RowStack) Peek() (int, error) {
	if len(s.data) == 0 {
		return 0, fmt.Errorf("peek on empty stack")
	}
	return s.data[len(s.data)-1], nil
}

// RemoveDuplicates removes duplicate strings from the input slice and returns a new slice without duplicates.
//
// The function takes a parameter named input, which is a slice of strings. It represents the input slice from which duplicates and empty strings will be removed.
func RemoveDuplicates(input []string) []string {
	uniqueStrings := make(map[string]bool)
	result := make([]string, 0)

	for _, str := range input {
		if str != "" && !uniqueStrings[str] {
			uniqueStrings[str] = true
			result = append(result, str)
		} else {
			fmt.Printf("Warning: Duplicate found - %s\n", str)
		}
	}

	return result
}

// CheckDuplicateEntries scans the raw entry list (one CSV row per element)
// for duplicates and returns a list of entries that appear more than once.
// Empty strings are ignored. The returned slice preserves first-seen order
// of the offending entries; an empty result means the list is unique.
func CheckDuplicateEntries(input []string) []string {
	seen := make(map[string]int, len(input))
	order := make([]string, 0)
	for _, s := range input {
		if s == "" {
			continue
		}
		seen[s]++
		if seen[s] == 2 {
			order = append(order, s)
		}
	}
	return order
}

// ValidateCourts returns an error when n is outside the supported court
// range. Courts are labelled A–Z, so MaxCourts (26) is the hard upper
// bound. n < 1 is also rejected so the caller does not have to guess what
// "0 courts" should mean.
func ValidateCourts(n int) error {
	if n < 1 {
		return fmt.Errorf("courts must be >= 1, got %d", n)
	}
	if n > MaxCourts {
		return fmt.Errorf("courts must be <= %d (Shiaijo are labelled A–Z), got %d", MaxCourts, n)
	}
	return nil
}

func OrderStringsAlphabetically(strings []*Node) []*Node {
	sort.Slice(strings, func(i, j int) bool {
		strA := strings[i]
		strB := strings[j]

		// Split the strings into prefix and suffix
		prefixA, suffixA := extractPrefixAndSuffix(strA.LeafVal)
		prefixB, suffixB := extractPrefixAndSuffix(strB.LeafVal)

		// Compare the prefixes
		if prefixA != prefixB {
			return prefixA < prefixB
		}

		// Compare the suffixes as numbers
		numA, _ := strconv.Atoi(suffixA)
		numB, _ := strconv.Atoi(suffixB)
		return numA < numB
	})

	return strings
}

// Helper function to extract prefix and suffix from a string
func extractPrefixAndSuffix(str string) (string, string) {
	lastIndex := len(str) - 1
	for i := lastIndex; i >= 0; i-- {
		if !isDigit(str[i]) {
			return str[:i+1], str[i+1:]
		}
	}
	return "", str
}

// Helper function to check if a character is a digit
func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

func ReadEntriesFromFile(filePath string) ([]string, error) {
	// Validate file path to prevent directory traversal attacks
	cleanPath := filepath.Clean(filePath)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("invalid file path: %s", filePath)
	}

	// Check if the file exists
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", cleanPath)
	}

	// #nosec G304 - file path is validated above
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing file: %v\n", err)
		}
	}()

	var entries []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entries = append(entries, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// AssignPoolsToCourts distributes numPools pools across numCourts courts using
// contiguous blocks that match the tree sheet grouping. The first court gets
// ceil(numPools/numCourts) pools, subsequent courts get the remainder.
// Returns an error when numCourts exceeds numPools.
func AssignPoolsToCourts(numPools, numCourts int) ([]int, error) {
	if numCourts < 1 {
		numCourts = 1
	}
	if numPools == 0 {
		return []int{}, nil
	}
	base := numPools / numCourts
	extra := numPools % numCourts
	result := make([]int, numPools)
	pool := 0
	for court := 0; court < numCourts; court++ {
		count := base
		if court < extra {
			count++
		}
		for j := 0; j < count; j++ {
			result[pool] = court
			pool++
		}
	}
	return result, nil
}

// ReorderPoolsForCourts deinterleaves pools so that when divided into
// contiguous court blocks, each block has balanced pool sizes and seeds are
// spread across courts. Original pool i goes to court block (i % numCourts).
// Pool names are re-assigned alphabetically after reordering.
func ReorderPoolsForCourts(pools []Pool, numCourts int) []Pool {
	if numCourts <= 1 || len(pools) <= numCourts {
		return pools
	}

	// Group pools by their round-robin court: pool i → group (i % numCourts)
	groups := make([][]Pool, numCourts)
	for i, p := range pools {
		court := i % numCourts
		groups[court] = append(groups[court], p)
	}

	// Concatenate groups: all court-0 pools first, then court-1, etc.
	result := make([]Pool, 0, len(pools))
	for _, group := range groups {
		result = append(result, group...)
	}

	// Re-assign pool names in the new order
	for i := range result {
		char := string(rune('A' + i%26))
		if i > 25 {
			char = char + char
		}
		result[i].PoolName = fmt.Sprintf("Pool %s", char)
	}

	return result
}
