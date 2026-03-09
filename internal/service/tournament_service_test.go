package service_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNewTournamentService(t *testing.T) {
	testFS := createTestFS(t)
	tournamentService, err := service.NewTournamentService(testFS)
	require.NoError(t, err)
	require.NotNil(t, tournamentService)
	defer tournamentService.Close()
}

func TestNewTournamentService_InvalidTemplate(t *testing.T) {
	tournamentService, err := service.NewTournamentService(createInvalidFS())
	assert.Error(t, err)
	assert.Nil(t, tournamentService)
}

func TestTournamentServiceClose(t *testing.T) {
	testFS := createTestFS(t)
	tournamentService, err := service.NewTournamentService(testFS)
	require.NoError(t, err)
	require.NotNil(t, tournamentService)

	// Test the Close method
	err = tournamentService.Close()
	require.NoError(t, err)

	// Closing twice should surface an error from the Excel client
	err = tournamentService.Close()
	assert.Error(t, err)
}
