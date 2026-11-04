package engine

import (
	"fmt"
	"log"
	"sort"

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
// their placement windows overlap on a shared court.
//
// Returns an empty (non-nil) slice when there are no clashes. A competition
// that cannot be placed on a timeline (no date or an unparseable start time)
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

	tourn, tournErr := e.store.LoadTournament()
	inheritedFromUnreadable := false
	if tournErr != nil && len(target.Courts) == 0 {
		inheritedFromUnreadable = true
	}
	defer func() {
		if inheritedFromUnreadable {
			log.Printf("engine: clash check for %q: tournament unreadable, inherited courts are taken as [\"A\"]: %v", compID, tournErr)
		}
	}()

	targetPlace := e.PlaceCompetition(target, tourn)
	if !targetPlace.Placed {
		// Target can't be placed on a timeline → nothing to compare against.
		return out, nil
	}

	targetCourts := make([]string, 0, len(targetPlace.Windows))
	for c := range targetPlace.Windows {
		targetCourts = append(targetCourts, c)
	}
	sort.Strings(targetCourts)

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
			// check, a single bad file shouldn't hide every other clash.
			continue
		}
		if !state.SameDate(target.Date, other.Date) {
			continue
		}
		if tournErr != nil && len(other.Courts) == 0 {
			inheritedFromUnreadable = true
		}
		otherPlace := e.PlaceCompetition(other, tourn)
		if !otherPlace.Placed {
			continue
		}

		var clashingCourts []string
		overlapStart := -1
		overlapEnd := -1
		for _, c := range targetCourts {
			wO, okO := otherPlace.Windows[c]
			if !okO {
				continue
			}
			wT := targetPlace.Windows[c]
			if overlaps(wT, wO) {
				clashingCourts = append(clashingCourts, c)
				oS := max(wT.Start, wO.Start)
				oE := min(wT.End, wO.End)
				if overlapStart == -1 || oS < overlapStart {
					overlapStart = oS
				}
				if overlapEnd == -1 || oE > overlapEnd {
					overlapEnd = oE
				}
			}
		}

		if len(clashingCourts) > 0 {
			out = append(out, ClashWarning{
				OtherCompID:   other.ID,
				OtherCompName: other.Name,
				Date:          other.Date,
				SharedCourts:  clashingCourts,
				OverlapStart:  fmtHHMM(overlapStart),
				OverlapEnd:    fmtHHMM(overlapEnd),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OtherCompName < out[j].OtherCompName })
	return out, nil
}

// fmtHHMM renders minutes-since-midnight as "HH:MM". It does NOT wrap at 24h:
// a footprint that pushes the overlap end past midnight renders as "24:15"
// rather than a misleading "00:15", so an operator reading the warning sees
// the next-day rollover. The value is display-only, no caller parses it back.
func fmtHHMM(mins int) string {
	if mins < 0 {
		mins = 0
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}
