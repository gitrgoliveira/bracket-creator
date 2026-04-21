package excel_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// createTestFS creates an in-memory filesystem for testing
func createTestFS(t *testing.T) fs.FS {
	data := loadTemplateData(t)
	return fstest.MapFS{
		"template.xlsx": &fstest.MapFile{
			Data:    data,
			Mode:    0644,
			ModTime: time.Now(),
		},
	}
}

func createInvalidFS() fs.FS {
	return fstest.MapFS{
		"template.xlsx": &fstest.MapFile{
			Data:    []byte{0x50, 0x4B, 0x03, 0x04},
			Mode:    0644,
			ModTime: time.Now(),
		},
	}
}

func loadTemplateData(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "template.xlsx")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestNewClient(t *testing.T) {
	testFS := createTestFS(t)
	client, err := excel.NewClient(testFS)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer client.Close()
}

func TestNewClient_InvalidTemplate(t *testing.T) {
	client, err := excel.NewClient(createInvalidFS())
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestSaveFile(t *testing.T) {
	// Skip this test if we can't create a real Excel file
	file, err := os.CreateTemp("", "test-excel-*.xlsx")
	if err != nil {
		t.Skip("Skipping test due to failure to create temp file")
	}

	// Get the filename and close the file
	tempFileName := file.Name()
	file.Close()

	// Remove the file to let the Excel client create it
	os.Remove(tempFileName)

	// Make sure the file gets deleted at the end
	defer os.Remove(tempFileName)

	// Create a real Excel file for testing
	excelFile := excelize.NewFile()
	defer excelFile.Close()

	// Create a client with the real Excel file via test helper
	client := &excel.Client{}
	client.SetFileForTest(excelFile)

	// Test saving to a valid path
	err = client.SaveFile(tempFileName)
	require.NoError(t, err)

	// Verify the file exists
	_, err = os.Stat(tempFileName)
	require.NoError(t, err)

	// Test saving to an invalid path (skip if running as root)
	if os.Getuid() != 0 {
		invalidPath := "/nonexistent/directory/file.xlsx"
		err = client.SaveFile(invalidPath)
		assert.Error(t, err)
	}
}

func TestClose(t *testing.T) {
	// Create a real Excel file
	excelFile := excelize.NewFile()

	// Create a client with the Excel file
	client := &excel.Client{}
	client.SetFileForTest(excelFile)

	// Test closing the file
	err := client.Close()
	require.NoError(t, err)

	// Test closing again (should fail since we set file to nil in Close())
	err = client.Close()
	assert.Error(t, err)
}
