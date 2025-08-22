// Package excel handles all Excel file operations
package excel

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// Client handles Excel file operations
type Client struct {
	file *excelize.File
}

// NewClient creates a new Excel client from a template
func NewClient(templateFS fs.FS) (*Client, error) {
	// Extract and open the template file
	templateFile, err := templateFS.Open("template.xlsx")
	if err != nil {
		return nil, fmt.Errorf("failed to open template file: %w", err)
	}
	defer func() {
		if closeErr := templateFile.Close(); closeErr != nil {
			fmt.Printf("Warning: failed to close template file: %v\n", closeErr)
		}
	}()

	// Read the template data
	templateData, err := io.ReadAll(templateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read template data: %w", err)
	}

	// Create Excel file from template data
	reader := bytes.NewReader(templateData)
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel template: %w", err)
	}

	return &Client{file: f}, nil
}

// SetFileForTest sets the Excel file for testing purposes only
func (c *Client) SetFileForTest(file *excelize.File) {
	c.file = file
}

// SaveFile saves the Excel file to the specified path
func (c *Client) SaveFile(path string) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Save the file
	if err := c.file.SaveAs(path); err != nil {
		return fmt.Errorf("failed to save Excel file: %w", err)
	}
	return nil
}

// Close closes the Excel file
func (c *Client) Close() error {
	if c.file == nil {
		return fmt.Errorf("excel file is already closed or not initialized")
	}

	if err := c.file.Close(); err != nil {
		return fmt.Errorf("failed to close Excel file: %w", err)
	}

	// Set file to nil to prevent double closing
	c.file = nil
	return nil
}
