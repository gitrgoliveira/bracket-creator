package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A malformed seed list must reach the operator as a VALIDATION error, so the
// HTTP layer answers 400 with the reason rather than 500.
//
// The path is reachable from the seeding panel without doing anything unusual.
// SaveSeeds persists each rank as it is typed and does not check the set, while
// LoadSeeds refuses to read a set with a gap in it, so an operator who enters
// seed 4 before seeds 1 to 3 leaves seeds.csv holding {4} and the next draw
// fails. Found in browser UAT of a 6-pool draw; the operator saw HTTP 500 and
// no message, which reads as a broken tool at the moment they could have fixed
// it themselves.
//
// This test pins the CLASSIFICATION only. Whether a gapped set should block the
// draw at all is a separate question: R2/D7 say surplus and unplaceable seeds
// degrade with a warning rather than refusing, and the comment in GenerateDraw
// already says "sparse ranks are handled by the seeding pass".
func TestGenerateDrawReportsMalformedSeedsAsValidation(t *testing.T) {
	cases := []struct {
		name  string
		seeds []domain.SeedAssignment
	}{
		{
			name:  "gap in the sequence",
			seeds: []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 4}},
		},
		{
			name:  "a lone high rank, which is what typing seed 4 first produces",
			seeds: []domain.SeedAssignment{{Name: "Alice", SeedRank: 4}},
		},
		{
			name:  "duplicate rank",
			seeds: []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 1}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "seed-validation"

			createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
			saveTestParticipants(t, store, compID,
				[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
			require.NoError(t, store.SaveSeeds(compID, tc.seeds),
				"SaveSeeds does not validate, which is how the bad state is reached")

			err := eng.GenerateDraw(compID)
			require.Error(t, err, "a malformed seed list must not draw")

			var validation *ValidationError
			assert.True(t, errors.As(err, &validation),
				"must be a ValidationError so the API answers 400, got %T: %v", err, err)
			assert.ErrorIs(t, err, domain.ErrInvalidSeedAssignments,
				"the sentinel must survive wrapping, or nothing downstream can classify it")
			assert.Contains(t, err.Error(), "seed",
				"the message must name what is wrong so the operator can fix it: %v", err)
		})
	}
}

// The sentinel is what makes the classification possible, and it must not
// swallow the human-readable reason: the operator needs to know WHICH rule the
// list broke, not merely that it is invalid.
func TestInvalidSeedAssignmentsSentinelKeepsItsReason(t *testing.T) {
	cases := []struct {
		name  string
		seeds []domain.SeedAssignment
		want  string
	}{
		{"gap", []domain.SeedAssignment{{Name: "A", SeedRank: 2}}, "sequential without gaps"},
		{"duplicate", []domain.SeedAssignment{{Name: "A", SeedRank: 1}, {Name: "B", SeedRank: 1}}, "duplicate seed rank"},
		{"zero rank", []domain.SeedAssignment{{Name: "A", SeedRank: 0}}, "greater than 0"},
		{"no name", []domain.SeedAssignment{{Name: "", SeedRank: 1}}, "name cannot be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateAssignments(tc.seeds)
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrInvalidSeedAssignments)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// The refusal message must name the ranks the operator has to go and type.
//
// "seed ranks must be sequential without gaps" states the rule but not the
// remedy, and the draw can be started from screens where the seeding panel's own
// "rank 1, 2, 3 are missing" warning is not on show.
func TestDrawRefusalNamesTheMissingSeedRanks(t *testing.T) {
	cases := []struct {
		name  string
		seeds []domain.SeedAssignment
		want  []string
	}{
		{
			name:  "one rank typed first",
			seeds: []domain.SeedAssignment{{Name: "Alice", SeedRank: 4}},
			want:  []string{"1, 2 and 3", "have not been set", "rank 4 has"},
		},
		{
			name:  "single missing rank",
			seeds: []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 3}},
			want:  []string{"seed rank 2", "has not been set", "rank 3 has"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "seed-message"
			createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
			saveTestParticipants(t, store, compID,
				[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
			require.NoError(t, store.SaveSeeds(compID, tc.seeds))

			err := eng.GenerateDraw(compID)
			require.Error(t, err)
			for _, want := range tc.want {
				assert.Containsf(t, err.Error(), want,
					"the operator must be told what to fix, got: %v", err)
			}
			assert.Contains(t, err.Error(), "generate the draw again",
				"the message must say what to do next: %v", err)
		})
	}
}

// A duplicate rank is not a gap, so the message stays with the precise
// description the validator already gives rather than inventing a missing rank.
func TestDrawRefusalKeepsThePreciseReasonWhenItIsNotAGap(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "seed-dup-message"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 1},
	}))

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate seed rank")
	assert.NotContains(t, err.Error(), "have not been set",
		"a duplicate is not a gap and must not be reported as one: %v", err)
}
