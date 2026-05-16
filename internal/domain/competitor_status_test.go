package domain_test

import (
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompetitorStatusValidation verifies FR-034: a CompetitorStatus with
// Eligible == false must carry a non-empty Reason, and PlayerID must always
// be set.
//
// This is a Red test — domain.CompetitorStatus and its Validate() method do
// not yet exist. The build must fail until the Green implementation (T078)
// lands in internal/domain/competitor_status.go.
func TestCompetitorStatusValidation(t *testing.T) {
	tests := []struct {
		name    string
		status  domain.CompetitorStatus
		wantErr bool
	}{
		{
			name:    "valid eligible",
			status:  domain.CompetitorStatus{PlayerID: "p1", Eligible: true, RecordedAt: time.Now()},
			wantErr: false,
		},
		{
			name: "valid ineligible with reason",
			status: domain.CompetitorStatus{
				PlayerID:   "p1",
				Eligible:   false,
				Reason:     "kiken at match m_12",
				MatchID:    "m_12",
				RecordedAt: time.Now(),
			},
			wantErr: false,
		},
		{
			name:    "invalid ineligible missing reason",
			status:  domain.CompetitorStatus{PlayerID: "p1", Eligible: false, Reason: "", RecordedAt: time.Now()},
			wantErr: true,
		},
		{
			name:    "invalid empty PlayerID",
			status:  domain.CompetitorStatus{PlayerID: "", Eligible: true, RecordedAt: time.Now()},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.status.Validate()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
