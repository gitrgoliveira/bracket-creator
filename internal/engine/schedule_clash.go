package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MinClashFootprintMinutes is the minimum time footprint assigned to a
// competition for court-clash detection. A competition's real duration comes
// from its schedule estimate, which is ~0 before it has a roster; flooring at
// this value means even empty competitions occupy a slot, so overlaps are
// caught while the operator is still laying out the schedule. Mirrors the
// create-form auto-stack minimum block (MIN_STACK_BLOCK_MIN in admin_setup.jsx).
const MinClashFootprintMinutes = 30

// ClashWarning describes a court (shiaijo) scheduling conflict between the
// queried competition and another competition: they run on the same date,
// share at least one court, and their time windows overlap.
type ClashWarning struct {
	OtherCompID   string   `json:"otherCompId"`
	OtherCompName string   `json:"otherCompName"`
	Date          string   `json:"date"`
	SharedCourts  []string `json:"sharedCourts"`
	OverlapStart  string   `json:"overlapStart"` // HH:MM
	OverlapEnd    string   `json:"overlapEnd"`   // HH:MM
}

// DetectClashesForCompetition returns the court-scheduling clashes between the
// competition identified by compID and every other competition. Two
// competitions clash when they share a date, share at least one court, and
// their [start, start+footprint) windows overlap. Footprint is
// max(estimated duration, MinClashFootprintMinutes).
//
// Returns an empty (non-nil) slice when there are no clashes. A competition
// that cannot be placed on a timeline — no date or an unparseable start time —
// is skipped rather than reported. Clashes are sorted by the other
// competition's name for a stable UI/test order.
func (e *Engine) DetectClashesForCompetition(compID string) ([]ClashWarning, error) {
	target, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	out := []ClashWarning{}

	tStart, ok := parseHHMM(target.StartTime)
	if !ok || strings.TrimSpace(target.Date) == "" {
		// Target can't be placed on a timeline → nothing to compare against.
		return out, nil
	}
	tEnd := tStart + e.clashFootprintMinutes(compID)

	// Resolve courts the same way the draw paths do: an empty competition
	// court list inherits the tournament's courts (or ["A"]). Without this a
	// legacy competition saved before court materialization — nil Courts —
	// would silently never clash even though at draw time it occupies a real
	// court. A LoadTournament failure leaves tourn nil; resolveClashCourts
	// then falls back to ["A"], matching resolveCompetitionCourts.
	tourn, _ := e.store.LoadTournament()
	targetCourts := resolveClashCourts(target.Courts, tourn)

	ids, err := e.store.ListCompetitions()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if id == compID {
			continue
		}
		other, err := e.store.LoadCompetition(id)
		if err != nil || other == nil {
			// Skip an unreadable competition rather than failing the whole
			// check — a single bad file shouldn't hide every other clash.
			continue
		}
		if !sameDate(target.Date, other.Date) {
			continue
		}
		shared := sharedCourts(targetCourts, resolveClashCourts(other.Courts, tourn))
		if len(shared) == 0 {
			continue
		}
		oStart, ok := parseHHMM(other.StartTime)
		if !ok {
			continue
		}
		oEnd := oStart + e.clashFootprintMinutes(id)
		// Half-open overlap: [tStart,tEnd) ∩ [oStart,oEnd) is non-empty.
		if tStart < oEnd && oStart < tEnd {
			out = append(out, ClashWarning{
				OtherCompID:   other.ID,
				OtherCompName: other.Name,
				Date:          other.Date,
				SharedCourts:  shared,
				OverlapStart:  fmtHHMM(max(tStart, oStart)),
				OverlapEnd:    fmtHHMM(min(tEnd, oEnd)),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OtherCompName < out[j].OtherCompName })
	return out, nil
}

// clashFootprintMinutes is the time a competition occupies for clash detection:
// max(estimated total duration, MinClashFootprintMinutes). It never errors — an
// estimate failure (config issue / missing roster) falls back to the floor so a
// single un-estimatable competition can't break the whole clash check.
func (e *Engine) clashFootprintMinutes(compID string) int {
	est, err := e.EstimateScheduleForCompetition(compID)
	if err != nil || est.TotalDurationMinutes < MinClashFootprintMinutes {
		return MinClashFootprintMinutes
	}
	return est.TotalDurationMinutes
}

// parseHHMM parses a "HH:MM" clock string into minutes-since-midnight. Returns
// ok=false for empty or malformed input (so callers can skip unplaceable
// competitions rather than treating them as midnight).
func parseHHMM(s string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// fmtHHMM renders minutes-since-midnight as "HH:MM". It does NOT wrap at 24h:
// a footprint that pushes the overlap end past midnight renders as "24:15"
// rather than a misleading "00:15", so an operator reading the warning sees
// the next-day rollover. The value is display-only — no caller parses it back.
func fmtHHMM(mins int) string {
	if mins < 0 {
		mins = 0
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// resolveClashCourts mirrors resolveCompetitionCourts (handlers_tournament.go):
// an empty competition court list inherits the tournament's courts, falling
// back to ["A"] when there is no tournament. Kept as a separate engine-local
// copy because internal/engine cannot import internal/mobileapp (import cycle).
func resolveClashCourts(compCourts []string, tourn *state.Tournament) []string {
	if len(compCourts) > 0 {
		return compCourts
	}
	if tourn != nil && len(tourn.Courts) > 0 {
		return append([]string(nil), tourn.Courts...)
	}
	return []string{"A"}
}

// sameDate is true when both dates are non-empty and equal (after trimming).
// An empty date means the competition isn't placed on a day, so it can't clash.
func sameDate(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && a == b
}

// sharedCourts returns the sorted intersection of two court-label lists.
// Deleting matched entries from the set as we go both de-dups (a repeated
// label in b is only emitted once) and avoids a second bookkeeping map.
func sharedCourts(a, b []string) []string {
	inA := make(map[string]struct{}, len(a))
	for _, c := range a {
		inA[c] = struct{}{}
	}
	out := []string{}
	for _, c := range b {
		if _, ok := inA[c]; ok {
			out = append(out, c)
			delete(inA, c)
		}
	}
	sort.Strings(out)
	return out
}
