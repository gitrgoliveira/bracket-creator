// The tournament's API members: the tournament itself and its password, the
// sign-in mode and the reset, backups and restore, court calls
// (announcements), sponsors and branding.
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

import { NOT_SENT_NO_CONNECTION } from './write_result.jsx';
import { TOURNAMENT_BASE, tournamentRefusal } from './document_versions.jsx';
import {
    _directFetch, _backupRequest, _backupFilename, _restoreForm, readHeaders, readFailure, refuseMaskedForOrganiser,
} from './api_client_requests.jsx';
import { _serverNowMs } from './api_client_clock.jsx';
import { _dequeue, _entriesFor } from './api_client_queue.jsx';
import {
    _ANNOUNCE_CLEAR_BASE, _announceBase, _courtCallLabel, currentDocVersion, rebaseQueuedVersion,
    overlayPendingTournament,
} from './api_client_overlays.jsx';
import { _forgetAllShownMatches } from './api_client_status.jsx';
import { _outboxWrite, _isFollowed } from './api_client_enqueue.jsx';
import { _newClientId } from './api_client_members.jsx';
import { _inFlight } from './api_client_store.jsx';

// The password change: direct (_directFetch), with the version it was made
// from. A save of the settings this device still has queued is made stale
// by it, so the caller waits for those first (API.queuedWritesOf).
async function updateTournamentPassword(config, password) {
    const { competitions: _comps, ...settings } = config || {};
    const sent = currentDocVersion(TOURNAMENT_BASE, settings.tournamentVersion);
    const res = await _directFetch('/api/tournament', {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-Tournament-Password': password
        },
        body: JSON.stringify({ ...settings, tournamentVersion: sent })
    });
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw tournamentRefusal(err, res.status);
    }
    const saved = await res.json();
    rebaseQueuedVersion(TOURNAMENT_BASE, 'tournamentVersion', sent, saved && saved.tournamentVersion);
    return saved;
}

export const tournamentApi = {
    // Read as the page reads (readHeaders, bc-hnam): the organiser's password
    // from an admin route, the reveal keys from a public page.
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament', { headers: readHeaders() });
        // "No tournament configured yet" is a normal bootstrap state, not an
        // error: return null so callers open the create-tournament gate quietly
        // instead of logging a console error. Newer servers signal it with a 200
        // and a null JSON body (handled by the res.json() return below); older
        // servers used a 404 (handled here). Genuine failures (5xx, network)
        // still throw below.
        if (res.status === 404) {
            return null;
        }
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw readFailure(res.status, err.error, 'tournament');
        }
        // A settings save this device still has queued shows (G4b).
        return overlayPendingTournament(refuseMaskedForOrganiser(await res.json()));
    },
    // Public endpoint: returns {mode: "file"|"locked", resetEnabled: bool}.
    // Mounted on App() boot to decide whether to render the "Forgot
    // password?" link in AuthModal and whether the /reset SPA route
    // should show a form or an "operator-disabled" message. Always
    // resolves (never rejects): non-2xx responses, network failures, and
    // JSON parse errors all fall back to the file-mode default so a
    // fresh-deploy SPA pointed at an older server without this endpoint
    // still works, and any transport error doesn't break sign-in.
    async fetchAuthConfig() {
        try {
            const res = await fetch('/api/auth-config');
            if (!res.ok) {
                return { mode: 'file', resetEnabled: true };
            }
            return await res.json();
        } catch {
            return { mode: 'file', resetEnabled: true };
        }
    },
    async fetchVersion() {
        try {
            const res = await fetch('/api/version');
            if (!res.ok) {
                return null;
            }
            return await res.json();
        } catch {
            return null;
        }
    },
    // Reset the admin password. Unauthenticated by design: the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). In file mode it answers
    // only a request made on the server machine (loopback peer and
    // loopback Host), else 403 reset_local_only. Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional: when absent the server
    // broadcasts an empty originator and ALL tabs log out.
    async resetPassword(newPassword, originatorId) {
        const body = { password: newPassword };
        if (originatorId) body.originatorId = originatorId;
        const res = await _directFetch('/api/tournament/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to reset password (Status ${res.status})`);
            e.status = res.status;
            // The refusal code (reset_local_only) lets the form word the
            // case; absent on a bare error.
            if (err.code) e.code = err.code;
            throw e;
        }
        // A reset: no learned match label is trusted (K8).
        _forgetAllShownMatches();
        // Backend returns 204 No Content: calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRanks etc. above).
        return true;
    },
    // Bootstrap a fresh tournament. In file mode the call is
    // unauthenticated (the AuthMiddleware's uninitialized-bootstrap
    // branch lets it through). In locked mode the server requires the
    // env-var password to authorize even the initial POST: pass
    // `authPassword` (typed by the operator into the locked-mode
    // CreateTournament form) so we can attach it as
    // X-Tournament-Password. The handler discards the body's
    // `password` field in locked mode but the header is what gets
    // verified.
    async createTournament(config, authPassword) {
        const headers = { 'Content-Type': 'application/json' };
        if (authPassword) {
            headers['X-Tournament-Password'] = authPassword;
        }
        const res = await _directFetch('/api/tournament', {
            method: 'POST',
            headers,
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create tournament");
        }
        return res.json();
    },
    // bc-obox G4b: the tournament settings, an outbox entry under the base
    // `tournament`, answered at once like every outbox door ({ pending,
    // settled }); `settled` resolves with the saved settings (their
    // tournamentVersion with them) or rejects with the refusal (409
    // tournament_changed carries `code` and `tournament`, the copy now). The
    // body never carries a password: an empty one keeps the stored one, so a
    // descriptor at rest holds none. A save that CHANGES the password (a body
    // password other than the session's, `password`) is the auth door and
    // stays direct (updateTournamentPassword), answering as it always did.
    async updateTournament(config, password, opts = {}) {
        const body = config || {};
        if (typeof body.password === 'string' && body.password !== '' && body.password !== password) {
            return updateTournamentPassword(body, password);
        }
        const { competitions: _comps, ...settings } = body;
        return _outboxWrite({
            kind: 'tournament', compID: '', matchID: '', method: 'PUT', url: '/api/tournament',
            payload: { ...settings, password: '', tournamentVersion: currentDocVersion(TOURNAMENT_BASE, settings.tournamentVersion) },
            password, onRefusal: opts && opts.onRefusal, base: TOURNAMENT_BASE, label: 'Tournament settings',
        });
    },
    updateTournamentPassword,
    async backupTournament(password) {
        const res = await _backupRequest('/api/tournament/backup', 'Failed to download tournament backup', { password });
        const filename = _backupFilename(res);
        const blob = await res.blob();
        return { blob, filename };
    },
    async backupCompetition(id, password) {
        const res = await _backupRequest(`/api/competitions/${encodeURIComponent(id)}/backup`, 'Failed to download competition backup', { password });
        const filename = _backupFilename(res);
        const blob = await res.blob();
        return { blob, filename };
    },
    planRestore({ file, from, only, undo, decisions } = {}, password) {
        return _backupRequest('/api/tournament/restore/plan', 'Failed to plan restore', {
            method: 'POST', password, body: _restoreForm({ file, from, only, undo, decisions }),
            sentence: NOT_SENT_NO_CONNECTION // the plan is a dry run: it changes nothing
        }).then(r => r.json());
    },
    async restore({ file, from, only, undo, decisions, allowRunning } = {}, password, confirmPassword) {
        const fd = _restoreForm({ file, from, only, undo, decisions });
        if (allowRunning === true) fd.append('allowRunning', 'true');
        const res = await _backupRequest('/api/tournament/restore', 'Failed to restore', {
            method: 'POST', password, confirmPassword: confirmPassword ?? '', body: fd
        });
        _forgetAllShownMatches();
        return res.json();
    },
    async listBackups(password) {
        const data = await (await _backupRequest('/api/tournament/backups', 'Failed to list backups', { password })).json();
        return data.backups || [];
    },
    downloadBackup(name, password) {
        return _backupRequest(`/api/tournament/backups/${encodeURIComponent(name)}`, 'Failed to download backup', { password }).then(r => r.blob());
    },
    deleteBackup(name, password, confirmPassword) {
        return _backupRequest(`/api/tournament/backups/${encodeURIComponent(name)}`, 'Failed to delete backup', {
            method: 'DELETE', password, confirmPassword: confirmPassword ?? ''
        }).then(r => r.json());
    },
    // bc-obox A1: a court call, an outbox entry under its own base
    // `announce:<id>`. The device makes the id (a lowercase UUID v4) and the
    // answer carries it at once ({ pending, settled, id }), so a withdrawal can
    // name the call before it has landed. `settled` resolves with the call as
    // the server stored it (`recorded` when the id was already known,
    // `expired` when its time was over on arrival), or { discarded: true,
    // withdrawn: true } when it was withdrawn before it was sent; a refusal
    // rejects.
    async sendAnnouncement(message, durationMinutes, password, opts = {}) {
        const id = _newClientId();
        const answer = _outboxWrite({
            kind: 'announce', compID: '', matchID: '', method: 'POST', url: '/api/tournament/announce',
            payload: { id, message, durationMinutes, modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _announceBase(id), label: _courtCallLabel(message),
        });
        return { ...answer, id };
    },
    async fetchAnnouncements() {
        const res = await fetch('/api/tournament/announcements');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch announcements (Status ${res.status})`);
        }
        return res.json();
    },
    // bc-obox A1: a court call withdrawn, an outbox entry in the call's base,
    // so it goes after the call's send. `settled` resolves true once the call
    // is down, a 404 included (it is already gone: it expired, or another
    // console withdrew it first, which is the outcome asked for, bc-cdbl); a
    // refusal rejects. A withdrawal made while the call's send is still
    // queued and not yet out takes the send out of the outbox, so the venue
    // screens never show a call already taken back: the send settles
    // { discarded: true, withdrawn: true }. The withdrawal is queued all the
    // same: another tab of this device may have that send out, and it can land
    // after the withdrawal does. The server remembers a withdrawal it could not
    // match (AnnouncementStore.Remove) and records the late send without
    // showing it, so the answer is a 404 or a 200, either one is done.
    async deleteAnnouncement(id, password, opts = {}) {
        const base = _announceBase(id);
        const send = _entriesFor(base).find(([k, d]) => d.kind === 'announce' && !_inFlight.has(d) && !_isFollowed(k));
        if (send) {
            const [key, d] = send;
            _dequeue(key, d, { value: { discarded: true, withdrawn: true } });
        }
        return _outboxWrite({
            kind: 'announce-remove', compID: '', matchID: '', method: 'DELETE',
            url: `/api/announcements/${encodeURIComponent(id)}`,
            payload: {}, password, onRefusal: opts && opts.onRefusal, base,
            label: _courtCallLabel(opts && opts.message),
        });
    },
    // bc-obox A1: every court call made at or before this moment taken down,
    // an outbox entry under its own base `announce-clear`, stamped
    // (`modifiedAt`): the server clears what was made at or before it and
    // keeps that floor, so a call made before the clear that arrives after it
    // shows nothing, and one made after it stands whatever the arrival order.
    // `settled` resolves with the calls left showing; a refusal rejects.
    async clearAnnouncements(password, opts = {}) {
        return _outboxWrite({
            kind: 'announce-clear', compID: '', matchID: '', method: 'DELETE', url: '/api/announcements',
            payload: { modifiedAt: _serverNowMs() },
            password, onRefusal: opts && opts.onRefusal, base: _ANNOUNCE_CLEAR_BASE, label: 'Every court call',
        });
    },
    // mp-c38: sponsor logo upload (multipart) and delete.
    async uploadSponsor({ file, name, link, password }) {
        const fd = new FormData();
        fd.append('name', name);
        if (link) fd.append('link', link);
        fd.append('file', file);
        const res = await _directFetch('/api/sponsors', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload sponsor (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteSponsor(index, password) {
        const res = await _directFetch(`/api/sponsors/${index}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete sponsor (Status ${res.status})`);
        }
    },
    // mp-scf: tournament branding: logo upload/delete.
    async uploadBrandingLogo({ file, password }) {
        const fd = new FormData();
        fd.append('file', file);
        const res = await _directFetch('/api/branding/logo', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload logo (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteBrandingLogo(password) {
        const res = await _directFetch('/api/branding/logo', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete logo (Status ${res.status})`);
        }
    },
};
