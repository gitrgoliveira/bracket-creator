package helper

func removeDuplicates(input []string) []string {
	uniqueStrings := make(map[string]bool)
	result := make([]string, 0)

	for _, str := range input {
		if !uniqueStrings[str] {
			uniqueStrings[str] = true
			result = append(result, str)
		}
	}

	return result
}
