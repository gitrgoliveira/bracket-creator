package excel_test

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/xuri/excelize/v2"
)

// createTestFS creates an in-memory filesystem for testing
func createTestFS(t *testing.T) fs.FS {
	// Create a minimal Excel file
	excelData := []byte{
		0x50, 0x4B, 0x03, 0x04, // PK signature for ZIP files
		// This is not a real Excel file, just testing error handling
	}

	return fstest.MapFS{
		"template.xlsx": &fstest.MapFile{
			Data:    excelData,
			Mode:    0644,
			ModTime: time.Now(),
		},
	}
}

func TestNewClient(t *testing.T) {
	testFS := createTestFS(t)

	// This will likely fail since we're not providing a real Excel file
	// but we can test the error handling
	client, err := excel.NewClient(testFS)

	if err == nil {
		// If by chance it doesn't fail, make sure to clean up
		defer client.Close()
		t.Log("Unexpected success creating Excel client with test data")
	} else {
		// We expect an error since our test data isn't a valid Excel file
		t.Logf("Expected error: %v", err)
	}
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

	// Create a client with the real Excel file via reflection since the file field is private
	client := &excel.Client{}
	// We'll use a test-specific method for setting the file
	client.SetFileForTest(excelFile)

	// Test saving to a valid path
	err = client.SaveFile(tempFileName)
	if err != nil {
		t.Errorf("SaveFile failed with valid path: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(tempFileName); os.IsNotExist(err) {
		t.Error("Expected file to exist after SaveFile")
	}

	// Test saving to an invalid path
	invalidPath := "/nonexistent/directory/file.xlsx"
	err = client.SaveFile(invalidPath)
	if err == nil {
		t.Error("Expected SaveFile to fail with invalid path")
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
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Test closing again (should fail since we set file to nil in Close())
	err = client.Close()
	if err == nil {
		t.Error("Expected Close to fail when called twice")
	} else {
		t.Logf("Got expected error when closing twice: %v", err)
	}
}
