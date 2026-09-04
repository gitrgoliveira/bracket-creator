package export

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// engineOnlySheets lists sheets legitimately produced by ONLY the
// blank-template export (Engine.ExportCompetitionXlsx), never by
// BuildResultsWorkbook. A future sheet addition that is intentionally
// one-sided belongs here, with a comment explaining why; anything else
// showing up as a delta is the bug TestExportPipelineSheetParity exists to
// catch, not a case to special-case away.
//
// Tags (helper.SheetTags): a numbered-tag sheet with embedded QR codes for
// printing BEFORE the competition runs. It has no place in the RESULTS
// workbook (downloaded AFTER scoring, to archive results), so
// BuildResultsWorkbook never renders it.
var engineOnlySheets = []string{helper.SheetTags}

// TestExportPipelineSheetParity guards the two workbook builders that both
// walk the "data / Pool Draw / Pool Matches / Elimination Matches / Tree N /
// Names to Print X / Kachinuki Detail / ..." sheet pipeline for one
// competition: Engine.ExportCompetitionXlsx (internal/engine/export.go, the
// blank-template export) and BuildResultsWorkbook (internal/export/builder.go,
// the results export). Before mp-yuy8 the two functions were hand-maintained
// parallel copies of the same sheet sequence, and a sheet added to one and
// not the other shipped as a real bug (mp-8b1b finding R8: the Kachinuki
// Detail sheet was added to only one of the two builders) -- every existing
// GetSheetList() assertion in this package (and in internal/engine) only
// exercises ONE of the two paths, so nothing previously caught a builder
// drifting out of sync with its twin.
//
// This test lives in package export (not engine) because internal/export
// already imports internal/engine; the reverse would be an import cycle.
//
// The two paths now converge on engine.RenderCompetitionWorkbook, the shared
// sheet pipeline mp-yuy8 extracted. That extraction does NOT make this guard
// a tautology, and it must not be simplified away or deleted: both builders
// still add their OWN path-specific extras around the shared pipeline (the
// blank-template export's Tags sheet, the results export's score/standings
// overlays, and whatever either grows later), so this test keeps asserting
// that those extras never silently diverge. The extraction made the test
// pass more trivially for the SHARED steps, not pointless: it is what caught
// this bead's own kachinuki_team_bracket_bouts fixture regression when the
// bracket-draw-mismatch guard (see ErrBracketDrawMismatch) was widened to
// cover more than the bronze-only shape.
//
// Table-driven over competition SHAPES: a single non-kachinuki fixture is not
// enough, because Engine.collectKachinukiMatches returns nil for a
// non-kachinuki competition and helper.WriteKachinukiDetailSheet no-ops on
// empty input -- neither builder emits helper.SheetKachinukiDetail on such a
// fixture, so the shipped Kachinuki Detail bug would sail straight through a
// single-shape version of this test. Each case's mustAppearInBoth list is
// asserted BEFORE the parity check, so a fixture that fails to exercise the
// path it claims to (and would let parity pass by both builders staying
// silent) fails loudly instead of pinning nothing.
func TestExportPipelineSheetParity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// configure mutates the competition testSetup already saved (loading
		// and re-saving it) and seeds any additional state (pools, bracket,
		// participants, tournament) the case needs.
		configure func(t *testing.T, store *state.Store, eng *engine.Engine, compID string)
		// mustAppearInBoth lists sheets that must be genuinely present in
		// BOTH workbooks, proving the fixture actually exercises the code
		// path it claims to.
		mustAppearInBoth []string
	}{
		{
			name: "mixed_two_court_with_pools",
			configure: func(t *testing.T, store *state.Store, eng *engine.Engine, compID string) {
				t.Helper()
				comp, err := store.LoadCompetition(compID)
				require.NoError(t, err)
				comp.Format = state.CompFormatMixed
				comp.Courts = []string{"A", "B"}
				require.NoError(t, store.SaveCompetition(comp))

				// A saved tournament: Engine.ExportCompetitionXlsx reads it
				// (best-effort) for the Tags sheet's QR public URL, so
				// exercise that read path too.
				require.NoError(t, store.SaveTournament(&state.Tournament{
					Name: "Parity Tournament", Venue: "Dojo Hall", Courts: []string{"A", "B"},
				}))

				// 2 courts exercises Tree pagination (NextPow2(numCourts))
				// and the per-court "Names to Print A"/"Names to Print B"
				// sheets.
				pools := makePools()
				require.NoError(t, store.SavePools(compID, pools))
				require.NoError(t, store.SavePoolMatches(compID, nil))
			},
			mustAppearInBoth: []string{helper.SheetPoolMatches, "Names to Print A", "Names to Print B"},
		},
		{
			// The shape mp-8b1b finding R8 shipped a bug on: a kachinuki team
			// competition with recorded bout data. Fixture mirrors
			// TestBuildResultsWorkbook_KachinukiDetailSheet (builder_test.go)
			// and TestCollectKachinukiMatches_* (internal/engine/kachinuki_export_test.go):
			// SubResults recorded directly on a saved bracket match, which both
			// Engine.collectKachinukiMatches and Engine.KachinukiDetailMatches
			// read straight off disk, independent of pool/elimination-sheet
			// rendering.
			//
			// Format is Knockout, not Mixed (mp-yuy8 task 1): a Mixed
			// competition with an EMPTY pools list but a bracket that
			// already carries real round content is not a shape any real
			// write path produces (Mixed always populates pools.csv before
			// it ever builds a bracket), and since bracketHasKnockoutContent
			// was widened to refuse a stored bracket with non-empty Rounds
			// that this workbook cannot re-derive a draw for,
			// EliminationDraw's pool-fed branch returning nil for an empty
			// pools list (not a genuine re-derivation failure) would
			// otherwise trip that refusal here. Knockout with no pools is
			// the shape this fixture actually needs: KnockoutLeavesFromBracket
			// reads the leaf order straight off the same hand-crafted
			// bracket, so the draw derives cleanly and the Kachinuki Detail
			// sheet assertion below still exercises the same "independent of
			// pool/elimination rendering" data path.
			name: "kachinuki_team_bracket_bouts",
			configure: func(t *testing.T, store *state.Store, eng *engine.Engine, compID string) {
				t.Helper()
				comp, err := store.LoadCompetition(compID)
				require.NoError(t, err)
				comp.Format = state.CompFormatKnockout
				comp.TeamMatchType = state.TeamMatchTypeKachinuki
				comp.TeamSize = 3
				require.NoError(t, store.SaveCompetition(comp))

				require.NoError(t, store.SavePools(compID, []helper.Pool{}))
				require.NoError(t, store.SavePoolMatches(compID, nil))
				require.NoError(t, store.SaveBracket(compID, &state.Bracket{
					Rounds: [][]state.BracketMatch{
						{
							{
								ID: "R1M0", SideA: "Ryu", SideB: "Tora",
								Winner: "Tora", Status: state.MatchStatusCompleted,
								Decision: "kachinuki-exhaustion", MatchNumber: 1,
								SubResults: []state.SubMatchResult{
									{Position: 1, SideA: "Ryu Ichiro", SideB: "Tora Taro", Winner: "Ryu Ichiro", Decision: "fought"},
									{Position: 2, SideA: "Ryu Ichiro", SideB: "Tora Jiro", Winner: "Tora Jiro", Decision: "fought"},
								},
							},
						},
					},
				}))
			},
			mustAppearInBoth: []string{helper.SheetKachinukiDetail},
		},
		{
			// A pure knockout competition (no pools) drives a different
			// leaf-order path (isPureKnockout / knockoutLeaves /
			// KnockoutLeavesFromBracket) than the pool-fed draw the first
			// case exercises. Fixture mirrors engineKnockoutLeaves
			// (internal/engine/bracket_identity_test.go): save participants,
			// then run the real engine start path so a genuine bracket lands
			// on disk for both builders to render.
			name: "pure_knockout_no_pools",
			configure: func(t *testing.T, store *state.Store, eng *engine.Engine, compID string) {
				t.Helper()
				comp, err := store.LoadCompetition(compID)
				require.NoError(t, err)
				comp.Kind = "individual"
				comp.Format = state.CompFormatKnockout
				comp.Status = state.CompStatusSetup
				comp.Courts = []string{"A"}
				comp.StartTime = "09:00"
				require.NoError(t, store.SaveCompetition(comp))

				players := make([]domain.Player, 4)
				for i := range players {
					players[i] = domain.Player{
						Name: fmt.Sprintf("Player%02d", i+1),
						// Unique dojo per player, else the draw's dojo-conflict
						// avoidance can reorder the leaves this case does not
						// care about.
						Dojo: fmt.Sprintf("Dojo%02d", i+1),
					}
				}
				require.NoError(t, store.SaveParticipants(compID, players))
				require.NoError(t, eng.StartCompetition(compID))
			},
			mustAppearInBoth: []string{"Tree 1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, err := os.MkdirTemp("", "export-pipeline-parity-*")
			require.NoError(t, err)
			defer os.RemoveAll(dir)

			store, err := state.NewStore(dir)
			require.NoError(t, err)
			eng := engine.New(store)

			compID := "parity-comp"
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:     compID,
				Name:   "Parity Comp",
				Courts: []string{"A"},
			}))

			tc.configure(t, store, eng, compID)

			engineBytes, err := eng.ExportCompetitionXlsx(compID)
			require.NoError(t, err)
			resultsBytes, err := BuildResultsWorkbook(store, eng, compID)
			require.NoError(t, err)

			engineSheets := sheetList(t, engineBytes)
			resultsSheets := sheetList(t, resultsBytes)

			for _, want := range tc.mustAppearInBoth {
				assert.Containsf(t, engineSheets, want,
					"case %q: fixture must make the ENGINE builder emit %q, or this case proves nothing", tc.name, want)
				assert.Containsf(t, resultsSheets, want,
					"case %q: fixture must make the RESULTS builder emit %q, or this case proves nothing", tc.name, want)
			}

			engineMinusDelta := make([]string, 0, len(engineSheets))
			for _, s := range engineSheets {
				if slices.Contains(engineOnlySheets, s) {
					continue
				}
				engineMinusDelta = append(engineMinusDelta, s)
			}

			// Exact ordered equality, not just set membership: sheet order is
			// the workbook's visible tab order, so the two builders emitting
			// the same sheets in a different order is a real divergence. The
			// standard assert.Equal failure diff on two slices names exactly
			// which element differs and where, so the reader still learns
			// which sheet moved or vanished without a hand-rolled message.
			assert.Equal(t, resultsSheets, engineMinusDelta,
				"case %q: results sheet list must equal the engine sheet list (minus engineOnlySheets), in order", tc.name)
		})
	}
}

// TestExportPipeline_BronzeOnlyMismatchErrorsInBothBuilders pins mp-yuy8
// PHASE 3's correction: a persisted bracket that already carries a
// third-place bout, but whose knockout draw cannot be re-derived at export
// time, is a state conflict, not a partial-render opportunity. Both workbook
// builders (engine.RenderCompetitionWorkbook, shared by
// Engine.ExportCompetitionXlsx and BuildResultsWorkbook) must now refuse this
// shape with engine.ErrBracketDrawMismatch rather than rendering an
// Elimination Matches sheet that carries only the lone 3rd-place block and no
// other knockout content -- a silently-partial workbook the operator has no
// way to tell is partial.
//
// The fixture and its reachability story are unchanged from the original
// bronze-only-fallback test this replaces: reachable through a real write
// path, not just a hand-edited bracket.json. comp.ExtraQualifiers carries no
// `started` guard in PUT /api/competitions/:id
// (internal/mobileapp/handlers_competition.go) -- unlike its
// Naginata/Engi/Format/Kind/TeamMatchType siblings, which all reject a change
// once the competition has started, `current.ExtraQualifiers =
// comp.ExtraQualifiers` merges unconditionally past draw-ready. An operator
// can therefore flip a Naginata competition's ExtraQualifiers to a value
// buildPoolFedDraw marks "out of scope" for the CURRENT pool shape after the
// original bracket -- bronze block included, since Naginata itself IS locked
// post-start -- was already built. This fixture hand-constructs the resulting
// shape directly (no pools, no participants, a bracket with an empty first
// round and a ThirdPlaceMatch) rather than replaying the PUT handler:
// EliminationDraw's re-derivation returns nil (poolDraw: no pools;
// knockoutLeaves: KnockoutLeavesFromBracket finds no rounds and
// KnockoutFinalsFromParticipants finds no participants) while
// bracket.ThirdPlaceMatch is still on disk from the original draw -- the
// exact "draw == nil, hasBronze == true" state that now errors.
func TestExportPipeline_BronzeOnlyMismatchErrorsInBothBuilders(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-pipeline-bronze-mismatch-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "bronze-mismatch-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Bronze Mismatch Comp",
		Kind:     "individual",
		Format:   state.CompFormatKnockout,
		Naginata: true,
		Courts:   []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			Status: state.MatchStatusScheduled,
		},
	}))

	_, err = eng.ExportCompetitionXlsx(compID)
	assert.ErrorIsf(t, err, engine.ErrBracketDrawMismatch,
		"blank-template export: must refuse the bronze-only-mismatch shape, not render it partially")

	_, err = BuildResultsWorkbook(store, eng, compID)
	assert.ErrorIsf(t, err, engine.ErrBracketDrawMismatch,
		"results export: must refuse the bronze-only-mismatch shape, not render it partially")
}

// sheetList opens xlsx bytes and returns its sheet names.
func sheetList(t *testing.T, data []byte) []string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	return f.GetSheetList()
}
