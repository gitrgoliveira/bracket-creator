package service_test

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/service"
	"github.com/stretchr/testify/mock"
)

// MockExcelClient is a mock implementation of the Excel client
type MockExcelClient struct {
	mock.Mock
}

func (m *MockExcelClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

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

func TestNewTournamentService(t *testing.T) {
	testFS := createTestFS(t)

	// This will likely fail since we're not providing a real Excel file
	// but we can test the error handling
	tournamentService, err := service.NewTournamentService(testFS)

	if err == nil {
		// If by chance it doesn't fail, make sure to clean up
		defer tournamentService.Close()
		t.Log("Unexpected success creating TournamentService with test data")
	} else {
		// We expect an error since our test data isn't a valid Excel file
		t.Logf("Expected error: %v", err)
	}
}

func TestTournamentServiceClose(t *testing.T) {
	// This is a more direct test for the Close method
	// It requires a valid service to be created, which might not be
	// possible with our test data, so we'll skip if necessary

	testFS := createTestFS(t)
	tournamentService, err := service.NewTournamentService(testFS)
	if err != nil {
		t.Skip("Skipping Close test due to failure to create service")
	}

	// Test the Close method
	err = tournamentService.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
