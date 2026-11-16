package mobileapp

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// loadCourtQueueEntries gathers the court queue entries of other competitions
// on the same day as served with a draw (preview brackets expose no public
// matches), eligibility-stamped exactly as a viewer payload stamps its own, so
// stampCourtQueuePositions can number a competition's matches court-wide. The
// served competition itself and competitions on other days are skipped before
// loading matches. Each competition is loaded on its own goroutine (safeGo),
// and the entries keep the order of ListCompetitions.
// A read-only pass through the public Store loads, each taking and releasing
// its own lock: never call it from inside Store.WithTransaction, whose
// competition lock a load of that competition would wait on. One
// competition's read fault is logged and that competition skipped; the
// viewer payload never fails for it. A panic in a load is returned as the
// error, for the caller to answer as an internal error.
func loadCourtQueueEntries(store *state.Store, served *state.Competition) ([]state.CourtQueueEntry, error) {
	ids, err := store.ListCompetitions()
	if err != nil {
		log.Printf("mobileapp: court queue: list competitions: %v", err)
		return nil, nil
	}

	loaded := make([][]state.CourtQueueEntry, len(ids))
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]
	for i, id := range ids {
		if served != nil && id == served.ID {
			continue
		}
		idx, compID := i, id
		safeGo(&wg, &panicRef, func() {
			loaded[idx] = otherCompetitionQueueEntries(store, compID, served)
		})
	}
	wg.Wait()
	if p := panicRef.Load(); p != nil {
		return nil, p
	}

	var allEntries []state.CourtQueueEntry
	for _, entries := range loaded {
		allEntries = append(allEntries, entries...)
	}
	return allEntries, nil
}

// otherCompetitionQueueEntries is loadCourtQueueEntries' read of one
// competition: nil when it fails to load, is on another day than served, or
// holds no entries.
func otherCompetitionQueueEntries(store *state.Store, id string, served *state.Competition) []state.CourtQueueEntry {
	comp, err := viewerLoadCompetition(store, id)
	if err != nil {
		log.Printf("mobileapp: court queue %s: load competition: %v", id, err)
		return nil
	}
	// state.CourtQueueEntriesOf owns the setup rule (no entries); this
	// early exit only spares reading the matches it would ignore.
	if comp == nil || comp.Status == state.CompStatusSetup {
		return nil
	}
	if served != nil && !state.SameQueueDay(comp.Date, served.Date) {
		return nil
	}
	poolMatches, err := store.LoadPoolMatches(id)
	if err != nil {
		log.Printf("mobileapp: court queue %s: load pool matches: %v", id, err)
		return nil
	}
	bracket, err := store.LoadBracket(id)
	if err != nil {
		log.Printf("mobileapp: court queue %s: load bracket: %v", id, err)
		return nil
	}
	bracket = stampViewerEligibility(store, id, poolMatches, bracket)
	return state.CourtQueueEntriesOf(comp, poolMatches, bracket)
}

// stampCourtQueuePositions derives court-wide queue positions across every
// competition on that court on the same day and stamps them onto poolMatches
// and bracket in-place.
func stampCourtQueuePositions(comp *state.Competition, poolMatches []state.MatchResult, bracket *state.Bracket, all []state.CourtQueueEntry) {
	ownEntries := state.CourtQueueEntriesOf(comp, poolMatches, bracket)
	if len(ownEntries) == 0 {
		return
	}
	compID := ""
	if comp != nil {
		compID = comp.ID
	}
	combined := make([]state.CourtQueueEntry, 0, len(ownEntries)+len(all))
	combined = append(combined, ownEntries...)
	for _, e := range all {
		if compID != "" && e.CompetitionID == compID {
			continue
		}
		combined = append(combined, e)
	}

	applyQueuePositions(poolMatches, bracket, state.CourtQueuePositions(combined))
}

// applyQueuePositions stamps positions, laid out as state.CourtQueueEntriesOf
// lays out the competition's entries (pool matches, then the bracket in draw
// order), onto its matches.
func applyQueuePositions(poolMatches []state.MatchResult, bracket *state.Bracket, positions []int) {
	for i := range poolMatches {
		poolMatches[i].QueuePosition = positions[i]
	}
	offset := len(poolMatches)
	for i, bm := range state.BracketMatchesInDrawOrder(bracket) {
		bm.QueuePosition = positions[offset+i]
	}
}

// anyMatchToAnnotate reports whether poolMatches or bracket carry a match
// annotateEligibility can act on: a SCHEDULED match with both side ids
// stamped (its ineligibleSides stamp; engine.BarredSides needs an id to look
// anyone up), a COMPLETED match a withdrawal or default win decided (its
// withdrawnStatus stamp, which the score editor words its clear from, so it
// must be there once every match is finished too), or a pool ippon-shobu bout
// finished with no winner (its ineligibleSides stamp, which says whether both
// sides are barred, the draw nobody can fight). Gates the
// LoadCompetitorStatus read in the viewer handlers behind it, so a
// competition with none never pays for a status-file read it cannot use.
func anyMatchToAnnotate(poolMatches []state.MatchResult, bracket *state.Bracket) bool {
	acts := func(status state.MatchStatus, decision, idA, idB string) bool {
		switch status {
		case state.MatchStatusScheduled:
			return idA != "" && idB != ""
		case state.MatchStatusCompleted:
			return domain.IsDefaultWinDecisionStr(decision)
		}
		return false
	}
	for i := range poolMatches {
		m := &poolMatches[i]
		if acts(m.Status, m.Decision, m.SideAID, m.SideBID) || drawnPoolTiebreak(m) {
			return true
		}
	}
	if bracket == nil {
		return false
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			bm := &bracket.Rounds[ri][mi]
			if acts(bm.Status, bm.Decision, bm.SideAID, bm.SideBID) {
				return true
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && acts(bm.Status, bm.Decision, bm.SideAID, bm.SideBID) {
		return true
	}
	return false
}

// drawnPoolTiebreak reports a pool tie-break bout (an ippon-shobu "-TB-" or a
// representative "-DH-" bout) finished with no winner: the one finished match
// that carries the ineligibleSides stamp, because two barred sides are what
// make it a draw nobody fought (engine.drawnTiebreakOfBarredSides).
func drawnPoolTiebreak(m *state.MatchResult) bool {
	return m.Status == state.MatchStatusCompleted && m.Winner == "" && state.IsPoolSupplementaryMatchID(m.ID)
}

// annotateEligibility stamps state.IneligibleSidesAnnotation (bc-cse) on
// every SCHEDULED pool match and bracket match whose resolved side ids are
// currently barred by a withdrawal recorded on a DIFFERENT match
// (engine.BarredSides, the SAME check StartMatchTx gates a write on), so a
// match LIST can grey/skip the row without attempting the write first. On a
// COMPLETED match a withdrawal or default win decided it stamps
// state.WithdrawnStatusAnnotation instead: where the withdrawn side's
// competitor status stands now, which the editor's clear control words its
// consequence from.
//
// Read-only, request-time only, exactly like stampCourtQueuePositions: called
// on the copy a viewer endpoint is about to serve, NEVER on a slice/bracket
// destined for a save, which is what keeps the annotation off
// pool-matches.csv and bracket.json (see the field's own doc comment,
// models.go). MUST run BEFORE stampCourtQueuePositions, which reads the stamp
// back to decide whether a barred match counts toward the queue (position 0 for one).
func annotateEligibility(poolMatches []state.MatchResult, bracket *state.Bracket, statuses map[string]domain.CompetitorStatus) {
	if len(statuses) == 0 {
		return
	}
	// stampable: the match is one whose barred sides are stamped (scheduled, or a
	// drawn tie-breaker bout, which may be fought again once a side is reinstated).
	stamp := func(stampable bool, matchID, sideAID, sideBID string) *state.IneligibleSidesAnnotation {
		if !stampable {
			return nil
		}
		a, b := engine.BarredSides(statuses, matchID, sideAID, sideBID)
		if a == nil && b == nil {
			return nil
		}
		// A side is stamped ONLY when its barring reason actually parses
		// (SplitStatusReason's ok); Go's IneligibleSidesAnnotation.A/B carry
		// `json:"...,omitempty"`, so an unparsed side left at its zero value
		// "" is indistinguishable on the wire from one never barred at all.
		// A caller that checks the annotation by nil-ness alone (this
		// match's row exists, so it must be barred) would then disagree
		// with one that checks the field VALUE (no "a"/"b" key present, so
		// not barred) -- bc-cse: stamping an all-unparsed, still-non-nil
		// {} annotation is exactly that trap. When NEITHER side's reason
		// parses, the whole annotation is left nil so both kinds of caller
		// agree the match is not barred.
		out := &state.IneligibleSidesAnnotation{}
		parsed := false
		if a != nil {
			if decision, _, ok := domain.SplitStatusReason(a.Reason); ok {
				out.A = decision
				parsed = true
			}
		}
		if b != nil {
			if decision, _, ok := domain.SplitStatusReason(b.Reason); ok {
				out.B = decision
				parsed = true
			}
		}
		if !parsed {
			return nil
		}
		return out
	}
	// The withdrawn side is the one decisionBy names, else the side that did
	// not win (withdrawnSideKey, ineligible_match.jsx, reads it the same way).
	withdrawn := func(status state.MatchStatus, decision, decisionBy, winnerID, sideAID, sideBID string) *state.WithdrawnStatusAnnotation {
		if status != state.MatchStatusCompleted || !domain.IsDefaultWinDecisionStr(decision) {
			return nil
		}
		var id string
		switch {
		case decisionBy == "aka":
			id = sideAID
		case decisionBy == "shiro":
			id = sideBID
		case winnerID != "" && winnerID == sideAID:
			id = sideBID
		case winnerID != "" && winnerID == sideBID:
			id = sideAID
		}
		st, ok := statuses[id]
		if id == "" || !ok {
			return nil
		}
		return &state.WithdrawnStatusAnnotation{Eligible: st.Eligible, MatchID: st.MatchID, Reinstateable: st.Reinstateable}
	}
	for i := range poolMatches {
		m := &poolMatches[i]
		m.IneligibleSides = stamp(m.Status == state.MatchStatusScheduled || drawnPoolTiebreak(m), m.ID, m.SideAID, m.SideBID)
		m.WithdrawnStatus = withdrawn(m.Status, m.Decision, m.DecisionBy, m.WinnerID, m.SideAID, m.SideBID)
	}
	if bracket == nil {
		return
	}
	stampBracket := func(bm *state.BracketMatch) {
		bm.IneligibleSides = stamp(bm.Status == state.MatchStatusScheduled, bm.ID, bm.SideAID, bm.SideBID)
		bm.WithdrawnStatus = withdrawn(bm.Status, bm.Decision, bm.DecisionBy, bm.WinnerID, bm.SideAID, bm.SideBID)
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			stampBracket(&bracket.Rounds[ri][mi])
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil {
		stampBracket(bm)
	}
}

// stampWithdrawnStatus gives a match write's result, before it is pushed, the
// withdrawnStatus stamp the viewer payloads carry (annotateEligibility). A
// host applies the push at once and refetches a moment later, so without it a
// score editor left on a withdrawal it had just recorded worded the clear from
// no stamp until the refetch landed. Only a completed withdrawal or default
// win carries the stamp, so only those pay for the status read.
func stampWithdrawnStatus(store matchPushStore, compID string, r *state.MatchResult) {
	if r.Status != state.MatchStatusCompleted || !domain.IsDefaultWinDecisionStr(r.Decision) {
		return
	}
	statuses, err := store.LoadCompetitorStatus(compID)
	if err != nil {
		log.Printf("mobileapp: push %s/%s: load competitor status: %v", compID, r.ID, err)
		return
	}
	one := []state.MatchResult{*r}
	annotateEligibility(one, nil, statuses)
	r.WithdrawnStatus = one[0].WithdrawnStatus
}
