package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// swissMatchIDPrefix is the canonical prefix for Swiss-system match
// IDs. The full ID format is "Swiss-R{round}-{idx}" so the existing
// pool-match scoring / SSE machinery can route updates through the
// same pool-matches.csv plumbing without a separate match store.
//
// Why piggyback on pool-matches.csv: every Swiss match has the same
// shape as a pool match (two named sides, a score, a decision). The
// existing scoring endpoint, eligibility gate, and SSE broadcast all
// key off MatchResult.ID. Adding a new "Swiss match" file would
// duplicate that infrastructure for no semantic gain.
const swissMatchIDPrefix = "Swiss-R"

// swissPoolName returns the synthetic "pool name" prefix used for a
// Swiss round so that helper.parsePoolMatchesFile / scoring.go can
// route the match through the existing pool-matches pipeline.
func swissPoolName(round int) string {
	return fmt.Sprintf("%s%d", swissMatchIDPrefix, round)
}

// swissMatchID composes the canonical wire ID for the k-th match in a
// given round. Round and index both 0-based on the wire are wrong,
// rounds are 1-based by spec, match indices are 0-based per existing
// pool-match convention.
func swissMatchID(round, idx int) string {
	return fmt.Sprintf("%s-%d", swissPoolName(round), idx)
}

// parseSwissMatchRound extracts the round number from a Swiss match
// ID. Returns (round, true) on success; (0, false) for any non-Swiss
// match ID or malformed shape. Used by SwissStandings to scope its
// match scan to Swiss matches only.
func parseSwissMatchRound(id string) (int, bool) {
	if !strings.HasPrefix(id, swissMatchIDPrefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(id, swissMatchIDPrefix)
	dash := strings.Index(rest, "-")
	if dash < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:dash])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// buildSwissRosterIndex builds identity-lookup tables over roster (the FULL
// participant list for the competition, not yet narrowed to this round's
// active/eligible set): byID maps a participant ID straight to its
// CompetitorKey (trivial, but keeps every lookup going through one helper);
// byName maps a display name to every CompetitorKey sharing that name, in
// roster order.
//
// The byName fallback exists for resolving a Swiss match SIDE that predates
// this fix (bc-cse): before buildSwissMatches stamped SideAID/SideBID, a
// persisted Swiss row carried only a name. Once every match this engine
// generates carries an id for a roster member who has one (effectively
// always -- see CompetitorKey), the byID branch of resolveSwissRosterKey is
// what actually resolves it; byName is the legacy fallback for a name-only
// row. The true OLD behaviour, before identity keys existed at all, was a
// single MERGED standings/pairing entry for every namesake -- not a
// deterministic pick of one of them -- so byName cannot "reproduce" it;
// resolveSwissRosterKey's last-registered pick (see its doc comment) is a
// new, arbitrary-but-consistent tie-break on data that no longer carries
// enough information to decide correctly. Retroactively re-keying
// already-persisted rounds is out of scope.
func buildSwissRosterIndex(roster []domain.Player) (byID map[string]string, byName map[string][]string) {
	byID = make(map[string]string, len(roster))
	byName = make(map[string][]string, len(roster))
	for _, p := range roster {
		k := helper.CompetitorKey(p.ID, p.Name, p.Dojo)
		if p.ID != "" {
			byID[p.ID] = k
		}
		byName[p.Name] = append(byName[p.Name], k)
	}
	return byID, byName
}

// resolveSwissRosterKey resolves a Swiss match side (id, name) to a SINGLE
// roster identity key from buildSwissRosterIndex. See that function's doc
// comment for why the id branch is authoritative and the name branch is a
// legacy-only fallback.
//
// A win, a bye, or a prior-pairing record can only be attributed to ONE
// competitor, so the id-less fallback must make a single, deterministic
// pick among same-name roster entries. It picks the LAST-registered one
// (ks[len(ks)-1], not ks[0]) to align with every other consumer of an
// id-less legacy row: registerStandingsPlayer's name key is last-write-wins
// by construction (a later map assignment overwrites an earlier one), and
// tiebreaker.go's newGroupKeyResolver builds its name index the same way.
// Before this alignment, a single id-less row resolved to the FIRST
// namesake here but the LAST namesake in standings/tiebreak, so
// GenerateSwissRound and SwissStandings deterministically disagreed about
// who a legacy row's win belonged to. Picking "first" instead of "last"
// would have been equally arbitrary; what matters is that every consumer
// picks the SAME one.
func resolveSwissRosterKey(byID map[string]string, byName map[string][]string, id, name string) (string, bool) {
	if id != "" {
		if k, ok := byID[id]; ok {
			return k, true
		}
	}
	if ks := byName[name]; len(ks) > 0 {
		return ks[len(ks)-1], true
	}
	return "", false
}

// swissFieldKeysFromMatches returns the set of competitor identity keys
// (CompetitorKey, resolved against byID/byName) that have appeared in prior
// Swiss matches (players and bye recipients alike), i.e. the frozen round-1
// field. Used by GenerateSwissRound for rounds > 1 to keep the field stable
// across rounds regardless of later check-in toggles (mp-w7x; PR #199
// review).
//
// bc-cse: this used to be keyed by bare name (swissFieldNamesFromMatches),
// which silently merged two same-name-different-dojo participants into one
// field slot -- the app's actual duplicate rule is name+dojo (helper.
// CheckDuplicateEntriesByNameDojo), which explicitly permits such namesakes.
// Keying by identity (id-preferred, see resolveSwissRosterKey) keeps the two
// distinct.
//
// An id-less side is deliberately NOT resolved via resolveSwissRosterKey's
// single-pick policy here: that policy exists because a win/bye/pairing must
// land on exactly one competitor, but field membership asks a different
// question -- "was this name part of the round-1 draw" -- where admitting
// only one namesake would wrongly evict the other from every later round
// (they never earn a fresh id-less row of their own to reclaim a slot, since
// a frozen field member no longer appears as an active participant to pair).
// So an id-less side admits EVERY roster key sharing its name. A row that
// DOES carry an id resolves to that one competitor exactly only on a byID
// HIT; on a MISS (e.g. a replaced participant's id, now stale because it no
// longer appears in the current roster's byID index) admit falls through to
// the same byName loop below and, exactly like an id-less row, admits every
// namesake sharing that name -- correct for that case too, since a stale id
// gives no more information than no id at all about which specific
// competitor's slot was replaced.
//
// This deliberately creates an asymmetry with resolveSwissRosterKey for an
// id-less (or stale-id) row: THIS function admits BOTH namesakes to the
// frozen field, but GenerateSwissRound's win/bye/prior-pairing counters
// (wins, hadBye, priorPair below, built via resolveSwissRosterKey) can only
// attribute that row's outcome to ONE of them -- the last-registered key.
// The other namesake therefore stays in the field with a clean slate (zero
// wins, no recorded bye, no recorded prior opponent) and can be re-paired in
// a later round against someone she has, in reality, already faced. This is
// correct given the data, not a bug to reconcile: an id-less row carries no
// way to tell the two namesakes apart, so crediting a win/bye/pairing to a
// second, arbitrarily-chosen key would be no more accurate than crediting
// the first, while field MEMBERSHIP must stay conservative because wrongly
// evicting a namesake here is unrecoverable (she never earns a fresh row of
// her own to reclaim a slot). Do not "fix" one half without the other:
// making resolveSwissRosterKey multi-admit too would break wins/byes (one
// row cannot credit two competitors), and making this function single-pick
// would start silently evicting namesakes from later rounds.
func swissFieldKeysFromMatches(matches []state.MatchResult, byID map[string]string, byName map[string][]string) map[string]bool {
	field := make(map[string]bool)
	admit := func(id, name string) {
		if name == "" {
			return
		}
		if id != "" {
			if k, ok := byID[id]; ok {
				field[k] = true
				return
			}
		}
		for _, k := range byName[name] {
			field[k] = true
		}
	}
	for _, m := range matches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		admit(m.SideAID, m.SideA)
		admit(m.SideBID, m.SideB)
	}
	return field
}

// GenerateSwissRound builds the matches for round `roundNumber` of the
// Swiss-format competition identified by compID. Returns only the
// new round's matches, the caller is responsible for merging them
// into the persisted pool-matches.csv. The caller-merge convention
// mirrors the HTTP handler shape: POST /swiss/generate-round loads
// existing matches, calls this method, appends, and saves under the
// store transaction.
//
// Algorithm (FR-050b, FR-050c, FR-050f):
//
//   - Round 1: fold pairing when seeds are present (1 vs N, 2 vs N-1,
//     …); deterministic-random pairing otherwise. The deterministic
//     RNG is keyed on compID so repeated calls produce the same round
//     1, important for retry semantics on transient I/O failures.
//   - Round N > 1: group active players by win count (desc); within
//     each group pair top with bottom while avoiding rematches. When
//     a player can't be paired without a rematch the algorithm pulls
//     the next-best fallback from an adjacent group.
//   - Kiken / fusenpai exclusion: players with CompetitorStatus
//     {Eligible: false} are removed from the active pool before
//     pairing.
//   - Bye handling: if the active-player count is odd, the lowest-
//     ranked player who has not yet had a bye (or the lowest-ranked
//     overall if all players already had byes) receives a bye,
//     auto-completed win, zero points scored. The bye-resolution
//     order is round-by-round within the lowest win-count group
//     because giving a bye to a top-of-table player would distort
//     the win race.
//
// Courts are assigned round-robin from comp.Courts. Per-court time
// slots are populated via assignPoolMatchSlots so the returned matches
// land on the correct schedule cells.
func (e *Engine) GenerateSwissRound(compID string, roundNumber int) ([]state.MatchResult, error) {
	if roundNumber < 1 {
		return nil, validationErrorf("swiss round number must be >= 1, got %d", roundNumber)
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Format != state.CompFormatSwiss {
		return nil, validationErrorf("competition %s is not swiss format (got %q)", compID, comp.Format)
	}
	if comp.SwissRounds > 0 && roundNumber > comp.SwissRounds {
		return nil, validationErrorf("round %d exceeds configured swissRounds %d", roundNumber, comp.SwissRounds)
	}

	participants, err := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return nil, err
	}
	if len(participants) < 2 {
		return nil, validationErrorf("swiss round requires at least 2 participants, got %d", len(participants))
	}

	// Identity index over the FULL roster (before any round-scoping filter
	// below), used to resolve prior Swiss matches' sides back to a stable
	// competitor identity (bc-cse). See buildSwissRosterIndex's doc comment.
	rosterByID, rosterByName := buildSwissRosterIndex(participants)

	// Filter out kiken/fusenpai players (FR-050f). LoadCompetitorStatus
	// returns an empty map when the file is missing (== "all eligible")
	// so a brand-new competition with no statuses yet behaves correctly.
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return nil, err
	}
	priorMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Determine the Swiss field for this round (mp-w7x; PR #199 review).
	//
	// Round 1 (the initial draw): exclude participants who have not checked
	// in, but ONLY when check-in tracking is enabled (comp.CheckInEnabled),
	// otherwise a stale/imported checked_in marker would shrink the field even
	// though the competition doesn't use check-in (PR #199 review). When
	// enabled, opt-in semantics still apply (see filterCheckedIn).
	//
	// Round N > 1: the field is FROZEN to whoever was part of the initial
	// draw. We derive it from the identities already present in prior Swiss
	// matches rather than re-reading mutable check-in state, so toggling a
	// participant's check-in after round 1 can neither inject a zero-history
	// player into a later round nor silently drop one. Withdrawals are handled
	// separately by the eligibility (kiken/fusenpai) filter below.
	if roundNumber == 1 {
		if comp.CheckInEnabled {
			participants = filterCheckedIn(participants)
		}
	} else {
		field := swissFieldKeysFromMatches(priorMatches, rosterByID, rosterByName)
		frozen := make([]domain.Player, 0, len(participants))
		for _, p := range participants {
			if field[helper.CompetitorKey(p.ID, p.Name, p.Dojo)] {
				frozen = append(frozen, p)
			}
		}
		participants = frozen
	}

	// After T154, store.LoadParticipants returns []domain.Player directly, so
	// the Swiss pipeline doesn't need a conversion at the boundary (NFR-007).
	// Drop kiken/fusenpai players (FR-050f); LoadCompetitorStatus returns an
	// empty map when the file is missing (== "all eligible").
	active := make([]domain.Player, 0, len(participants))
	for _, p := range participants {
		if p.ID != "" {
			if st, ok := statuses[p.ID]; ok && !st.Eligible {
				continue
			}
		}
		active = append(active, p)
	}
	if len(active) < 2 {
		return nil, validationErrorf("swiss round requires at least 2 eligible participants, got %d", len(active))
	}

	// keyToPlayer resolves an active player's identity key back to the full
	// domain.Player (name + id) so buildSwissMatches can stamp
	// SideA/SideB/SideAID/SideBID once pairing has settled on identities.
	keyToPlayer := make(map[string]domain.Player, len(active))
	for _, p := range active {
		keyToPlayer[helper.CompetitorKey(p.ID, p.Name, p.Dojo)] = p
	}

	// Build the prior-pairings set (for rematch avoidance) and the
	// per-player win / bye counters. Only Swiss matches contribute,
	// non-Swiss entries (defensively skipped) would skew the standings.
	// Keyed by competitor identity (bc-cse), not bare name: two participants
	// sharing a display name from different dojos are explicitly legal
	// (helper.CheckDuplicateEntriesByNameDojo) and must be tracked
	// independently -- a name-keyed map would cross-attribute one's win, bye,
	// or prior opponent onto the other.
	priorPair := make(map[string]bool)
	wins := make(map[string]int)
	hadBye := make(map[string]bool)
	for _, m := range priorMatches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		keyA, okA := resolveSwissRosterKey(rosterByID, rosterByName, m.SideAID, m.SideA)
		if !okA {
			continue
		}
		if m.SideB == "" {
			hadBye[keyA] = true
		} else if keyB, okB := resolveSwissRosterKey(rosterByID, rosterByName, m.SideBID, m.SideB); okB {
			priorPair[pairKey(keyA, keyB)] = true
		}
		if m.Status == state.MatchStatusCompleted && m.Winner != "" {
			if winnerKey, ok := resolveSwissRosterKey(rosterByID, rosterByName, m.WinnerID, m.Winner); ok {
				wins[winnerKey]++
			}
		}
	}

	// Build a "rank" for each active player. For round 1 with seeds,
	// rank = seed (lower seed number = higher rank). For round 1
	// without seeds, rank = alphabetical position. For round N > 1,
	// rank within a win group falls back to seed/name. The rank is
	// only used for tiebreaking (bye selection, deterministic
	// orderings).
	rankByKey := buildRankByKey(active)

	pairings, byeKey, err := e.computeSwissPairings(active, wins, priorPair, hadBye, rankByKey, roundNumber, compID)
	if err != nil {
		return nil, err
	}

	matches := buildSwissMatches(pairings, byeKey, roundNumber, comp.Courts, keyToPlayer)

	// Schedule slot assignment (same per-court cursor logic as pool
	// matches). Reload tournament for ceremony / multiplier tuning.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	// Apply the team-size default (same guard as competition.go StartCompetition).
	// GenerateSwissRound reloads comp from disk; if TeamSize was 0 in the stored
	// config, the default must be applied here so assignPoolMatchSlots uses the
	// correct per-match duration for team competitions.
	if comp.Kind == "team" && comp.TeamSize == 0 {
		comp.TeamSize = 5
	}
	matches, _ = assignPoolMatchSlots(matches, comp, tournament)

	return matches, nil
}

// pairKey returns a canonical (order-independent) key for the pair (a, b).
// Generic over any ordered string token: the Swiss pipeline passes
// competitor identity keys (CompetitorKey), not names, but the
// order-independence rule is the same either way.
func pairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

// buildRankByKey computes a 1-based rank for each player suitable for
// tiebreaking. Players with explicit seeds rank by seed number
// (ascending = higher rank); unseeded players are ranked after seeded
// ones by name alphabetical order (with ties among identical names
// broken by their existing order in `players`, i.e. stable, so two
// same-name-different-dojo competitors get a fully deterministic order).
// The returned map is keyed by CompetitorKey (bc-cse), not display name:
// the rest of the Swiss pipeline uses that identity end to end so two
// competitors sharing a name from different dojos are never merged (see
// CompetitorKey's doc comment, engi.go).
func buildRankByKey(players []domain.Player) map[string]int {
	type ranked struct {
		key  string
		name string
		seed int
	}
	rs := make([]ranked, len(players))
	for i, p := range players {
		rs[i] = ranked{key: helper.CompetitorKey(p.ID, p.Name, p.Dojo), name: p.Name, seed: p.Seed}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		si, sj := rs[i].seed, rs[j].seed
		switch {
		case si > 0 && sj > 0:
			return si < sj
		case si > 0:
			return true
		case sj > 0:
			return false
		default:
			return rs[i].name < rs[j].name
		}
	})
	out := make(map[string]int, len(rs))
	for i, r := range rs {
		out[r.key] = i + 1
	}
	return out
}

// computeSwissPairings is the pairing core. It returns (pairs, bye, err)
// where pairs is the list of (sideA, sideB) tuples and bye is the
// identity key (CompetitorKey) of the bye recipient (empty string when no
// bye applies). Every map here (wins, priorPair, hadBye, rankByKey) is keyed
// by CompetitorKey, not display name (bc-cse).
func (e *Engine) computeSwissPairings(
	active []domain.Player,
	wins map[string]int,
	priorPair map[string]bool,
	hadBye map[string]bool,
	rankByKey map[string]int,
	roundNumber int,
	compID string,
) ([][2]string, string, error) {
	// Round 1: fold pairing if seeded, deterministic-random otherwise.
	if roundNumber == 1 {
		return e.firstRoundPairings(active, hadBye, rankByKey, compID)
	}

	// Round N > 1: group by wins descending, then run a "top vs
	// bottom" pairing within each group with rematch avoidance.
	return e.subsequentRoundPairings(active, wins, priorPair, hadBye, rankByKey)
}

// firstRoundPairings implements FR-050b round-1 pairing.
//
//   - When ANY player has a seed > 0, perform fold pairing on the
//     full sorted order: seed 1 vs N, seed 2 vs N-1, etc. Unseeded
//     players land below seeded ones (rank by name). If the player
//     count is odd, the lowest-ranked player (highest seed number)
//     gets the bye and is removed before folding.
//
//   - When NO player has a seed, perform deterministic-random
//     pairing keyed on compID so retries produce the same result
//     (important for SSE replay / handler-retry semantics).
//
// Operates on competitor identity keys (CompetitorKey), not display names,
// throughout (bc-cse): the deterministic shuffle's OUTPUT ORDER depends only
// on the RNG sequence and slice length, not on the string values being
// permuted, so switching from names to keys does not change the pairing
// produced for any roster that has no same-name collisions -- the ordering
// keys sort identically either way, only the map's value type differs.
func (e *Engine) firstRoundPairings(
	active []domain.Player,
	hadBye map[string]bool,
	rankByKey map[string]int,
	compID string,
) ([][2]string, string, error) {
	anySeeded := false
	for _, p := range active {
		if p.Seed > 0 {
			anySeeded = true
			break
		}
	}

	// Order by rank (seed → name). The order is used either directly
	// (fold) or as a starting permutation (random).
	keys := make([]string, len(active))
	for i, p := range active {
		keys[i] = helper.CompetitorKey(p.ID, p.Name, p.Dojo)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return rankByKey[keys[i]] < rankByKey[keys[j]]
	})

	bye := ""
	if len(keys)%2 == 1 {
		// Lowest-ranked unmatched player who hasn't had a bye gets it.
		bye = pickByeFromOrdered(keys, hadBye)
		keys = removeName(keys, bye)
	}

	if anySeeded {
		// Fold: keys is already in rank order (top → bottom).
		pairs := foldPair(keys)
		return pairs, bye, nil
	}

	// Deterministic random.
	rng := rand.New(rand.NewSource(seedFromString(compID + ":round1"))) // #nosec G404, non-crypto deterministic shuffle
	shuffled := append([]string(nil), keys...)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	pairs := make([][2]string, 0, len(shuffled)/2)
	for i := 0; i+1 < len(shuffled); i += 2 {
		pairs = append(pairs, [2]string{shuffled[i], shuffled[i+1]})
	}
	return pairs, bye, nil
}

// foldPair turns an ordered slice into fold pairings: top vs bottom,
// second-top vs second-bottom, … Generic over any ordered string token
// (identity keys, in the Swiss pipeline).
func foldPair(names []string) [][2]string {
	n := len(names) / 2
	pairs := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		pairs = append(pairs, [2]string{names[i], names[len(names)-1-i]})
	}
	return pairs
}

// pickByeFromOrdered scans `ordered` (already sorted lowest-rank-last)
// from the bottom and returns the first token that has not yet had a
// bye. If every token has had a bye, falls back to the lowest-ranked
// regardless (FR-050c: "no previous bye" preferred, not required).
// Generic over any ordered string token; the Swiss pipeline passes identity
// keys, and hadBye must be keyed the same way for this to correctly treat
// two same-name-different-dojo competitors' bye histories independently.
func pickByeFromOrdered(ordered []string, hadBye map[string]bool) string {
	if len(ordered) == 0 {
		return ""
	}
	for i := len(ordered) - 1; i >= 0; i-- {
		if !hadBye[ordered[i]] {
			return ordered[i]
		}
	}
	return ordered[len(ordered)-1]
}

// removeName drops the first occurrence of target from names. Generic over
// any ordered string token (identity keys, in the Swiss pipeline).
func removeName(names []string, target string) []string {
	out := make([]string, 0, len(names)-1)
	for _, n := range names {
		if n != target {
			out = append(out, n)
		}
	}
	return out
}

// subsequentRoundPairings implements FR-050c pairing for rounds 2+:
//
//  1. Group active players by win count (desc).
//  2. If the active count is odd, pick a bye from the lowest-win
//     group (preferring a player without a prior bye); remove them.
//  3. Pair top-vs-bottom within each group, falling back to the next
//     group when a rematch can't be avoided.
//  4. Within each group, players are ordered by rank (seed → name).
//
// wins/priorPair/hadBye/rankByKey are all keyed by CompetitorKey (bc-cse),
// not display name, so two same-name-different-dojo competitors are paired,
// win-tracked, and bye-tracked independently.
func (e *Engine) subsequentRoundPairings(
	active []domain.Player,
	wins map[string]int,
	priorPair map[string]bool,
	hadBye map[string]bool,
	rankByKey map[string]int,
) ([][2]string, string, error) {
	// Sort all active players by (-wins, rank).
	ordered := make([]string, len(active))
	for i, p := range active {
		ordered[i] = helper.CompetitorKey(p.ID, p.Name, p.Dojo)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		wi, wj := wins[ordered[i]], wins[ordered[j]]
		if wi != wj {
			return wi > wj
		}
		return rankByKey[ordered[i]] < rankByKey[ordered[j]]
	})

	bye := ""
	if len(ordered)%2 == 1 {
		// Pick the bye from the LOWEST win bucket, giving a bye to
		// a leading player would distort the win race. Within the
		// bucket, lowest-ranked player without a prior bye.
		lowestWinBucket := lowestWinBucketNames(ordered, wins)
		bye = pickByeFromOrdered(lowestWinBucket, hadBye)
		ordered = removeName(ordered, bye)
	}

	// Pair within win-groups. When pairing fails inside a group
	// (rematch wall), pull a candidate from the next group up/down.
	pairs := pairWithinWinGroups(ordered, priorPair)
	return pairs, bye, nil
}

// lowestWinBucketNames returns the suffix of `ordered` that shares the
// minimum win count. `ordered` is assumed to be sorted by (-wins,…).
// Generic over any ordered string token (identity keys, in the Swiss
// pipeline).
func lowestWinBucketNames(ordered []string, wins map[string]int) []string {
	if len(ordered) == 0 {
		return nil
	}
	minWins := wins[ordered[len(ordered)-1]]
	out := []string{}
	for _, n := range ordered {
		if wins[n] == minWins {
			out = append(out, n)
		}
	}
	return out
}

// pairWithinWinGroups walks `ordered` (sorted top-to-bottom by wins
// then rank), peels off the head's win group, and pairs top-with-
// bottom within it. When a player can't be paired without a rematch,
// the algorithm pulls a partner from the NEXT win group (so the
// leading player still gets a match, at a slight win-race distortion
// cost, preferable to forcing a rematch).
//
// This is a deliberately simple algorithm rather than a perfect
// Monrad / weighted-matching implementation: it satisfies the
// acceptance criteria (avoid rematches, prefer same-win pairings)
// without the complexity of full graph matching. For tournaments
// where the "fall through to next group" case dominates, a richer
// matcher could replace this, the test suite (T175) covers the
// happy-path correctness.
//
// ordered/priorPair carry competitor identity keys (CompetitorKey),
// not display names (bc-cse), so rematch avoidance (priorPair) correctly
// distinguishes two same-name-different-dojo competitors.
func pairWithinWinGroups(ordered []string, priorPair map[string]bool) [][2]string {
	pairs := [][2]string{}

	// remaining holds the still-unpaired names in priority order.
	remaining := append([]string(nil), ordered...)

	for len(remaining) >= 2 {
		head := remaining[0]
		// Find the partner: scan the rest of `remaining` for a non-
		// rematch, preferring same-win partner (which appears earlier
		// because of the sort). When same-win options exhaust, the
		// scan naturally falls through to lower-win opponents.
		partnerIdx := -1
		for j := 1; j < len(remaining); j++ {
			if !priorPair[pairKey(head, remaining[j])] {
				partnerIdx = j
				break
			}
		}
		if partnerIdx == -1 {
			// Every remaining candidate is a rematch. Force the
			// nearest opponent (first in remaining), the operator
			// will see a rematch but at least every player gets a
			// match.
			partnerIdx = 1
		}
		pairs = append(pairs, [2]string{head, remaining[partnerIdx]})
		remaining = append(remaining[:partnerIdx], remaining[partnerIdx+1:]...)
		remaining = remaining[1:]
	}
	return pairs
}

// buildSwissMatches turns the (pairings, bye, round, courts) tuple into
// MatchResult entries with synthetic IDs, round-robin court assignment, and
// the appropriate Status for played-vs-bye matches.
//
// pairings and byeKey carry competitor IDENTITY KEYS (CompetitorKey), not
// display names (bc-cse) -- resolved back to a display Name (and, when
// available, participant ID) via keyToPlayer, exactly mirroring pools.go's
// regular-match generation (pools.go:207, `SideAID: m.SideA.ID`). This is
// what lets two same-name-different-dojo competitors be stamped with
// distinct SideAID/SideBID instead of colliding on a shared SideA/SideB
// string with no way to tell them apart downstream.
func buildSwissMatches(pairings [][2]string, byeKey string, round int, courts []string, keyToPlayer map[string]domain.Player) []state.MatchResult {
	if len(courts) == 0 {
		// Defensive: any non-empty match still needs a court field.
		// Use a single anonymous court so downstream renderers don't
		// crash on empty strings.
		courts = []string{""}
	}
	matches := make([]state.MatchResult, 0, len(pairings)+1)
	idx := 0
	for i, p := range pairings {
		a, b := keyToPlayer[p[0]], keyToPlayer[p[1]]
		matches = append(matches, state.MatchResult{
			ID:      swissMatchID(round, idx),
			SideA:   a.Name,
			SideB:   b.Name,
			SideAID: a.ID,
			SideBID: b.ID,
			Status:  state.MatchStatusScheduled,
			Court:   courts[i%len(courts)],
		})
		idx++
	}
	if byeKey != "" {
		bye := keyToPlayer[byeKey]
		matches = append(matches, state.MatchResult{
			ID:       swissMatchID(round, idx),
			SideA:    bye.Name,
			SideB:    "",
			SideAID:  bye.ID,
			Winner:   bye.Name,
			WinnerID: bye.ID,
			IpponsA:  nil,
			IpponsB:  nil,
			Status:   state.MatchStatusCompleted,
			// Bye assigned to the next court in rotation purely for
			// consistency with the played-match shape; the schedule
			// estimator will see the bye as zero-duration via the
			// "Completed" status and skip its slot.
			Court: courts[len(pairings)%len(courts)],
		})
	}
	return matches
}

// seedFromString derives a stable 64-bit seed from s, used to drive
// the deterministic round-1 random pairing. SHA-256 is overkill but
// already imported elsewhere; the first 8 bytes give us a uniform
// distribution suitable for math/rand.NewSource.
func seedFromString(s string) int64 {
	sum := sha256.Sum256([]byte(s))
	return int64(binary.BigEndian.Uint64(sum[:8])) // #nosec G115, deterministic test seed, sign doesn't matter
}

// SwissStandings computes the cumulative standings for the Swiss
// competition compID. Ranks are assigned by:
//
//  1. Wins (descending)
//  2. Points scored (descending), total ippons given across all
//     completed Swiss matches the player participated in
//  3. Head-to-head (descending): when (1) and (2) tie, the player
//     who won the direct match between them ranks higher
//  4. Stable name order (alphabetical) as a final deterministic
//     tiebreak, guarantees idempotent output
//
// Returns one entry per participant (including byes), with Rank set
// 1..N. Excludes participants with no Swiss matches recorded (a
// future round may still pair them; their absence from the file is
// a "not yet played" signal, not a "ranked last" one), but the
// participants are still emitted with zeros so the standings page
// can render the full roster.
//
// FR-050e.
func (e *Engine) SwissStandings(compID string) ([]state.PlayerStanding, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	participants, err := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return nil, err
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Dispatch on competition type at this single seam, mirroring the pool
	// path (computeStandingsFrom): an engi (flag-scored) competition ranks by
	// wins then accumulated own-side flags, so it delegates to the engi
	// standings core rather than the ippon tally below (engi.go
	// hard-separation principle). Swiss's head-to-head tiebreak is preserved
	// inside that core. Team vs individual is a kendo-internal branch handled
	// inline in the tally and sort below: a team match carries no ippons of its
	// own, so its tie-break columns (IV/IL/IT/PW/PL) come from SubResults.
	if comp.Engi {
		return e.computeEngiSwissStandings(participants, matches)
	}
	isTeam := comp.TeamSize > 0

	// Indexed by IDENTITY, not bare name: two participants sharing a name
	// from different dojos are explicitly allowed, and a bare-name key here
	// collapsed them into one standings row before this fix.
	byKey, order := newStandingsIndex(participants)

	// Tally W/L/D and ippons across every Swiss match. Skip non-Swiss
	// rows (a stray pool-match in the file would otherwise contribute
	// to Swiss standings, defensive but should never happen if the
	// engine is the only writer). Match sides resolve via
	// lookupStandingsPlayer (id-preferred, name fallback): buildSwissMatches
	// now stamps SideAID/SideBID (bc-cse) exactly like pools.go, so this
	// resolves unambiguously for any match this engine generates going
	// forward; a pre-fix persisted Swiss row with no side ids still falls
	// back to name, matching the old behaviour for that legacy data.
	headToHead := make(map[string]map[string]string) // winner key → opponent key → winner key
	for _, m := range matches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		// Bye matches: SideA wins, no points scored, no head-to-head.
		if m.SideB == "" {
			if sA := lookupStandingsPlayer(byKey, m.SideAID, m.SideA); sA != nil {
				sA.Wins++
			}
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			continue
		}
		sA := lookupStandingsPlayer(byKey, m.SideAID, m.SideA)
		sB := lookupStandingsPlayer(byKey, m.SideBID, m.SideB)
		if sA == nil || sB == nil {
			continue
		}
		if isTeam && len(m.SubResults) > 0 {
			// Team match: the parent carries no ippons of its own, so the
			// tie-break columns (IV/IL/IT/PW/PL) come from the sub-bouts,
			// via the same shared accrual the pool path uses in
			// computeStandingsFrom.
			accrueTeamSubResults(sA, sB, m)
		} else {
			// Individual scoring: ippons at match level, via countScoringIppons
			// (not len): bye-marked completed matches contain nil ippons (count 0,
			// correct), and a real completed match can retain "•" unfilled-slot
			// placeholders or empty entries that are not scored points.
			sA.IpponsGiven += countScoringIppons(m.IpponsA)
			sA.IpponsTaken += countScoringIppons(m.IpponsB)
			sB.IpponsGiven += countScoringIppons(m.IpponsB)
			sB.IpponsTaken += countScoringIppons(m.IpponsA)
		}

		// Winner by id where recorded, else by name; see resolveWinnerSide.
		winnerIsA, winnerIsB := resolveWinnerSide(m)
		keyA := standingsPlayerKey(sA.Player.ID, sA.Player.Name)
		keyB := standingsPlayerKey(sB.Player.ID, sB.Player.Name)
		switch {
		case winnerIsA:
			sA.Wins++
			sB.Losses++
			recordHeadToHead(headToHead, keyA, keyB, keyA)
		case winnerIsB:
			sB.Wins++
			sA.Losses++
			recordHeadToHead(headToHead, keyA, keyB, keyB)
		case state.IsDraw(m.Decision) || m.Winner == "":
			sA.Draws++
			sB.Draws++
			recordHeadToHead(headToHead, keyA, keyB, "")
		}
	}

	// Assemble + sort. Tie-breakers: wins > ippons-given (points
	// scored) > head-to-head > name (stable). Reusing the existing
	// PlayerStanding shape keeps the wire contract identical to the
	// pool-standings endpoint so the frontend can render either with
	// the same table.
	standings := make([]state.PlayerStanding, 0, len(order))
	for _, s := range order {
		// Compose a human-readable score summary via the shared format
		// helpers so this table and the pool-standings table can't drift.
		if isTeam {
			s.ScoreSummary = teamScoreSummary(s)
		} else {
			s.ScoreSummary = individualScoreSummary(s)
		}
		standings = append(standings, *s)
	}
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if isTeam {
			// Full ordered team chain (W, L, T, IV, IL, IT, PW, PL) packed into
			// one score, then head-to-head, then name; see teamStandingPoints.
			if pa, pb := teamStandingPoints(a), teamStandingPoints(b); pa != pb {
				return pa > pb
			}
		} else {
			if a.Wins != b.Wins {
				return a.Wins > b.Wins
			}
			if a.IpponsGiven != b.IpponsGiven {
				return a.IpponsGiven > b.IpponsGiven
			}
		}
		// Head-to-head: if a beat b directly, a ranks higher.
		keyA := standingsPlayerKey(a.Player.ID, a.Player.Name)
		keyB := standingsPlayerKey(b.Player.ID, b.Player.Name)
		if winner, ok := lookupH2H(headToHead, keyA, keyB); ok {
			if winner == keyA {
				return true
			}
			if winner == keyB {
				return false
			}
		}
		return a.Player.Name < b.Player.Name
	})
	for i := range standings {
		standings[i].Rank = i + 1
	}
	return standings, nil
}

func recordHeadToHead(h2h map[string]map[string]string, sideA, sideB, winner string) {
	if h2h[sideA] == nil {
		h2h[sideA] = make(map[string]string)
	}
	if h2h[sideB] == nil {
		h2h[sideB] = make(map[string]string)
	}
	h2h[sideA][sideB] = winner
	h2h[sideB][sideA] = winner
}

func lookupH2H(h2h map[string]map[string]string, a, b string) (string, bool) {
	if m, ok := h2h[a]; ok {
		if w, ok2 := m[b]; ok2 {
			return w, true
		}
	}
	return "", false
}

// CurrentSwissRoundCompleted reports whether every match in the
// currently-active Swiss round is completed. Returns true when the
// current round is 0 (not started, vacuously "complete enough" so
// the first round can be generated) or every match in
// pool-matches.csv whose ID parses to the current round has
// Status == Completed.
//
// FR-050d. Used by the POST /swiss/generate-round handler as the
// pre-condition gate.
func (e *Engine) CurrentSwissRoundCompleted(compID string) (bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	if comp == nil {
		return false, notFoundErrorf("competition %s not found", compID)
	}
	if comp.SwissCurrentRound == 0 {
		return true, nil
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return false, err
	}
	for _, m := range matches {
		round, ok := parseSwissMatchRound(m.ID)
		if !ok || round != comp.SwissCurrentRound {
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			return false, nil
		}
	}
	return true, nil
}

// AdvanceSwissRound is the high-level engine wrapper for the
// POST /swiss/generate-round handler. It:
//
//  1. Validates that the current round is completed (FR-050d).
//  2. Generates the next round via GenerateSwissRound.
//  3. Appends the new matches to pool-matches.csv (merging with
//     prior rounds, Swiss runs the same persistence shape as
//     pools, but cross-round, so each save carries the cumulative
//     state).
//  4. Atomically bumps SwissCurrentRound on the competition config.
//
// Returns the new round's matches (NOT the merged list) so the
// handler can broadcast them and the caller of the API gets a clean
// per-round payload.
//
// All store mutations run under the per-competition lock via the
// store atomic primitives.
func (e *Engine) AdvanceSwissRound(compID string) ([]state.MatchResult, int, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, 0, err
	}
	if comp == nil {
		return nil, 0, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Format != state.CompFormatSwiss {
		return nil, 0, validationErrorf("competition %s is not swiss format", compID)
	}
	if comp.SwissRounds > 0 && comp.SwissCurrentRound >= comp.SwissRounds {
		return nil, 0, validationErrorf("all %d swiss rounds already completed", comp.SwissRounds)
	}
	completed, err := e.CurrentSwissRoundCompleted(compID)
	if err != nil {
		return nil, 0, err
	}
	if !completed {
		return nil, 0, &SwissRoundNotCompletedError{
			CompID: compID,
			Round:  comp.SwissCurrentRound,
		}
	}

	nextRound := comp.SwissCurrentRound + 1
	newMatches, err := e.GenerateSwissRound(compID, nextRound)
	if err != nil {
		return nil, 0, err
	}

	prior, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, 0, err
	}
	merged := make([]state.MatchResult, 0, len(prior)+len(newMatches))
	merged = append(merged, prior...)
	merged = append(merged, newMatches...)
	if err := e.store.SavePoolMatches(compID, merged); err != nil {
		return nil, 0, err
	}

	// Commit the bump under the per-comp lock so a concurrent
	// AdvanceSwissRound call can't generate the same round twice.
	if _, err := e.store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s vanished during AdvanceSwissRound", compID)
		}
		// Defensive re-check: if a concurrent writer already advanced
		// the round (unlikely given the single-writer admin UI), keep
		// the higher value.
		if current.SwissCurrentRound < nextRound {
			current.SwissCurrentRound = nextRound
		}
		return current, nil
	}); err != nil {
		return nil, 0, err
	}

	// Sync the in-memory comp record so callers reading the engine
	// response see the post-bump round number. Status transition to
	// "pools" mirrors the existing pool-format flow so the rest of
	// the app (queue position, schedule view) treats Swiss matches
	// like in-progress pool matches.
	if comp.Status == state.CompStatusSetup || comp.Status == "" {
		if _, serr := e.store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
			if current == nil {
				return nil, nil
			}
			if current.Status == state.CompStatusSetup || current.Status == "" {
				current.Status = state.CompStatusPools
			}
			return current, nil
		}); serr != nil {
			// Non-fatal: the matches landed; status is cosmetic. Log
			// via the normal engine error channel.
			return newMatches, nextRound, nil // status bump best-effort
		}
	}

	return newMatches, nextRound, nil
}

// SwissRoundNotCompletedError is returned by AdvanceSwissRound when
// the current round still has un-completed matches. Handlers should
// map this to HTTP 409.
//
// FR-050d.
type SwissRoundNotCompletedError struct {
	CompID string
	Round  int
}

func (e *SwissRoundNotCompletedError) Error() string {
	return fmt.Sprintf("swiss round %d for competition %s has incomplete matches", e.Round, e.CompID)
}
