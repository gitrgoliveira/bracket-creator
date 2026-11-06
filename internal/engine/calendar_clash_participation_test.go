package engine

import (
	"testing"
)

func TestCompetitionPlacement_TakesPartInClashes(t *testing.T) {
	tests := []struct {
		name      string
		placement CompetitionPlacement
		want      bool
	}{
		{
			name:      "Placed true",
			placement: CompetitionPlacement{Placed: true},
			want:      true,
		},
		{
			name:      "NotPlacedReason date_outside_tournament",
			placement: CompetitionPlacement{Placed: false, NotPlacedReason: "date_outside_tournament"},
			want:      true,
		},
		{
			name:      "no_start_time",
			placement: CompetitionPlacement{Placed: false, NotPlacedReason: "no_start_time"},
			want:      false,
		},
		{
			name:      "no_date",
			placement: CompetitionPlacement{Placed: false, NotPlacedReason: "no_date"},
			want:      false,
		},
		{
			name:      "draw_unreadable",
			placement: CompetitionPlacement{Placed: false, NotPlacedReason: "draw_unreadable"},
			want:      false,
		},
		{
			name:      "empty",
			placement: CompetitionPlacement{},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.placement.TakesPartInClashes(); got != tt.want {
				t.Errorf("TakesPartInClashes() = %v, want %v", got, tt.want)
			}
		})
	}
}
