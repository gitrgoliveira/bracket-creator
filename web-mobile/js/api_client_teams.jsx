// The team API members: starting and match lineups, the lineup in force, the
// kachinuki roster, and a team's members (add, rename, clear).
//
// A piece of the API object (bc-jsxs): api_client.jsx spreads every piece into
// `API` (api_client_tournament.jsx, api_client_competitions.jsx,
// api_client_matches.jsx, api_client_teams.jsx, api_client_outbox.jsx, and the
// two event members in api_client_events.jsx), so `window.API` keeps one shape.
// Declaration-only: at load it builds its object and runs nothing else, so the
// import cycles between the outbox's pieces stay safe (check-load-cycles.mjs).
// A member another member calls is a named function of the module that owns it,
// called directly, never through `API`. Never import api_client.jsx from here
// (it would run the outbox twice).

import { _fetchJson, _readLineup } from './api_client_requests.jsx';
import { _outboxWrite, _queuedLineupSave } from './api_client_enqueue.jsx';
import {
    _withListedMembersForgotten, _memberWrite, _newClientId, _shownMember,
} from './api_client_members.jsx';
import { _lineupKey } from './api_client_queue.jsx';

export const teamsApi = {
    // The GET answers saved: false when nothing is stored; lineupOrNull
    // makes that null ("blank, editable"). A 404 means the competition does
    // not exist and throws. PUT replaces; DELETE clears. The read is of the
    // exact round: the Lineups page reads a team's starting lineup (round 0)
    // this way, and the match surfaces (score sheet, displays, the at-court
    // lineup panel) do not read rounds at all: they read fetchLineupInForce
    // through resolveMatchLineup.
    async fetchTeamLineup(compID, teamId, round) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`);
    },
    // memberIds (bc-tmid pass 3) is optional and keyed by the same position
    // as positions: the squad member id half of a lineup, sent alongside
    // the name so the server can persist both together. Omitted entirely
    // when the caller passes nothing, so a caller that never adopted squad
    // members (or an older bundle) round-trips exactly as before.
    //
    // changed (operator decision 2026-10-07, "Only changed positions") names the
    // positions this save changes: the server reads only those and lands them
    // on the lineup it holds when the save arrives, so a position another device
    // changed meanwhile stays, and the answer is the whole lineup it then holds
    // (lineup_save.jsx builds the body: positions and memberIds for the changed
    // positions only). Omitted, the body is a whole lineup that replaces the
    // stored one.
    async putTeamLineup(compID, teamId, round, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an outbox entry under the lineup's own base (a starting
        // lineup belongs to no match), answered at once like every write; a
        // save made while the last one is queued joins it (_coalesceTarget).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/lineups/${round}`,
            payload: {
                teamId, competitionId: compID, round, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
            base: _lineupKey(compID, teamId, { round }),
        });
    },
    async fetchSquads(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/team-members`, {
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load team members");
        }
        const data = await res.json();
        // A member the server lists exists, whatever was known of them.
        return _withListedMembersForgotten(data.teamMembers, compID) || {};
    },
    // The server gives the new member its display index (operator ruling:
    // assigned automatically as members are added); clearTeamMember below is
    // the nearest counterpart and only blanks a name: an index, once minted, is
    // never freed. Every member write's `settled` resolves with the member as
    // the server stamped it, and the server announces the change
    // (lineup_updated, with the team) to every device.
    async addTeamMember(compID, teamId, name, password, opts = {}) {
        // R1: the id is minted here, so the lineup save naming the member is
        // queued behind the add (putMatchLineup's `after`) with the id it will
        // have; a resend of the add is answered as stored. The member shown
        // carries no stamp: nothing holds it yet, and the server's copy, when
        // a list brings it, stands over it.
        const id = _newClientId();
        const trimmed = String(name || '').trim();
        return _memberWrite({
            compID, teamId, method: 'POST',
            url: `/api/competitions/${compID}/teams/${teamId}/members`,
            payload: { id, name: trimmed }, password,
            member: { ...(opts.index ? { index: opts.index } : {}), id, name: trimmed },
            opts,
        });
    },
    // Keeps memberId's id and index; only the display name changes. Answers 200
    // with the member, stamped.
    async renameTeamMember(compID, teamId, memberId, name, password, opts = {}) {
        // R1: an outbox entry; `opts.member` is the member as shown, which the
        // answer keeps (its number) with the new name, and which a refusal
        // puts back (`revertMember`).
        const trimmed = String(name || '').trim();
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: { name: trimmed }, password,
            member: _shownMember(before, { id: memberId, name: trimmed }),
            opts: { ...opts, was: before },
        });
    },
    // The operator's "removal": blanks memberId's Name back to "" and
    // leaves the id and display index untouched, so a bout already fought
    // that names this position keeps meaning the same person (bc-pnum).
    // Answers 200 with the member, nameless and stamped. Refused with a 409
    // once the competition has started (state.ErrTeamMemberClearAfterStart);
    // the server's own message is operator-facing, so it is surfaced verbatim
    // rather than remapped here.
    async clearTeamMember(compID, teamId, memberId, password, opts = {}) {
        // R1: an outbox entry, as renameTeamMember; the member shows unnamed at once.
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: {}, password,
            member: _shownMember(before, { id: memberId, name: '' }),
            opts: { ...opts, was: before },
        });
    },
    // mp-825 / mp-bkg: per-match lineup endpoints. Match ID takes the
    // place of the round key: successive encounters between the same
    // two teams each carry an independent lineup entry. Nothing here reads
    // the lineup saved for exactly one match: what a team fields at a match
    // is fetchLineupInForce.
    // The lineup a team fields at a match (operator ruling 2026-10-05: a team
    // carries the lineup of its previous match unless one is entered for the
    // match). The server owns the rule (engine/lineup_in_force.go): the
    // match's own lineup, else the latest of the team's earlier matches'
    // lineups, else its starting lineup. `saved: false` -> null (nothing
    // applies). A lineup carries where it was saved: `sourceMatchId` (this
    // match, or an earlier one it is carried from) or `sourceRound` (0, the
    // starting lineup). A 404 means the competition does not exist.
    async fetchLineupInForce(compID, teamId, matchId) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineup-in-force/${matchId}`);
    },
    // bc-kfnl: a kachinuki encounter's fighters each side has left, for the
    // advisory line above the live bout (kachinuki_advisory.jsx). The server
    // reads it through the same call that appends the next pairing. Public,
    // bounded like every read (_fetchJson). It NEVER throws: the line is a
    // guide only, so any failure, a timeout, a non-OK answer (400 not
    // kachinuki, 404 unknown match) or an answer without both sides reads as
    // null, and the editor then shows no line.
    async fetchKachinukiRoster(compID, matchID) {
        try {
            const { res, body } = await _fetchJson(
                `/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/kachinuki-roster`, {});
            if (!res.ok || !body || !body.sideA || !body.sideB) return null;
            return body;
        } catch (_e) {
            return null;
        }
    },
    // memberIds (bc-pnum gap closure) is optional and keyed by the same
    // position as positions, exactly like putTeamLineup's own memberIds
    // above: the squad member id half of a lineup, sent alongside the name
    // so the server can persist both together. Omitted entirely when the
    // caller passes nothing, so a caller that never adopted squad members
    // (or an older bundle) round-trips exactly as before; the outbox entry
    // carries them, so a replayed lineup write carries the ids too. `changed`
    // names the positions
    // the save changes, as for putTeamLineup above.
    async putMatchLineup(compID, teamId, matchId, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an entry of the MATCH, sent strictly in its order with
        // the match's other writes, and answered at once ({pending, settled}):
        // nothing waits on the network. `settled` resolves with the whole
        // lineup the server holds, or rejects with its refusal (a finished
        // match's 409 carries its code in `error` and its sentence in
        // `message`, kept by _refusalError); `opts.onRefusal` may claim it.
        // writeId, session and basis ride along; the lineup PUT reads none.
        // R1: it follows the team's member writes queued before it (`after`
        // their last entry, strict order on their base), so a member it names
        // by a minted id is added, and a rename lands, before the lineup that
        // places them: _followMintedMembers, as it is queued (K4).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: matchId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {
                teamId, competitionId: compID, matchId, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Removes the match's own lineup: an outbox entry of the match (kind
    // `lineup-remove`, R1), answered at once and sent in the match's order like
    // every write of it; `settled` resolves once the server has removed it. A
    // lineup that is not there is removed already (a 404 settles as done).
    async deleteMatchLineup(compID, teamId, matchId, password, opts = {}) {
        // R1: an entry of the MATCH, after every write of it queued before it
        // (a save of the lineup included, so no wait for one is needed), and
        // answered at once. A 404 (nothing saved to remove) is the outcome
        // asked for. `settled` resolves true, or rejects with the refusal.
        return _outboxWrite({
            kind: 'lineup-remove', compID, matchID: matchId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Whether a save of this lineup is still queued. `target` is { matchId } for a
    // match's own lineup or { round } for a team's Lineups-page round (0: its
    // starting lineup). The team sheet asks it before it shows a read of the
    // lineup (a read answered while a save is queued lacks that save). Nothing
    // is composed on a queued save: a new save of the lineup joins it and names
    // what it changed, and a removal is ordered after it by its own stamp.
    queuedLineupSave(compID, teamId, target) {
        return _queuedLineupSave(compID, teamId, target || {});
    },
};
