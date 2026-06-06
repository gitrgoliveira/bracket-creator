// Package service handles business logic for the application
package service

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
)

// TournamentService handles tournament operations
type TournamentService struct {
	excelClient *excel.Client
}

// NewTournamentService creates a new tournament service
func NewTournamentService() (*TournamentService, error) {
	return newTournamentServiceWithFactory(excel.NewClient)
}

func newTournamentServiceWithFactory(f func() (*excel.Client, error)) (*TournamentService, error) {
	client, err := f()
	if err != nil {
		return nil, fmt.Errorf("failed to create Excel client: %w", err)
	}
	return &TournamentService{excelClient: client}, nil
}

// Close closes any resources used by the service
func (s *TournamentService) Close() error {
	return s.excelClient.Close()
}

// Additional service methods would be implemented here
