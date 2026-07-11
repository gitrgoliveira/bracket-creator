// Command engi_samples generates four sample Engi (kata) Excel workbooks by
// driving the REAL engine + export code paths added in PR #351 (bead mp-wvba),
// so a reviewer can open the actual output before approving:
//
//  1. engi-blank-mixed.xlsx            blank template (ExportCompetitionXlsx), unscored
//  2. engi-partial-mixed-3rd-place.xlsx  results workbook, pools + semis + 3rd-place
//     playoff scored, FINAL left empty (part-full)
//  3. engi-mixed-complete.xlsx         results workbook, mixed fully scored incl. bronze
//  4. engi-league-complete.xlsx        results workbook, league round-robin fully scored
//
// Run: go run ./scratch/engi_samples
// Output: ./engi-samples/ (untracked; delete when done reviewing).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/export"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// validFlags are engi-legal flag pairs (odd totals in {1,3,5}); cycled to vary
// scores across matches. Includes a 5-0 shutout so the loser's real "0" (the
// Copilot fix) is visible, and lopsided/close bouts for variety.
var validFlags = [][2]int{{3, 2}, {5, 0}, {2, 3}, {4, 1}, {3, 0}, {1, 2}, {0, 5}, {2, 1}}

func check(err error, ctx string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", ctx, err))
	}
}

// recordFlags scores one engi match. It uses RecordMatchResultWithIneligibility
// (NOT RecordMatchResult): only that variant carries the engi dispatch seam that
// routes to recordEngiMatchResult, applies the flag counts, decides the winner,
// and marks the bout completed. RecordMatchResult takes the kendo ippon path and
// would silently leave an engi bout unscored. This mirrors the HTTP score handler
// (handlers_match.go), which calls the …WithIneligibilityTx variant.
func recordFlags(eng *engine.Engine, compID, matchID string, a, b int) {
	if _, err := eng.RecordMatchResultWithIneligibility(compID, matchID, &state.MatchResult{FlagsA: a, FlagsB: b}); err != nil {
		check(err, fmt.Sprintf("score %s (%d-%d)", matchID, a, b))
	}
}

func newEngine() (cleanup func(), store *state.Store, eng *engine.Engine) {
	dir, err := os.MkdirTemp("", "engi-sample-*")
	check(err, "mkdtemp")
	store, err = state.NewStore(dir)
	check(err, "new store")
	eng = engine.New(store)
	cleanup = func() { check(os.RemoveAll(dir), "cleanup "+dir) }
	return
}

// engiPairs builds n engi competitors. Each is ONE participant: member 1 in
// Name, member 2 in DisplayName, with a shared dojo.
func engiPairs(n int) []domain.Player {
	m1first := []string{"Yuki", "Haru", "Ren", "Aoi", "Sora", "Kai", "Rin", "Mei"}
	m2first := []string{"Jun", "Nao", "Emi", "Taro", "Hana", "Ken", "Yui", "Dai"}
	last := []string{"Tanaka", "Sato", "Suzuki", "Ito", "Yamamoto", "Nakamura", "Kobayashi", "Kato"}
	dojos := []string{"Kita Dojo", "Minami Dojo", "Higashi Dojo", "Nishi Dojo", "Chuo Dojo", "Sakura Dojo", "Ume Dojo", "Take Dojo"}
	ps := make([]domain.Player, n)
	for i := 0; i < n; i++ {
		ps[i] = domain.Player{
			Name:        fmt.Sprintf("%s %s", m1first[i%len(m1first)], last[i%len(last)]),
			DisplayName: fmt.Sprintf("%s %s", m2first[i%len(m2first)], last[(i+3)%len(last)]),
			Dojo:        dojos[i%len(dojos)],
		}
	}
	return ps
}

func engiComp(id, format string, poolSize, poolWinners int, courts []string) *state.Competition {
	return &state.Competition{
		ID:           id,
		Name:         "Engi Sample (" + format + ")",
		Kind:         "individual",
		Format:       format,
		PoolSize:     poolSize,
		PoolSizeMode: "min",
		PoolWinners:  poolWinners,
		RoundRobin:   true,
		Courts:       courts,
		StartTime:    "09:00",
		Status:       "setup",
		Engi:         true,
		Naginata:     true, // enables the single 3rd-place playoff on the bracket
	}
}

func scorePools(eng *engine.Engine, store *state.Store, compID string) {
	ms, err := store.LoadPoolMatches(compID)
	check(err, "load pool matches "+compID)
	for i, m := range ms {
		fa, fb := validFlags[i%len(validFlags)][0], validFlags[i%len(validFlags)][1]
		recordFlags(eng, compID, m.ID, fa, fb)
		// Mirror the HTTP handler: advance pools→bracket after each score so a
		// mixed competition seeds its knockout finalists (and a league finalizes
		// standings) as soon as the feeding pools complete.
		if _, err := eng.MaybeAutoCompletePools(compID); err != nil {
			check(err, "auto-complete pools "+compID)
		}
	}
}

// scoreRound scores every playable, not-yet-completed match in round rIdx.
// Byes (a side empty) auto-advance and are skipped. Reloads the bracket so
// sides propagated from the previous round are present.
func scoreRound(eng *engine.Engine, store *state.Store, compID string, rIdx int) {
	b, err := store.LoadBracket(compID)
	check(err, "load bracket")
	for i := range b.Rounds[rIdx] {
		m := b.Rounds[rIdx][i]
		if m.Status == state.MatchStatusCompleted || m.SideA == "" || m.SideB == "" {
			continue
		}
		fa, fb := validFlags[(rIdx*5+i)%len(validFlags)][0], validFlags[(rIdx*5+i)%len(validFlags)][1]
		recordFlags(eng, compID, m.ID, fa, fb)
	}
}

func scoreBronze(eng *engine.Engine, store *state.Store, compID string) {
	b, err := store.LoadBracket(compID)
	check(err, "load bracket for bronze")
	tp := b.ThirdPlaceMatch
	if tp == nil || tp.SideA == "" || tp.SideB == "" {
		return
	}
	recordFlags(eng, compID, tp.ID, 3, 2)
}

// scoreBracket scores every round before the final, then the 3rd-place playoff,
// then optionally the final. Leaving the final unscored (scoreFinal=false)
// yields a genuinely part-full workbook with a decided bronze but an open final.
func scoreBracket(eng *engine.Engine, store *state.Store, compID string, scoreFinal bool) {
	b, err := store.LoadBracket(compID)
	check(err, "load bracket top")
	if b == nil || len(b.Rounds) == 0 {
		panic("no bracket rounds for " + compID)
	}
	finalIdx := len(b.Rounds) - 1
	for r := 0; r < finalIdx; r++ {
		scoreRound(eng, store, compID, r)
	}
	scoreBronze(eng, store, compID)
	if scoreFinal {
		scoreRound(eng, store, compID, finalIdx)
	}
}

func writeResults(store *state.Store, eng *engine.Engine, compID, path string) {
	data, err := export.BuildResultsWorkbook(store, eng, compID)
	check(err, "build results "+compID)
	check(os.WriteFile(path, data, 0o600), "write "+path)
	fmt.Printf("  wrote %s (%d KB)\n", filepath.Base(path), len(data)/1024)
}

func writeBlank(eng *engine.Engine, compID, path string) {
	data, err := eng.ExportCompetitionXlsx(compID)
	check(err, "export blank "+compID)
	check(os.WriteFile(path, data, 0o600), "write "+path)
	fmt.Printf("  wrote %s (%d KB)\n", filepath.Base(path), len(data)/1024)
}

func main() {
	outDir := "engi-samples"
	check(os.MkdirAll(outDir, 0o750), "mkdir out")

	// 1. BLANK TEMPLATE — mixed engi, draw-ready, unscored.
	fmt.Println("1. blank template (mixed)")
	{
		cleanup, store, eng := newEngine()
		defer cleanup()
		id := "blank-mixed"
		check(store.SaveCompetition(engiComp(id, state.CompFormatMixed, 4, 2, []string{"A"})), "save comp")
		check(store.SaveParticipants(id, engiPairs(8)), "save participants")
		check(eng.StartCompetition(id), "start")
		writeBlank(eng, id, filepath.Join(outDir, "engi-blank-mixed.xlsx"))
	}

	// 2. PART-FULL — mixed engi: pools + semifinals + 3rd-place playoff scored,
	//    FINAL deliberately left open.
	fmt.Println("2. part-full results (mixed, 3rd-place scored, final open)")
	{
		cleanup, store, eng := newEngine()
		defer cleanup()
		id := "partial-mixed"
		check(store.SaveCompetition(engiComp(id, state.CompFormatMixed, 4, 2, []string{"A"})), "save comp")
		check(store.SaveParticipants(id, engiPairs(8)), "save participants")
		check(eng.StartCompetition(id), "start")
		scorePools(eng, store, id)
		scoreBracket(eng, store, id, false) // semis + bronze, no final
		writeResults(store, eng, id, filepath.Join(outDir, "engi-partial-mixed-3rd-place.xlsx"))
	}

	// 3. MIXED COMPLETE — pools + full bracket + 3rd-place + final.
	fmt.Println("3. complete results (mixed = pools + playoffs)")
	{
		cleanup, store, eng := newEngine()
		defer cleanup()
		id := "mixed-full"
		check(store.SaveCompetition(engiComp(id, state.CompFormatMixed, 4, 2, []string{"A"})), "save comp")
		check(store.SaveParticipants(id, engiPairs(8)), "save participants")
		check(eng.StartCompetition(id), "start")
		scorePools(eng, store, id)
		scoreBracket(eng, store, id, true)
		writeResults(store, eng, id, filepath.Join(outDir, "engi-mixed-complete.xlsx"))
	}

	// 4. LEAGUE COMPLETE — single round-robin, fully scored.
	fmt.Println("4. complete results (league round-robin)")
	{
		cleanup, store, eng := newEngine()
		defer cleanup()
		id := "league-full"
		check(store.SaveCompetition(engiComp(id, state.CompFormatLeague, 6, 1, []string{"A"})), "save comp")
		check(store.SaveParticipants(id, engiPairs(6)), "save participants")
		check(eng.StartCompetition(id), "start")
		scorePools(eng, store, id)
		writeResults(store, eng, id, filepath.Join(outDir, "engi-league-complete.xlsx"))
	}

	fmt.Printf("\nDone. 4 files in ./%s/\n", outDir)
}
