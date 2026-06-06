package service

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTournamentService_ExcelClientError(t *testing.T) {
	_, err := newTournamentServiceWithFactory(func() (*excel.Client, error) {
		return nil, fmt.Errorf("injected excel failure")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create Excel client")
}
