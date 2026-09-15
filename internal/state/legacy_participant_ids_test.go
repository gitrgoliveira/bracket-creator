package state_test

// legacy_participant_ids_test.go pins the participants.csv storage-format
// repair, and above all the question it turns on.
//
// A roster with no id column and a roster carrying non-UUID ids whose
// has_participant_ids flag never landed are BYTE-IDENTICAL on disk. Both
// parse as "no ids", and the second parses SHIFTED: the id read as the name,
// the name read as the dojo. Minting over that shift writes it back
// permanently and destroys the id every other record points at -- which is
// what happened the last time a read-side roster rewrite was attempted here.
//
// So the repair asks the competition's OTHER files what column 0 is, and
// acts only on an answer:
//
//	ids referenced elsewhere match column 0        -> column 0 is an id
//	names referenced elsewhere sit in column 1     -> column 0 is an id
//	names referenced elsewhere sit in column 0     -> column 0 is a name
//	nothing on disk can say                        -> leave the file alone

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyRosterFlagRepairedRatherThanMintedOver is Option 1's whole
// point: where the draw proves column 0 is an id, the file was never wrong
// and only the flag was. Nothing is minted, and the original id -- which
// pools.csv, the match rows and every other id-keyed record already point
// at -- survives.
func TestLegacyRosterFlagRepairedRatherThanMintedOver(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	legacy := "race-p1,Aaron Adams,Team Alpha\n"
	require.NoError(t, os.WriteFile(rosterPath, []byte(legacy), 0o600))
	// The draw carries the id in column 8 and the name in column 2, which is
	// what proves column 0 of the roster is an id and not a name.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "pools.csv"),
		[]byte("Pool A,Aaron Adams,0,,Team Alpha,,,race-p1\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "race-p1", players[0].ID, "the stored id is kept, never regenerated")
	assert.Equal(t, "Aaron Adams", players[0].Name, "and the shifted parse is gone")
	assert.Equal(t, "Team Alpha", players[0].Dojo)

	raw, err := os.ReadFile(rosterPath)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(raw), "the roster bytes were never wrong, so they are not rewritten")

	comp, err := fresh.LoadCompetition("c1")
	require.NoError(t, err)
	assert.True(t, comp.HasParticipantIDs, "the flag is what gets repaired")
}

// TestLegacyRosterLeftAloneWhenNothingCanProveTheColumn is the other half,
// and the one that keeps the corruption impossible. Same roster as above,
// with no draw to appeal to: the file could equally be a competitor named
// "race-p1" from a dojo called "Aaron Adams", so nothing is written.
func TestLegacyRosterLeftAloneWhenNothingCanProveTheColumn(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	legacy := "race-p1,Aaron Adams,Team Alpha\n"
	require.NoError(t, os.WriteFile(rosterPath, []byte(legacy), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	raw, err := os.ReadFile(rosterPath)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(raw),
		"an unprovable roster is never rewritten: minting here would bake in the shifted parse")

	comp, err := fresh.LoadCompetition("c1")
	require.NoError(t, err)
	assert.False(t, comp.HasParticipantIDs, "and nothing is asserted about a column nothing could identify")
}

// TestLegacyRosterMintedWhenTheDrawNamesTheCompetitors: the ordinary
// pre-id roster. The draw names these people and those names sit in column
// 0, so the rows genuinely have no ids and get them.
func TestLegacyRosterMintedWhenTheDrawNamesTheCompetitors(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	require.NoError(t, os.WriteFile(rosterPath, []byte("Rin Sato,Seibukan\nYuki Tanaka,Tobukan\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "pools.csv"),
		[]byte("Pool A,Rin Sato,0,,Seibukan,,\nPool A,Yuki Tanaka,1,,Tobukan,,\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	for _, p := range players {
		assert.NotEmpty(t, p.ID, "%s gets an id", p.Name)
	}
	assert.Equal(t, "Rin Sato", players[0].Name, "with no column shift")
	assert.Equal(t, "Seibukan", players[0].Dojo)

	raw, err := os.ReadFile(rosterPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), players[0].ID, "and the ids are persisted")
}

// TestLegacyRosterProvedByTheBracket: a knockout-only competition has no
// pools.csv and no pool-matches.csv, so bracket.json is the only file that
// can answer. This pins that it is actually consulted -- the population it
// serves (a legacy knockout competition) would otherwise fall silently into
// the unprovable class and never be repaired.
func TestLegacyRosterProvedByTheBracket(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	require.NoError(t, os.WriteFile(rosterPath, []byte("Rin Sato,Seibukan\nYuki Tanaka,Tobukan\n"), 0o600))
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m1", SideA: "Rin Sato", SideB: "Yuki Tanaka", Winner: "Rin Sato", Status: state.MatchStatusCompleted},
		}},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	for _, p := range players {
		assert.NotEmpty(t, p.ID, "%s: the bracket names these competitors, which proves column 0 is a name", p.Name)
	}
	assert.Equal(t, "Rin Sato", players[0].Name, "with no column shift")
	assert.Equal(t, "Seibukan", players[0].Dojo)

	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.Len(t, bracket.Rounds, 1)
	require.Len(t, bracket.Rounds[0], 1)
	byName := map[string]string{players[0].Name: players[0].ID, players[1].Name: players[1].ID}
	assert.Equal(t, byName["Rin Sato"], bracket.Rounds[0][0].SideAID, "and the bracket row is stamped in the same pass")
	assert.Equal(t, byName["Yuki Tanaka"], bracket.Rounds[0][0].SideBID)
}

// TestLegacyRosterLeftAloneWhenTheRowShapeIsNotCorroborated pins the second
// legacy ambiguity, which column 0 says nothing about.
//
// The ORIGINAL writer emitted "Name, DisplayName, Dojo" whenever the display
// name differed from the name, which SanitizeName makes true for nearly
// every competitor, and "Name, Dojo" otherwise -- both shapes in one file. A
// NON-zekken parser reads three fields as [Name, Dojo, Metadata], so such a
// row loads with the DISPLAY NAME as the dojo. Reading it wrong is
// survivable; rewriting it from that parse is not, because the file then
// encodes the wrong dojo and the truth is gone.
func TestLegacyRosterLeftAloneWhenTheRowShapeIsNotCorroborated(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	legacy := "Rin Sato, R. SATO, Seibukan\nYuki Tanaka, Tobukan\n"
	require.NoError(t, os.WriteFile(rosterPath, []byte(legacy), 0o600))
	// The draw names both competitors, so column 0 IS provably a name -- the
	// mint path is reached. The draw also records Rin Sato's dojo as
	// Seibukan, which contradicts the parse's "R. SATO" and is what stops it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "pools.csv"),
		[]byte("Pool A,Rin Sato,0,,Seibukan,,\nPool A,Yuki Tanaka,1,,Tobukan,,\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	raw, err := os.ReadFile(rosterPath)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(raw),
		"rewriting this row would persist the display name as the dojo, so the file is left alone")
}

// TestLegacyRosterMintedWhenTheDrawCorroboratesTheDojo is the same shape
// reaching the opposite verdict: a two-field roster has no shape to mistake,
// and the draw agrees with the dojo it parsed, so the ids are assigned.
func TestLegacyRosterMintedWhenTheDrawCorroboratesTheDojo(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	rosterPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	require.NoError(t, os.WriteFile(rosterPath, []byte("Rin Sato, Seibukan\nYuki Tanaka, Tobukan\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "pools.csv"),
		[]byte("Pool A,Rin Sato,0,,Seibukan,,\nPool A,Yuki Tanaka,1,,Tobukan,,\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	for _, p := range players {
		assert.NotEmpty(t, p.ID)
		assert.NotEqual(t, p.DisplayName, p.Dojo, "%s: the dojo is a dojo, not a display name", p.Name)
	}
	assert.Equal(t, "Seibukan", players[0].Dojo)
}

// TestLegacyTeamRosterMintedAndSquadsMigratedInOnePass is where the two
// legacy repairs meet, and the population the squad work exists for.
//
// A team's members live in Player.Metadata, so a team roster row is WIDE
// (name, dojo, then one field per member) in a competition with no zekken
// column -- the same shape the display-name check refuses when the draw
// contradicts it. Here the draw agrees, the parse is right, and both
// repairs must land in the SAME pass: ids first, then the squads keyed
// under them. Before the roster repair existed, the squad migration had no
// id to key on and silently did nothing.
func TestLegacyTeamRosterMintedAndSquadsMigratedInOnePass(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", Kind: "team", TeamSize: 3}))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "participants.csv"),
		[]byte("Tora, Tora Dojo, Sato, Kimura, Abe\nKaze, Kaze Dojo, Mori, Oda, Ito\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "pools.csv"),
		[]byte("Pool A,Tora,0,,Tora Dojo,,\nPool A,Kaze,1,,Kaze Dojo,,\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	byName := map[string]string{}
	for _, p := range players {
		require.NotEmpty(t, p.ID, "%s is identified", p.Name)
		byName[p.Name] = p.ID
	}
	assert.Equal(t, "Tora Dojo", players[0].Dojo, "a team row is wide because of its MEMBERS, and the draw corroborates the dojo")
	assert.Equal(t, []string{"Sato", "Kimura", "Abe"}, players[0].Metadata, "the members survive the rewrite")

	squads, err := fresh.LoadSquads("c1")
	require.NoError(t, err)
	assert.Equal(t, []string{"Sato", "Kimura", "Abe", "", ""}, squadNames(squads[byName["Tora"]]),
		"and the squad is migrated under the id the same pass just minted, padded to the floor (TeamSize 3 + 2 reserves)")
	assert.Equal(t, []string{"Mori", "Oda", "Ito", "", ""}, squadNames(squads[byName["Kaze"]]))
}
