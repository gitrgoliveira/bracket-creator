package mobileapp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	pnAlice = "11111111-1111-4111-8111-111111111111"
	pnBob   = "22222222-2222-4222-8222-222222222222"
	pnCarol = "33333333-3333-4333-8333-333333333333"
)

// pnFixture is an officiated tournament with one individual mixed competition
// in its pool phase, numbered K1..K3 by its pools.csv.
type pnFixture struct {
	store *state.Store
	pn    *publicNames
	comp  *state.Competition
	now   time.Time
}

func newPNFixture(t *testing.T, publicNames string) pnFixture {
	t.Helper()
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Date: "01-06-2030", Password: "secret-pw", Courts: []string{"A"},
		Mode: state.TournamentModeOfficiated,
	}))
	secret, err := state.NewRevealSecret()
	require.NoError(t, err)
	comp := &state.Competition{
		ID: "juniors", Name: "Juniors", Kind: "individual", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, NumberPrefix: "K", PoolSize: 3, PoolWinners: 1,
		PublicNames: publicNames, RevealSecret: secret,
	}
	require.NoError(t, store.SaveCompetition(comp))
	players := []domain.Player{
		{ID: pnAlice, Name: "Alice Arai", DisplayName: "ARAI", Dojo: "Dojo One"},
		{ID: pnBob, Name: "Bob Baba", DisplayName: "BABA", Dojo: "Dojo Two"},
		{ID: pnCarol, Name: "Carol Chiba", DisplayName: "CHIBA", Dojo: "Dojo Three"},
	}
	require.NoError(t, store.SaveParticipants(comp.ID, players))
	numbered := append([]domain.Player(nil), players...)
	for i := range numbered {
		numbered[i].Number = "K" + string(rune('1'+i))
	}
	require.NoError(t, store.SavePools(comp.ID, []helper.Pool{{PoolName: "A", Players: numbered}}))

	pn := newPublicNames(store, NewFileVerifier(store))
	now := time.Date(2030, 6, 1, 12, 0, 0, 0, time.Local)
	pn.now = func() time.Time { return now }
	loaded, err := store.LoadCompetition(comp.ID)
	require.NoError(t, err)
	return pnFixture{store: store, pn: pn, comp: loaded, now: now}
}

func judgeRequest(t *testing.T, pn *publicNames, headers map[string]string) (audience, bool, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/viewer/competitions/juniors", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	aud, ok := pn.judge(c)
	return aud, ok, w
}

func TestAudienceKeys(t *testing.T) {
	assert.Equal(t, "public", audience{}.key())
	assert.Equal(t, "organiser", organiserAudience().key())

	a := audience{
		competitions: map[string]bool{"z-comp": true, "a-comp": true},
		competitors:  map[string]map[string]bool{"m-comp": {pnBob: true, pnAlice: true}},
	}
	assert.Equal(t, "reveal|c:a-comp,c:z-comp,p:m-comp="+pnAlice+"+"+pnBob, a.key())
	assert.True(t, a.revealsAll("a-comp"))
	assert.False(t, a.revealsAll("m-comp"))
	assert.True(t, a.revealedCompetitors("m-comp")[pnAlice])

	onlyPersons := audience{competitors: map[string]map[string]bool{"m": {pnAlice: true}}}
	assert.Equal(t, "reveal|p:m="+pnAlice, onlyPersons.key())
}

func TestParseRevealKeys(t *testing.T) {
	k := strings.Repeat("ab", 32)
	got := parseRevealKeys(" juniors:" + k + ", bad id:" + k + ",juniors:" + strings.ToUpper(k) +
		",juniors:short,nocolon," + "juniors:" + k + ",seniors:" + k)
	assert.Equal(t, []revealKeyEntry{{"juniors", k}, {"seniors", k}}, got)

	var many []string
	for i := 0; i < maxRevealKeys+5; i++ {
		many = append(many, "c"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+string(rune('a'+i/26))+":"+k)
	}
	assert.Len(t, parseRevealKeys(strings.Join(many, ",")), maxRevealKeys)
	assert.Nil(t, parseRevealKeys(strings.Repeat("x", maxRevealHeaderBytes+1)))
	assert.Nil(t, parseRevealKeys(""))
}

func TestJudge_PasswordAndPublic(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesCompetition)

	aud, ok, w := judgeRequest(t, f.pn, nil)
	require.True(t, ok)
	assert.Equal(t, "public", aud.key())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.Contains(t, w.Header().Get("Vary"), revealKeyHeader)

	aud, ok, _ = judgeRequest(t, f.pn, map[string]string{"X-Tournament-Password": "secret-pw"})
	require.True(t, ok)
	assert.True(t, aud.isOrganiser())

	// A password sent but wrong is 401, never the masked view, even with a
	// good reveal key beside it.
	_, ok, w = judgeRequest(t, f.pn, map[string]string{
		"X-Tournament-Password": "wrong",
		revealKeyHeader:         "juniors:" + state.CompetitionRevealKey(f.comp),
	})
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"error":"invalid admin password"}`, w.Body.String())
}

type errVerifier struct{ PasswordVerifier }

func (errVerifier) Verify(string) (bool, error) { return false, errors.New("boom") }

func TestJudge_VerifierError500(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	f.pn.verifier = errVerifier{NewFileVerifier(f.store)}
	_, ok, w := judgeRequest(t, f.pn, map[string]string{"X-Tournament-Password": "x"})
	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestJudge_CompetitionKey(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesCompetition)
	key := state.CompetitionRevealKey(f.comp)
	require.NotEmpty(t, key)
	hdr := map[string]string{revealKeyHeader: "juniors:" + key}

	aud, ok, _ := judgeRequest(t, f.pn, hdr)
	require.True(t, ok)
	assert.True(t, aud.revealsAll("juniors"))

	// A wrong key, or a key for another competition id, reveals nothing.
	aud, _, _ = judgeRequest(t, f.pn, map[string]string{revealKeyHeader: "juniors:" + strings.Repeat("0", 64)})
	assert.Equal(t, "public", aud.key())
	aud, _, _ = judgeRequest(t, f.pn, map[string]string{revealKeyHeader: "seniors:" + key})
	assert.Equal(t, "public", aud.key())

	// Past the expiry (the day after the tournament, 01:00, with no
	// closing time) the same key reveals nothing; moving the expiry later
	// makes it work again (E3).
	f.pn.now = func() time.Time { return time.Date(2030, 6, 2, 1, 0, 0, 0, time.Local) }
	aud, _, _ = judgeRequest(t, f.pn, hdr)
	assert.Equal(t, "public", aud.key())
	f.comp.RevealUntil = "2030-06-03T00:00"
	require.NoError(t, f.store.SaveCompetition(f.comp))
	aud, _, _ = judgeRequest(t, f.pn, hdr)
	assert.True(t, aud.revealsAll("juniors"))

	// Switching to per-person links (wrong kind), to no links, or to names
	// shown drops the competition key.
	for _, kind := range []string{state.PublicNamesPerson, state.PublicNamesHidden, state.PublicNamesShown} {
		f.comp.PublicNames = kind
		require.NoError(t, f.store.SaveCompetition(f.comp))
		aud, _, _ = judgeRequest(t, f.pn, hdr)
		assert.Equal(t, "public", aud.key(), kind)
	}
}

func TestJudge_CompetitorKey(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesPerson)
	key := state.CompetitorRevealKey(f.comp, pnBob)
	require.NotEmpty(t, key)

	aud, ok, _ := judgeRequest(t, f.pn, map[string]string{revealKeyHeader: "juniors:" + key})
	require.True(t, ok)
	assert.False(t, aud.revealsAll("juniors"))
	assert.Equal(t, map[string]bool{pnBob: true}, aud.revealedCompetitors("juniors"))

	// The competition key is refused under per-person links.
	f.comp.PublicNames = state.PublicNamesCompetition
	ckey := state.CompetitionRevealKey(f.comp)
	f.comp.PublicNames = state.PublicNamesPerson
	aud, _, _ = judgeRequest(t, f.pn, map[string]string{revealKeyHeader: "juniors:" + ckey})
	assert.Equal(t, "public", aud.key())

	// Under a competition link, a competitor key is refused.
	f.comp.PublicNames = state.PublicNamesCompetition
	require.NoError(t, f.store.SaveCompetition(f.comp))
	aud, _, _ = judgeRequest(t, f.pn, map[string]string{revealKeyHeader: "juniors:" + key})
	assert.Equal(t, "public", aud.key())
}

// countingVerifier counts Verify calls and can hold them until released.
type countingVerifier struct {
	PasswordVerifier
	calls   atomic.Int32
	right   string
	err     error
	release chan struct{}
}

func (v *countingVerifier) Verify(p string) (bool, error) {
	v.calls.Add(1)
	if v.release != nil {
		<-v.release
	}
	if v.err != nil {
		return false, v.err
	}
	return p == v.right, nil
}

func TestCachedVerifier(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	inner := &countingVerifier{right: "right"}
	v := newCachedVerifier(inner, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		ok, err := v.Verify("right")
		require.NoError(t, err)
		assert.True(t, ok)
	}
	assert.EqualValues(t, 1, inner.calls.Load(), "the correct password skips bcrypt once seen")

	for i := 0; i < 3; i++ {
		ok, err := v.Verify("wrong")
		require.NoError(t, err)
		assert.False(t, ok)
	}
	assert.EqualValues(t, 2, inner.calls.Load(), "a wrong password costs one check while cached")

	now = now.Add(wrongPasswordCacheTTL)
	ok, err := v.Verify("wrong")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.EqualValues(t, 3, inner.calls.Load(), "the wrong-password cache lasts 60 s")

	inner.err = errors.New("boom")
	_, err = v.Verify("other")
	require.Error(t, err)
	_, err = v.Verify("other")
	require.Error(t, err)
	assert.EqualValues(t, 5, inner.calls.Load(), "an error is never cached")

	// The mode and policy methods pass through.
	assert.Same(t, inner, v.PasswordVerifier)
}

func TestCachedVerifier_ConcurrentSharesOneCheck(t *testing.T) {
	inner := &countingVerifier{right: "right", release: make(chan struct{})}
	v := newCachedVerifier(inner, time.Now)
	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = v.Verify("wrong")
		}(i)
	}
	require.Eventually(t, func() bool { return inner.calls.Load() == 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	close(inner.release)
	wg.Wait()
	assert.EqualValues(t, 1, inner.calls.Load())
	for _, r := range results {
		assert.False(t, r)
	}
}

func TestCachedVerifier_WrongCacheBounded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	v := newCachedVerifier(&countingVerifier{right: "right"}, func() time.Time { return now })
	for i := 0; i < wrongPasswordCacheMax+10; i++ {
		_, err := v.Verify("wrong-" + strings.Repeat("x", i%7) + string(rune(i)))
		require.NoError(t, err)
	}
	assert.LessOrEqual(t, len(v.bad), wrongPasswordCacheMax)
	now = now.Add(wrongPasswordCacheTTL)
	_, err := v.Verify("fresh-wrong")
	require.NoError(t, err)
	assert.Len(t, v.bad, 1, "expired entries are dropped when the cache is full")
}

func TestNewPublicNames_WrapsOnlyLockedMode(t *testing.T) {
	store := newTempStore(t)
	file := newPublicNames(store, NewFileVerifier(store))
	_, cached := file.verifier.(*cachedVerifier)
	assert.False(t, cached, "file mode keeps its cheap plain compare, whose password can change")

	hash, err := bcrypt.GenerateFromPassword([]byte("locked-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	bv, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)
	locked := newPublicNames(store, bv)
	cv, cached := locked.verifier.(*cachedVerifier)
	require.True(t, cached)
	assert.Equal(t, "locked", cv.Mode())
	ok, err := cv.Verify("locked-pw")
	require.NoError(t, err)
	assert.True(t, ok)
}

func individualMask() *nameMask {
	comp := &state.Competition{ID: "c", Kind: "individual", PublicNames: state.PublicNamesHidden}
	return buildNameMask(comp, []domain.Player{
		{ID: pnAlice, Name: "Alice Arai", Dojo: "Dojo One", Number: "K1"},
		{ID: pnBob, Name: "Bob Baba", Dojo: "Dojo Two", Number: "K2"},
		{ID: pnCarol, Name: "Carol Chiba", Dojo: "Dojo Three"},
	}, nil)
}

func TestMaskView_Individual(t *testing.T) {
	v := maskView{m: individualMask()}
	assert.True(t, v.namesHidden())
	assert.True(t, v.mark().NamesHidden)

	assert.Equal(t, "K1", v.side("Alice Arai", pnAlice))
	assert.Equal(t, "?", v.side("Carol Chiba", pnCarol), "an id with no number fails closed")
	assert.Equal(t, "?", v.side("Legacy Name", ""), "a named side with no id fails closed")
	assert.Equal(t, "?", v.side("Somebody", "44444444-4444-4444-8444-444444444444"))
	assert.Equal(t, "", v.side("", ""))
	assert.Equal(t, "Winner of r1-m3", v.side("Winner of r1-m3", ""))
	assert.Equal(t, "Pool A-1st", v.side("Pool A-1st", ""))

	players := []domain.Player{
		{ID: pnAlice, Name: "Alice Arai", DisplayName: "ARAI", Dojo: "Dojo One", Metadata: []string{"2 dan"}},
		{ID: pnCarol, Name: "Carol Chiba", DisplayName: "CHIBA", Dojo: "Dojo Three"},
	}
	v.players(players)
	assert.Equal(t, "K1", players[0].Name)
	assert.Equal(t, "K1", players[0].DisplayName)
	assert.Equal(t, "Dojo One", players[0].Dojo, "dojo stays (Q3 b)")
	assert.Equal(t, "", players[1].Name, "before a number exists the row carries no name")
	assert.Equal(t, "", players[1].DisplayName)

	ms := []state.MatchResult{{
		ID: "Pool A-1", SideA: "Alice Arai", SideAID: pnAlice, SideB: "Bob Baba", SideBID: pnBob,
		Winner: "Bob Baba", WinnerID: pnBob,
	}}
	v.matches(ms)
	assert.Equal(t, "K1", ms[0].SideA)
	assert.Equal(t, "K2", ms[0].SideB)
	assert.Equal(t, "K2", ms[0].Winner)
	assert.Equal(t, pnAlice, ms[0].SideAID, "ids are kept")

	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "r1-m1", SideA: "Alice Arai", SideAID: pnAlice, SideB: "Old Row"},
			{ID: "r1-m2", SideA: "Pool B-1st", SideB: ""},
		}, {{ID: "r2-m1", SideA: "Winner of r1-m1", SideB: "Winner of r1-m2"}}},
		ThirdPlaceMatch: &state.BracketMatch{ID: "bronze", SideA: "Bob Baba", SideAID: pnBob},
	}
	origBronze := b.ThirdPlaceMatch
	v.bracket(b)
	assert.Equal(t, "K1", b.Rounds[0][0].SideA)
	assert.Equal(t, "?", b.Rounds[0][0].SideB)
	assert.Equal(t, "Pool B-1st", b.Rounds[0][1].SideA)
	assert.Equal(t, "Winner of r1-m1", b.Rounds[1][0].SideA)
	assert.Equal(t, "K2", b.ThirdPlaceMatch.SideA)
	assert.Equal(t, "Bob Baba", origBronze.SideA, "the bronze is masked in a copy")

	shared := &domain.Player{ID: pnAlice, Name: "Alice Arai", Dojo: "Dojo One"}
	pools := []helper.Pool{{PoolName: "A", Players: []domain.Player{*shared}, Matches: []helper.Match{{SideA: shared, SideB: nil}}}}
	v.pools(pools)
	assert.Equal(t, "K1", pools[0].Players[0].Name)
	assert.Equal(t, "K1", pools[0].Matches[0].SideA.Name)
	assert.Nil(t, pools[0].Matches[0].SideB)
	assert.Equal(t, "Alice Arai", shared.Name, "a pairing's player pointer is never written through")

	rows := []state.PlayerStanding{{Player: domain.Player{ID: pnBob, Name: "Bob Baba"}}}
	v.standings(rows)
	assert.Equal(t, "K2", rows[0].Player.Name)

	awards := []state.FightingSpiritAward{
		{Title: "Spirit", RecipientName: "Bob Baba", RecipientDojo: "Dojo Two"},
		{Title: "Spirit", RecipientName: "Bob Baba", RecipientDojo: "Elsewhere"},
	}
	got := v.awards(awards)
	assert.Equal(t, "K2", got[0].RecipientName)
	assert.Equal(t, "?", got[1].RecipientName)
	assert.Equal(t, "Bob Baba", awards[0].RecipientName, "awards are masked in a copy")
}

func TestMaskView_IndividualRevealOverlay(t *testing.T) {
	v := maskView{m: individualMask(), reveal: map[string]bool{pnAlice: true}}
	assert.Equal(t, "Alice Arai", v.side("Alice Arai", pnAlice))
	assert.Equal(t, "K2", v.side("Bob Baba", pnBob))
	ps := []domain.Player{{ID: pnAlice, Name: "Alice Arai", DisplayName: "ARAI"}, {ID: pnBob, Name: "Bob Baba"}}
	v.players(ps)
	assert.Equal(t, "Alice Arai", ps[0].Name)
	assert.Equal(t, "ARAI", ps[0].DisplayName)
	assert.Equal(t, "K2", ps[1].Name)
	assert.Equal(t, "Alice Arai", v.awardRecipient("Alice Arai", "Dojo One"))
}

func TestMaskView_ZeroViewChangesNothing(t *testing.T) {
	var v maskView
	assert.False(t, v.namesHidden())
	ms := []state.MatchResult{{SideA: "Alice Arai", SideAID: pnAlice}}
	v.matches(ms)
	assert.Equal(t, "Alice Arai", ms[0].SideA)
	ps := []domain.Player{{Name: "Alice Arai"}}
	v.players(ps)
	assert.Equal(t, "Alice Arai", ps[0].Name)
	assert.Equal(t, "Alice Arai", v.side("Alice Arai", ""))
}

const (
	pnTeamA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pnTeamB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	pnMemA1 = "a1a1a1a1-a1a1-4a1a-8a1a-a1a1a1a1a1a1"
	pnMemA2 = "a2a2a2a2-a2a2-4a2a-8a2a-a2a2a2a2a2a2"
	pnMemB1 = "b1b1b1b1-b1b1-4b1b-8b1b-b1b1b1b1b1b1"
)

func teamMask() *nameMask {
	comp := &state.Competition{ID: "t", Kind: "team", TeamSize: 3, PublicNames: state.PublicNamesPerson}
	return buildNameMask(comp, []domain.Player{
		{ID: pnTeamA, Name: "Tigers", Dojo: "Dojo One", Number: "T1", Metadata: []string{"Old Member"}},
		{ID: pnTeamB, Name: "Lions", Dojo: "Dojo Two", Number: "T2"},
	}, map[string][]domain.TeamMember{
		pnTeamA: {{ID: pnMemA1, Index: 1, Name: "Ken Kato"}, {ID: pnMemA2, Index: 2, Name: "Ryo Mori"}},
		pnTeamB: {{ID: pnMemB1, Index: 1, Name: "Sho Ono"}},
	})
}

func TestMaskView_Team(t *testing.T) {
	v := maskView{m: teamMask()}
	assert.Equal(t, "Tigers", v.side("Tigers", pnTeamA), "team names stay (Q-teams)")

	ps := []domain.Player{{ID: pnTeamA, Name: "Tigers", Metadata: []string{"Old Member"}}}
	v.players(ps)
	assert.Equal(t, "Tigers", ps[0].Name)
	assert.Nil(t, ps[0].Metadata, "a legacy roster's member names go")

	subs := []state.SubMatchResult{
		{Position: 1, SideA: "Ken Kato", SideAMemberID: pnMemA1, SideB: "Sho Ono", SideBMemberID: pnMemB1, Winner: "Ken Kato", WinnerMemberID: pnMemA1},
		{Position: 2, SideA: "Ryo Mori", SideAMemberID: pnMemA2, SideB: "Unknown Fighter", Winner: "Unknown Fighter"},
		{Position: 3, SideA: "Tigers", SideB: "Lions", Winner: "Lions"},
	}
	m := state.MatchResult{
		ID: "Pool A-1", SideA: "Tigers", SideAID: pnTeamA, SideB: "Lions", SideBID: pnTeamB,
		Winner: "Tigers", WinnerID: pnTeamA, SubResults: subs, RepPlayerA: "Ryo Mori", RepPlayerB: "Nobody Here",
	}
	v.match(&m)
	assert.Equal(t, "Tigers", m.SideA)
	assert.Equal(t, "Tigers", m.Winner)
	assert.Equal(t, "T1.1", m.SubResults[0].SideA)
	assert.Equal(t, "T2.1", m.SubResults[0].SideB)
	assert.Equal(t, "T1.1", m.SubResults[0].Winner)
	assert.Equal(t, "T1.2", m.SubResults[1].SideA)
	assert.Equal(t, "?", m.SubResults[1].SideB, "a fighter with no member id fails closed")
	assert.Equal(t, "?", m.SubResults[1].Winner)
	assert.Equal(t, "Tigers", m.SubResults[2].SideA, "a quick-score row names the teams, which stay")
	assert.Equal(t, "Lions", m.SubResults[2].Winner)
	assert.Equal(t, "T1.2", m.RepPlayerA)
	assert.Equal(t, "?", m.RepPlayerB)
	assert.Equal(t, "Ken Kato", subs[0].SideA, "bout rows are masked in a copy")

	lu := domain.TeamLineup{TeamID: pnTeamA, Positions: map[domain.Position]string{"1": "Ken Kato", "2": "Typed Name"},
		MemberIDs: map[domain.Position]string{"1": pnMemA1}}
	orig := lu.Positions
	v.lineup(&lu)
	assert.Equal(t, "T1.1", lu.Positions["1"])
	assert.Equal(t, "?", lu.Positions["2"])
	assert.Equal(t, "Ken Kato", orig["1"], "the lineup's map is replaced, not written through")

	sq := v.squads(map[string][]domain.TeamMember{pnTeamA: {{ID: pnMemA1, Index: 1, Name: "Ken Kato"}, {ID: "x", Index: 3}}})
	assert.Equal(t, "T1.1", sq[pnTeamA][0].Name)
	assert.Equal(t, "", sq[pnTeamA][1].Name, "an unnamed slot stays unnamed")

	got := v.awards([]state.FightingSpiritAward{{RecipientName: "Sho Ono", RecipientDojo: "Dojo Two"}, {RecipientName: "Sho Ono", RecipientDojo: "Dojo One"}})
	assert.Equal(t, "T2.1", got[0].RecipientName)
	assert.Equal(t, "?", got[1].RecipientName)
}

func TestMaskView_TeamLinkRevealsItsMembers(t *testing.T) {
	v := maskView{m: teamMask(), reveal: map[string]bool{pnTeamA: true}}
	m := state.MatchResult{SideA: "Tigers", SideAID: pnTeamA, SideB: "Lions", SideBID: pnTeamB,
		SubResults: []state.SubMatchResult{{SideA: "Ken Kato", SideAMemberID: pnMemA1, SideB: "Sho Ono", SideBMemberID: pnMemB1, Winner: "Sho Ono", WinnerMemberID: pnMemB1}},
		RepPlayerA: "Ryo Mori"}
	v.match(&m)
	assert.Equal(t, "Ken Kato", m.SubResults[0].SideA, "E7: a team's link shows its members")
	assert.Equal(t, "T2.1", m.SubResults[0].SideB)
	assert.Equal(t, "T2.1", m.SubResults[0].Winner)
	assert.Equal(t, "Ryo Mori", m.RepPlayerA)
	sq := v.squads(map[string][]domain.TeamMember{
		pnTeamA: {{ID: pnMemA1, Index: 1, Name: "Ken Kato"}},
		pnTeamB: {{ID: pnMemB1, Index: 1, Name: "Sho Ono"}},
	})
	assert.Equal(t, "Ken Kato", sq[pnTeamA][0].Name)
	assert.Equal(t, "T2.1", sq[pnTeamB][0].Name)
	ps := []domain.Player{{ID: pnTeamA, Name: "Tigers", Metadata: []string{"Ken Kato"}}}
	v.players(ps)
	assert.Equal(t, []string{"Ken Kato"}, ps[0].Metadata)
}

func TestFailClosedMask(t *testing.T) {
	v := maskView{m: failClosedMask(&state.Competition{ID: "c"})}
	assert.Equal(t, "?", v.side("Alice Arai", pnAlice))
	assert.Equal(t, "Winner of r1-m1", v.side("Winner of r1-m1", ""))
	tv := maskView{m: failClosedMask(&state.Competition{ID: "t", Kind: "team", TeamSize: 3})}
	assert.Equal(t, "?", tv.member(pnTeamA, pnMemA1, "Ken Kato", "Tigers"))
}

func TestPublicNamesView_FromStore(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)

	// The first build may run the store's load-time repair, which writes
	// the files it was built from, so its entry is rebuilt once.
	_, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	v, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	require.True(t, v.namesHidden())
	assert.Equal(t, "K2", v.side("Bob Baba", pnBob), "numbers come from the draw (pools.csv)")

	org, err := f.pn.view(f.comp, organiserAudience())
	require.NoError(t, err)
	assert.False(t, org.namesHidden(), "the organiser sees names")

	first := v.m
	again, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	assert.Same(t, first, again.m, "the mask is cached while its files are unchanged")

	// A roster change invalidates it.
	players, err := f.store.LoadParticipants(f.comp.ID, f.comp.EffectiveWithZekkenName())
	require.NoError(t, err)
	players = append(players, domain.Player{ID: "55555555-5555-4555-8555-555555555555", Name: "Dan Doi", Dojo: "Dojo Four"})
	require.NoError(t, f.store.SaveParticipants(f.comp.ID, players))
	rebuilt, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	assert.NotSame(t, first, rebuilt.m)
	assert.Equal(t, "?", rebuilt.side("Dan Doi", "55555555-5555-4555-8555-555555555555"), "no number yet: fails closed")

	shown := *f.comp
	shown.PublicNames = state.PublicNamesShown
	sv, err := f.pn.view(&shown, audience{})
	require.NoError(t, err)
	assert.False(t, sv.namesHidden())
}

func TestPublicNamesView_SelfRunAlwaysShowsNames(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	require.True(t, f.pn.hides(f.comp))

	tour, err := f.store.LoadTournament()
	require.NoError(t, err)
	tour.Mode = state.TournamentModeSelfRun
	require.NoError(t, f.store.SaveTournament(tour))

	assert.False(t, f.pn.hides(f.comp), "self-run shows names whatever the competition's setting says")
	v, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	assert.False(t, v.namesHidden())
	loaded, err := f.store.LoadCompetition(f.comp.ID)
	require.NoError(t, err)
	assert.Equal(t, state.PublicNamesHidden, loaded.PublicNames, "the competition keeps its setting")

	tour.Mode = state.TournamentModeOfficiated
	require.NoError(t, f.store.SaveTournament(tour))
	assert.True(t, f.pn.hides(f.comp), "switched back to officiated, the setting applies again")
}

// A real name that equals its own mask (a competitor named "?" or by their own
// number) is still masked: the zekken name goes with it, never left readable.
func TestMaskCurrentMatchPayload_NameEqualToItsMaskStillMasksTheZekkenName(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	v, err := f.pn.view(f.comp, audience{})
	require.NoError(t, err)
	require.True(t, v.namesHidden())

	payload := gin.H{
		"sideA": gin.H{"playerId": pnAlice, "name": "K1", "displayName": "ARAI"},
		"sideB": gin.H{"playerId": "unknown-id", "name": maskedUnknown, "displayName": "BABA"},
	}
	maskCurrentMatchPayload(v, payload)
	a, b := payload["sideA"].(gin.H), payload["sideB"].(gin.H)
	assert.Equal(t, "K1", a["displayName"], "a name equal to the competitor's number still masks the zekken name")
	assert.Equal(t, maskedUnknown, b["displayName"], "a name equal to the unknown-competitor mark still masks the zekken name")
}

// Reveal links with a RevealUntil of their own work before tournament.md exists,
// as state.RevealOpen and the reveal-links read say they do.
func TestRevealAudience_NoTournamentYet(t *testing.T) {
	store := newTempStore(t) // no tournament saved: LoadTournament answers nil
	secret, err := state.NewRevealSecret()
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "juniors", Name: "Juniors", Kind: "individual", Format: state.CompFormatKnockout,
		PublicNames: state.PublicNamesCompetition, RevealSecret: secret, RevealUntil: "2099-01-01T10:00",
	}))
	comp, err := store.LoadCompetition("juniors")
	require.NoError(t, err)
	pn := newPublicNames(store, NewFileVerifier(store))

	aud := pn.revealAudience("juniors:" + state.CompetitionRevealKey(comp))
	assert.True(t, aud.revealsAll("juniors"), "the link reveals the names")
	own := streamRevealFor(pn, comp, audience{competitions: map[string]bool{"juniors": true}})
	assert.True(t, own.revealsAll("juniors"), "a stream holding the link keeps what it unlocks")
}

func TestPublicNamesView_UnknownCompetitionFailsClosed(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	ghost := &state.Competition{ID: "ghost", PublicNames: state.PublicNamesHidden}
	v, err := f.pn.view(ghost, audience{})
	require.Error(t, err)
	require.True(t, v.namesHidden())
	assert.Equal(t, "?", v.side("Alice Arai", pnAlice))
}

func TestCORSAllowsRevealKey(t *testing.T) {
	store := newTempStore(t)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html></html>")}})
	r, hub, limiter := NewRouter(store, engine.New(store), res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), revealKeyHeader)
}
