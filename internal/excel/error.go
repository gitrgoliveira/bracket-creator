package excel

import "fmt"

// handleError consistently handles Excel-related errors
func handleError(operation string, err error) error {
	if err != nil {
		return fmt.Errorf("excel %s operation failed: %w", operation, err)
	}
	return nil
}
