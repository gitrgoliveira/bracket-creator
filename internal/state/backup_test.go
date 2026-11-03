package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_CapturesEveryFileInTournamentAndCompetitionScope(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{
		Name:         "All-Japan Kendo Taikai",
		Date:         "03-11-2026",
		Venue:        "Nippon Budokan",
		Courts:       []string{"A", "B"},
		Password:     "top-secret-password",
		DurationDays: 2,
		Mode:         TournamentModeOfficiated,
	}))

	// Populate branding/ and sponsors/ under the data root.
	logoBytes := []byte("PNG-LOGO-BYTES")
	sponsorBytes := []byte("PNG-SPONSOR-BYTES")
	require.NoError(t, store.root.stageFile(filepath.Join(dir, BrandingDirName, "logo.png"), logoBytes, 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, SponsorsDirName, "s1.png"), sponsorBytes, 0o600))

	const compA = "men-individual"
	const compB = "women-team"

	require.NoError(t, store.SaveCompetition(&Competition{
		ID:       compA,
		Name:     "Men Individual",
		Date:     "03-11-2026",
		Status:   CompStatusPools,
		Kind:     "team",
		TeamSize: 3,
	}))
	require.NoError(t, store.SaveCompetition(&Competition{
		ID:     compB,
		Name:   "Women Team",
		Date:   "04-11-2026",
		Status: CompStatusSetup,
	}))

	players := []domain.Player{
		{ID: "p1", Name: "Alice", Dojo: "DojoA"},
		{ID: "p2", Name: "Bob", Dojo: "DojoB"},
	}
	require.NoError(t, store.SaveParticipants(compA, players))
	require.NoError(t, store.SaveSeeds(compA, []domain.SeedAssignment{
		{ID: "p1", Name: "Alice", Dojo: "DojoA", SeedRank: 1},
	}))
	require.NoError(t, store.SavePools(compA, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))
	require.NoError(t, store.SavePoolMatches(compA, []MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2", Status: MatchStatusScheduled, Court: "A"},
	}))
	require.NoError(t, store.SaveBracket(compA, &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "r1-0", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2", Status: MatchStatusScheduled, Court: "A"}},
		},
	}))
	require.NoError(t, store.SetCompetitorStatus(compA, domain.CompetitorStatus{
		PlayerID: "p2",
		Eligible: false,
		Reason:   "kiken",
		MatchID:  "Pool A-0",
	}))
	require.NoError(t, store.WithTransaction(compA, func(tx StoreTx) error {
		return tx.SetTeamLineup(compA, domain.TeamLineup{
			TeamID:        "p1",
			Round:         0,
			CompetitionID: compA,
			Positions: map[domain.Position]string{
				domain.PosSenpo:  "M1",
				domain.PosChuken: "M2",
				domain.PosTaisho: "M3",
			},
		}, 5)
	}))
	teamMembersBytes := []byte("members:\n  p1:\n    - id: tm1\n      index: 1\n      name: M1\n")
	require.NoError(t, store.root.writeFile(store.compPath(compA, teamMembersFilename), teamMembersBytes, 0o600))
	require.NoError(t, store.SaveRankOverride(compA, "Pool A", "p1", 1))
	require.NoError(t, store.AppendMatchHistory(compA, MatchHistoryEntry{
		MatchID:  "Pool A-0",
		Door:     "score",
		Stamp:    100,
		Changed:  []string{"score"},
		Outcomes: map[string]string{"score": HistoryOutcomeApplied},
	}))
	require.NoError(t, store.AppendScoreLog(compA, "Pool A-0", ScoreLogLine{
		V: ScoreLogFormatVersion,
		Event: &domain.ScoreEvent{
			ID:      "ev-1",
			Session: "sess-1",
			Seq:     1,
			Stamp:   100,
			Side:    "a",
			Kind:    domain.ScoreIpponAdd,
			Letter:  "M",
		},
	}))

	quarantinedBytes := []byte("{corrupt bracket payload")
	unknownBytes := []byte("custom operator note")
	require.NoError(t, store.root.writeFile(store.compPath(compA, "bracket.json.corrupt-20261103-100000"), quarantinedBytes, 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compA, "extra", "notes.txt"), unknownBytes, 0o600))

	expectedCompAFiles := []string{
		"competitions/" + compA + "/config.md",
		"competitions/" + compA + "/participants.csv",
		"competitions/" + compA + "/seeds.csv",
		"competitions/" + compA + "/pools.csv",
		"competitions/" + compA + "/pool-matches.csv",
		"competitions/" + compA + "/bracket.json",
		"competitions/" + compA + "/competitor-status.yaml",
		"competitions/" + compA + "/lineups.yaml",
		"competitions/" + compA + "/team-members.yaml",
		"competitions/" + compA + "/overrides.json",
		"competitions/" + compA + "/history/Pool%20A-0.jsonl",
		"competitions/" + compA + "/events/Pool%20A-0.jsonl",
		"competitions/" + compA + "/bracket.json.corrupt-20261103-100000",
		"competitions/" + compA + "/extra/notes.txt",
	}

	// 1. Competition scope captures only compA's files.
	compSnap, err := store.Snapshot(ScopeCompetition, compA)
	require.NoError(t, err)
	assert.Equal(t, ScopeCompetition, compSnap.Scope)
	assert.Equal(t, map[string]string{compA: "Men Individual"}, compSnap.Competitions)
	assert.NotContains(t, compSnap.Files, "tournament.md")
	assert.NotContains(t, compSnap.Files, "branding/logo.png")
	assert.NotContains(t, compSnap.Files, "sponsors/s1.png")
	assert.NotContains(t, compSnap.Files, "competitions/"+compB+"/config.md")
	for _, rel := range expectedCompAFiles {
		got, ok := compSnap.Files[rel]
		require.Truef(t, ok, "competition snapshot missing %s", rel)
		onDisk, rerr := store.root.readFile(filepath.Join(dir, filepath.FromSlash(rel)))
		require.NoError(t, rerr)
		assert.Equal(t, onDisk, got, "mismatch for %s", rel)
	}

	// 2. Tournament scope captures tournament.md, branding/, sponsors/, and every competition.
	tournSnap, err := store.Snapshot(ScopeTournament, "")
	require.NoError(t, err)
	assert.Equal(t, ScopeTournament, tournSnap.Scope)
	assert.Equal(t, "All-Japan Kendo Taikai", tournSnap.TournamentName)
	assert.Equal(t, map[string]string{
		compA: "Men Individual",
		compB: "Women Team",
	}, tournSnap.Competitions)
	assert.Contains(t, tournSnap.Files, "tournament.md")
	assert.Equal(t, logoBytes, tournSnap.Files["branding/logo.png"])
	assert.Equal(t, sponsorBytes, tournSnap.Files["sponsors/s1.png"])
	assert.Contains(t, tournSnap.Files, "competitions/"+compB+"/config.md")
	for _, rel := range expectedCompAFiles {
		assert.Containsf(t, tournSnap.Files, rel, "tournament snapshot missing %s", rel)
	}
}

func TestSnapshot_ExcludesWALBackupsRestoreAndTempFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{
		Name:   "Exclusion Check",
		Date:   "03-11-2026",
		Venue:  "Hall",
		Courts: []string{"A"},
	}))
	const compID = "kendo-open"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kendo Open"}))

	// Populate excluded directories and temp files across the tree.
	require.NoError(t, store.root.stageFile(filepath.Join(dir, ".wal", "uncommitted.tmp"), []byte("x"), 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, BackupsDirName, "20261103T120000Z-scheduled.zip"), []byte("zip"), 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, RestoreDirName, "tok-1", "journal.json"), []byte("{}"), 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, BrandingDirName, "keep.png"), []byte("png"), 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, BrandingDirName, "upload.tmp"), []byte("tmp"), 0o600))
	require.NoError(t, store.root.stageFile(filepath.Join(dir, SponsorsDirName, "s.png.tmp-123-456"), []byte("tmp"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, "config.md.tmp-999-111"), []byte("tmp"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, "scratch.tmp"), []byte("tmp"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, matchHistoryDir, "m1.jsonl.tmp"), []byte("tmp"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, BackupsDirName, "nested.zip"), []byte("zip"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, RestoreDirName, "nested"), []byte("x"), 0o600))
	require.NoError(t, store.root.stageFile(store.compPath(compID, ".wal", "nested.json"), []byte("{}"), 0o600))

	for _, scope := range []SnapshotScope{ScopeTournament, ScopeCompetition} {
		snap, serr := store.Snapshot(scope, compID)
		require.NoError(t, serr)
		for path := range snap.Files {
			assert.NotContains(t, path, ".wal", "scope %s captured .wal path %s", scope, path)
			assert.NotContains(t, path, BackupsDirName+"/", "scope %s captured backups path %s", scope, path)
			assert.NotContains(t, path, RestoreDirName+"/", "scope %s captured .restore path %s", scope, path)
			assert.NotContains(t, path, ".tmp", "scope %s captured temp file %s", scope, path)
		}
	}
}

func TestSnapshot_StripsPasswordFromTournamentFrontMatter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const secret = "s3cr3t-admin-pass-phrase!"
	require.NoError(t, store.SaveTournament(&Tournament{
		Name:         "Winter Taikai",
		Date:         "15-12-2026",
		Venue:        "Kyoto Butokuden",
		Courts:       []string{"A", "B", "C"},
		Password:     secret,
		DurationDays: 1,
		Mode:         TournamentModeSelfRun,
		PublicURL:    "https://example.org/taikai",
	}))

	// Verify on-disk tournament.md actually contains the password before snapshotting.
	rawOnDisk, err := store.root.readFile(filepath.Join(dir, "tournament.md"))
	require.NoError(t, err)
	require.Contains(t, string(rawOnDisk), secret)

	snap, err := store.Snapshot(ScopeTournament, "")
	require.NoError(t, err)

	captured, ok := snap.Files["tournament.md"]
	require.True(t, ok, "tournament.md must be present in tournament snapshot")
	assert.NotContains(t, string(captured), secret, "captured tournament.md must never contain the password value")
	assert.NotContains(t, string(captured), "password", "captured tournament.md must not carry the password key")
	require.NoError(t, CheckFrontMatter(captured), "captured tournament.md must parse with CheckFrontMatter")

	var parsed Tournament
	require.NoError(t, parseFrontMatter(captured, &parsed))
	assert.Empty(t, parsed.Password)
	assert.Equal(t, "Winter Taikai", parsed.Name)
	assert.Equal(t, "15-12-2026", parsed.Date)
	assert.Equal(t, "Kyoto Butokuden", parsed.Venue)
	assert.Equal(t, []string{"A", "B", "C"}, parsed.Courts)
	assert.Equal(t, TournamentModeSelfRun, parsed.Mode)
	assert.Equal(t, "https://example.org/taikai", parsed.PublicURL)
}

func TestSnapshot_UnknownCompetitionReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.Snapshot(ScopeCompetition, "does-not-exist")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCompetitionNotFound), "expected ErrCompetitionNotFound, got: %v", err)
	assert.True(t, errors.Is(err, os.ErrNotExist), "expected os.ErrNotExist, got: %v", err)

	_, err = store.Snapshot(ScopeCompetition, "../escape")
	require.Error(t, err)

	_, err = store.Snapshot("bogus-scope", "")
	require.Error(t, err)
}

func TestCheckFrontMatter(t *testing.T) {
	validTourn, err := writeFrontMatter(&Tournament{
		Name:   "Valid Tournament",
		Date:   "03-11-2026",
		Venue:  "Arena",
		Courts: []string{"A"},
	})
	require.NoError(t, err)
	assert.NoError(t, CheckFrontMatter(validTourn))

	validComp, err := writeFrontMatter(&Competition{
		ID:     "c1",
		Name:   "Valid Competition",
		Date:   "03-11-2026",
		Status: CompStatusSetup,
	})
	require.NoError(t, err)
	assert.NoError(t, CheckFrontMatter(validComp))

	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"empty input", nil},
		{"no front matter fence", []byte("name: Test\n")},
		{"unclosed fence", []byte("---\nname: Test\n")},
		{"empty front matter", []byte("---\n---\n")},
		{"non-mapping scalar body", []byte("---\njust-a-string\n---\n")},
		{"broken yaml", []byte("---\nname: [unclosed\n---\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, CheckFrontMatter(tc.raw))
		})
	}
}

func TestDataRootBackupHelpers(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	backupsDir := filepath.Join(dir, BackupsDirName)
	stagedPath := filepath.Join(backupsDir, "staged.zip")
	renamedPath := filepath.Join(backupsDir, "final.zip")

	require.NoError(t, store.root.stageFile(stagedPath, []byte("initial-bytes"), 0o600))
	require.NoError(t, store.root.atomicWrite(stagedPath, []byte("overwritten-bytes"), 0o600))
	require.NoError(t, store.root.syncDir(backupsDir))
	require.NoError(t, store.root.rename(stagedPath, renamedPath))

	seen := make(map[string][]byte)
	require.NoError(t, store.root.walkDir(backupsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, rerr := store.root.readFile(path)
		if rerr != nil {
			return rerr
		}
		seen[path] = data
		return nil
	}))

	assert.NotContains(t, seen, stagedPath)
	assert.Equal(t, []byte("overwritten-bytes"), seen[renamedPath])
}
