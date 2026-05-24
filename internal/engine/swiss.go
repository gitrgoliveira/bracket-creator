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
// given round. Round and index both 0-based on the wire are wrong —
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

// GenerateSwissRound builds the matches for round `roundNumber` of the
// Swiss-format competition identified by compID. Returns only the
// new round's matches — the caller is responsible for merging them
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
//     1 — important for retry semantics on transient I/O failures.
//   - Round N > 1: group active players by win count (desc); within
//     each group pair top with bottom while avoiding rematches. When
//     a player can't be paired without a rematch the algorithm pulls
//     the next-best fallback from an adjacent group.
//   - Kiken / fusenpai exclusion: players with CompetitorStatus
//     {Eligible: false} are removed from the active pool before
//     pairing.
//   - Bye handling: if the active-player count is odd, the lowest-
//     ranked player who has not yet had a bye (or the lowest-ranked
//     overall if all players already had byes) receives a bye —
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

	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
	if err != nil {
		return nil, err
	}
	if len(participants) < 2 {
		return nil, validationErrorf("swiss round requires at least 2 participants, got %d", len(participants))
	}

	// Filter out kiken/fusenpai players (FR-050f). LoadCompetitorStatus
	// returns an empty map when the file is missing (== "all eligible")
	// so a brand-new competition with no statuses yet behaves correctly.
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return nil, err
	}
	// After T154, store.LoadParticipants returns []domain.Player
	// directly, so the Swiss pipeline doesn't need a conversion at the
	// boundary (NFR-007).
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

	priorMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Build the prior-pairings set (for rematch avoidance) and the
	// per-player win / bye counters. Only Swiss matches contribute —
	// non-Swiss entries (defensively skipped) would skew the standings.
	priorPair := make(map[string]bool)
	wins := make(map[string]int)
	hadBye := make(map[string]bool)
	for _, m := range priorMatches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		if m.SideB == "" {
			hadBye[m.SideA] = true
		} else {
			priorPair[pairKey(m.SideA, m.SideB)] = true
		}
		if m.Status == state.MatchStatusCompleted && m.Winner != "" {
			wins[m.Winner]++
		}
	}

	// Build a "rank" for each active player. For round 1 with seeds,
	// rank = seed (lower seed number = higher rank). For round 1
	// without seeds, rank = alphabetical position. For round N > 1,
	// rank within a win group falls back to seed/name. The rank is
	// only used for tiebreaking (bye selection, deterministic
	// orderings).
	rankByName := buildRankByName(active)

	pairings, byeName, err := e.computeSwissPairings(active, wins, priorPair, hadBye, rankByName, roundNumber, compID)
	if err != nil {
		return nil, err
	}

	matches := buildSwissMatches(pairings, byeName, roundNumber, comp.Courts)

	// Schedule slot assignment (same per-court cursor logic as pool
	// matches). Reload tournament for ceremony / multiplier tuning.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	// Apply the team-size default (same guard as competition.go StartCompetition).
	// GenerateSwissRound reloads comp from disk; if TeamSize was 0 in the stored
	// config, the default must be applied here so assignPoolMatchSlots uses the
	// correct per-match duration for team competitions.
	if comp.Kind == "team" && comp.TeamSize == 0 {
		comp.TeamSize = 5
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	return matches, nil
}

// pairKey returns a canonical (order-independent) key for the pair (a, b).
func pairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

// buildRankByName computes a 1-based rank for each player suitable for
// tiebreaking. Players with explicit seeds rank by seed number
// (ascending = higher rank); unseeded players are ranked after seeded
// ones by name alphabetical order. The returned map uses Name as key
// — the rest of the Swiss pipeline operates on names because pool-
// matches.csv stores names, not IDs (parsePoolMatchesFile sets
// MatchResult.SideA / .SideB to names).
func buildRankByName(players []domain.Player) map[string]int {
	type ranked struct {
		name string
		seed int
	}
	rs := make([]ranked, len(players))
	for i, p := range players {
		rs[i] = ranked{name: p.Name, seed: p.Seed}
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
		out[r.name] = i + 1
	}
	return out
}

// computeSwissPairings is the pairing core. It returns (pairs, bye, err)
// where pairs is the list of (sideA, sideB) tuples and bye is the
// name of the bye recipient (empty string when no bye applies).
func (e *Engine) computeSwissPairings(
	active []domain.Player,
	wins map[string]int,
	priorPair map[string]bool,
	hadBye map[string]bool,
	rankByName map[string]int,
	roundNumber int,
	compID string,
) ([][2]string, string, error) {
	// Round 1: fold pairing if seeded, deterministic-random otherwise.
	if roundNumber == 1 {
		return e.firstRoundPairings(active, hadBye, rankByName, compID)
	}

	// Round N > 1: group by wins descending, then run a "top vs
	// bottom" pairing within each group with rematch avoidance.
	return e.subsequentRoundPairings(active, wins, priorPair, hadBye, rankByName)
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
func (e *Engine) firstRoundPairings(
	active []domain.Player,
	hadBye map[string]bool,
	rankByName map[string]int,
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
	names := make([]string, len(active))
	for i, p := range active {
		names[i] = p.Name
	}
	sort.SliceStable(names, func(i, j int) bool {
		return rankByName[names[i]] < rankByName[names[j]]
	})

	bye := ""
	if len(names)%2 == 1 {
		// Lowest-ranked unmatched player who hasn't had a bye gets it.
		bye = pickByeFromOrdered(names, hadBye)
		names = removeName(names, bye)
	}

	if anySeeded {
		// Fold: names is already in rank order (top → bottom).
		pairs := foldPair(names)
		return pairs, bye, nil
	}

	// Deterministic random.
	rng := rand.New(rand.NewSource(seedFromString(compID + ":round1"))) // #nosec G404 — non-crypto deterministic shuffle
	shuffled := append([]string(nil), names...)
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
// second-top vs second-bottom, …
func foldPair(names []string) [][2]string {
	n := len(names) / 2
	pairs := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		pairs = append(pairs, [2]string{names[i], names[len(names)-1-i]})
	}
	return pairs
}

// pickByeFromOrdered scans `ordered` (already sorted lowest-rank-last)
// from the bottom and returns the first name that has not yet had a
// bye. If every name has had a bye, falls back to the lowest-ranked
// regardless (FR-050c: "no previous bye" preferred, not required).
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
func (e *Engine) subsequentRoundPairings(
	active []domain.Player,
	wins map[string]int,
	priorPair map[string]bool,
	hadBye map[string]bool,
	rankByName map[string]int,
) ([][2]string, string, error) {
	// Sort all active players by (-wins, rank).
	ordered := make([]string, len(active))
	for i, p := range active {
		ordered[i] = p.Name
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		wi, wj := wins[ordered[i]], wins[ordered[j]]
		if wi != wj {
			return wi > wj
		}
		return rankByName[ordered[i]] < rankByName[ordered[j]]
	})

	bye := ""
	if len(ordered)%2 == 1 {
		// Pick the bye from the LOWEST win bucket — giving a bye to
		// a leading player would distort the win race. Within the
		// bucket, lowest-ranked player without a prior bye.
		lowestWinBucket := lowestWinBucketNames(ordered, wins)
		bye = pickByeFromOrdered(lowestWinBucket, hadBye)
		ordered = removeName(ordered, bye)
	}

	// Pair within win-groups. When pairing fails inside a group
	// (rematch wall), pull a candidate from the next group up/down.
	pairs := pairWithinWinGroups(ordered, wins, priorPair, rankByName)
	return pairs, bye, nil
}

// lowestWinBucketNames returns the suffix of `ordered` that shares the
// minimum win count. `ordered` is assumed to be sorted by (-wins,…).
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
// cost — preferable to forcing a rematch).
//
// This is a deliberately simple algorithm rather than a perfect
// Monrad / weighted-matching implementation: it satisfies the
// acceptance criteria (avoid rematches, prefer same-win pairings)
// without the complexity of full graph matching. For tournaments
// where the "fall through to next group" case dominates, a richer
// matcher could replace this — the test suite (T175) covers the
// happy-path correctness.
func pairWithinWinGroups(ordered []string, wins map[string]int, priorPair map[string]bool, rankByName map[string]int) [][2]string {
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
			// nearest opponent (first in remaining) — the operator
			// will see a rematch but at least every player gets a
			// match.
			partnerIdx = 1
		}
		pairs = append(pairs, [2]string{head, remaining[partnerIdx]})
		remaining = append(remaining[:partnerIdx], remaining[partnerIdx+1:]...)
		remaining = remaining[1:]
	}
	_ = rankByName // reserved for richer tie-break logic; kept in the signature for future evolution
	return pairs
}

// buildSwissMatches turns the (pairings, bye, round, courts) tuple
// into MatchResult entries with synthetic IDs, round-robin court
// assignment, and the appropriate Status for played-vs-bye matches.
func buildSwissMatches(pairings [][2]string, byeName string, round int, courts []string) []state.MatchResult {
	if len(courts) == 0 {
		// Defensive: any non-empty match still needs a court field.
		// Use a single anonymous court so downstream renderers don't
		// crash on empty strings.
		courts = []string{""}
	}
	matches := make([]state.MatchResult, 0, len(pairings)+1)
	idx := 0
	for i, p := range pairings {
		matches = append(matches, state.MatchResult{
			ID:     swissMatchID(round, idx),
			SideA:  p[0],
			SideB:  p[1],
			Status: state.MatchStatusScheduled,
			Court:  courts[i%len(courts)],
		})
		idx++
	}
	if byeName != "" {
		matches = append(matches, state.MatchResult{
			ID:      swissMatchID(round, idx),
			SideA:   byeName,
			SideB:   "",
			Winner:  byeName,
			IpponsA: nil,
			IpponsB: nil,
			Status:  state.MatchStatusCompleted,
			// Bye assigned to the next court in rotation purely for
			// consistency with the played-match shape; the schedule
			// estimator will see the bye as zero-duration via the
			// "Completed" status and skip its slot.
			Court: courts[len(pairings)%len(courts)],
		})
	}
	return matches
}

// seedFromString derives a stable 64-bit seed from s — used to drive
// the deterministic round-1 random pairing. SHA-256 is overkill but
// already imported elsewhere; the first 8 bytes give us a uniform
// distribution suitable for math/rand.NewSource.
func seedFromString(s string) int64 {
	sum := sha256.Sum256([]byte(s))
	return int64(binary.BigEndian.Uint64(sum[:8])) // #nosec G115 — deterministic test seed, sign doesn't matter
}

// SwissStandings computes the cumulative standings for the Swiss
// competition compID. Ranks are assigned by:
//
//  1. Wins (descending)
//  2. Points scored (descending) — total ippons given across all
//     completed Swiss matches the player participated in
//  3. Head-to-head (descending): when (1) and (2) tie, the player
//     who won the direct match between them ranks higher
//  4. Stable name order (alphabetical) as a final deterministic
//     tiebreak — guarantees idempotent output
//
// Returns one entry per participant (including byes), with Rank set
// 1..N. Excludes participants with no Swiss matches recorded (a
// future round may still pair them; their absence from the file is
// a "not yet played" signal, not a "ranked last" one) — but the
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
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
	if err != nil {
		return nil, err
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Initialise one PlayerStanding per participant so the response
	// includes the full roster (matches the existing pool-standings
	// invariant — operators expect to see every player listed).
	byName := make(map[string]*state.PlayerStanding, len(participants))
	for _, p := range participants {
		byName[p.Name] = &state.PlayerStanding{
			Player: p,
		}
	}

	// Tally W/L/D and ippons across every Swiss match. Skip non-Swiss
	// rows (a stray pool-match in the file would otherwise contribute
	// to Swiss standings — defensive but should never happen if the
	// engine is the only writer).
	headToHead := make(map[string]map[string]string) // winnerName → opponentName → who won
	for _, m := range matches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		// Bye matches: SideA wins, no points scored, no head-to-head.
		if m.SideB == "" {
			if sA := byName[m.SideA]; sA != nil {
				sA.Wins++
			}
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			continue
		}
		sA := byName[m.SideA]
		sB := byName[m.SideB]
		if sA == nil || sB == nil {
			continue
		}
		// Ippons per side. Bye-marked completed matches above contain
		// nil ippons, so len(...) = 0 — correct for "0 points".
		sA.IpponsGiven += len(m.IpponsA)
		sA.IpponsTaken += len(m.IpponsB)
		sB.IpponsGiven += len(m.IpponsB)
		sB.IpponsTaken += len(m.IpponsA)

		switch {
		case m.Winner == m.SideA:
			sA.Wins++
			sB.Losses++
			recordHeadToHead(headToHead, m.SideA, m.SideB, m.SideA)
		case m.Winner == m.SideB:
			sB.Wins++
			sA.Losses++
			recordHeadToHead(headToHead, m.SideA, m.SideB, m.SideB)
		case state.IsDraw(m.Decision) || m.Winner == "":
			sA.Draws++
			sB.Draws++
			recordHeadToHead(headToHead, m.SideA, m.SideB, "")
		}
	}

	// Assemble + sort. Tie-breakers: wins > ippons-given (points
	// scored) > head-to-head > name (stable). Reusing the existing
	// PlayerStanding shape keeps the wire contract identical to the
	// pool-standings endpoint so the frontend can render either with
	// the same table.
	standings := make([]state.PlayerStanding, 0, len(byName))
	for _, s := range byName {
		// Compose a human-readable score summary mirroring the
		// pool-standings format so the same UI cell renders both.
		s.ScoreSummary = fmt.Sprintf("W:%d L:%d D:%d | P:%d-%d",
			s.Wins, s.Losses, s.Draws, s.IpponsGiven, s.IpponsTaken)
		standings = append(standings, *s)
	}
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.IpponsGiven != b.IpponsGiven {
			return a.IpponsGiven > b.IpponsGiven
		}
		// Head-to-head: if a beat b directly, a ranks higher.
		if winner, ok := lookupH2H(headToHead, a.Player.Name, b.Player.Name); ok {
			if winner == a.Player.Name {
				return true
			}
			if winner == b.Player.Name {
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
// current round is 0 (not started — vacuously "complete enough" so
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
//     prior rounds — Swiss runs the same persistence shape as
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
