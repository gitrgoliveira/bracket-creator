package helper

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"sort"
	"strconv"
)

var WebFs embed.FS
var TemplateFile embed.FS

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
func (s *RowStack) Pop() int {
	if len(s.data) == 0 {
		fmt.Println("Stack is empty")
		return -1
	}
	index := len(s.data) - 1
	value := s.data[index]
	s.data = s.data[:index]
	return value
}

func (s *RowStack) Peek() int {
	if len(s.data) == 0 {
		panic("Stack is empty")
	}
	return s.data[len(s.data)-1]
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
	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", filePath)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

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
