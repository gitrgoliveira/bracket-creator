// Package mobileapp, handlers_lineup.go owns the
// `/api/competitions/:cid/teams/:tid/lineups/:round` endpoints
// (Slice 7.B / T127).
//
// GET returns a teamLineupRead for a (team, round) tuple (200, saved false
// when nothing is stored; 404 only for an unknown competition). PUT
// sets/replaces it, for round 0 only (the team's starting lineup: a later round
// is a 400, lineupRoundRefused), DELETE removes it. A PUT, of either scope, may
// name the positions it changed (LineupRequest.Changed) and then lands only
// those on the lineup stored when it arrives (lineupSave.base). A third public GET,
// .../lineup-in-force/:matchId, answers which lineup the team fields at a match
// (see lineupInForceRead), and a fourth, .../matches/:mid/kachinuki-roster, a
// kachinuki encounter's fighters not yet retired (engine.KachinukiRoster).
//
// All store I/O goes through the TeamLineupStore + CompetitionStore
// interfaces (deps.go) rather than the concrete *state.Store
// (NFR-002). The handler needs CompetitionStore to look up the
// competition's TeamSize, which TeamLineup.ValidatePositions uses to
// check that submitted position KEYS are valid for that size — it
// enforces no completeness or vacancy rule (mp-gmcg: team sizes are
// unregulated and a partial lineup must be persistable). The public GETs
// also use it to turn an unknown competition id into a 404, rather than
// silently reading an empty map off a directory that was never created.
package mobileapp

import (
	"errors"
	"log"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// lineupRoundRefused is the 400 a PUT of a lineup for round 1 or later answers.
// Releases up to v2.1.1 saved one from the Lineups page; the state layer gives a
// team that has one a lineup for each match it is seated in
// (state.settleRoundLineups), and a team now carries the lineup of its previous
// match instead.
const lineupRoundRefused = "A lineup is saved as the team's starting lineup or for a match."

// lineupSetStatus maps a SetTeamLineup error to the right HTTP status. Domain
// lineup validation failures (a position key not valid for the team size, or a
// non-positive team size) all carry the "team_lineup:" prefix and are client
// errors (400). Anything else is a server fault (YAML parse / disk I/O) and must
// be a 500 so a real failure is not misreported as a bad request. compID is
// already validated upstream by requireValidCompID, so ValidateCompetitionID
// cannot be the source here.
//
// CONTRACT: this classification relies on the "team_lineup:" prefix being at
// position 0 of the message. The store path (SetTeamLineup -> setTeamLineupLocked
// and the domain ValidatePositions in internal/domain/team_lineup.go) returns
// these validation errors with the prefix FIRST. Most are plain sentinels or
// %q-%v fmt.Errorf; checkDuplicateMembers does use %w (so callers can match
// ErrLineupDuplicateMember), which is fine here ONLY because it still puts
// "team_lineup:" at position 0. Do NOT add context in FRONT of the prefix,
// as fmt.Errorf("lineup %s: %w", id, err) would: that falls through to 500.
// If a producer ever needs a leading context, switch this to a typed error
// checked via errors.As instead of a prefix match.
func lineupSetStatus(err error) int {
	if strings.HasPrefix(err.Error(), "team_lineup:") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// LineupRequest is the body for PUT /lineups/:round and the match-scoped
// PUT /match-lineups/:matchId. teamID, round/matchID, and compID are pinned
// by the URL path.
//
// MemberIDs (bc-tmid pass 3) is the id half of a lineup, keyed by the SAME
// Position as Positions: a client sets both together, the name for display
// and the id for identity. It is entirely optional on the wire: an older
// client that only ever sent "positions" leaves MemberIDs nil, which
// ShouldBindJSON leaves as a nil map, domain.TeamLineup.MemberIDs then
// stores nil, and json/yaml omitempty both drop it -- so an old client's
// write behaves exactly as it did before this field existed. Its keys are
// validated by the same domain.TeamLineup.ValidatePositions call the
// Positions keys already went through (it walks MemberIDs as its own loop
// beside Positions), so an invalid position key here is rejected exactly
// like an invalid Positions key, with no separate check needed here.
//
// Changed (operator decision 2026-10-07, "Only changed positions") names the
// positions the sender changed, so two devices changing different positions of
// one lineup both keep their change. Absent, the body is the whole lineup and
// replaces the stored one, which is how a save queued by an older build replays.
// Present, it must list at least one position: the server then reads only those
// keys, whatever else the body holds (the client may send its whole form), and
// lands them on the lineup it reads under the write's lock (lineupSave.base). A
// pointer, because an empty list is a refusal and an absent one is not.
type LineupRequest struct {
	Positions map[domain.Position]string `json:"positions"`
	MemberIDs map[domain.Position]string `json:"memberIds,omitempty"`
	Changed   *[]domain.Position         `json:"changed,omitempty"`
	// ChangedAt is when each changed position was changed on the device
	// (server-relative ms), so a change made before a kachinuki match started
	// reaches the lineup the match started with whenever it arrives
	// (engine.FollowLineupSave). A save without it (an older build's) is
	// ordered by its arrival, as every lineup save was.
	ChangedAt map[domain.Position]int64 `json:"changedAt,omitempty"`
	// WriteID is the client's id for the save (bc-evts stage 3 review 11,
	// Y8): a save this door has applied under it is answered as recorded,
	// never applied again (writeOnce), so a late duplicate puts nothing back.
	WriteID string `json:"writeId,omitempty"`
}

// changedPositions are the positions the save names as changed, and whether it
// names any list at all: absent means the body is the whole lineup.
func (r LineupRequest) changedPositions() (changed []domain.Position, partial bool) {
	if r.Changed == nil {
		return nil, false
	}
	return *r.Changed, true
}

// memberIDsRead is the member ids a save is judged on, the positions it reads:
// those of the changed positions when it names them, every one when it is the
// whole lineup.
func (r LineupRequest) memberIDsRead() map[domain.Position]string {
	changed, partial := r.changedPositions()
	if !partial {
		return r.MemberIDs
	}
	read := make(map[domain.Position]string, len(changed))
	for _, p := range changed {
		if id := r.MemberIDs[p]; id != "" {
			read[p] = id
		}
	}
	return read
}

// validLineupRequest answers 400, and reports false, for a save no write could
// make sense of before it reads any lineup: a list of changed positions that is
// empty, or a name longer than a competitor's (MaxLenPlayerName) in any position
// the save stores, which is every position of a whole lineup and only the changed
// ones of a partial save (the rest of its body is ignored). Both lineup PUTs ask
// it, as both store the names they are sent.
func validLineupRequest(c *gin.Context, req LineupRequest) bool {
	if err := validWriteID(req.WriteID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	changed, partial := req.changedPositions()
	if partial && len(changed) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": domain.ErrLineupNoChangedPositions.Error()})
		return false
	}
	stored := req.Positions
	if partial {
		stored = make(map[domain.Position]string, len(changed))
		for _, p := range changed {
			if name, ok := req.Positions[p]; ok {
				stored[p] = name
			}
		}
	}
	for pos, name := range stored {
		if err := validateMaxLen("positions."+string(pos), name, MaxLenPlayerName); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return false
		}
	}
	return true
}

// RegisterPublicLineupHandlers wires the read-only GET
// /competitions/:id/teams/:tid/lineups/:round,
// /competitions/:id/teams/:tid/match-lineups/:matchId and
// /competitions/:id/teams/:tid/lineup-in-force/:matchId endpoints, and the
// kachinuki queue read /competitions/:id/matches/:mid/kachinuki-roster, on an
// unauthenticated router group. Lineup data (position assignments) is not
// sensitive, coaches and viewers can see who plays where, and the
// AdminLineup form needs to load the current lineup without holding
// admin credentials for the initial read. PUT and DELETE remain on the
// admin group via RegisterLineupHandlers.
//
// The first two GETs answer with a teamLineupRead and the third with a
// lineupInForceRead (see their docs); a 404 means the competition does not
// exist. The kachinuki roster read is public for the same reason, and because
// the self-run public score sheet mounts the same team editor that shows it.
//
// Slice 7.B / T127.
func RegisterPublicLineupHandlers(r *gin.RouterGroup, store TeamLineupStore, comps CompetitionStore, eng LineupEngine) {
	r.GET("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
			return
		}
		lineups, err := store.LoadTeamLineups(compID)
		if err != nil {
			internalError(c, err)
			return
		}
		lineup, found := findRoundLineup(lineups, teamID, round)
		if !found {
			lineup = domain.TeamLineup{TeamID: teamID, CompetitionID: compID, Round: round, Positions: map[domain.Position]string{}}
		}
		c.JSON(http.StatusOK, teamLineupRead{TeamLineup: lineup, Saved: found})
	})

	// Match-scoped read (mp-825): exactly the lineup saved for this match,
	// never a lineup carried from elsewhere. Whatever a team carries into a
	// match is read from the lineup-in-force route below.
	r.GET("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
			return
		}
		lineups, err := store.LoadTeamLineups(compID)
		if err != nil {
			internalError(c, err)
			return
		}
		lineup, found := findMatchLineup(lineups, teamID, matchID)
		if !found {
			lineup = domain.TeamLineup{TeamID: teamID, CompetitionID: compID, MatchID: matchID, Positions: map[domain.Position]string{}}
		}
		c.JSON(http.StatusOK, teamLineupRead{TeamLineup: lineup, Saved: found})
	})

	// The lineup in force (operator ruling 2026-10-05): the match's own lineup,
	// else the one the team carries from its previous match or round. The
	// engine owns the rule; this is the one door every surface reads it
	// through. Same contract as the two reads above: a competition that exists
	// always answers 200, with `saved: false` when no lineup applies.
	r.GET("/competitions/:id/teams/:tid/lineup-in-force/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
			return
		}
		in, err := eng.LineupInForce(compID, teamID, matchID)
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, newLineupInForceRead(compID, teamID, matchID, in))
	})

	// The kachinuki queue (bc-kfnl, operator ruling 2026-09-24): each side's
	// fighters who have not retired, read through the same engine call that
	// appends the next pairing, for the advisory line the score sheet shows
	// above the live bout. Read-only and advisory: nothing is decided on it.
	// 200 with each side's {lineupFound, on, remaining}; 404 for an unknown
	// competition or match; 400 for a competition that is not kachinuki; 500
	// when the lineups cannot be read.
	r.GET("/competitions/:id/matches/:mid/kachinuki-roster", func(c *gin.Context) {
		compID, ok := requireValidCompID(c)
		if !ok {
			return
		}
		matchID := c.Param("mid")
		if matchID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "match ID is required"})
			return
		}
		// The engine reads config.md once and answers an unknown competition or
		// match with a NotFoundError naming which, so the handler reads nothing
		// of its own and answers with that error's own words.
		roster, err := eng.KachinukiRoster(compID, matchID)
		var notFound *engine.NotFoundError
		switch {
		case errors.As(err, &notFound):
			c.JSON(http.StatusNotFound, gin.H{"error": notFound.Error()})
		case errors.Is(err, engine.ErrNotKachinuki):
			c.JSON(http.StatusBadRequest, gin.H{"error": "this competition's team matches are not kachinuki"})
		case err != nil:
			internalError(c, err)
		default:
			c.JSON(http.StatusOK, roster)
		}
	})
}

// lineupInForceRead is the response body of the lineup-in-force GET. It embeds
// the stored lineup that is in force, so its own `matchId`/`round` are the
// source's, and adds the same `saved` marker teamLineupRead carries plus the
// source spelled out: `sourceMatchId` when the lineup was saved for a match
// (the one asked about, or an earlier match of the team it is carried from),
// `sourceRound` when it is a Lineups-page lineup (round 0 is the starting
// lineup; a pointer, because 0 is a value to send). Neither is present when
// nothing is saved.
type lineupInForceRead struct {
	domain.TeamLineup
	Saved         bool   `json:"saved"`
	SourceMatchID string `json:"sourceMatchId,omitempty"`
	SourceRound   *int   `json:"sourceRound,omitempty"`
}

// newLineupInForceRead shapes the engine's answer. A team with nothing in
// force gets an empty lineup that echoes what was asked, as the other reads do.
func newLineupInForceRead(compID, teamID, matchID string, in engine.InForceLineup) lineupInForceRead {
	if !in.Found {
		return lineupInForceRead{TeamLineup: domain.TeamLineup{
			TeamID: teamID, CompetitionID: compID, MatchID: matchID, Positions: map[domain.Position]string{},
		}}
	}
	out := lineupInForceRead{TeamLineup: in.Lineup, Saved: true, SourceMatchID: in.Source.MatchID}
	if out.Positions == nil {
		out.Positions = map[domain.Position]string{}
	}
	if in.Source.MatchID == "" {
		round := in.Source.Round
		out.SourceRound = &round
	}
	return out
}

// teamLineupRead is the response body for both public lineup GETs (bc-k404,
// operator decision 2026-09-27: "there either is one or there isn't and
// it's empty"). It embeds domain.TeamLineup and adds ONE field, Saved:
// whether a lineup is actually stored at the (team, round) or (team,
// matchId) key asked for. `saved` is the sole marker of "nothing here" --
// an empty positions map is not one (a saved lineup can legitimately be
// empty), and echoing the requested round/matchId is not one either (the
// unsaved body does that too, so the caller can tell what it asked for).
// Never omitempty: the caller must always be able to read it.
//
// Embedding domain.TeamLineup is safe today because neither it nor
// domain.Position implements MarshalJSON, so its fields are promoted into
// the flat JSON object rather than nested under a "TeamLineup" key. The
// handler tests decode the RAW response body (map[string]any) rather than
// through this struct specifically so that a future MarshalJSON silently
// dropping `saved` would fail them rather than pass unnoticed.
type teamLineupRead struct {
	domain.TeamLineup
	Saved bool `json:"saved"`
}

// findMatchLineup scans the loaded lineup map for the match-scoped entry
// matching (teamID, matchID), avoiding any dependency on the store's
// internal key format. Returns the lineup and whether it was found.
func findMatchLineup(lineups map[string]domain.TeamLineup, teamID, matchID string) (domain.TeamLineup, bool) {
	for _, l := range lineups {
		if l.MatchID == matchID && l.TeamID == teamID && !l.IsRemoved() {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}

// findRoundLineup is the round-scoped sibling of findMatchLineup: it scans the
// loaded map by fields for the (teamID, round) entry that is NOT match-scoped
// (MatchID == ""), so the handlers don't replicate state's internal
// "<teamID>-<round>" key format. Returns the lineup and whether it was found.
func findRoundLineup(lineups map[string]domain.TeamLineup, teamID string, round int) (domain.TeamLineup, bool) {
	for _, l := range lineups {
		if l.MatchID == "" && l.TeamID == teamID && l.Round == round && !l.IsRemoved() {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}

// RegisterLineupHandlers wires the PUT/DELETE lineup endpoints under
// the admin (auth-protected) group. The corresponding GET is public
// and registered via RegisterPublicLineupHandlers.
//
// DELETE is manager-only per the spec; for now we rely on the
// existing AuthMiddleware (mounted on the admin router group in
// server.go) as the auth boundary. A richer role check lands when
// per-role auth is implemented.
//
// The `tx CompetitionTransactor` parameter is the T156 hook.
// The PUT body wraps its three store calls; load comp (for teamSize),
// set lineup, reload lineup (for the response), all in one
// WithTransaction so they all commit under a single per-comp lock
// acquire. `hub Broadcaster` receives an EventLineupUpdated after
// each successful write so SSE clients can re-fetch lineup data.
// `*state.Store` satisfies the first three interfaces (TeamLineupStore +
// CompetitionStore + CompetitionTransactor); the SSE hub satisfies
// Broadcaster and is wired separately in production.
//
// The match lineup PUT is public in a self-run tournament: the public score
// sheet saves it when a competitor names a bout's fighter (bc-dhas).
// `tl`/`verifier` tell an anonymous caller apart (selfRunAnonymous), who may
// not change the lineup of a match that has finished (see the PUT).
func RegisterLineupHandlers(r *gin.RouterGroup, store TeamLineupStore, comps CompetitionStore, tx CompetitionTransactor, hub Broadcaster, tl TournamentLoader, verifier PasswordVerifier, keepers ...KachinukiLineupKeeper) {
	seaters := make([]KachinukiOpenerSeater, 0, len(keepers))
	for _, k := range keepers {
		if k != nil {
			seaters = append(seaters, k)
		}
	}
	announce := func(compID, teamID, matchID string) {
		announceLineupChange(hub, comps, seaters, compID, teamID, matchID)
	}
	// A save's changes made before a kachinuki match started reach the lineup
	// the match started with, in the save's transaction (engine.FollowLineupSave).
	// A follow that cannot store what it worked out fails the transaction
	// (stage 6 review 7), as a pin that cannot be kept fails the start.
	// A carried lineup the domain refuses as worked out is no failure of the
	// change (stage 6 review 8, m7): the change is kept, the refused lineup
	// stays as stored, and the operator is told (the warning); a store that
	// cannot keep what was worked out fails the transaction.
	follow := func(stx state.StoreTx, comp *state.Competition, saved domain.TeamLineup, changed []domain.Position) (string, error) {
		warning := ""
		for _, k := range keepers {
			if k == nil {
				continue
			}
			if _, err := k.FollowLineupSave(stx, comp, saved, changed); err != nil {
				var derived *engine.DerivedLineupError
				if !errors.As(err, &derived) {
					return "", err
				}
				warning = derived.Warning()
			}
		}
		return warning, nil
	}
	// A lineup save is ordered by arrival: a late duplicate of one is
	// answered as recorded (writeOnce, Y8).
	once := newWriteOnce()
	r.PUT("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		// A lineup is the team's starting lineup (round 0) or one for a match: a
		// team carries the lineup of its previous match, so there is nothing a
		// lineup for a later round would mean. Answered before the body or the
		// competition is read, whatever either holds.
		if round >= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": lineupRoundRefused})
			return
		}
		var req LineupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if !validLineupRequest(c, req) {
			return
		}

		save := lineupSave{compID: compID, teamID: teamID, round: round, req: req, once: once}
		saveLineup(c, tx, announce, follow, save, nil, func(lineups map[string]domain.TeamLineup) (domain.TeamLineup, bool) {
			return findRoundLineup(lineups, teamID, round)
		})
	})

	r.DELETE("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		// The optional {writeId, removedAt} body: a late duplicate of a
		// removal already applied is answered as recorded (Y8, m8), and the
		// removal is ordered by when it was made (m1).
		body, ok := bindLineupRemoval(c)
		if !ok {
			return
		}
		removed, warning := false, ""
		target := lineupSave{compID: compID, teamID: teamID, round: round}.target()
		ran := once.do(target, body.WriteID, func() bool {
			// Taken away as a change stamped when it was made, its record
			// kept, and every carried lineup of the team worked out again in
			// the same transaction, as after a save, only when a lineup was
			// taken away (m8): a copy or a start made while it was in force
			// keeps what it carried.
			txErr := tx.WithTransaction(compID, func(stx state.StoreTx) error {
				var err error
				if removed, err = stx.RemoveTeamLineupAt(compID, teamID, "", round, body.RemovedAt); err != nil || !removed {
					return err
				}
				comp, err := stx.LoadCompetition(compID)
				if err != nil || comp == nil {
					return err
				}
				warning, err = follow(stx, comp, domain.TeamLineup{TeamID: teamID, Round: round}, nil)
				return err
			})
			if txErr != nil {
				internalError(c, txErr)
				return false
			}
			return true
		})
		if ran && c.Writer.Written() {
			return
		}
		if warning != "" {
			c.JSON(http.StatusOK, gin.H{"warning": warning})
		} else {
			c.Status(http.StatusNoContent)
		}
		if ran && removed {
			announce(compID, teamID, "")
		}
	})

	// Match-scoped PUT/DELETE (mp-825). Mirrors the round-scoped flow but
	// keys the lineup by matchID, so successive encounters keep separate
	// lineups.
	r.PUT("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		anonymous, ok := selfRunAnonymous(c, tl, verifier)
		if !ok {
			return
		}
		var req LineupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if !validLineupRequest(c, req) {
			return
		}

		save := lineupSave{compID: compID, teamID: teamID, matchID: matchID, req: req, once: once}
		// An anonymous self-run caller writes from the public score sheet,
		// so the score path's rule holds: the match must exist and hold the
		// team by id (errTeamNotInMatch), and once it has finished its lineup
		// is part of the result, which only the organiser corrects (holdSelfReportedWriteUnderTx refuses the same
		// caller on the result itself). Read under the save's lock, like that
		// check, and refused with the same result_finalized body. An
		// organiser keeps the always-editable rule.
		var guard func(stx state.StoreTx, judge *memberJudge) *txResponse
		if anonymous {
			guard = func(stx state.StoreTx, judge *memberJudge) *txResponse {
				snap, found, err := matchSnapshotOrErr(stx, compID, matchID, "lineup")
				if err != nil {
					log.Printf("mobileapp: PUT %s: %v", c.Request.URL.Path, err)
					return &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
				}
				if !found {
					return &txResponse{status: http.StatusNotFound, body: gin.H{"error": "match not found"}}
				}
				if teamID != snap.Pairing.SideAID && teamID != snap.Pairing.SideBID {
					return errTeamNotInMatch.response()
				}
				if isMatchFinalized(snap.Status) {
					return resultFinalized("This match has finished, so its lineup can no longer be changed. Contact the tournament organizer to correct it.").response()
				}
				return memberIDsOutsideTeam(judge, teamID, req.memberIDsRead())
			}
		}
		saveLineup(c, tx, announce, follow, save, guard, func(lineups map[string]domain.TeamLineup) (domain.TeamLineup, bool) {
			return findMatchLineup(lineups, teamID, matchID)
		})
	})

	r.DELETE("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		// The optional {writeId, removedAt} body (Z3, m1): a late duplicate
		// of a removal already applied is answered as recorded, and the
		// removal is ordered by when it was made against the saves of the
		// same lineup, like every change (an older build's, with no stamp,
		// is stamped as it lands).
		body, ok := bindLineupRemoval(c)
		if !ok {
			return
		}
		applied, changed, warning := false, false, ""
		target := lineupSave{compID: compID, teamID: teamID, matchID: matchID}.target()
		ran := once.do(target, body.WriteID, func() bool {
			// On a kachinuki match that has started, taking its own lineup
			// away keeps the lineup in force then as its own, a snapshot
			// (engine.UsePreviousLineupTx); elsewhere it only removes it.
			remove := func() (bool, error) {
				return store.RemoveTeamLineupAt(compID, teamID, matchID, 0, body.RemovedAt)
			}
			for _, k := range keepers {
				if k != nil {
					remove = func() (bool, error) { return k.UsePreviousLineupAt(compID, teamID, matchID, body.RemovedAt) }
					break
				}
			}
			var err error
			if changed, err = remove(); err != nil {
				var derived *engine.DerivedLineupError
				if !errors.As(err, &derived) {
					internalError(c, err)
					return false
				}
				// Kept, with a carried lineup that could not follow it (m7).
				warning = derived.Warning()
			}
			applied = true
			return true
		})
		if !ran {
			c.Status(http.StatusNoContent)
			return
		}
		if !applied {
			return
		}
		if warning != "" {
			c.JSON(http.StatusOK, gin.H{"warning": warning})
		} else {
			c.Status(http.StatusNoContent)
		}
		if changed {
			announce(compID, teamID, matchID)
		}
	})
}

// errTeamNotInMatch refuses a participant's lineup write for a team the
// match does not hold by id: nothing reads such a lineup, and each one would
// add a key to lineups.yaml. A side with no id yet (an unresolved knockout
// feeder) takes no participant's lineup either.
var errTeamNotInMatch = &selfRunRefusal{
	status:  http.StatusNotFound,
	code:    "team_not_in_match",
	message: "This team is not in this match. Check the score sheet and try again.",
}

// memberIDsOutsideTeam refuses a lineup that places a member id the team does
// not hold (400 team_member_not_in_team): ids are bare UUIDs, so the team's own
// squad is the only thing that says whose they are, and a lineup naming another
// team's member would put that member on this team's score sheet. A members
// file that cannot be read refuses the save with errTeamMembersUnreadable (409),
// not a 500 the offline queue retries forever; a save that names no member id
// (a cleared position) never reads it.
func memberIDsOutsideTeam(judge *memberJudge, teamID string, memberIDs map[domain.Position]string) *txResponse {
	for pos, id := range memberIDs {
		if id == "" {
			continue
		}
		held, err := judge.holds(teamID, id)
		if err != nil {
			return errTeamMembersUnreadable.response()
		}
		if !held {
			return errLineupMemberNotInTeam(pos).response()
		}
	}
	return nil
}

// memberIDsTheTeamHolds is a lineup save's member ids without any id the team
// does not hold (stage 3 review 3, F2). Such an id names nobody: most often the
// one a device minted for a member add the server then refused, which a lineup
// save queued after the add still carries. Stored, it would sit on the lineup as
// a member no list holds; taken out, the position keeps its name and no id, which
// resolves to nothing (bc-pnum), the shape a name typed while the members were
// not loaded already has. The anonymous self-run caller is refused for such an
// id before this (memberIDsOutsideTeam); this is the organiser's case, which is
// never refused for it. Every team has members recorded (seeded to its team
// size plus reserves on load and on every roster save), so an empty list means
// an id no team member holds. A team-members file that cannot be read refuses
// the save with errTeamMembersUnreadable (409), as every member-id check does
// (match-writes.md): which ids are held cannot be told, and keeping them
// unjudged would store an id that may name nobody. The judge is the save's one
// (memberJudge), shared with memberIDsOutsideTeam, so the file is read once.
func memberIDsTheTeamHolds(judge *memberJudge, teamID string, memberIDs map[domain.Position]string) (map[domain.Position]string, *txResponse) {
	if len(memberIDs) == 0 {
		return memberIDs, nil
	}
	kept := make(map[domain.Position]string, len(memberIDs))
	for pos, id := range memberIDs {
		if id == "" {
			kept[pos] = id
			continue
		}
		held, err := judge.holds(teamID, id)
		if err != nil {
			return nil, errTeamMembersUnreadable.response()
		}
		if !held {
			log.Printf("mobileapp: lineup save for team %s in %s: member %s is not one of the team's; position %q kept by its name", teamID, judge.compID, id, pos)
			continue
		}
		kept[pos] = id
	}
	return kept, nil
}

// teamMemberIDs is the set of member ids a team holds in squads: the one answer to
// "is this member the team's" that every member check asks.
func teamMemberIDs(squads map[string][]domain.TeamMember, teamID string) map[string]bool {
	held := make(map[string]bool, len(squads[teamID]))
	for _, m := range squads[teamID] {
		held[m.ID] = true
	}
	return held
}

// codeTeamMemberNotInTeam is the refusal code for a member id the named team does
// not hold. The lineup refusal and the representative pick's refusal both answer it.
const codeTeamMemberNotInTeam = "team_member_not_in_team"

func errLineupMemberNotInTeam(pos domain.Position) *selfRunRefusal {
	return &selfRunRefusal{
		status:  http.StatusBadRequest,
		code:    codeTeamMemberNotInTeam,
		message: "The member chosen for " + string(pos) + " is not on this team. Pick again from the list.",
	}
}

// lineupSave is one PUT of a lineup: whose it is, the match it is for (none for
// the team's starting lineup, round 0) and what the body said.
type lineupSave struct {
	compID, teamID string
	matchID        string
	round          int
	req            LineupRequest
	// once records the writeIds the lineup door applied (Y8): a late
	// duplicate of a save is answered with the lineup as stored.
	once *writeOnce
}

// target names the lineup a save writes, for writeOnce.
func (s lineupSave) target() string {
	return "lineup\x1f" + s.compID + "\x1f" + s.teamID + "\x1f" + s.matchID + "\x1f" + strconv.Itoa(s.round)
}

// lineupToStore is the lineup this save stores, built under the write's lock,
// with the positions it changed (for the follow). Its changed positions are
// landed on the lineup stored here, so what another device saved a moment ago
// is kept, and the result is the lineup stored for this team at this scope. A
// body that names no changed positions (an older build's whole save) changes
// every position at its arrival (wholeSaveChanges).
func (w lineupSave) lineupToStore(stx state.StoreTx, comp *state.Competition) (engine.LandedLineup, []domain.Position, *txResponse) {
	changed, partial := w.req.changedPositions()
	if !partial {
		// An older build's whole save is a change of every position at its
		// arrival (it sends no stamp, so landPartial stamps it now): the
		// positions it names, and every other position the team's lineups
		// hold, emptied. So it reaches no copy or pin made before it arrived,
		// and its History keeps what they read (operator decision L1).
		var resp *txResponse
		if changed, resp = w.wholeSaveChanges(stx); resp != nil {
			return engine.LandedLineup{}, nil, resp
		}
		if len(changed) == 0 {
			// Nothing to land: the lineup alone is set (no Records).
			return engine.LandedLineup{Lineup: domain.TeamLineup{
				TeamID: w.teamID, CompetitionID: w.compID, Round: w.round, MatchID: w.matchID,
				Positions: w.req.Positions, MemberIDs: w.req.MemberIDs,
			}, Live: true}, nil, nil
		}
	}
	landed, resp := w.landPartial(stx, comp, changed)
	if resp != nil {
		return engine.LandedLineup{}, nil, resp
	}
	for i := range landed.Records {
		l := &landed.Records[i]
		l.TeamID, l.CompetitionID, l.Round, l.MatchID = w.teamID, w.compID, w.round, w.matchID
		if !l.IsRemoved() {
			// A save landed on the lineup the match started with made it the
			// operator's own, carried from the same point
			// (engine.LandMatchLineupSave); one that landed only on a removed
			// lineup (every change made before a removal stamped after it,
			// m1) leaves that pin as it is.
			landed.Lineup = *l
		}
	}
	return landed, changed, nil
}

// wholeSaveChanges is the changed positions of a whole save: the positions
// it names and every other position the team's lineups in force hold, which
// it empties (an empty name in w.req.Positions).
func (w *lineupSave) wholeSaveChanges(stx state.StoreTx) ([]domain.Position, *txResponse) {
	lineups, err := stx.LoadTeamLineups(w.compID)
	if err != nil {
		log.Printf("mobileapp: lineup save for %s: LoadTeamLineups: %v", w.compID, err)
		return nil, &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
	}
	set := map[domain.Position]bool{}
	for p := range w.req.Positions {
		set[p] = true
	}
	for p := range w.req.MemberIDs {
		set[p] = true
	}
	for _, l := range lineups {
		if l.TeamID != w.teamID {
			continue
		}
		for p := range l.Positions {
			set[p] = true
		}
		for p := range l.MemberIDs {
			set[p] = true
		}
	}
	positions := maps.Clone(w.req.Positions)
	if positions == nil {
		positions = map[domain.Position]string{}
	}
	for p := range set {
		if _, ok := positions[p]; !ok {
			positions[p] = ""
		}
	}
	w.req.Positions = positions
	return slices.SortedFunc(maps.Keys(set), domain.ComparePositions), nil
}

// landPartial is the lineup a partial save stores, read under the write's
// lock over what this transaction loads (the store's own reads would wait on
// the lock it holds). The starting lineup: its stored entry with the changes
// on it. A match's own lineup: engine.LandMatchLineupSave, the carry rule by
// stamp (operator decision L1): a first stamped save takes the positions it
// does not name from what the match carried at its stamp, a later one joins
// them, and an unstamped first save lands on the lineup in force when it
// arrives. The match's own lineup is read whole, never merged with the one
// before it, so a base left empty would drop every carried position.
func (w lineupSave) landPartial(stx state.StoreTx, comp *state.Competition, changed []domain.Position) (engine.LandedLineup, *txResponse) {
	internal := func(what string, err error) (engine.LandedLineup, *txResponse) {
		log.Printf("mobileapp: lineup save for %s: %s: %v", w.compID, what, err)
		return engine.LandedLineup{}, &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
	}
	refused := func(err error) (engine.LandedLineup, *txResponse) {
		return engine.LandedLineup{}, &txResponse{status: lineupSetStatus(err), body: gin.H{"error": err.Error()}}
	}
	// The server's time as the save lands, after any other lineup change
	// that landed before it (state.LineupArrivalStamp). A change sent with no
	// stamp (an older build's) is ordered by its arrival: it is stamped with
	// it.
	now := state.LineupArrivalStamp(w.compID)
	stamps := map[domain.Position]int64{}
	for _, p := range changed {
		if s := w.req.ChangedAt[p]; s > 0 {
			stamps[p] = s
		} else {
			stamps[p] = now
		}
	}
	// The removed lineups' records too: a carry reads them by stamp.
	lineups, err := stx.LoadTeamLineupsWithRemoved(w.compID)
	if err != nil {
		return internal("LoadTeamLineups", err)
	}
	if w.matchID == "" {
		out, err := engine.LandStartingLineupSave(lineups, w.teamID, changed, w.req.Positions, w.req.MemberIDs, stamps, now)
		if err != nil {
			return refused(err)
		}
		return out, nil
	}
	pool, err := stx.LoadPoolMatches(w.compID)
	if err != nil {
		return internal("LoadPoolMatches", err)
	}
	bracket, err := stx.LoadBracket(w.compID)
	if err != nil {
		return internal("LoadBracket", err)
	}
	out, err := engine.LandMatchLineupSave(lineups, pool, bracket, comp.IsKnockoutEnabled(), comp.TeamSize,
		w.teamID, w.matchID, changed, w.req.Positions, w.req.MemberIDs, stamps, now)
	if err != nil {
		return refused(err)
	}
	return out, nil
}

// saveLineup is the body the round and the match lineup PUTs share. Under one
// per-comp lock (T156, the same atomicity argument the engine
// UpdatePoolMatchByID / UpdateBracket primitives make) it loads the
// competition for its team size, runs guard (the match PUT's rule for a
// participant; nil when there is none), builds the lineup to store
// (lineupSave.lineupToStore), saves it and reads it back with find for the
// response. The answer is written after the lock releases (txResponse), and a
// saved lineup is broadcast so SSE clients re-fetch it.
// lineupAnswer is a lineup save's answer when a carried lineup could not
// follow it (engine.DerivedLineupError, stage 6 review 8, m7): the lineup as
// stored, and the sentence the operator is told.
type lineupAnswer struct {
	domain.TeamLineup
	Warning string `json:"warning,omitempty"`
}

func saveLineup(c *gin.Context, tx CompetitionTransactor, announce func(compID, teamID, matchID string), follow func(stx state.StoreTx, comp *state.Competition, saved domain.TeamLineup, changed []domain.Position) (string, error), save lineupSave, guard func(stx state.StoreTx, judge *memberJudge) *txResponse, find func(map[string]domain.TeamLineup) (domain.TeamLineup, bool)) {
	compID := save.compID
	var respErr *txResponse
	var persistedLineup domain.TeamLineup
	warning := ""
	duplicate := false
	txErr := tx.WithTransaction(compID, func(stx state.StoreTx) error {
		// TeamSize is competition-level: a 3-person team and a 5-person team
		// cannot coexist in the same competition. We need it here to drive
		// Validate(); not having a competition is a 404.
		comp, err := stx.LoadCompetition(compID)
		if err != nil {
			log.Printf("mobileapp: PUT %s: LoadCompetition: %v", c.Request.URL.Path, err)
			respErr = &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
			return nil
		}
		if comp == nil {
			respErr = &txResponse{status: http.StatusNotFound, body: gin.H{"error": "competition not found"}}
			return nil
		}
		teamSize := comp.TeamSize
		if teamSize <= 0 {
			respErr = &txResponse{
				status: http.StatusBadRequest,
				body:   gin.H{"error": "competition is not configured for team play (teamSize must be > 0)"},
			}
			return nil
		}
		// A late duplicate of a save this door applied (Y8): answered with
		// the lineup as stored, judged under the competition's lock so it
		// is ordered after the save it duplicates and anything since.
		if save.once.seenAt(save.target(), save.req.WriteID) {
			duplicate = true
			lineups, err := stx.LoadTeamLineups(compID)
			if err != nil {
				log.Printf("mobileapp: PUT %s: LoadTeamLineups: %v", c.Request.URL.Path, err)
				respErr = &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
				return nil
			}
			if stored, ok := find(lineups); ok {
				persistedLineup = stored
			} else {
				persistedLineup = domain.TeamLineup{TeamID: save.teamID, CompetitionID: compID, Round: save.round, MatchID: save.matchID, Positions: map[domain.Position]string{}}
			}
			return nil
		}
		// ONE judge for the save, so team-members.yaml is read at most once
		// (stage 3 review 4, G8).
		judge := &memberJudge{stx: stx, compID: compID}
		if guard != nil {
			if respErr = guard(stx, judge); respErr != nil {
				return nil
			}
		}
		// No member id the team does not hold is ever stored (stage 3 review 3,
		// F2): the anonymous caller was refused for one by its guard, and the
		// organiser's save keeps the position by its name alone. Only the ids
		// the save lands are judged and stripped, those of its changed
		// positions when it names them (memberIDsRead, stage 3 review 11, Y9):
		// the rest of its body is not read, so it reads no members file for them.
		read := save.req.memberIDsRead()
		var kept map[domain.Position]string
		if kept, respErr = memberIDsTheTeamHolds(judge, save.teamID, read); respErr != nil {
			return nil
		}
		for pos, id := range read {
			if kept[pos] != id {
				delete(save.req.MemberIDs, pos)
			}
		}
		var landed engine.LandedLineup
		var changed []domain.Position
		if landed, changed, respErr = save.lineupToStore(stx, comp); respErr != nil {
			return nil
		}
		// Every record of the slot the save landed on (the live lineup, and
		// a removed one a change made before its removal joined, m1), in one
		// staged write; a save with nothing to land sets its lineup alone.
		var setErr error
		if landed.Records == nil {
			setErr = stx.SetTeamLineup(compID, landed.Lineup, teamSize)
		} else {
			setErr = stx.ReplaceTeamLineupSlot(compID, save.teamID, save.matchID, save.round, landed.Records, teamSize)
		}
		lineup := domain.TeamLineup{TeamID: save.teamID, CompetitionID: compID, Round: save.round, MatchID: save.matchID, Positions: map[domain.Position]string{}}
		if err := setErr; err != nil {
			// Domain validation errors ("team_lineup:" prefix) are 400; a
			// YAML/disk fault is a 500 (see lineupSetStatus).
			respErr = &txResponse{status: lineupSetStatus(err), body: gin.H{"error": err.Error()}}
			return nil
		}
		if warning, err = follow(stx, comp, lineup, changed); err != nil {
			// Returned so the transaction aborts: the save is not kept
			// without the lineups it moves (answered 500, sent again). A
			// carried lineup the domain refuses is no such failure: the save
			// is kept and the answer carries the warning (m7).
			return err
		}
		// Reload after write so the response carries the persisted
		// CompetitionID (auto-stamped by Set) and any future server-managed
		// fields. This reload reads the same on-disk state as the Set above
		// because no concurrent writer can have taken the per-comp lock
		// between them.
		lineups, err := stx.LoadTeamLineups(compID)
		if err != nil {
			log.Printf("mobileapp: PUT %s: LoadTeamLineups: %v", c.Request.URL.Path, err)
			respErr = &txResponse{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
			return nil
		}
		if persisted, ok := find(lineups); ok {
			persistedLineup = persisted
		} else {
			// No lineup of its own is in force: every change of the save was
			// made before a removal stamped after it (m1), so it joined the
			// lineup that removal took away. The answer says the slot holds
			// none, as it is stored.
			persistedLineup = lineup
		}
		return nil
	})
	if txErr != nil {
		internalError(c, txErr)
		return
	}
	if respErr != nil {
		c.JSON(respErr.status, respErr.body)
		return
	}
	if warning != "" {
		c.JSON(http.StatusOK, lineupAnswer{TeamLineup: persistedLineup, Warning: warning})
	} else {
		c.JSON(http.StatusOK, persistedLineup)
	}
	if duplicate {
		return
	}
	// Recorded once the save has committed: a save the door refused, or one
	// whose commit failed, is judged again when it is sent again.
	save.once.remember(save.target(), save.req.WriteID)
	announce(compID, save.teamID, save.matchID)
}

// announceLineupChange is the ONE announcement of a lineup or team-member
// write: the lineup-updated event, then bout 1 of each kachinuki match of the
// team brought to what that match's lineup says (operator decision K1 (c),
// engine/kachinuki_opener.go: its own lineup, or the one it started with, at
// any status; the lineup in force while it is scheduled) and a match_updated
// for each match that moved, read fresh from the store. A seat that fails is
// logged: the match's next write seats it.
func announceLineupChange(hub Broadcaster, comps CompetitionStore, seaters []KachinukiOpenerSeater, compID, teamID, matchID string) {
	hub.Broadcast(EventLineupUpdated, lineupUpdatedPayload(compID, teamID, matchID))
	for _, seater := range seaters {
		if seater == nil {
			continue
		}
		changed, err := seater.ReseatKachinukiOpeners(compID, teamID)
		if err != nil {
			log.Printf("mobileapp: competition %s team %s: bout 1 not seated from the lineup: %v", compID, teamID, err)
		}
		for _, id := range changed {
			broadcastMatchNow(hub, comps, compID, id, gin.H{"competitionId": compID, "matchId": id, "result": nil})
		}
	}
}

// lineupUpdatedPayload is the data of an EventLineupUpdated, the one place it is
// built: the competition, the team whose lineups changed and, for a lineup saved
// for a match, that match. A team's starting lineup names no match, and nor does
// a change to a team's members (a rename or a clear), which can reach the team's
// lineups at several matches. A listener that reads one team's lineup can tell by
// this whether the change is its own.
func lineupUpdatedPayload(compID, teamID, matchID string) gin.H {
	data := gin.H{"competitionId": compID, "teamId": teamID}
	if matchID != "" {
		data["matchId"] = matchID
	}
	return data
}

// parseLineupParams extracts (compID, teamID, round) from the URL and
// writes a 400 response when round can't be parsed as int. compID
// goes through requireValidCompID to enforce the
// ValidateCompetitionID character whitelist.
//
// teamID is treated as opaque here, and that is now a CHOICE rather than
// the absence of an alternative: the squad endpoints are a team-management
// surface and do validate the id against participants.csv
// (state.AddTeamMember). Lineups deliberately do not follow them. A lineup
// keyed on an id no team holds is overwritten by the next save for that
// team and renders nowhere meanwhile, whereas a squad member minted under
// one can never be removed, so only the irreversible side earns the check.
// The on-disk file is keyed by the composite string and the id is never
// used as a filesystem path.
func parseLineupParams(c *gin.Context) (compID, teamID string, round int, ok bool) {
	compID, teamID, ok = requireValidCompIDAndTeam(c)
	if !ok {
		return "", "", 0, false
	}
	roundStr := c.Param("round")
	round, err := strconv.Atoi(roundStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "round must be an integer"})
		return "", "", 0, false
	}
	if round < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "round must be non-negative"})
		return "", "", 0, false
	}
	return compID, teamID, round, true
}

// parseMatchLineupParams extracts (compID, teamID, matchID) from the URL
// for the match-scoped lineup endpoints (mp-825). matchID is opaque
// (like teamID); it's never used as a filesystem path, only as a map
// key and a lookup against persisted match IDs, so no regex is imposed
// beyond non-empty.
func parseMatchLineupParams(c *gin.Context) (compID, teamID, matchID string, ok bool) {
	compID, teamID, ok = requireValidCompIDAndTeam(c)
	if !ok {
		return "", "", "", false
	}
	matchID = c.Param("matchId")
	if matchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "match ID is required"})
		return "", "", "", false
	}
	return compID, teamID, matchID, true
}
