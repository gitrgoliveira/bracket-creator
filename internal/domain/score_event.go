package domain

import (
	"errors"
	"fmt"
	"strconv"
)

// A score event is one tap on a running match's score sheet (bc-evts,
// operator ruling 2026-10-12: "timing should be per tap/event and not per
// snapshot"). The events of a match are kept in its event log
// (state.ScoreLogLine) and folded, in (Stamp, ID) order, onto the latest
// baseline of each group by engine.ReplayScoring. This file is the one owner
// of what an event may say and of the per-bout fold; the JS mirror
// (web-mobile/js/score_events.jsx) is pinned to it by the golden table
// internal/engine/testdata/score_replay.json.

// ScoreEventKind names what a tap did. The names are dotted
// ("<what>.<how>") and travel verbatim on the wire and in the log.
type ScoreEventKind string

const (
	// ScoreIpponAdd strikes Letter for Side on bout Pos.
	ScoreIpponAdd ScoreEventKind = "ippon.add"
	// ScoreIpponRemove takes off the mark whose id is Target (D2: a removal
	// names its mark, so two devices removing two marks at once remove both).
	ScoreIpponRemove ScoreEventKind = "ippon.remove"
	// ScoreFoulAdd adds a hansoku for Side; the second outstanding one lands an
	// H for the opponent (ApplyFoulIncrement), whose mark id is this event's.
	ScoreFoulAdd ScoreEventKind = "foul.add"
	// ScoreFoulRemove takes one outstanding hansoku off Side.
	ScoreFoulRemove ScoreEventKind = "foul.remove"
	// ScoreEnchoAdd / ScoreEnchoRemove raise or lower the overtime count.
	ScoreEnchoAdd    ScoreEventKind = "encho.add"
	ScoreEnchoRemove ScoreEventKind = "encho.remove"
	// ScoreFlagAdd / ScoreFlagRemove move one engi referee flag for Side.
	ScoreFlagAdd    ScoreEventKind = "flag.add"
	ScoreFlagRemove ScoreEventKind = "flag.remove"
	// ScoreBoutDecision sets a team bout's decision: Value "hikiwake" (the
	// operator's Tie), "fusensho" (Side is the side AWARDED the bout, as the
	// team editor's Fusensho button names it), or "clear".
	ScoreBoutDecision ScoreEventKind = "bout.decision"
	// ScoreBoutFighter names the fighter of Side on a kachinuki row after the
	// first (MemberID and Name).
	ScoreBoutFighter ScoreEventKind = "bout.fighter"
	// ScoreRepPick picks Side's representative for the representative bout
	// (MemberID; empty clears the pick).
	ScoreRepPick ScoreEventKind = "rep.pick"
	// ScorePoolRep names Side's fighter for a pool representative or
	// tie-break bout (Name).
	ScorePoolRep ScoreEventKind = "pool.rep"
	// ScoreDaihyosenVerdict records the judges' decision on the tied
	// representative bout for Side, or clears it (Value "clear").
	ScoreDaihyosenVerdict ScoreEventKind = "daihyosen.verdict"
)

// The bout decision values a ScoreBoutDecision event may carry.
const (
	BoutDecisionHikiwake = "hikiwake"
	BoutDecisionFusensho = "fusensho"
	BoutDecisionClear    = "clear"
)

// ScoreSideA and ScoreSideB spell a side on an event, as the JS editors do:
// "a" is the match's SideA (Aka), "b" its SideB (Shiro).
const (
	ScoreSideA = "a"
	ScoreSideB = "b"
)

// MaxEngiFlags is the most referee flags one side of an engi match can hold
// (a five-referee panel). It moved here from admin_scoring_engi.jsx's
// MAX_FLAGS: Go owns the rule, the JS mirrors it (constitution II).
const MaxEngiFlags = 5

// Event field bounds. An event is small; a field past these is a malformed
// request, refused before anything is logged.
const (
	MaxScoreEventIDLen    = 128
	MaxScoreEventFieldLen = 256
)

// The outcomes the replay gives each event (engine.ReplayScoring). They are
// recomputed from the whole log at every replay, never stored, so the order
// events arrive in cannot change them.
const (
	// ScoreOutcomeApplied: the event is in the match.
	ScoreOutcomeApplied = "applied"
	// ScoreOutcomeRecorded: a baseline built with this event in view already
	// holds it (the basis contains it), so it is not folded a second time.
	ScoreOutcomeRecorded = "recorded"
	// ScoreOutcomeDuplicate: an event with this id is already in the log.
	ScoreOutcomeDuplicate = "duplicate"
	// ScoreOutcomeCapped: the side, or the bout, already holds the most
	// points it can (D3); nothing changed.
	ScoreOutcomeCapped = "capped"
	// ScoreOutcomeBeforeBaseline: the event was made before a baseline that
	// was built without it (D5), so the baseline's value stands.
	ScoreOutcomeBeforeBaseline = "beforeBaseline"
	// ScoreOutcomeMatchClosed: a finish or a decision stamped BEFORE this tap
	// closed the match; a tap stamped after it is kept in the history and
	// changes nothing. Correct the result to change a closed match.
	ScoreOutcomeMatchClosed = "matchClosed"
	// ScoreOutcomeNoTarget: the mark, foul, flag, overtime period or bout the
	// event names is not there.
	ScoreOutcomeNoTarget = "noTarget"
	// ScoreOutcomeNotAllowed: the event is not one this match or bout takes
	// (a fusensho against a side that already won the bout, a verdict on an
	// untied bout, a match-level overtime on a kachinuki encounter, the maru
	// of a fusensho removed as a point).
	ScoreOutcomeNotAllowed = "notAllowed"
)

// The outcomes the score-events door gives an event it does NOT log, so the
// replay never sees them. The device is told, and may send the event again
// with the same id once the cause is gone (a clock relearned).
const (
	// ScoreOutcomeNotStarted: the match is not running (not yet started, or
	// sent back to the queue), so a tap has nothing to score (Q3).
	ScoreOutcomeNotStarted = "notStarted"
	// ScoreOutcomeRefused: a rule of the door refused it (the reason says
	// which: a self-run participant's limits, a knockout match after it that
	// a change here would reach, a log that cannot be read).
	ScoreOutcomeRefused = "refused"
	// ScoreOutcomeClockSkew: the event's stamp is too far in the server's
	// future to be ordered; the device relearns its clock and re-stamps it.
	ScoreOutcomeClockSkew = "clock_skew"
)

var errScoreEvent = errors.New("score event")

// Validate checks the event's own shape. It says nothing about the match the
// event is for: the replay judges that.
func (e ScoreEvent) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w %q: %s", errScoreEvent, e.ID, fmt.Sprintf(format, a...))
	}
	if e.ID == "" || len(e.ID) > MaxScoreEventIDLen {
		return bad("id must be 1 to %d characters", MaxScoreEventIDLen)
	}
	for name, v := range map[string]string{"session": e.Session, "letter": e.Letter, "target": e.Target, "value": e.Value, "memberId": e.MemberID, "name": e.Name} {
		if len(v) > MaxScoreEventFieldLen {
			return bad("%s is longer than %d characters", name, MaxScoreEventFieldLen)
		}
	}
	if e.Stamp <= 0 {
		return bad("stamp must be positive")
	}
	if e.Seq < 0 {
		return bad("seq must not be negative")
	}
	if e.Pos < DaihyosenBoutPos {
		return bad("pos %d is not a bout", e.Pos)
	}
	sideNeeded := true
	switch e.Kind {
	case ScoreIpponAdd:
		if !IsScoringIppon(e.Letter) || e.Letter == DefaultWinIppon || len([]rune(e.Letter)) != 1 {
			return bad("letter %q is not a strike", e.Letter)
		}
	case ScoreIpponRemove:
		sideNeeded = false
		if e.Target == "" {
			return bad("a removal names its mark (target)")
		}
	case ScoreFoulAdd, ScoreFoulRemove, ScoreFlagAdd, ScoreFlagRemove, ScoreRepPick, ScorePoolRep, ScoreBoutFighter:
	case ScoreEnchoAdd, ScoreEnchoRemove:
		sideNeeded = false
	case ScoreBoutDecision:
		switch e.Value {
		case BoutDecisionHikiwake, BoutDecisionClear:
			sideNeeded = false
		case BoutDecisionFusensho:
		default:
			return bad("bout decision %q is not hikiwake, fusensho or clear", e.Value)
		}
		if e.Pos == 0 {
			return bad("a bout decision names a bout")
		}
	case ScoreDaihyosenVerdict:
		if e.Value == BoutDecisionClear {
			sideNeeded = false
		} else if e.Value != "" {
			return bad("verdict value %q is not empty or clear", e.Value)
		}
		if e.Pos != DaihyosenBoutPos {
			return bad("a verdict is for the representative bout (pos %d)", DaihyosenBoutPos)
		}
	default:
		return bad("unknown kind %q", e.Kind)
	}
	if sideNeeded && e.Side != ScoreSideA && e.Side != ScoreSideB {
		return bad("side %q is not a or b", e.Side)
	}
	if !sideNeeded && e.Side != "" && e.Side != ScoreSideA && e.Side != ScoreSideB {
		return bad("side %q is not a or b", e.Side)
	}
	return nil
}

// DaihyosenBoutPos is the position of a team match's representative bout,
// the same value as state.DaihyosenSubPosition (domain cannot import state).
const DaihyosenBoutPos = -1

// ScoreEvent is one tap. Pos 0 is the match itself (an individual or engi
// match, or a team encounter's own overtime), n a numbered team bout, -1 the
// representative bout. ID is opaque and minted by the device; it never
// embeds the device's revision session (that is stripped from public
// payloads, and mark ids carry event ids to every viewer). Seq counts the
// events of one Session on one match from 1, so a gap can be seen
// (engine.ScoreLogGaps); the order the replay folds in is (Stamp, ID) alone.
type ScoreEvent struct {
	ID       string         `json:"id"`
	Session  string         `json:"session,omitempty"`
	Seq      int64          `json:"seq,omitempty"`
	Stamp    int64          `json:"stamp"`
	Kind     ScoreEventKind `json:"kind"`
	Pos      int            `json:"pos,omitempty"`
	Side     string         `json:"side,omitempty"`
	Letter   string         `json:"letter,omitempty"`
	Target   string         `json:"target,omitempty"`
	Value    string         `json:"value,omitempty"`
	MemberID string         `json:"memberId,omitempty"`
	Name     string         `json:"name,omitempty"`
}

// StruckIppons is a side's ippons without the maru a decision awarded and
// without placeholders or the hantei mark: the points the competitor
// actually struck (FIK Art. 32, "any point scored ... shall remain valid").
// engine.struckIppons and the JS struckIppons (result_slot.jsx) answer the
// same.
func StruckIppons(ippons []string) []string {
	var out []string
	for _, v := range ippons {
		if IsScoringIppon(v) && v != DefaultWinIppon {
			out = append(out, v)
		}
	}
	return out
}

// BoutScore is one bout's scoring as the fold sees it: the ippon slices with
// the id of each mark beside it (Marks[i] names Ippons[i]; "" for a
// placeholder), the OUTSTANDING hansoku counts (an H already landed is in
// the opponent's slice), the overtime count, and the team editor's Tie and
// Fusensho states. A match-level individual score is a BoutScore whose Draw
// and Fusensho stay unset.
type BoutScore struct {
	IpponsA, IpponsB []string
	MarksA, MarksB   []string
	FoulsA, FoulsB   int
	Encho            int
	Draw             bool
	// Fusensho is the side AWARDED a per-bout fusensho ("a"/"b"), "" for none.
	Fusensho string
	// pre is the score before the fusensho was given (the team editor's
	// _preFusensho), replay-internal: a toggle-off restores it. It is never
	// persisted, so a fusensho a baseline brought ends through ClearFusensho.
	pre *BoutScore
}

// Clone deep-copies the score.
func (b BoutScore) Clone() BoutScore {
	c := b
	c.IpponsA, c.IpponsB = cloneSlice(b.IpponsA), cloneSlice(b.IpponsB)
	c.MarksA, c.MarksB = cloneSlice(b.MarksA), cloneSlice(b.MarksB)
	if b.pre != nil {
		p := b.pre.Clone()
		c.pre = &p
	}
	return c
}

func cloneSlice(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

func (b *BoutScore) side(s string) (ippons, marks *[]string, fouls *int) {
	if s == ScoreSideA {
		return &b.IpponsA, &b.MarksA, &b.FoulsA
	}
	return &b.IpponsB, &b.MarksB, &b.FoulsB
}

func otherSide(s string) string {
	if s == ScoreSideA {
		return ScoreSideB
	}
	return ScoreSideA
}

// alignMarks pads or trims a mark slice to its ippon slice, so Marks[i]
// always names Ippons[i].
func alignMarks(ippons, marks []string) []string {
	out := make([]string, len(ippons))
	copy(out, marks)
	return out
}

// appendMark is AppendIppon with the mark id kept beside the entry: it fills
// the first placeholder slot, else appends.
func appendMark(ippons, marks []string, entry, id string) ([]string, []string) {
	marks = alignMarks(ippons, marks)
	for i, v := range ippons {
		if v == "" || v == IpponPlaceholder {
			out := AppendIppon(ippons, entry)
			m := cloneSlice(marks)
			m[i] = id
			return out, m
		}
	}
	return AppendIppon(ippons, entry), append(cloneSlice(marks), id)
}

// struckWithMarks is StruckIppons keeping each kept entry's mark id.
func struckWithMarks(ippons, marks []string) ([]string, []string) {
	marks = alignMarks(ippons, marks)
	var outI, outM []string
	for i, v := range ippons {
		if IsScoringIppon(v) && v != DefaultWinIppon {
			outI = append(outI, v)
			outM = append(outM, marks[i])
		}
	}
	return outI, outM
}

// Points is the scoring count of each side (CountScoringIppons).
func (b BoutScore) Points() (int, int) {
	return CountScoringIppons(b.IpponsA), CountScoringIppons(b.IpponsB)
}

// AddIppon strikes letter for side, the mark named id. A side at
// MaxIpponsPerSide, or a strike that would make the impossible 2-2 (the
// first side to two decided the bout), is capped and changes nothing.
func (b *BoutScore) AddIppon(side, letter, id string) string {
	ip, _, _ := b.side(side)
	oip, _, _ := b.side(otherSide(side))
	// Each side counts only the points it struck: a fusensho's maru are no
	// strikes, and they go as soon as this strike lands, on either side
	// (ApplyBoutScoreEdit).
	mine, theirs := len(StruckIppons(*ip)), len(StruckIppons(*oip))
	if mine >= MaxIpponsPerSide || theirs >= MaxIpponsPerSide {
		return ScoreOutcomeCapped
	}
	next := b.Clone()
	ip, mk, _ := next.side(side)
	*ip, *mk = appendMark(*ip, *mk, letter, id)
	*b = ApplyBoutScoreEdit(*b, next)
	return ScoreOutcomeApplied
}

// RemoveMark takes off the mark named target, on either side. The maru of a
// fusensho is not a point the operator struck and is not removed this way
// (the bout decision's clear ends it).
func (b *BoutScore) RemoveMark(target string) string {
	for _, s := range []string{ScoreSideA, ScoreSideB} {
		ip, mk, _ := b.side(s)
		marks := alignMarks(*ip, *mk)
		for i, id := range marks {
			if id != target {
				continue
			}
			if (*ip)[i] == DefaultWinIppon {
				return ScoreOutcomeNotAllowed
			}
			next := b.Clone()
			nip, nmk, _ := next.side(s)
			*nip = append(cloneSlice((*ip)[:i]), (*ip)[i+1:]...)
			*nmk = append(cloneSlice(marks[:i]), marks[i+1:]...)
			*b = ApplyBoutScoreEdit(*b, next)
			return ScoreOutcomeApplied
		}
	}
	return ScoreOutcomeNoTarget
}

// AddFoul adds one hansoku for side (ApplyFoulIncrement); the H it may land
// for the opponent is the mark named id.
func (b *BoutScore) AddFoul(side, id string) string {
	next := b.Clone()
	ip, _, fouls := next.side(side)
	oip, omk, _ := next.side(otherSide(side))
	f, landsH := ApplyFoulIncrement(*fouls, CountScoringIppons(*oip), CountScoringIppons(*ip))
	*fouls = f
	if landsH {
		*oip, *omk = appendMark(*oip, *omk, "H", id)
	}
	*b = ApplyBoutScoreEdit(*b, next)
	return ScoreOutcomeApplied
}

// RemoveFoul takes one outstanding hansoku off side. An H already landed is a
// mark of the opponent's and is removed by its id.
func (b *BoutScore) RemoveFoul(side string) string {
	_, _, fouls := b.side(side)
	if *fouls <= 0 {
		return ScoreOutcomeNoTarget
	}
	next := b.Clone()
	_, _, nf := next.side(side)
	*nf--
	*b = ApplyBoutScoreEdit(*b, next)
	return ScoreOutcomeApplied
}

// ApplyFoulIncrement is the one hansoku rule (the JS applyFoulIncrement,
// admin_scoring_shared.jsx): the second outstanding foul lands an H for the
// opponent and the count goes back to 0; when either side already holds
// MaxIpponsPerSide the count still goes back to 0 but no H lands.
func ApplyFoulIncrement(fouls, opponentPoints, ownPoints int) (int, bool) {
	next := fouls + 1
	if next < 2 {
		return next, false
	}
	if opponentPoints >= MaxIpponsPerSide || ownPoints >= MaxIpponsPerSide {
		return 0, false
	}
	return 0, true
}

// AdjustEncho moves the overtime count by delta (±1), floored at 0.
func (b *BoutScore) AdjustEncho(delta int) string {
	if b.Encho+delta < 0 {
		return ScoreOutcomeNoTarget
	}
	b.Encho += delta
	return ScoreOutcomeApplied
}

// ClearFusensho ends a fusensho: the awarded side keeps only the points it
// struck (its maru go), the other side keeps its own (the JS clearFusensho).
func ClearFusensho(b BoutScore) BoutScore {
	out := b.Clone()
	out.pre = nil
	if b.Fusensho == "" {
		return out
	}
	ip, mk, _ := out.side(b.Fusensho)
	*ip, *mk = struckWithMarks(*ip, *mk)
	out.Fusensho = ""
	return out
}

// fusenshoBase is the score a fusensho is given over: the snapshot taken when
// one was given, else the score without its fusensho.
func fusenshoBase(b BoutScore) BoutScore {
	if b.pre != nil {
		return b.pre.Clone()
	}
	return ClearFusensho(b)
}

// FusenshoAllowed says whether side may be awarded the bout: not when the
// other side already struck MaxIpponsPerSide (the circles against two points
// would be a 2-2). The JS fusenshoAllowed.
func FusenshoAllowed(b BoutScore, side string) bool {
	if b.Fusensho == side {
		return true
	}
	base := fusenshoBase(b)
	ip, _, _ := base.side(otherSide(side))
	return len(StruckIppons(*ip)) < MaxIpponsPerSide
}

// SetFusensho awards side the bout by fusensho (the toggle-on half of the JS
// applyFusenshoToggle): one maru per point (DefaultWinIppons, one in
// overtime), the other side keeps the points it struck, both foul counts and
// the Tie go. The maru marks are named id, id:1. Awarding the side already
// awarded changes nothing.
func (b *BoutScore) SetFusensho(side, id string) string {
	if b.Fusensho == side {
		return ScoreOutcomeApplied
	}
	if !FusenshoAllowed(*b, side) {
		return ScoreOutcomeNotAllowed
	}
	snap := fusenshoBase(*b)
	next := b.Clone()
	next.FoulsA, next.FoulsB, next.Draw = 0, 0, false
	next.pre = &snap
	maru := DefaultWinIppons(b.Encho > 0)
	marks := make([]string, len(maru))
	for i := range maru {
		marks[i] = id
		if i > 0 {
			marks[i] = id + ":" + strconv.Itoa(i)
		}
	}
	wip, wmk, _ := next.side(side)
	*wip, *wmk = maru, marks
	sip, smk, _ := snap.side(otherSide(side))
	oip, omk, _ := next.side(otherSide(side))
	*oip, *omk = struckWithMarks(*sip, *smk)
	next.Fusensho = side
	*b = next
	return ScoreOutcomeApplied
}

// ClearBoutDecision is the bout decision's "clear": a fusensho goes back to
// the score it was given over (the toggle-off half of applyFusenshoToggle),
// and the Tie goes.
func (b *BoutScore) ClearBoutDecision() string {
	if b.Fusensho != "" {
		if b.pre != nil {
			p := b.pre.Clone()
			p.Encho = b.Encho
			*b = p
		} else {
			*b = ClearFusensho(*b)
		}
	}
	b.Draw = false
	b.pre = nil
	return ScoreOutcomeApplied
}

// SetDraw marks the bout a Tie (the JS setDrawFor, as a set rather than a
// toggle): any fusensho ends first.
func (b *BoutScore) SetDraw() string {
	*b = ClearFusensho(*b)
	b.Draw = true
	return ScoreOutcomeApplied
}

// ApplyBoutScoreEdit is the one rule for a scoring edit on a bout (the JS
// applyBoutScoreEdit): taking a mark off the side a fusensho went against is
// a correction and keeps it (the snapshot follows it); every other edit ends
// the fusensho (ClearFusensho, applied last, so its maru never survive as
// ordinary points) and the Tie.
func ApplyBoutScoreEdit(prev, next BoutScore) BoutScore {
	if removesLosingSideMark(prev, next) {
		if next.pre != nil {
			p := next.pre.Clone()
			lost := otherSide(prev.Fusensho)
			pip, pmk, _ := p.side(lost)
			nip, nmk, _ := next.side(lost)
			*pip, *pmk = cloneSlice(*nip), cloneSlice(*nmk)
			next.pre = &p
		}
		return next
	}
	out := ClearFusensho(next)
	out.Draw = false
	return out
}

func removesLosingSideMark(prev, next BoutScore) bool {
	if prev.Fusensho == "" || next.Fusensho != prev.Fusensho {
		return false
	}
	won, lost := prev.Fusensho, otherSide(prev.Fusensho)
	pw, _, _ := prev.side(won)
	nw, _, _ := next.side(won)
	pl, _, _ := prev.side(lost)
	nl, _, _ := next.side(lost)
	return equalStrings(*pw, *nw) && prev.FoulsA == next.FoulsA && prev.FoulsB == next.FoulsB && len(*nl) < len(*pl)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BoutWinnerSide is the one winner rule for a team bout (the JS
// boutWinnerSide, admin_scoring_team.jsx): a hantei on a tied bout names its
// side, a Tie names none, a fusensho names the awarded side, else the side
// with more points. "" for no winner.
func BoutWinnerSide(aCount, bCount int, draw bool, fusensho, hanteiSide string) string {
	if (hanteiSide == ScoreSideA || hanteiSide == ScoreSideB) && aCount == bCount {
		return hanteiSide
	}
	if draw {
		return ""
	}
	if fusensho == ScoreSideA || fusensho == ScoreSideB {
		return fusensho
	}
	switch {
	case aCount > bCount:
		return ScoreSideA
	case bCount > aCount:
		return ScoreSideB
	}
	return ""
}

// Played says whether anything happened on the bout (the JS
// subBoutHasBeenPlayed).
func (b BoutScore) Played() bool {
	return len(b.IpponsA) > 0 || len(b.IpponsB) > 0 || b.FoulsA > 0 || b.FoulsB > 0 || b.Fusensho != "" || b.Draw || b.Encho > 0
}

// AdjustFlags moves one engi side's flag count by delta (±1) within
// 0..MaxEngiFlags.
func AdjustFlags(flags, delta int) (int, string) {
	next := flags + delta
	if next > MaxEngiFlags {
		return flags, ScoreOutcomeCapped
	}
	if next < 0 {
		return flags, ScoreOutcomeNoTarget
	}
	return next, ScoreOutcomeApplied
}
