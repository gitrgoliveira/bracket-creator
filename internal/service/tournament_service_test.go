package service_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTournamentService(t *testing.T) {
	svc, err := service.NewTournamentService()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()
}

func TestTournamentServiceClose(t *testing.T) {
	svc, err := service.NewTournamentService()
	require.NoError(t, err)
	require.NotNil(t, svc)

	err = svc.Close()
	require.NoError(t, err)

	// Closing twice should surface an error from the Excel client.
	err = svc.Close()
	assert.Error(t, err)
}

// TestTournamentServiceLifecycle tests the full lifecycle of a tournament service.
func TestTournamentServiceLifecycle(t *testing.T) {
	t.Run("create and close", func(t *testing.T) {
		svc, err := service.NewTournamentService()
		require.NoError(t, err)
		require.NotNil(t, svc)

		err = svc.Close()
		assert.NoError(t, err)
	})

	t.Run("multiple service instances", func(t *testing.T) {
		svc1, err := service.NewTournamentService()
		require.NoError(t, err)
		defer svc1.Close()

		svc2, err := service.NewTournamentService()
		require.NoError(t, err)
		defer svc2.Close()

		assert.NotNil(t, svc1)
		assert.NotNil(t, svc2)
	})
}

// TestTournamentServiceIntegration tests realistic tournament service workflows.
func TestTournamentServiceIntegration(t *testing.T) {
	svc, err := service.NewTournamentService()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	assert.NotNil(t, svc)
}
