package mobileapp

import (
	"errors"
	"log"
	"net/http"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// holdSelfReportedWriteUnderTx is the one judge of an anonymous self-run score
// write. It runs inside WithTransaction (under the per-comp lock), so the
// stored match it is handed (snap, found: read once for every guard of the
// write by the caller) cannot change before the write lands. It
// refuses the write or lets it through, and it can rewrite it on the way: a
// representative bout the organiser decided is written back exactly as
// stored. In order:
//
//   - A match that has finished is the organiser's to correct
//     (errResultFinalized), except that an exact resend of the finish already
//     recorded (engine.ExactScoreReplay) passes: the engine answers it as
//     recorded and writes nothing to the match (bc-xrpl).
//   - An encounter has one representative bout: a write listing two is
//     refused (errDuplicateRepBout). A participant's write never creates one
//     (only the add route does, with its tie, pool, kachinuki, engi and
//     eligibility checks), so a row for a representative bout the match does
//     not have is one the write inherited, from a sheet a moment behind a
//     remove made elsewhere or a queued write replayed after it. The row is
//     dropped and logged, as stripInvalidHantei drops what a write inherited,
//     and the rest of the write goes on to be written as usual, the timestamp
//     guard deciding a stale one. A write that finishes the match is refused
//     instead (errRepBoutRemoved), since a finish may take its winner from
//     that row. Nor does it remove one (only the remove route does, with its
//     unscored check): a write listing the bouts without the row the match
//     has comes from a sheet a moment behind an add, so the stored row is
//     kept, scored or not, and a finish is refused (errRepBoutAdded).
//   - Who a write seats is not judged here. It is judged on what the write
//     LANDS, after the engine has merged it and in the same transaction
//     (landedMembersRefusal): the engine rewrites a write before its own merge
//     (a kachinuki bout log is merged by position, member ids are filled in,
//     the winner is worked out), so a reading of the payload can disagree with
//     what is stored.
//   - Once the organiser recorded a judges' decision on the representative
//     bout, the write's row must send it back (sameHanteiVerdict) or leave the
//     row's ippons out (engine.KeepsStoredDaihyosenVerdict), a write listing
//     bouts must list that row, and a winner the write names, by name or by
//     id, must be the side the decided bout names; otherwise
//     repBoutHanteiRefusal. The row is then replaced by a copy of the stored
//     one, its points, fouls, overtime, sub-decision and names included, so a
//     participant can finish on the decided bout but never change it.
//   - With none recorded, a row carrying the judges'-decision mark would record
//     one (repBoutHanteiRefusal).
//
// The write's winner is read against the STORED sides and their ids, never the
// ones the write sends, through the attribution the engine uses (ids first), so
// an id naming the other side cannot pass behind a matching name. A start
// (startOnly) keeps the stored bouts whatever it sends (engine.keepQueuedScore),
// so its sub-results are not judged, and the engine derives its winner from
// the bouts it keeps.
//
// Fails closed: the caller's read of the snapshot rejects the request on a load
// error rather than allowing the write (matchSnapshotOrErr). A match in neither
// store (found false) passes on purpose: the write that follows under the same
// lock refuses it (the engine's errMatchNotFound), so nothing can land on it.
func holdSelfReportedWriteUnderTx(compID, matchID string, snap matchSnapshot, found bool, result *state.MatchResult, comp *state.Competition, startOnly bool, score *seenResultCheck) error {
	if !found {
		return nil
	}
	if isMatchFinalized(snap.Status) {
		// The participant's own finish sent again because its answer was
		// lost (the offline queue replays it with the stamp it was sent
		// with) is no change to a finished match: the engine answers it as
		// recorded and writes nothing (engine.ExactScoreReplay, the same
		// probe over the same stored match, under this same lock, runs first
		// in RecordMatchResultWithIneligibilityTx). The rewrites below shape
		// a write that will land, so they have nothing to do here. Any
		// resend that differs where it would land is no such replay and is
		// refused as before.
		if engine.ExactScoreReplay(snap.Stored, result, comp, !snap.InBracket, score.probeLines(snap, result)) {
			return nil
		}
		return errResultFinalized
	}
	var subs []state.SubMatchResult
	if !startOnly {
		subs = result.SubResults
	}
	row := state.DaihyosenSubIndex(subs)
	if row >= 0 && state.DaihyosenSubIndex(subs[row+1:]) >= 0 {
		return errDuplicateRepBout
	}
	stored := snap.RepBout
	if row >= 0 && stored == nil {
		if result.Status == state.MatchStatusCompleted {
			return errRepBoutRemoved
		}
		// Into a fresh slice, which stays a list (never nil) even when the row
		// was all it held: nil would tell the engine to keep the stored bouts.
		kept := make([]state.SubMatchResult, 0, len(subs)-1)
		result.SubResults = append(append(kept, subs[:row]...), subs[row+1:]...)
		log.Printf("mobileapp: dropped the representative bout from a self-run score write to %s/%s because the match has none; the rest of the write was kept", compID, matchID)
		return nil
	}
	if stored != nil && row < 0 && subs != nil && !stored.HanteiDecided() {
		if result.Status == state.MatchStatusCompleted {
			return errRepBoutAdded
		}
		storedRow := state.CloneSubResults([]state.SubMatchResult{*stored})[0]
		result.SubResults = append(slices.Clone(subs), storedRow)
		log.Printf("mobileapp: kept the representative bout a self-run score write to %s/%s left out; the rest of the write was kept", compID, matchID)
		return nil
	}
	if stored == nil || !stored.HanteiDecided() {
		if row >= 0 && subs[row].HanteiDecided() {
			return repBoutHanteiRefusal(false)
		}
		return nil
	}
	switch {
	case row >= 0:
		in := &subs[row]
		if in.HanteiDecided() && !sameHanteiVerdict(stored, in) ||
			!in.HanteiDecided() && !engine.KeepsStoredDaihyosenVerdict(stored, in) {
			return repBoutHanteiRefusal(true)
		}
	case subs != nil:
		return repBoutHanteiRefusal(true) // listing the bouts without it drops it
	}
	if result.Winner != "" || result.WinnerID != "" {
		claimed := snap.Pairing
		claimed.Winner, claimed.WinnerID = result.Winner, result.WinnerID
		side := domain.AttributeWinnerSide(claimed)
		if side == domain.MatchSideNone || side != state.SubBoutWinnerSide(*stored, snap.Pairing.SideA, snap.Pairing.SideB) {
			return repBoutHanteiRefusal(true)
		}
	}
	if row >= 0 {
		// A fresh slice and a deep copy: result.SubResults shares the
		// request's backing array, and the engine fills free ippon slots in
		// place, which must never reach the stored row.
		kept := slices.Clone(subs)
		kept[row] = state.CloneSubResults([]state.SubMatchResult{*stored})[0]
		result.SubResults = kept
	}
	return nil
}

// errRepMemberNotInTeam refuses a representative pick, a participant's or the
// organiser's (landedMembersRefusal), that names a member the side's team does
// not hold (400 team_member_not_in_team). The lineup refusal
// (errLineupMemberNotInTeam) says the same thing about a fielded position.
var errRepMemberNotInTeam = &selfRunRefusal{
	status:  http.StatusBadRequest,
	code:    codeTeamMemberNotInTeam,
	message: "The representative chosen is not on this team. Pick again from the list.",
}

// landedWriteRefusal carries the refusal of a write judged on what it LANDED
// out of the write's transaction, as an error. Returning it is what aborts the
// transaction: the engine's match write, its history line and its eligibility
// record are all staged in that transaction, so a refusal stored into the
// caller's engErr from inside it would commit them instead. The caller unwraps
// it once WithTransaction has returned and answers it like any other refusal.
type landedWriteRefusal struct{ refusal *selfRunRefusal }

func (e *landedWriteRefusal) Error() string { return e.refusal.Error() }

func (e *landedWriteRefusal) Unwrap() error { return e.refusal }

// introducesJudgedMember is the gate in front of landedMembersRefusal: it says
// whether a score write, as the judges before the engine leave it (subs),
// introduces a member id on a row that judge looks at, against the match as it
// stood before the write (before, the snapshot read at the top of it). Those
// rows are judgedRows', the selection the judge (membersOutsideTeams) reads too:
// the representative bout's, for anyone's write (either side's pick, against
// before.RepBout, the row the judge compares it with), and with numbered a
// participant's numbered rows (either fighter and the winner, against the stored
// row at the same Position). A position the stored match lacks holds no id, so
// every id its row carries is an introduction.
//
// The invariant that makes the gate equivalent to judging every write: the
// engine never lands a member id the judge would refuse that the payload did
// not introduce. It adds an id to a row only by copying the one the stored row
// holds at that position (preserveKachinukiMemberIDs), by deriving the
// winner's from the row's own landed side ids (ResolveMemberWinnerID,
// ReconcileWinnerMemberID), or, in a kachinuki match, by its own seating from
// the lineup the match fields, its empty places filled by member number
// (engine.kachinukiFielded): bout 1's seat and the rename along its winner's
// streak (ReseatKachinukiOpenersTx), and the pairing Record bout appends
// (MaybeAdvanceKachinuki). A seated id is always one the side's own team holds,
// so the judge accepts it at any position (pinned in the engine by
// TestEngineLandsNoMemberIDThePayloadDidNotIntroduce). The seat owns bout 1: a
// payload's foreign id there does not land, and so is never refused. So a write
// that introduces none leaves the staged match unread. The echo is the common case: the team
// editor stamps member ids on every row that has a lineup, and a gate that
// fired on any id would parse pool-matches.csv (and bracket.json for a knockout
// match) again under the competition's write lock on nearly every autosave.
//
// Only the ids are compared, never a team id from before.Pairing: the judge
// reads a side's team from the LANDED pairing, and on the pool branch a payload
// side id over an id-less stored side lands (reconcileSides), so a gate keyed on
// the stored team ids would skip exactly that write.
func introducesJudgedMember(subs []state.SubMatchResult, before matchSnapshot, numbered bool) bool {
	for _, j := range judgedRows(subs, before, numbered) {
		if introducesFighter(j.row, j.stored) || (!j.rep && introducesWinnerMember(j.row, j.stored)) {
			return true
		}
	}
	return false
}

// judgedRow is a row the member judge looks at with the stored row it compares
// the row with, and whether it is the representative bout's.
type judgedRow struct {
	row, stored state.SubMatchResult
	rep         bool
}

// judgedRows is the ONE selection of what the member judge looks at, read by the
// gate (introducesJudgedMember, on the payload's rows) and by the judge
// (membersOutsideTeams, on the landed rows), so a row added to one is added to
// the other. Per row, not per field: the winner check needs the row's two side
// ids with the stored row's as its fallback. Every representative-bout row is
// judged for anyone, against before.RepBout (the row the judge compares a pick
// with, the empty row when the match has none); with numbered, a participant's
// write, every other row too, against the stored row at its Position
// (state.SubResultAt: a position the stored match lacks holds no id, so every id
// its row carries is an introduction). Rows come in the order given.
func judgedRows(subs []state.SubMatchResult, before matchSnapshot, numbered bool) []judgedRow {
	var storedRows []state.SubMatchResult
	if before.Stored != nil {
		storedRows = before.Stored.SubResults
	}
	var out []judgedRow
	for _, row := range subs {
		switch {
		case row.Position == state.DaihyosenSubPosition:
			var stored state.SubMatchResult
			if before.RepBout != nil {
				stored = *before.RepBout
			}
			out = append(out, judgedRow{row: row, stored: stored, rep: true})
		case numbered:
			out = append(out, judgedRow{row: row, stored: state.SubResultAt(storedRows, row.Position)})
		}
	}
	return out
}

// introducesFighter reports whether a row puts a member id on either side that
// the stored row at the same position does not already hold.
func introducesFighter(row, stored state.SubMatchResult) bool {
	return introducesID(row.SideAMemberID, stored.SideAMemberID) ||
		introducesID(row.SideBMemberID, stored.SideBMemberID)
}

// landedMembersRefusal judges who a score write SEATS by what it landed, read
// back through stx after the engine wrote it (a StoreTx reads its own staged
// bytes), against the match as it stood before (before, the snapshot read once
// at the top of the write). Introduced means the stored row now holds an id it
// did not hold before at that position: an id the row already had is its own,
// whoever seated it, and a write answers for what it introduces, not for what it
// inherited. In the match as it now stands:
//
//   - a representative bout's pick must be on its side's team
//     (errRepMemberNotInTeam), for any caller, as the lineup's member check is
//     (handlers_lineup.go: the pick names a SIDE, whose team a correction
//     elsewhere can change under an open editor, and every surface resolves the
//     bare id through the side's team);
//   - with numbered, a participant's write: each numbered row's fighters must be
//     on their side's team (errBoutFighterNotInTeam), and a winner member id the
//     row now holds must be one of its two fighters (errWinnerMemberNotInBout).
//     The organiser's numbered rows are not judged (the lineup's always-editable
//     rule, mp-q722).
//
// It judges the landed match rather than the payload because the engine rewrites
// a write before its own merge: a kachinuki bout log is merged by position,
// member ids are filled in and the winner is worked out, and two merge rules
// decide from that rewritten content (the newer scoring a finish displaces, a
// finish held whole). A reading of the payload could skip a row the real write
// landed. A group the merge does not land is the stored one in the landed
// match, so it introduces nothing and is never judged: a stale change the write
// NAMES is kept in the history and answered in heldGroups, never refused, and
// one it does not name is copied from the stored match, neither written nor
// reported. Only team membership is read, never the match's status, so the
// organiser can pick again at once, on a finished match too.
//
// A refusal is returned as a *landedWriteRefusal and the caller returns it from
// the transaction, which writes nothing. The caller runs this only for a write
// the engine accepted (a superseded or rolled-back write must still commit its
// history and its restore) and that introduces a judged member id
// (introducesJudgedMember). A members file that cannot be read refuses a write
// that needs it, terminally (errTeamMembersUnreadable): nothing is written,
// which the offline queue reads as final.
func landedMembersRefusal(stx state.StoreTx, compID, matchID string, before matchSnapshot, numbered bool) error {
	after, found, err := matchSnapshotOrErr(stx, compID, matchID, "member")
	if err != nil || !found {
		return err
	}
	judge := &memberJudge{stx: stx, compID: compID}
	err = membersOutsideTeams(judge, before, after, numbered)
	var refusal *selfRunRefusal
	if errors.As(err, &refusal) {
		return &landedWriteRefusal{refusal}
	}
	return err
}

// membersOutsideTeams is landedMembersRefusal's judgement: it answers the first
// refusal, an error of another kind being a read that failed. The rows it
// judges are judgedRows' over the landed rows, the same selection the gate
// (introducesJudgedMember) makes over the payload's.
func membersOutsideTeams(judge *memberJudge, before, after matchSnapshot, numbered bool) error {
	for _, j := range judgedRows(after.Stored.SubResults, before, numbered) {
		refusal := errRepMemberNotInTeam
		if !j.rep {
			if err := winnerMemberOutsideBout(j.row, j.stored); err != nil {
				return err
			}
			refusal = errBoutFighterNotInTeam
		}
		if err := rowMembersOutsideTeams(judge, after.Pairing, j.row, j.stored, refusal); err != nil {
			return err
		}
	}
	return nil
}

// errBoutFighterNotInTeam refuses a participant's numbered bout row that seats a
// fighter the side's team does not hold (400 team_member_not_in_team, the code
// the lineup and the representative pick answer too). A numbered row carries
// its fighters' member ids from the editor (a kachinuki pairing's pick, the
// fixed-order name box), and every surface reads them through the side's team.
var errBoutFighterNotInTeam = &selfRunRefusal{
	status:  http.StatusBadRequest,
	code:    codeTeamMemberNotInTeam,
	message: "The fighter chosen is not on this team. Pick again from the list.",
}

// errWinnerMemberNotInBout refuses a participant's numbered bout row whose
// winner member is not one of the row's two fighters. The winner id is read
// first by domain.AttributeWinnerSide, so an id that names no fighter of the
// bout decides who won it, and who stayed on in a kachinuki encounter.
var errWinnerMemberNotInBout = &selfRunRefusal{
	status:  http.StatusBadRequest,
	code:    "winner_member_not_in_bout",
	message: "The winner recorded is not one of the two fighters in this bout.",
}

// errTeamMembersUnreadable refuses a write that needs the team members to judge
// a choice when the competition's team-members.yaml cannot be read: 409, never
// the 500 the offline queue retries forever, so the operator is told once and
// retries after the file is repaired. The sentence is the page's own and says
// what to check, since a participant cannot open the data folder; the read's
// error is logged where it happens (memberJudge.holds).
var errTeamMembersUnreadable = &selfRunRefusal{
	status:  http.StatusConflict,
	code:    "team_members_unreadable",
	message: "The team members could not be read, so this choice cannot be checked. The organiser needs to check team-members.yaml in the tournament data.",
}

// memberJudge is what the member checks of ONE score write share: the
// transaction they run in, and the competition's team members, each team's
// member ids built at most once, and only when a row introduces a member id
// to judge. A write that introduces none never reads the file, so a corrupt
// team-members.yaml blocks no ordinary scoring; one that introduces several
// reads it once.
type memberJudge struct {
	stx    state.StoreTx
	compID string

	read   bool
	squads map[string][]domain.TeamMember
	err    error

	// ids is each team's member ids (teamMemberIDs), built on the first ask.
	ids map[string]map[string]bool
}

// holds reports whether the team holds the member (teamMemberIDs, the one
// answer to that question). A members file that cannot be read is
// errTeamMembersUnreadable, logged with the file's name, for the rest of the
// write.
func (j *memberJudge) holds(teamID, memberID string) (bool, error) {
	if !j.read {
		j.read = true
		if j.squads, j.err = j.stx.LoadSquads(j.compID); j.err != nil {
			log.Printf("mobileapp: team-members.yaml for %s cannot be read: %v", j.compID, j.err)
		}
	}
	if j.err != nil {
		return false, errTeamMembersUnreadable
	}
	held, ok := j.ids[teamID]
	if !ok {
		if j.ids == nil {
			j.ids = make(map[string]map[string]bool)
		}
		held = teamMemberIDs(j.squads, teamID)
		j.ids[teamID] = held
	}
	return held[memberID], nil
}

// name is the stored name of a member the team holds, "" when it holds no
// member of that id or the member has no name yet. It reads the members file
// the way holds does, so a write that asks both reads it once.
func (j *memberJudge) name(teamID, memberID string) (string, error) {
	held, err := j.holds(teamID, memberID)
	if err != nil || !held {
		return "", err
	}
	for _, m := range j.squads[teamID] {
		if m.ID == memberID {
			return m.Name, nil
		}
	}
	return "", nil
}

// introducesMember reports whether a write puts a member id on a side that the
// stored row does not already hold, on a side whose team is known. A write
// answers for what it introduces, not for what it inherited (the write guard's
// rule): the stored id is the row's own, whoever seated it (a correction since
// seated another team), and a side with no team id has nothing to judge a
// member against.
func introducesMember(teamID, memberID, storedID string) bool {
	return teamID != "" && introducesID(memberID, storedID)
}

// introducesID reports whether a write's member id is one the stored row at the
// same position does not hold: the test every judge and the gate in front of
// them (introducesJudgedMember) share.
func introducesID(memberID, storedID string) bool {
	return memberID != "" && memberID != storedID
}

// rowMembersOutsideTeams judges the member ids a landed bout row names against
// the team that holds each side, for the representative bout and the numbered
// rows alike (membersOutsideTeams). Ids are bare UUIDs, so the team's own
// members are the only thing that says whose they are: a member of another
// team, or an id no team holds, is refused (refusal) rather than kept. A side
// is judged only for what the row introduces (introducesMember). stored is the
// row at the same position before the write, an empty row when the match had
// none.
func rowMembersOutsideTeams(j *memberJudge, pairing domain.WinnerAttribution, row, stored state.SubMatchResult, refusal *selfRunRefusal) error {
	for _, side := range []struct {
		teamID, memberID, storedID string
	}{
		{pairing.SideAID, row.SideAMemberID, stored.SideAMemberID},
		{pairing.SideBID, row.SideBMemberID, stored.SideBMemberID},
	} {
		if !introducesMember(side.teamID, side.memberID, side.storedID) {
			continue
		}
		held, err := j.holds(side.teamID, side.memberID)
		if err != nil {
			return err
		}
		if !held {
			return refusal
		}
	}
	return nil
}

// winnerMemberOutsideBout judges the winner member a landed numbered row
// INTRODUCES (it differs from the row's at the same position before the write):
// it must be empty or one of the row's two fighters, a side the landed row
// carries no id for counting as the fighter the row held there before. A winner id
// the stored row already holds is never judged, whoever it names: a legacy row,
// a correction that seated another team and a renamed member leave such ids
// behind, and refusing them would refuse every later write of the match. Pure:
// it reads no store.
func winnerMemberOutsideBout(row, stored state.SubMatchResult) error {
	if !introducesWinnerMember(row, stored) {
		return nil
	}
	winner := row.WinnerMemberID
	a, b := row.SideAMemberID, row.SideBMemberID
	if a == "" {
		a = stored.SideAMemberID
	}
	if b == "" {
		b = stored.SideBMemberID
	}
	if winner == a || winner == b {
		return nil
	}
	return errWinnerMemberNotInBout
}

// introducesWinnerMember reports whether a row's winner member id is one the
// write changes against the stored row at the same position, as
// introducesMember does for a side's fighter.
func introducesWinnerMember(row, stored state.SubMatchResult) bool {
	return introducesID(row.WinnerMemberID, stored.WinnerMemberID)
}
