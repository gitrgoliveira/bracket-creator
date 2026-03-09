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

// TestNewTournamentServiceRobustness tests service creation with various filesystem configurations
func TestNewTournamentServiceRobustness(t *testing.T) {
	tests := []struct {
		name    string
		setupFS func() fs.FS
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid template",
			setupFS: func() fs.FS {
				return createTestFS(t)
			},
			wantErr: false,
		},
		{
			name: "invalid Excel data",
			setupFS: func() fs.FS {
				return createInvalidFS()
			},
			wantErr: true,
			errMsg:  "failed to create Excel client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := service.NewTournamentService(tt.setupFS())

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, service)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, service)
				defer service.Close()
			}
		})
	}
}

// TestTournamentServiceLifecycle tests the full lifecycle of a tournament service
func TestTournamentServiceLifecycle(t *testing.T) {
	t.Run("create and close", func(t *testing.T) {
		testFS := createTestFS(t)
		svc, err := service.NewTournamentService(testFS)
		require.NoError(t, err)
		require.NotNil(t, svc)

		// Service should be usable after creation
		assert.NotNil(t, svc)

		// Close should succeed
		err = svc.Close()
		assert.NoError(t, err)
	})

	t.Run("multiple service instances", func(t *testing.T) {
		testFS1 := createTestFS(t)
		svc1, err := service.NewTournamentService(testFS1)
		require.NoError(t, err)
		defer svc1.Close()

		testFS2 := createTestFS(t)
		svc2, err := service.NewTournamentService(testFS2)
		require.NoError(t, err)
		defer svc2.Close()

		// Both services should exist independently
		assert.NotNil(t, svc1)
		assert.NotNil(t, svc2)
	})
}

// TestTournamentServiceIntegration tests realistic tournament service workflows
func TestTournamentServiceIntegration(t *testing.T) {
	testFS := createTestFS(t)
	svc, err := service.NewTournamentService(testFS)
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	// Service should be ready for operations
	assert.NotNil(t, svc)

	// In the future, this would test workflow methods like:
	// - CreatePoolTournament(players, poolSize)
	// - CreatePlayoffTournament(players, seeding)
	// - ApplySeeding(tournament, seedAssignments)
	// etc.
}
