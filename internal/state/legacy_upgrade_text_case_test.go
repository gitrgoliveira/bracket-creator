package state_test

// legacy_upgrade_text_case_test.go pins the load repair that rewrites text
// saved before every door canonicalised it (operator decisions D14, D15 and
// D20, 2026-11-01): dojos, team member names and award recipients
// title-cased, zekken in capitals. It also pins D24 (b), 2026-11-02: the draw
// and the seeds take the roster's spelling where they differ from it only by
// letter case. Bouts already fought keep their names as recorded (D18), so
// nothing here touches a result.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rewriteFile replaces old with new in a stored file, as a hand edit or an
// earlier release would have left it.
func rewriteFile(t *testing.T, path, old, new string) {
	t.Helper()
	raw, err := bctest.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), old, "the fixture must hold %q in %s", old, path)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), old, new)), 0o600))
}

type fileStamp struct {
	body    string
	modTime time.Time
}

func stampFiles(t *testing.T, dir string, names ...string) map[string]fileStamp {
	t.Helper()
	out := make(map[string]fileStamp, len(names))
	for _, n := range names {
		p := filepath.Join(dir, "competitions", "c1", n)
		raw, err := bctest.ReadFile(p)
		require.NoError(t, err)
		info, err := os.Stat(p)
		require.NoError(t, err)
		out[n] = fileStamp{body: string(raw), modTime: info.ModTime()}
	}
	return out
}

// The roster's dojo and zekken, an id-less seeds.csv row, a pools.csv member
// and an award recipient saved in another case are rewritten on load, and a
// second load writes nothing.
func TestLegacyTextCaseUpgrade_RosterSeedsPoolsAndAwards(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", WithZekkenName: true,
		FightingSpiritAwards: []state.FightingSpiritAward{
			{Title: "Fighting spirit", RecipientName: "aiko mori", RecipientDojo: "tobukan"},
		},
	}))
	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", DisplayName: "RINSATO", Dojo: "Seibukan"},
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", DisplayName: "YUKI", Dojo: "Tobukan"},
	}))
	compDir := filepath.Join(dir, "competitions", "c1")
	rewriteFile(t, filepath.Join(compDir, "participants.csv"), "Seibukan", "seibukan")
	rewriteFile(t, filepath.Join(compDir, "participants.csv"), "RINSATO", "rinsato")
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "seeds.csv"),
		[]byte("Rank,Name,Dojo\n1,rin sato,seibukan\n"), 0o600))
	// Pre-append-column shape: PoolName,Name,Position,DisplayName,Dojo,Seed,Number.
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "pools.csv"),
		[]byte("Pool A,Rin Sato,0,rinsato,seibukan,,\nPool A,Yuki Tanaka,1,YUKI,Tobukan,,\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	players, err := fresh.LoadParticipants("c1", true)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.Equal(t, "Seibukan", players[0].Dojo)
	assert.Equal(t, "RINSATO", players[0].DisplayName)
	raw, err := bctest.ReadFile(filepath.Join(compDir, "participants.csv"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "seibukan", "the roster's dojo is rewritten on disk")
	assert.NotContains(t, string(raw), "rinsato", "the roster's zekken is rewritten on disk")

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Rin Sato", seeds[0].Name)
	assert.Equal(t, "Seibukan", seeds[0].Dojo)
	assert.Equal(t, rinID, seeds[0].ID, "the title-cased row resolves on the roster and gets its id")

	pools, err := fresh.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)
	assert.Equal(t, "Seibukan", pools[0].Players[0].Dojo)
	assert.Equal(t, "RINSATO", pools[0].Players[0].DisplayName)
	assert.Equal(t, rinID, pools[0].Players[0].ID, "the title-cased member resolves on the roster and gets its id")

	comp, err := fresh.LoadCompetition("c1")
	require.NoError(t, err)
	require.Len(t, comp.FightingSpiritAwards, 1)
	assert.Equal(t, "Aiko Mori", comp.FightingSpiritAwards[0].RecipientName)
	assert.Equal(t, "Tobukan", comp.FightingSpiritAwards[0].RecipientDojo)

	// A second load converges on what the first wrote: nothing is rewritten.
	before := stampFiles(t, dir, "participants.csv", "seeds.csv", "pools.csv", "config.md")
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	assert.Equal(t, before, stampFiles(t, dir, "participants.csv", "seeds.csv", "pools.csv", "config.md"),
		"a second load writes nothing")
}

// Earlier releases title-cased a word that begins with a digit ("2nd" stored
// as "2Nd"). Title-casing never lowers a letter, so what they stored is left
// as saved on load (operator decision D23): the roster, an id-less seeds.csv
// row, a pools.csv member and an award recipient keep "2Nd", the seed row and
// the pool member still resolve to the roster's competitor, and a second load
// writes nothing.
func TestLegacyTextCaseUpgrade_KeepsAnEarlierReleasesDigitLedWordsAsSaved(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", WithZekkenName: true,
		FightingSpiritAwards: []state.FightingSpiritAward{
			{Title: "Fighting spirit", RecipientName: "Aiko 3Rd", RecipientDojo: "4Th Dojo"},
		},
	}))
	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin 2Nd", DisplayName: "RIN", Dojo: "2Nd Dojo"},
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", DisplayName: "YUKI", Dojo: "3RD Dojo"},
	}))
	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "seeds.csv"),
		[]byte("Rank,Name,Dojo\n1,Rin 2Nd,2Nd Dojo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "pools.csv"),
		[]byte("Pool A,Rin 2Nd,0,RIN,2Nd Dojo,,\nPool A,Yuki Tanaka,1,YUKI,3RD Dojo,,\n"), 0o600))
	rosterBefore := stampFiles(t, dir, "participants.csv")

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	assert.Equal(t, rosterBefore, stampFiles(t, dir, "participants.csv"), "the roster is not rewritten")
	players, err := fresh.LoadParticipants("c1", true)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.Equal(t, "Rin 2Nd", players[0].Name)
	assert.Equal(t, "2Nd Dojo", players[0].Dojo)
	assert.Equal(t, "3RD Dojo", players[1].Dojo)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Rin 2Nd", seeds[0].Name)
	assert.Equal(t, "2Nd Dojo", seeds[0].Dojo)
	assert.Equal(t, rinID, seeds[0].ID, "the row resolves on the roster as saved")

	pools, err := fresh.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)
	assert.Equal(t, "Rin 2Nd", pools[0].Players[0].Name)
	assert.Equal(t, "2Nd Dojo", pools[0].Players[0].Dojo)
	assert.Equal(t, rinID, pools[0].Players[0].ID, "the member resolves on the roster as saved")

	comp, err := fresh.LoadCompetition("c1")
	require.NoError(t, err)
	require.Len(t, comp.FightingSpiritAwards, 1)
	assert.Equal(t, "Aiko 3Rd", comp.FightingSpiritAwards[0].RecipientName)
	assert.Equal(t, "4Th Dojo", comp.FightingSpiritAwards[0].RecipientDojo)

	before := stampFiles(t, dir, "participants.csv", "seeds.csv", "pools.csv", "config.md")
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	assert.Equal(t, before, stampFiles(t, dir, "participants.csv", "seeds.csv", "pools.csv", "config.md"),
		"a second load writes nothing")
}

// A process that stopped after the roster was rewritten in the stored form
// and before seeds.csv was converges on the next load: the id-less seed row is
// put in the roster's form first, so it still resolves.
func TestLegacyTextCaseUpgrade_SeedsConvergeAfterACrashBetweenFiles(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "seeds.csv"),
		[]byte("Rank,Name,Dojo\n1,rin sato,seibukan\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Rin Sato", seeds[0].Name)
	assert.Equal(t, "Seibukan", seeds[0].Dojo)
	assert.Equal(t, rinID, seeds[0].ID)
}

// drawSpellingFixture is an earlier release's tournament (operator decision
// D24 (b)): the paste box stored "rin 2nd" as typed, and the draw and seeds
// took the old reader's "Rin 2Nd". Sho 3Rd was saved that way (D23), so the
// roster and every copy agree on it. It returns the ids of Rin, Ken, Yuki and
// Sho, in that order.
func drawSpellingFixture(t *testing.T, s *state.Store, dir string, rawRoster bool) (rin, ken, yuki, sho string) {
	t.Helper()
	rin, ken, yuki, sho = helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rin, Name: "Rin 2nd", Dojo: "Kenshinkan 2nd"},
		{ID: ken, Name: "Ken 2nd", Dojo: "Seibukan"},
		{ID: yuki, Name: "Yuki Tanaka", Dojo: "Tobukan"},
		{ID: sho, Name: "Sho 3Rd", Dojo: "Tobukan"},
	}))
	require.NoError(t, s.SavePools("c1", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: rin, Name: "Rin 2Nd", Dojo: "Kenshinkan 2Nd", PoolPosition: 0},
		{ID: ken, Name: "Ken 2Nd", Dojo: "Seibukan", PoolPosition: 1},
		{ID: yuki, Name: "Yuki Tanaka", Dojo: "Tobukan", PoolPosition: 2},
		{ID: sho, Name: "Sho 3Rd", Dojo: "Tobukan", PoolPosition: 3},
	}}}))
	require.NoError(t, s.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Rin 2Nd", SideAID: rin, SideB: "Yuki Tanaka", SideBID: yuki,
			Winner: "Rin 2Nd", WinnerID: rin, IpponsA: []string{"M", "M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Rin 2Nd", SideAID: rin, SideB: "Ken 2Nd", SideBID: ken, Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Ken 2Nd", SideAID: ken, SideB: "Yuki Tanaka", SideBID: yuki, IpponsA: []string{"K"}, Status: state.MatchStatusRunning},
		{ID: "Pool A-3", SideA: "Sho 3Rd", SideAID: sho, SideB: "Yuki Tanaka", SideBID: yuki, Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", SideA: "Rin 2Nd", SideAID: rin, SideB: "Yuki Tanaka", SideBID: yuki,
				Winner: "Rin 2Nd", WinnerID: rin, Status: state.MatchStatusCompleted},
			// A bye the draw completed: never fought.
			{ID: "m-r1-1", SideA: "Ken 2Nd", SideAID: ken, Winner: "Ken 2Nd", WinnerID: ken, Status: state.MatchStatusCompleted},
		},
		{
			{ID: "m-r2-0", SideA: "Rin 2Nd", SideAID: rin, SideB: "Ken 2Nd", SideBID: ken, Status: state.MatchStatusScheduled},
		},
	}}))
	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "seeds.csv"),
		[]byte("Rank,Name,Dojo,ID\n1,Rin 2Nd,Kenshinkan 2Nd,"+rin+"\n2,Ken 2Nd,Seibukan,\n"), 0o600))
	for _, f := range []string{"pools.csv", "pool-matches.csv", "bracket.json"} {
		raw, err := bctest.ReadFile(filepath.Join(compDir, f))
		require.NoError(t, err)
		require.Contains(t, string(raw), "Rin 2Nd", "the fixture's %s holds the earlier release's spelling", f)
	}
	if rawRoster {
		rewriteFile(t, filepath.Join(compDir, "participants.csv"), "Rin 2nd", "rin 2nd")
		rewriteFile(t, filepath.Join(compDir, "participants.csv"), "Kenshinkan 2nd", "kenshinkan 2nd")
		rewriteFile(t, filepath.Join(compDir, "participants.csv"), "Ken 2nd", "ken 2nd")
	}
	return rin, ken, yuki, sho
}

func bracketMatchByID(t *testing.T, b *state.Bracket, id string) state.BracketMatch {
	t.Helper()
	for _, round := range b.Rounds {
		for _, m := range round {
			if m.ID == id {
				return m
			}
		}
	}
	require.FailNow(t, "no bracket match "+id)
	return state.BracketMatch{}
}

// TestLegacyTextCaseUpgrade_DrawAndSeedsTakeTheRostersSpelling pins D24 (b):
// the roster an earlier release stored raw is rewritten to "Rin 2nd", and
// every copy the draw and the seeds hold of it by id takes that spelling,
// except a match already fought (D18). An id-less seed row is given its id
// once by a case-insensitive match and merges onto the roster.
func TestLegacyTextCaseUpgrade_DrawAndSeedsTakeTheRostersSpelling(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	rin, ken, _, _ := drawSpellingFixture(t, s, dir, true)

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	byID := map[string]domain.Player{}
	for _, p := range players {
		byID[p.ID] = p
	}
	assert.Equal(t, "Rin 2nd", byID[rin].Name)
	assert.Equal(t, "Kenshinkan 2nd", byID[rin].Dojo)
	assert.Equal(t, 1, byID[rin].Seed)
	assert.Equal(t, 2, byID[ken].Seed, "the id-less seed row resolves on the roster and merges")

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 2)
	assert.Equal(t, domain.SeedAssignment{SeedRank: 1, Name: "Rin 2nd", Dojo: "Kenshinkan 2nd", ID: rin}, seeds[0])
	assert.Equal(t, domain.SeedAssignment{SeedRank: 2, Name: "Ken 2nd", Dojo: "Seibukan", ID: ken}, seeds[1])

	pools, err := fresh.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	names := make([]string, 0, len(pools[0].Players))
	for _, p := range pools[0].Players {
		names = append(names, p.Name+" / "+p.Dojo)
	}
	assert.Equal(t, []string{"Rin 2nd / Kenshinkan 2nd", "Ken 2nd / Seibukan", "Yuki Tanaka / Tobukan", "Sho 3Rd / Tobukan"}, names,
		"every pool member takes the roster's spelling; Sho 3Rd, saved that way, keeps it")

	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	sides := map[string][3]string{}
	for _, m := range matches {
		sides[m.ID] = [3]string{m.SideA, m.SideB, m.Winner}
	}
	assert.Equal(t, [3]string{"Rin 2Nd", "Yuki Tanaka", "Rin 2Nd"}, sides["Pool A-0"], "a finished match keeps its names (D18)")
	assert.Equal(t, [3]string{"Rin 2nd", "Ken 2nd", ""}, sides["Pool A-1"], "a match not yet fought takes the roster's spelling")
	assert.Equal(t, [3]string{"Ken 2Nd", "Yuki Tanaka", ""}, sides["Pool A-2"], "a running match keeps its names (D18)")
	assert.Equal(t, [3]string{"Sho 3Rd", "Yuki Tanaka", ""}, sides["Pool A-3"])

	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	fought := bracketMatchByID(t, bracket, "m-r1-0")
	assert.Equal(t, [2]string{"Rin 2Nd", "Rin 2Nd"}, [2]string{fought.SideA, fought.Winner}, "a fought knockout match keeps its names (D18)")
	bye := bracketMatchByID(t, bracket, "m-r1-1")
	assert.Equal(t, [2]string{"Ken 2nd", "Ken 2nd"}, [2]string{bye.SideA, bye.Winner}, "a bye was never fought: its side and winner converge together")
	next := bracketMatchByID(t, bracket, "m-r2-0")
	assert.Equal(t, [2]string{"Rin 2nd", "Ken 2nd"}, [2]string{next.SideA, next.SideB})

	files := []string{"participants.csv", "seeds.csv", "pools.csv", "pool-matches.csv", "bracket.json"}
	before := stampFiles(t, dir, files...)
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	assert.Equal(t, before, stampFiles(t, dir, files...), "a second load writes nothing")
}

// TestLegacyTextCaseUpgrade_DrawConvergesOnARosterAlreadyRewritten: a crash
// after the roster's rewrite and before the copies' leaves a roster that reads
// as stored, and the next load still gives the copies its spelling.
func TestLegacyTextCaseUpgrade_DrawConvergesOnARosterAlreadyRewritten(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	drawSpellingFixture(t, s, dir, false)
	rosterBefore := stampFiles(t, dir, "participants.csv")

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	assert.Equal(t, rosterBefore, stampFiles(t, dir, "participants.csv"), "the roster is already in its stored form")
	pools, err := fresh.LoadPools("c1")
	require.NoError(t, err)
	assert.Equal(t, "Rin 2nd", pools[0].Players[0].Name)
	assert.Equal(t, "Kenshinkan 2nd", pools[0].Players[0].Dojo)
	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == "Pool A-1" {
			assert.Equal(t, "Rin 2nd", m.SideA)
		}
	}
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	assert.Equal(t, "Rin 2nd", bracketMatchByID(t, bracket, "m-r2-0").SideA)
	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	assert.Equal(t, "Rin 2nd", seeds[0].Name)
}

// Two participants.csv rows that are one competitor once their text is in the
// stored form ("John Smith, kenshinkan" beside "John Smith, Kenshinkan", which
// only a hand edit writes) are still refused, as a CorruptFileError naming the
// file and both lines (operator decision D21), counted as the file counts
// them, a blank line included.
func TestLoadParticipants_DuplicateRowsAreNamedByTheirLines(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: helper.NewUUID4(), Name: "John Smith", Dojo: "Kenshinkan"},
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	path := filepath.Join(dir, "competitions", "c1", "participants.csv")
	raw, err := bctest.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	johnLine := -1
	for i, l := range lines {
		if strings.Contains(l, "John Smith") {
			johnLine = i + 1
		}
	}
	require.Positive(t, johnLine, "the fixture holds John Smith's row")
	dup := helper.NewUUID4() + ",john smith,kenshinkan"
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n\n"+dup+"\n"), 0o600))
	dupLine := len(lines) + 2

	fresh := freshLegacyUpgradeStore(t, dir)
	_, err = fresh.LoadParticipants("c1", false)
	require.Error(t, err)
	cf, ok := state.AsCorruptFile(err)
	require.True(t, ok, "the refusal is a located file error: %v", err)
	assert.Equal(t, "participants.csv", cf.File)
	assert.Equal(t, dupLine, cf.Line)
	assert.Equal(t, 1, cf.Column)
	assert.Contains(t, cf.Detail, fmt.Sprintf("lines %d and %d", johnLine, dupLine))
	assert.NotContains(t, cf.Detail, "John", "the detail names lines, never the competitor")
}

// Member names saved in another case are title-cased, and a lineup position
// naming that member by id follows; a position with no member id keeps the
// name as typed. The repair works from the difference, so a load that
// stopped after the members were written still fixes the lineup next time.
func TestLegacyTextCaseUpgrade_TeamMembersAndLineups(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]
	sato, err := s.AddTeamMember("c1", teamID, "Sato Ken")
	require.NoError(t, err)
	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "sato ken",
			domain.PositionNumbered(2): "ghost walker",
		},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): sato.ID},
		History: map[domain.Position][]domain.PositionChange{
			domain.PositionNumbered(2): {{Stamp: 1, Name: "sato ken", MemberID: sato.ID}, {Stamp: 2, Name: "old name"}},
		},
	}, 3))
	compDir := filepath.Join(dir, "competitions", "c1")
	rewriteFile(t, filepath.Join(compDir, "team-members.yaml"), "Sato Ken", "sato ken")

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	squads, err := fresh.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads[teamID], 1)
	assert.Equal(t, "Sato Ken", squads[teamID][0].Name)
	lineups, err := fresh.LoadTeamLineups("c1")
	require.NoError(t, err)
	l, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok)
	assert.Equal(t, "Sato Ken", l.Positions[domain.PositionNumbered(1)], "a position naming the member by id follows the member")
	assert.Equal(t, "ghost walker", l.Positions[domain.PositionNumbered(2)], "a position with no member id is left as stored")
	history := l.History[domain.PositionNumbered(2)]
	require.Len(t, history, 2)
	assert.Equal(t, "Sato Ken", history[0].Name, "a history entry naming the member by id follows the member")
	assert.Equal(t, "old name", history[1].Name, "a history entry with no member id is left as stored")

	// The crash case: the members are already canonical but the lineup was not
	// rewritten. The next load fixes the lineup from the difference.
	rewriteFile(t, filepath.Join(compDir, "lineups.yaml"), "Sato Ken", "SATO KEN")
	recovered := freshLegacyUpgradeStore(t, dir)
	recovered.EnsureLegacyUpgraded("c1")
	lineups, err = recovered.LoadTeamLineups("c1")
	require.NoError(t, err)
	l, ok = roundZeroLineup(lineups, teamID)
	require.True(t, ok)
	assert.Equal(t, "Sato Ken", l.Positions[domain.PositionNumbered(1)])

	before := stampFiles(t, dir, "team-members.yaml", "lineups.yaml")
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	assert.Equal(t, before, stampFiles(t, dir, "team-members.yaml", "lineups.yaml"), "a second load writes nothing")
}

// Two members of one team whose names differ only in case collapse to one
// spelling. Both are kept (a member is never removed by a repair) and the
// team is logged so the organiser can rename one.
func TestLegacyTextCaseUpgrade_MembersThatCollapseToOneSpellingAreBothKept(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]
	_, err := s.AddTeamMember("c1", teamID, "Sato Ken")
	require.NoError(t, err)
	_, err = s.AddTeamMember("c1", teamID, "Mori Jun")
	require.NoError(t, err)
	// An earlier release compared member names exactly, so it let a second
	// "sato ken" in beside "Sato Ken".
	rewriteFile(t, filepath.Join(dir, "competitions", "c1", "team-members.yaml"), "Mori Jun", "sato ken")

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	squads, err := fresh.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads[teamID], 2)
	assert.Equal(t, "Sato Ken", squads[teamID][0].Name)
	assert.Equal(t, "Sato Ken", squads[teamID][1].Name)
	assert.NotEqual(t, squads[teamID][0].ID, squads[teamID][1].ID)
}

// A roster whose text is already canonical is not rewritten for its layout
// alone: a zekken competition's row with no zekken column of its own, and
// spacing after the commas, are left as the file holds them.
func TestLegacyTextCaseUpgrade_CanonicalRosterInAnOlderLayoutIsNotRewritten(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", WithZekkenName: true}))
	rinID, yukiID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", DisplayName: "RINSATO", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", DisplayName: "YUKI", Dojo: "Tobukan"},
	}))
	path := filepath.Join(dir, "competitions", "c1", "participants.csv")
	older := rinID + ", Rin Sato, Seibukan\n" + yukiID + ", Yuki Tanaka, YUKI, Tobukan, checked_in\n"
	require.NoError(t, os.WriteFile(path, []byte(older), 0o600))
	before := stampFiles(t, dir, "participants.csv")

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	assert.Equal(t, before, stampFiles(t, dir, "participants.csv"), "canonical text in an older layout is left as stored")
	players, err := fresh.LoadParticipants("c1", true)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.Equal(t, "Seibukan", players[0].Dojo)
	assert.Equal(t, "YUKI", players[1].DisplayName)
	assert.True(t, players[1].CheckedIn)

	// The same file with one dojo in another case is rewritten whole.
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(older, "Tobukan", "tobukan", 1)), 0o600))
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	raw, err := bctest.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "tobukan", "a dojo in another case is rewritten")
	assert.Contains(t, string(raw), "Tobukan")
}

// A position whose member id names a member takes the member's stored name
// whatever spelling it held, accents included: the client matches a typed
// name to a member ignoring case and accents.
func TestLegacyTextCaseUpgrade_APositionNamingAMemberTakesItsName(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]
	jose, err := s.AddTeamMember("c1", teamID, "José Ruiz")
	require.NoError(t, err)
	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Jose Ruiz"},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): jose.ID},
		History: map[domain.Position][]domain.PositionChange{
			domain.PositionNumbered(1): {{Stamp: 1, Name: "jose ruiz", MemberID: jose.ID}},
		},
	}, 3))

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	lineups, err := fresh.LoadTeamLineups("c1")
	require.NoError(t, err)
	l, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok)
	assert.Equal(t, "José Ruiz", l.Positions[domain.PositionNumbered(1)])
	require.Len(t, l.History[domain.PositionNumbered(1)], 1)
	assert.Equal(t, "José Ruiz", l.History[domain.PositionNumbered(1)][0].Name)

	before := stampFiles(t, dir, "team-members.yaml", "lineups.yaml")
	again := freshLegacyUpgradeStore(t, dir)
	again.EnsureLegacyUpgraded("c1")
	assert.Equal(t, before, stampFiles(t, dir, "team-members.yaml", "lineups.yaml"), "a second load writes nothing")
}
