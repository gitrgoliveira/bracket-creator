package state

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// testRevealSecret is a fixed, well-formed reveal secret (32 bytes as hex).
var testRevealSecret = strings.Repeat("ab", revealSecretBytes)

func TestPublicNames_Values(t *testing.T) {
	for _, v := range []string{PublicNamesShown, PublicNamesPerson, PublicNamesCompetition, PublicNamesHidden} {
		assert.True(t, ValidPublicNames(v), "%q is a value", v)
	}
	// One kind of link at a time: there is no "both".
	for _, v := range []string{"both", "Person", " hidden", "none", "numbers"} {
		assert.False(t, ValidPublicNames(v), "%q is not a value", v)
		assert.False(t, PublicNamesHidesNames(v), "%q hides nothing", v)
		assert.False(t, PublicNamesHasLinks(v), "%q gives no link", v)
	}

	assert.False(t, PublicNamesHidesNames(PublicNamesShown))
	assert.True(t, PublicNamesHidesNames(PublicNamesPerson))
	assert.True(t, PublicNamesHidesNames(PublicNamesCompetition))
	assert.True(t, PublicNamesHidesNames(PublicNamesHidden))

	assert.False(t, PublicNamesHasLinks(PublicNamesShown))
	assert.True(t, PublicNamesHasLinks(PublicNamesPerson))
	assert.True(t, PublicNamesHasLinks(PublicNamesCompetition))
	assert.False(t, PublicNamesHasLinks(PublicNamesHidden))

	var nilComp *Competition
	assert.False(t, nilComp.HidesNames())
	assert.False(t, (&Competition{}).HidesNames())
	assert.True(t, (&Competition{PublicNames: PublicNamesHidden}).HidesNames())
}

func TestValidRevealUntil(t *testing.T) {
	assert.True(t, ValidRevealUntil(""))
	assert.True(t, ValidRevealUntil("2026-11-22T18:30"))
	for _, v := range []string{"22-11-2026T18:30", "2026-11-22 18:30", "2026-11-22", "2026-11-22T18:30:00", "2026-13-01T10:00", "soon"} {
		assert.False(t, ValidRevealUntil(v), "%q", v)
	}
}

func TestNewRevealSecret(t *testing.T) {
	a, err := NewRevealSecret()
	require.NoError(t, err)
	b, err := NewRevealSecret()
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9a-f]{64}$`, a)
	assert.NotEqual(t, a, b, "every secret is new")
	assert.NotEmpty(t, CompetitionRevealKey(&Competition{PublicNames: PublicNamesCompetition, RevealSecret: a}))
}

func hmacHex(secretHex, label string) string {
	secret, _ := hex.DecodeString(secretHex)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestCompetitionRevealKey(t *testing.T) {
	c := &Competition{PublicNames: PublicNamesCompetition, RevealSecret: testRevealSecret}
	key := CompetitionRevealKey(c)
	assert.Equal(t, hmacHex(testRevealSecret, "competition"), key)
	assert.Regexp(t, `^[0-9a-f]{64}$`, key)
	assert.Equal(t, key, CompetitionRevealKey(c), "derived, so the same every time")

	other := &Competition{PublicNames: PublicNamesCompetition, RevealSecret: strings.Repeat("cd", revealSecretBytes)}
	assert.NotEqual(t, key, CompetitionRevealKey(other), "another secret, another key")

	for name, comp := range map[string]*Competition{
		"nil":          nil,
		"names shown":  {PublicNames: PublicNamesShown, RevealSecret: testRevealSecret},
		"per person":   {PublicNames: PublicNamesPerson, RevealSecret: testRevealSecret},
		"hidden":       {PublicNames: PublicNamesHidden, RevealSecret: testRevealSecret},
		"no secret":    {PublicNames: PublicNamesCompetition},
		"not hex":      {PublicNames: PublicNamesCompetition, RevealSecret: strings.Repeat("zz", revealSecretBytes)},
		"short secret": {PublicNames: PublicNamesCompetition, RevealSecret: "abcd"},
		"long secret":  {PublicNames: PublicNamesCompetition, RevealSecret: testRevealSecret + "00"},
	} {
		assert.Empty(t, CompetitionRevealKey(comp), name)
	}
}

func TestCompetitorRevealKey(t *testing.T) {
	c := &Competition{PublicNames: PublicNamesPerson, RevealSecret: testRevealSecret}
	k1 := CompetitorRevealKey(c, "p-1")
	k2 := CompetitorRevealKey(c, "p-2")
	assert.Equal(t, hmacHex(testRevealSecret, "competitor:p-1"), k1)
	assert.NotEqual(t, k1, k2, "each competitor has their own key")
	assert.NotEqual(t, hmacHex(testRevealSecret, "competition"), k1)

	// A competitor whose id spells the competition label still gets a key
	// that is not the competition key.
	assert.NotEqual(t, hmacHex(testRevealSecret, "competition"), CompetitorRevealKey(c, "competition"))

	assert.Empty(t, CompetitorRevealKey(c, ""), "no id, no key")
	assert.Empty(t, CompetitorRevealKey(nil, "p-1"))
	for _, kind := range []string{PublicNamesShown, PublicNamesCompetition, PublicNamesHidden} {
		comp := &Competition{PublicNames: kind, RevealSecret: testRevealSecret}
		assert.Empty(t, CompetitorRevealKey(comp, "p-1"), "kind %q gives no per-person key", kind)
	}
	assert.Empty(t, CompetitorRevealKey(&Competition{PublicNames: PublicNamesPerson}, "p-1"), "no secret")
}

func TestRevealKeys_SameSecretSameLinksAfterSwitching(t *testing.T) {
	c := &Competition{PublicNames: PublicNamesPerson, RevealSecret: testRevealSecret}
	before := CompetitorRevealKey(c, "p-1")
	c.PublicNames = PublicNamesHidden
	assert.Empty(t, CompetitorRevealKey(c, "p-1"))
	c.PublicNames = PublicNamesPerson
	assert.Equal(t, before, CompetitorRevealKey(c, "p-1"))
}

func TestRevealExpiryAndOpen(t *testing.T) {
	loc := time.FixedZone("venue", 2*60*60)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		require.NoError(t, err)
		return v
	}
	oneDay := &Tournament{Date: "21-11-2026", DurationDays: 1, ClosingTime: "18:00"}
	twoDays := &Tournament{Date: "21-11-2026", DurationDays: 2, ClosingTime: "17:30"}
	noClosing := &Tournament{Date: "21-11-2026", DurationDays: 1}
	textClosing := &Tournament{Date: "21-11-2026", DurationDays: 1, ClosingTime: "about six"}
	zeroDays := &Tournament{Date: "21-11-2026", ClosingTime: "18:00"}
	undated := &Tournament{DurationDays: 1, ClosingTime: "18:00"}

	comp := func(kind, until string) *Competition {
		return &Competition{PublicNames: kind, RevealUntil: until, RevealSecret: testRevealSecret}
	}

	tests := []struct {
		name   string
		c      *Competition
		t      *Tournament
		expiry string // "" means none can be worked out
	}{
		{"closing time plus an hour", comp(PublicNamesCompetition, ""), oneDay, "2026-11-21 19:00"},
		{"last day of several", comp(PublicNamesPerson, ""), twoDays, "2026-11-22 18:30"},
		{"no closing time: 01:00 the next morning", comp(PublicNamesCompetition, ""), noClosing, "2026-11-22 01:00"},
		{"closing time that is not HH:MM counts as none", comp(PublicNamesCompetition, ""), textClosing, "2026-11-22 01:00"},
		{"duration not set counts as one day", comp(PublicNamesCompetition, ""), zeroDays, "2026-11-21 19:00"},
		{"organiser's date and time", comp(PublicNamesCompetition, "2026-11-25T09:15"), oneDay, "2026-11-25 09:15"},
		{"organiser's date and time without a tournament", comp(PublicNamesCompetition, "2026-11-25T09:15"), nil, "2026-11-25 09:15"},
		{"organiser's time that does not read", comp(PublicNamesCompetition, "25-11-2026 09:15"), oneDay, ""},
		{"no tournament", comp(PublicNamesCompetition, ""), nil, ""},
		{"undated tournament", comp(PublicNamesCompetition, ""), undated, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RevealExpiry(tc.c, tc.t, loc)
			if tc.expiry == "" {
				assert.False(t, ok)
				assert.False(t, RevealOpen(tc.c, tc.t, at("2026-11-21 10:00")), "no expiry, no reveal")
				return
			}
			require.True(t, ok)
			want := at(tc.expiry)
			assert.True(t, want.Equal(got), "expiry %s, want %s", got, want)
			assert.True(t, RevealOpen(tc.c, tc.t, want.Add(-time.Minute)), "open a minute before")
			assert.False(t, RevealOpen(tc.c, tc.t, want), "closed at the expiry")
			assert.False(t, RevealOpen(tc.c, tc.t, want.Add(time.Minute)), "closed after")
		})
	}

	_, ok := RevealExpiry(nil, oneDay, loc)
	assert.False(t, ok)
	assert.False(t, RevealOpen(nil, oneDay, at("2026-11-21 10:00")))
}

func TestRevealOpen_OnlyForAKindWithLinks(t *testing.T) {
	tourn := &Tournament{Date: "21-11-2026", DurationDays: 1, ClosingTime: "18:00"}
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{PublicNamesShown, PublicNamesHidden, "both"} {
		c := &Competition{PublicNames: kind, RevealSecret: testRevealSecret}
		assert.False(t, RevealOpen(c, tourn, now), "kind %q", kind)
	}
	for _, kind := range []string{PublicNamesPerson, PublicNamesCompetition} {
		c := &Competition{PublicNames: kind, RevealSecret: testRevealSecret}
		assert.True(t, RevealOpen(c, tourn, now), "kind %q", kind)
	}
}

func TestRevealOpen_NeedsAWellFormedSecret(t *testing.T) {
	tourn := &Tournament{Date: "21-11-2026", DurationDays: 1, ClosingTime: "18:00"}
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, secret := range []string{"", "not-hex", "abcd"} {
		c := &Competition{PublicNames: PublicNamesCompetition, RevealSecret: secret}
		assert.False(t, RevealOpen(c, tourn, now), "secret %q derives no key, so nothing is open", secret)
	}
}

func TestRevealOpen_ReadsTheExpiryInNowsTimeZone(t *testing.T) {
	c := &Competition{PublicNames: PublicNamesCompetition, RevealUntil: "2026-11-21T12:00", RevealSecret: testRevealSecret}
	east := time.FixedZone("east", 3*60*60)
	// 11:30 in east is before 12:00 in east (08:30 UTC).
	assert.True(t, RevealOpen(c, nil, time.Date(2026, 11, 21, 11, 30, 0, 0, east)))
	// 12:30 in east is past 12:00 in east, although 09:30 UTC.
	assert.False(t, RevealOpen(c, nil, time.Date(2026, 11, 21, 12, 30, 0, 0, east)))
	// 12:30 read in UTC is past 12:00 read in UTC.
	assert.False(t, RevealOpen(c, nil, time.Date(2026, 11, 21, 12, 30, 0, 0, time.UTC)))
	// Moving the expiry later makes the link work again.
	c.RevealUntil = "2026-11-21T13:00"
	assert.True(t, RevealOpen(c, nil, time.Date(2026, 11, 21, 12, 30, 0, 0, time.UTC)))
}

func TestPublicNames_StoredButSecretNeverOnTheWire(t *testing.T) {
	c := &Competition{
		ID:           "c1",
		PublicNames:  PublicNamesPerson,
		RevealUntil:  "2026-11-25T09:15",
		RevealSecret: testRevealSecret,
	}

	j, err := json.Marshal(c)
	require.NoError(t, err)
	assert.Contains(t, string(j), `"publicNames":"person"`)
	assert.Contains(t, string(j), `"revealUntil":"2026-11-25T09:15"`)
	assert.NotContains(t, string(j), testRevealSecret)
	assert.NotContains(t, strings.ToLower(string(j)), "secret")

	// A client cannot set it either.
	var in Competition
	require.NoError(t, json.Unmarshal([]byte(`{"revealSecret":"`+testRevealSecret+`","RevealSecret":"`+testRevealSecret+`"}`), &in))
	assert.Empty(t, in.RevealSecret)

	y, err := yaml.Marshal(c)
	require.NoError(t, err)
	assert.Contains(t, string(y), "public_names: person")
	assert.Contains(t, string(y), "reveal_until: 2026-11-25T09:15")
	assert.Contains(t, string(y), "reveal_secret: "+testRevealSecret)

	// Absent keys: names shown, the end of the tournament, no secret.
	y, err = yaml.Marshal(&Competition{ID: "c2"})
	require.NoError(t, err)
	for _, key := range []string{"public_names", "reveal_until", "reveal_secret"} {
		assert.NotContains(t, string(y), key)
	}
}

func TestPublicNames_SurviveSaveAndLoad(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	c := &Competition{
		ID:           "comp-hide",
		Name:         "Juniors",
		Kind:         "individual",
		Format:       CompFormatKnockout,
		PublicNames:  PublicNamesCompetition,
		RevealUntil:  "2026-11-25T09:15",
		RevealSecret: testRevealSecret,
	}
	require.NoError(t, store.SaveCompetition(c))
	loaded, err := store.LoadCompetition("comp-hide")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, PublicNamesCompetition, loaded.PublicNames)
	assert.Equal(t, "2026-11-25T09:15", loaded.RevealUntil)
	assert.Equal(t, testRevealSecret, loaded.RevealSecret)
	assert.Equal(t, CompetitionRevealKey(c), CompetitionRevealKey(loaded))

	tourn := &Tournament{Name: "Open", Date: "21-11-2026", DurationDays: 1, DefaultPublicNames: PublicNamesHidden}
	require.NoError(t, store.SaveTournament(tourn))
	lt, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, lt)
	assert.Equal(t, PublicNamesHidden, lt.DefaultPublicNames)

	j, err := json.Marshal(lt)
	require.NoError(t, err)
	assert.Contains(t, string(j), `"defaultPublicNames":"hidden"`)
}

func TestConfigVersion_PublicNamesFields(t *testing.T) {
	base := func() *Competition {
		return &Competition{ID: "v", Name: "V", Format: CompFormatKnockout, Kind: "individual"}
	}
	baseVer := ConfigVersion(base())

	for name, mutate := range map[string]func(*Competition){
		"PublicNames": func(c *Competition) { c.PublicNames = PublicNamesHidden },
		"RevealUntil": func(c *Competition) { c.RevealUntil = "2026-11-25T09:15" },
	} {
		c := base()
		mutate(c)
		assert.NotEqual(t, baseVer, ConfigVersion(c), "%s is a setting", name)
	}

	c := base()
	c.RevealSecret = testRevealSecret
	assert.Equal(t, baseVer, ConfigVersion(c), "the secret is server-managed, not a setting")
}

// A record that never set the public-names fields hashes as it did before
// they existed, so an upgrade made mid-event refuses no save opened before it.
func TestVersions_UnsetPublicNamesFieldsAreNotHashed(t *testing.T) {
	comp, err := json.Marshal(configVersionPayload{})
	require.NoError(t, err)
	assert.NotContains(t, string(comp), "publicNames")
	assert.NotContains(t, string(comp), "revealUntil")
	tourn, err := json.Marshal(tournamentVersionPayload{})
	require.NoError(t, err)
	assert.NotContains(t, string(tourn), "defaultPublicNames")
}
