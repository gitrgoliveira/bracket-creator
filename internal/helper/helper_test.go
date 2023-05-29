package helper

import "testing"

func TestRemoveDuplicates(t *testing.T) {
	input := []string{"apple", "banana", "orange", "apple", "kiwi", "banana"}
	removeDuplicates(input)
}
