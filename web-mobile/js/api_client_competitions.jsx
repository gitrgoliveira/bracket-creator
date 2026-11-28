// The competitions' API members: the list and one competition's detail, its
// settings, the draw, schedule estimates and the calendar, exports and
// imports, awards, participants and check-in, competitor status, pool ranks,
// Swiss rounds and league tie-breakers.
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

import { normalizeCompetitionDetail, buildPlayerMetadata } from './api_serializers.jsx';
import { NOT_SENT_NO_CONNECTION } from './write_result.jsx';
import { awardsBase } from './document_versions.jsx';
import {
    normalizeViewerCompItem, parseSkippedCompetitionsHeader, _fetchJson, _directFetch,
    readHeaders, readFailure, publishDraw, unpublishDraw,
} from './api_client_requests.jsx';
import { _serverNowMs } from './api_client_clock.jsx';
import { _poolRanksBase } from './api_client_queue.jsx';
import {
    _overlayPendingFields, _checkInBase, _emitLocalCheckIns, _checkInLabel, currentDocVersion,
} from './api_client_overlays.jsx';
import { _forgetShownMatches, _forgetAllShownMatches } from './api_client_status.jsx';
import { _outboxWrite } from './api_client_enqueue.jsx';
import { noteReadSeq } from './read_seq.jsx';

// _downloadWorkbook GETs an admin-gated .xlsx through the direct door and
// returns its Blob; a non-2xx throws the server's own sentence, else `fallback`.
// A download changes nothing, so a dropped connection says "Not sent".
async function _downloadWorkbook(path, password, fallback) {
    const res = await _directFetch(path, {
        method: 'GET',
        headers: password ? { 'X-Tournament-Password': password } : {}
    }, NOT_SENT_NO_CONNECTION);
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw new Error(err.error || fallback);
    }
    return res.blob();
}

export const competitionsApi = {
    // `bounded` gives up after FETCH_TIMEOUT_MS, the body read included
    // (_fetchJson), instead of waiting on a connection that never answers.
    // The organiser's refresh after a write asks for it (admin.jsx
    // refreshCompsBestEffort): the score editor that made the write waits on
    // that refresh, and it is best-effort. Other callers wait as long as the
    // aggregate takes, which can be long for a large tournament on a slow link.
    // Every viewer read here goes as the page reads (readHeaders, bc-hnam): the
    // password from an admin route, the reveal keys from a public page, nothing
    // from a court board.
    async fetchCompetitions({ bounded = false } = {}) {
        if (bounded) {
            const { res, body } = await _fetchJson('/api/viewer/competitions', { headers: readHeaders() });
            if (!res.ok) throw readFailure(res.status, body.error, 'competitions');
            // _fetchJson reads an unreadable body as {}: never hand that on as
            // the competitions.
            if (!Array.isArray(body)) throw new Error('Failed to fetch competitions (unreadable answer)');
            return noteReadSeq(body.map((c) => _overlayPendingFields(normalizeViewerCompItem(c))), res);
        }
        const res = await fetch('/api/viewer/competitions', { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error, 'competitions');
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        // A court or time this device still has queued shows (R4). The
        // answer's stream position rides on the array returned (read_seq.jsx).
        return noteReadSeq(comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c))), res);
    },
    // fetchCourtMatches returns the court-scoped match feed: only the
    // competitions that have a real match physically on `court` right now, each
    // with its full {config, poolMatches, bracket} payload (same per-comp shape
    // as fetchCompetitions). The operator console sources its cross-competition
    // court view from this instead of the whole-tournament aggregate. Returns a
    // normalized competitions array so callers can run tournamentMatches() over
    // { competitions } exactly as they do for the aggregate.
    // The console reads it from an admin route, so it carries the password and
    // the operator scores against real names (readHeaders).
    async fetchCourtMatches(court) {
        const res = await fetch(`/api/viewer/court/${encodeURIComponent(court)}/matches`, { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error || `Failed to fetch court matches (Status ${res.status})`);
        }
        const data = await res.json();
        const comps = Array.isArray(data.competitions) ? data.competitions : [];
        return noteReadSeq(comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c))), res);
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`, { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = readFailure(res.status, err.error, 'competition details');
            // The server answered: a caller can tell that from a request
            // that never reached it (the data-issue banner's Reload).
            e.status = res.status;
            throw e;
        }
        const data = await res.json();
        return noteReadSeq(_overlayPendingFields(normalizeCompetitionDetail(data), id), res);
    },
    async updateCompetition(id, config, password) {
        // Go's domain.Player uses json:"metadata" (array); the JS layer uses the
        // friendlier danGrade string. Convert before encoding so the backend can
        // unmarshal the dan grade into Player.Metadata.
        // `rosterShuffled: true` (Shuffle unseeded's roster PUT) rides in
        // `config` untouched: the server records it only with a roster that
        // lands, and the answer carries the stored marker back.
        let body = config;
        if (config.players) {
            body = {
                ...config,
                players: config.players.map((player) => {
                    // Only transform players that have been through normalizePlayer
                    // (which always adds an explicit danGrade key). A Go-sourced player
                    // object without danGrade must be passed through unchanged: if we
                    // destructure it, rest would be empty and metadata[0] would be lost.
                    if (!('danGrade' in player)) return player;
                    const { danGrade, metadata: _m, ...p } = player;
                    const metadata = buildPlayerMetadata(danGrade, player.metadata);
                    return metadata === undefined ? p : { ...p, metadata };
                }),
            };
        }
        const res = await _directFetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || "Failed to update competition");
            // A roster PUT built from a roster that is no longer the stored
            // one (another desk added, removed or seeded a competitor since):
            // nothing was saved, and the answer carries the roster as it
            // stands, normalized as a fetched detail's, for the page to lay
            // its change on and send again (stage 6 review 12, C1).
            if (res.status === 409 && err.code === 'roster_changed') {
                e.code = 'roster_changed';
                e.players = normalizeCompetitionDetail({ config: { players: err.players || [] } }).config.players;
                e.rosterVersion = err.rosterVersion || '';
            }
            if (res.status === 409 && err.code === 'config_changed') {
                e.code = 'config_changed';
                e.configVersion = err.configVersion || '';
                e.competition = err.competition || null;
            }
            if (res.status === 409 && ['undo_stale', 'retime_retry', 'retime_unavailable', 'retime_past_midnight', 'court_past_midnight'].includes(err.code)) {
                e.code = err.code;
            }
            throw e;
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await _directFetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create competition");
        }
        return res.json();
    },
    // exportPDFs POSTs to /api/print/:type (admin-gated) and returns
    // { blob, skipped }: `blob` is a ZIP of the produced PDFs, `skipped` is
    // the parsed X-Skipped-Competitions header as [{id, reason}, ...] (empty
    // array when the header is absent, i.e. nothing was skipped). `type` is
    // one of registration|names|tags|pools-trees|full-bracket|all.
    //
    // Throws with the server's error message on non-2xx (e.g. 503 when
    // LibreOffice is absent, 422 when every competition was skipped or no
    // pages were produced); the thrown Error also carries a `skipped` array
    // (from the JSON body's `skipped` field, e.g. the all-skipped 422 case),
    // empty when the body carries none. Generation runs LibreOffice and can
    // take 30–60s, so callers should show a busy state.
    async exportPDFs(type, password) {
        const res = await _directFetch(`/api/print/${type}`, {
            method: 'POST',
            headers: password ? { 'X-Tournament-Password': password } : {}
        }, NOT_SENT_NO_CONNECTION); // generating PDFs changes nothing
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || "Failed to generate PDFs");
            e.skipped = Array.isArray(err.skipped) ? err.skipped : [];
            throw e;
        }
        const skipped = parseSkippedCompetitionsHeader(res.headers.get('X-Skipped-Competitions'));
        const blob = await res.blob();
        return { blob, skipped };
    },
    // exportResults GETs the RESULTS-populated workbook (played scores,
    // winners and decision suffixes written as values; pool standings kept as
    // live formulas except where the app's order differs) from the admin-gated /api/competitions/:id/export-results endpoint. This
    // is distinct from AdminExport's "Download blank template", which yields a
    // BLANK formula template (exportDrawTemplate below). Returns an .xlsx Blob;
    // throws the server's error message on non-2xx.
    async exportResults(compID, password) {
        return _downloadWorkbook(`/api/competitions/${compID}/export-results`, password, "Failed to export results");
    },
    // exportDrawTemplate GETs the blank template of a competition (no results,
    // live formulas) from the admin-gated /api/competitions/:id/export endpoint
    // (engine.ExportCompetitionXlsx): its STORED draw, the same one the printed
    // PDFs carry and the app runs, or, before the draw, the blank layout the
    // draw would print with its places empty. AdminExport's "Download blank
    // template" uses it in every status.
    // Returns an .xlsx Blob; throws the server's error message on non-2xx.
    async exportDrawTemplate(compID, password) {
        return _downloadWorkbook(`/api/competitions/${compID}/export`, password, "Failed to export the blank template");
    },
    async startCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to start competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async generateDraw(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/generate-draw`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate draw");
        }
        _forgetShownMatches(id);
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // fetchDrawWarnings: GET /competitions/:id/draw-warnings. The seeding rules
    // never refuse a draw; where a configuration cannot satisfy them all, the
    // deepest constraint gives way and the operator is told what was relaxed.
    // This is that advice, and it is admin-only (the public viewer surfaces
    // deliberately do not carry it).
    //
    // Advisory, so a failure is swallowed into an empty list: a competition
    // page must never break, or lose its draw, over a warning it could not
    // fetch.
    async fetchDrawWarnings(id, password) {
        try {
            const res = await fetch(`/api/competitions/${id}/draw-warnings`, {
                headers: password ? { 'X-Tournament-Password': password } : {}
            });
            if (!res.ok) return [];
            const data = await res.json();
            return data.warnings || [];
        } catch {
            return [];
        }
    },
    async discardDraw(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/draw`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to discard draw");
        }
        _forgetShownMatches(id);
    },
    publishDraw,
    unpublishDraw,
    // quarantineBracket: POST /competitions/:id/bracket/quarantine. The way out
    // of a bracket.json that will not parse, which otherwise blocks every score
    // write for that competition. Renames the broken file aside (it is NEVER
    // deleted: it is the only record of the knockout results it holds, and
    // because it does not parse nothing can count them). What happens next
    // depends on the format, and the response says which: `rebuilt` is true when
    // the knockout was rebuilt from pools.csv (mixed), false when there was no
    // knockout stage to rebuild and moving the file aside was the whole repair
    // (league, Swiss). 400 when the bracket reads fine, and 400 with a reason
    // for any competition that draws its bracket DIRECTLY, where the file is the
    // only record of the draw and rebuilding would invent different pairings
    // rather than restore these.
    async quarantineBracket(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/bracket/quarantine`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        const body = await res.json().catch(() => ({}));
        if (!res.ok) {
            throw new Error(body.error || "Failed to quarantine the bracket file");
        }
        return body;
    },
    // completeCompetition: POST /competitions/:id/complete. The only trigger
    // for a bracket-based competition (knockout, or mixed once its knockout is
    // running) to reach status "completed": MaybeAutoCompletePools only
    // auto-transitions the League format, so a finished bracket otherwise sits
    // in "pools"/"knockout" forever and the public viewer's Awards tab (gated
    // on status === "completed") never becomes reachable. 400 when the
    // competition isn't in "pools" or "knockout" status (already completed, or
    // not started yet), or a naginata 3rd-place match is still unscored; 404
    // when the competition doesn't exist.
    async completeCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/complete`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to complete competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Stateless estimate for a competition not yet saved, GET /api/schedule/draft-estimate.
    async estimateDraft(shape, password, signal) {
        const params = new URLSearchParams();
        const src = shape || {};
        const keys = [
            'format', 'participants', 'kind', 'teamSize', 'teamMatchType',
            'poolSize', 'poolSizeMode', 'poolWinners', 'roundRobin',
            'extraQualifiers', 'twoThirdPlaces', 'naginata', 'swissRounds',
            'poolMatchDurationSeconds', 'knockoutMatchDurationSeconds',
            'startTime', 'date',
        ];
        keys.forEach((k) => {
            const v = src[k];
            if (v !== undefined && v !== null && v !== "" && !Number.isNaN(v)) {
                params.append(k, v);
            }
        });
        let courtsNum;
        if (Array.isArray(src.courts)) {
            courtsNum = src.courts.length;
        } else if (src.courts !== undefined && src.courts !== null) {
            courtsNum = Number(src.courts);
        }
        if (Number.isFinite(courtsNum) && courtsNum > 0) {
            params.append('courts', courtsNum);
        }
        const qs = params.toString();
        const url = qs ? `/api/schedule/draft-estimate?${qs}` : '/api/schedule/draft-estimate';
        const res = await fetch(url, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Could not work out an estimate right now.");
        }
        return res.json();
    },
    async estimateCompetitionSchedule(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/estimate`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    // Court (shiaijo) clashes between this competition and every other one
    // (same day, shared court, overlapping time windows). Returns a possibly
    // empty array of ClashWarning objects. Non-blocking: surfaced as a warning.
    async getScheduleClashes(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/clashes`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to check schedule clashes");
        }
        return res.json();
    },
    async fetchCalendar(password, signal) {
        const res = await fetch('/api/schedule/calendar', {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load the calendar");
        }
        return res.json();
    },
    async previewMove({ competitionId, date, startTime, courts } = {}, password, signal) {
        const params = new URLSearchParams();
        if (competitionId !== undefined && competitionId !== null && competitionId !== "") {
            params.append('competitionId', competitionId);
        }
        if (date !== undefined && date !== null && date !== "") {
            params.append('date', date);
        }
        if (startTime !== undefined && startTime !== null && startTime !== "") {
            params.append('startTime', startTime);
        }
        if (Array.isArray(courts) && courts.length > 0) {
            params.append('courts', courts.join(','));
        }
        const qs = params.toString();
        const url = qs ? `/api/schedule/calendar/preview?${qs}` : '/api/schedule/calendar/preview';
        const res = await fetch(url, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to preview the move");
        }
        return res.json();
    },
    // Preview of the number prefix a save would assign when the field is
    // empty (bc-pnum G2/R6): the CREATE form calls this to pre-fill the
    // prefix field with the SAME value assignDefaultNumberPrefix would pick
    // server-side, so what the operator sees before saving matches what
    // would actually land. Create only: the competition doesn't exist yet,
    // so there is nothing to exclude from the taken set. bc-pnum B5: an
    // excludeID parameter used to exist for a settings-screen pre-fill
    // caller that was built and retired before this endpoint's first
    // release; it never had a second caller and is removed, not merely
    // unused.
    async getNumberPrefixDefault(name, password, signal) {
        const params = new URLSearchParams();
        if (name) params.append('name', name);
        const res = await fetch(`/api/number-prefix-default?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to derive number prefix");
        }
        return res.json();
    },
    async importCompetitions(formData, password) {
        const res = await _directFetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        // An import brings competitions whose matches may reuse ids a label
        // was learned for: none is trusted (K8).
        _forgetAllShownMatches();
        return res.json();
    },
    // Deleting a competition asks for the password again: `confirmPassword` is
    // what the operator typed in that moment, sent beside the sign-in password.
    // A refusal of it is a 401 that carries a `confirm_password_*` code; it is
    // thrown as an Error with that `code` and nothing else (no queue, no parking,
    // no sign-out), so the dialog that asked can show the sentence and ask again.
    async deleteCompetition(id, password, confirmPassword) {
        const res = await _directFetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, 'X-Confirm-Password': confirmPassword ?? '' }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw Object.assign(new Error(err.error || "Failed to delete competition"), { code: err.code });
        }
        // Its match ids are free for a competition made again under the same
        // name (X4); another device hears of it as a tournament_updated push.
        _forgetShownMatches(id);
        return true;
    },
    // Returns the updated competition JSON (including the new `status`) so
    // callers can apply it to local state immediately instead of waiting for
    // the SSE refresh / tournament refetch to land.
    async invalidateCompetition(id, password) {
        const res = await _directFetch(`/api/competitions/${id}/invalidate`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to invalidate competition");
        }
        return res.json();
    },
    // Replace the full fighting-spirit awards list for a competition.
    // awards: array of { title, recipientName, recipientDojo? }.
    // awardsVersion: the version of the list this save was made from (the
    // competition detail's awardsVersion, or the last save's answer); a save
    // from an older list is refused (409 awards_changed).
    // bc-obox G4b: an outbox entry under the base `awards:<comp>`, answered at
    // once ({ pending, settled }); `settled` resolves with the competition as
    // saved (its awardsVersion with it) or rejects with the refusal (409
    // awards_changed carries `code`, `awardsVersion` and
    // `fightingSpiritAwards`, the list now). The whole list is sent, so a save
    // made while one is queued takes its place (_coalesceTarget).
    async updateCompetitionAwards(id, awards, password, awardsVersion, opts = {}) {
        const base = awardsBase(id);
        return _outboxWrite({
            kind: 'awards', compID: id, matchID: '', method: 'PUT', url: `/api/competitions/${id}/awards`,
            payload: { fightingSpiritAwards: awards, awardsVersion: currentDocVersion(base, awardsVersion) },
            password, onRefusal: opts && opts.onRefusal, base, label: 'Fighting-spirit awards',
        });
    },
    // T190-T193 (US13: Swiss format). Generate the next Swiss round.
    // Backend pre-conditions: format=swiss; all matches in the current
    // round must be completed; swissCurrentRound must be < swissRounds.
    // Returns { round, matches, swissCurrentRound } on 201, throws on
    // 4xx/5xx. 409 with code="round_incomplete" surfaces a friendly
    // "current round still has incomplete matches" message in the UI;
    // the caller checks `e.code === "round_incomplete"` to branch.
    async swissGenerateRound(compID, password) {
        const res = await _directFetch(`/api/competitions/${compID}/swiss/generate-round`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            // Preserve the structured 409 payload (code + round) on the
            // thrown Error so the caller can show a precise message. The
            // generic .message stays as the server-reported error string.
            const e = new Error(err.error || "Failed to generate Swiss round");
            if (err.code) e.code = err.code;
            if (err.round !== undefined) e.round = err.round;
            throw e;
        }
        return res.json();
    },
    // T190-T193 (US13). Public endpoint: returns cumulative Swiss
    // standings ranked by wins > points > head-to-head. Returns []
    // when the competition hasn't started yet. Each entry is a
    // state.PlayerStanding (same shape used by pool standings).
    async swissStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/swiss/standings`, { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error || "Failed to load Swiss standings");
        }
        return res.json();
    },
    async leagueStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/league/standings`, { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error || "Failed to load league standings");
        }
        return res.json();
    },
    // bc-obox C1/C2: a check-in tap (checkedIn true) or its undo (false), an
    // outbox entry under the competition's base `checkin:<comp>`, answered at
    // once ({ pending, settled }) and shown at once (subscribeLocalParticipantPatch,
    // and every fetch lays it over its players until it lands). The stamp is
    // the tap's, so the later tap wins whatever arrives first. `settled`
    // resolves with the server's answer: the participant as stored, or 200
    // { applied: false, reason: 'superseded', participant } when another
    // desk's later tap already changed them (the desk takes `participant`;
    // when it is not what was asked the operator is told once, through
    // `opts.onRefusal` if it returns true, else the queue alert). A refusal
    // (409 check_in_not_in_self_run, a 404) rejects. opts: `was` ({ [pid]:
    // bool }, what the row showed before the tap, put back on a refusal or a
    // discard; default the opposite of `checkedIn`), `name` (how the
    // held-writes list names it), `onRefusal`.
    async toggleCheckIn(compID, pid, checkedIn, password, opts = {}) {
        const answer = _outboxWrite({
            kind: 'checkin', compID, matchID: '', method: checkedIn ? 'PUT' : 'DELETE',
            url: `/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}/checkin`,
            payload: { modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _checkInBase(compID),
            was: opts && opts.was && typeof opts.was === 'object' ? opts.was : { [pid]: !checkedIn },
            label: _checkInLabel(opts && opts.name),
        });
        _emitLocalCheckIns(compID, [[pid, !!checkedIn]]);
        return answer;
    },
    // Bulk check-in for one competition. participantIds is an array of pids as
    // built by checkinApiPid/rdApiPid: a stable UUID, or "" for a legacy
    // UUID-less row (bc-pnum operator ruling: this write is id only, never
    // the "name|dojo" composite -- name and dojo are operator-editable after
    // the draw, so that composite is not a safe wire identifier). An
    // id-less pid resolves to nothing server-side and is reported back via
    // notFound. An outbox entry like toggleCheckIn's, in the same base;
    // `settled` resolves with { checkedIn, alreadyCheckedIn, notFound,
    // superseded: [participant as stored] }, the superseded ones taken as
    // stored (told once when not checked in). opts.was is per id (the ids of
    // one tap may have started in different states; default not checked in).
    // Used by the Registration desk's "check in a whole dojo" action.
    async bulkCheckIn(compID, participantIds, password, opts = {}) {
        const ids = Array.isArray(participantIds) ? participantIds : [];
        const answer = _outboxWrite({
            kind: 'checkin-bulk', compID, matchID: '', method: 'POST',
            url: `/api/competitions/${encodeURIComponent(compID)}/participants/checkin-bulk`,
            payload: { participantIds: ids, modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _checkInBase(compID),
            was: opts && opts.was && typeof opts.was === 'object'
                ? opts.was : Object.fromEntries(ids.filter(Boolean).map((id) => [id, false])),
            label: _checkInLabel(opts && opts.name, ids.filter(Boolean).length),
        });
        _emitLocalCheckIns(compID, ids.filter(Boolean).map((id) => [id, true]));
        return answer;
    },
    async addParticipant(compID, payload, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/participants`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add participant');
        }
        return res.json();
    },
    async replaceParticipant(compID, pid, payload, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to replace participant');
        }
        return res.json();
    },
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated: same contract as
    // the other /api/competitions/:id/* GETs surfaced to the viewer.
    async fetchCompetitorStatuses(compID) {
        const res = await fetch(`/api/competitions/${compID}/competitor-status`, { headers: readHeaders() });
        if (!res.ok) throw readFailure(res.status, "", "competitor statuses");
        const data = await res.json();
        return data.statuses || [];
    },
    async reinstateCompetitor(compID, playerID, password, opts = {}) {
        // R1: an outbox entry under the competitor's own base, answered at
        // once; `settled` resolves with the status the server stored, or
        // rejects with its refusal (`opts.onRefusal` may claim it).
        return _outboxWrite({
            kind: 'reinstate', compID, matchID: '', method: 'POST',
            url: `/api/competitions/${compID}/competitors/${playerID}/reinstate`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
            base: `competitor:${compID}:${playerID}`,
        });
    },
    // overridePoolRanks records a whole group of pool ranks, a chusen's order,
    // as ONE write: ranks is [{ playerId, rank }], and the server records
    // every rank together and answers for the final order only. Sent one rank
    // at a time, the order passed through states nobody chose, so a knockout
    // match whose place the final order keeps could be named and reopened.
    //
    // playerId is REQUIRED on every entry (operator ruling bc-pnum): the
    // server resolves a pool member by id only (poolHasPlayerID,
    // handlers_competition.go) and rejects the request with 400 when it is
    // missing -- two pool members can legally share a display name from
    // different dojos, so a name can never disambiguate them. Neither a name
    // nor a dojo is sent: the server never reads them.
    //
    // forceDownstreamReopen is the operator's confirmation of a 409
    // downstream_knockout_played refusal: a new order that moves a qualifier
    // out of a knockout match the old one already fought is refused exactly
    // like a pool result correction (and one being fought is refused outright,
    // downstream_knockout_running). Both are parsed into the same structured
    // errors recordScore throws, so the chusen panel offers the same
    // confirm-and-retry (write_result.jsx's attemptScoreWrite); `ranking`
    // marks the refusal as a rank change rather than a corrected result, for
    // the dialog's copy.
    overridePoolRanks(compID, poolID, ranks, password, forceDownstreamReopen, opts = {}) {
        // bc-evts stage 3 review 11, Y7 (the governing rule: "ALL goes to the
        // outbox ... but never blocks the UI"): an outbox entry under the
        // competition's pool-rank base, so a chusen's order and a reset of
        // the overrides are sent strictly in the order they were made,
        // answered pending at once. `settled` carries the server's answer, or
        // rejects with the refusal (_doorRefusal 'pool-ranks': a downstream
        // refusal is the structured error attemptScoreWrite reads, marked as
        // a ranking), which `opts.onRefusal` may claim. An older server's
        // empty 200 body is a success.
        const payload = { ranks };
        if (forceDownstreamReopen) payload.forceDownstreamReopen = true;
        return _outboxWrite({
            kind: 'pool-ranks', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/pools/${encodeURIComponent(poolID)}/override-rank`,
            payload, password, onRefusal: opts && opts.onRefusal,
            base: _poolRanksBase(compID),
        });
    },
    // Phase 3b (mp-8rc9): league tie-breaker operator API.
    //
    // leagueTiebreakCandidates: GET /competitions/:id/league-tiebreak/candidates
    // Returns { candidates: [{teamNames, teams, minPosition, maxPosition}] }
    // where teams is [{id, name, dojo}] (id "" for a legacy competitor): groupTeamIds
    // in admin_pools.jsx derives the teamIds a namesake group needs from it.
    async leagueTiebreakCandidates(compID) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/candidates`, { headers: readHeaders() });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error || `Failed to load league tie-breaker candidates (${res.status})`);
        }
        return res.json();
    },
    // chusenCandidates: GET /competitions/:id/chusen-candidates
    // Consequential team-pool ties the daihyosen could not settle; the operator
    // resolves each by chusen (drawing lots), recorded via overridePoolRanks.
    async chusenCandidates(compID, password) {
        // Lives in the admin-gated competition router, so it needs the
        // admin password header (unlike the public league candidates GET).
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/chusen-candidates`, {
            headers: { 'X-Tournament-Password': password || '' }
        });
        if (!res.ok) {
            // Surface the server-provided error + status so auth/config failures
            // are diagnosable from the admin UI, matching the other helpers.
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load chusen candidates (${res.status})`);
        }
        const data = await res.json();
        // candidates: ties still waiting for a chusen. recorded: ties a chusen
        // already settled, each with the recorded `ranks` parallel to `teams`,
        // so a chusen recorded in the wrong order can be changed.
        return { candidates: data.candidates || [], recorded: data.recorded || [] };
    },
    // leagueTiebreakGenerate: POST /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }. teamIds is REQUIRED
    // (operator ruling bc-pnum): the server selects the tied group by id
    // only and 400s outright when teamIds is missing, has fewer than two
    // entries, carries a blank entry, or has a duplicate -- teamNames alone
    // is ambiguous the moment two tied teams share a display name across
    // dojos, which this project explicitly allows. The caller MUST NOT
    // invoke this without a real id for every team (admin_pools.jsx gates
    // the "Run tie-breaker" button on groupTeamIds(...) !== undefined for
    // exactly this reason, disabling it rather than sending a request that
    // can only 400).
    // Returns { matches: MatchResult[] } on 201.
    // Throws on 400 (invalid group), 409 (matches already exist).
    async leagueTiebreakGenerate(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to generate league tie-breaker (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },
    // leagueTiebreakRemove: DELETE /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }: the tied group whose
    // unscored matches to remove. teamIds is REQUIRED, same contract as
    // leagueTiebreakGenerate above -- a namesake-holding group can only ever
    // have been CREATED via teamIds, so it must be removable the same way
    // or it could never be deleted again. Same UI-level gating applies: the
    // "Remove unscored tie-breaker" button is disabled when groupTeamIds(...)
    // is undefined.
    // Returns { deleted: number } on 200.
    // Throws on 404 (no matches found), 409 (any match already scored).
    async leagueTiebreakRemove(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to remove league tie-breaker matches (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },
    // runTiebreakerRound: POST /competitions/:id/pools/:poolId/tiebreak-round
    // "Run another tie-breaker round" over an individual tie a finished
    // round left level. Body { playerIds }: one run of the
    // chusen candidates' stillLevel, by participant id only. Returns
    // { matches } on 201, the bouts added. Throws on 400 with the server's
    // sentence (no longer such a run, the lots already ordered it, the pools
    // are over) and 404 (competition or pool not found).
    async runTiebreakerRound(compID, poolName, playerIds, password) {
        const res = await _directFetch(`/api/competitions/${encodeURIComponent(compID)}/pools/${encodeURIComponent(poolName)}/tiebreak-round`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ playerIds }),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Could not add the round. Check the connection and try again.");
        }
        return res.json();
    },
};
