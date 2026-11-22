// court_bridge.jsx: BroadcastChannel-based court-local data hub.
//
// Phase 2 of the mp-gpra flaky-wifi hardening initiative (mp-9ukk). When
// the server link is down, the operator tab becomes the display tab's local
// data hub: match patches are broadcast on confirm or enqueue, and the
// display tab can bootstrap from the operator tab's competition snapshot on
// cold load.
//
// ## Message schema (versioned)
//
//   { v: 1, type: 'patch'|'snapshot-req'|'snapshot',
//     origin: <string tabId>,
//     court:  <string, e.g. "A">,
//     compId: <string>,          // patch + snapshot; empty for snapshot-req
//     payload: <object|null> }   // match-patch event data | court slice of competitions
//
// ## Self-echo suppression
//
// Each tab generates a random `_tabId` on first import. Outbound messages
// carry `origin: _tabId`. Inbound messages where `msg.origin === _tabId`
// are silently dropped so no tab reacts to its own broadcasts.
//
// ## Snapshot handshake
//
// On load, the display tab publishes `snapshot-req` for its court. Any
// operator tab that has registered a snapshot provider via
// `setSnapshotProvider(fn)` replies with a `snapshot` containing that
// court's competitions slice. If multiple operator tabs answer, the display
// uses the first reply (the update is idempotent: same court data).
//
// ## Graceful degrade
//
// When BroadcastChannel is unavailable (very old browser) openBridge()
// returns a no-op handle. Behavior degrades to today's SSE-only flow with
// no regression.

import { applyPatchToCompetitions as _applyPatchToCompetitions, matchInComp as _matchInComp } from './patch.jsx';
import { competitionHidesNames, isTeamCompetition } from './competition_shape.jsx';
import { squadMemberLabel } from './squad_member_label.jsx';

// freshnessMs: the amber->red dot threshold.
// 20 s sits below the 35 s SSE silence watchdog and above the 15 s SSE
// heartbeat, so a quiet (but open) operator tab does NOT flap to stale
// between matches, yet a crashed/closed operator tab becomes stale promptly.
// Exported as a named constant so tests and callers share one value.
export const freshnessMs = 20000;

// BroadcastChannel name. All same-origin tabs share one channel; messages
// are scoped by the `court` field inside each message payload.
const CHANNEL_NAME = 'bc-court-hub-v1';

// Module-level tab identity. Stable for the lifetime of this module instance
// (one per tab). crypto.randomUUID is available in modern browsers and jsdom.
export const _tabId = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : `t${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;

// -----------------------------------------------------------------------
// deriveLinkState: pure exported function so vitest can test it without
// mounting any component.
//
// Returns:
//   'connected' : SSE to server is up (sseConnected === true)
//   'local'     : server down, but operator-tab broadcast is fresh
//                 (lastBroadcastAt within freshnessMs of now)
//   'stale'     : no server AND no recent operator broadcast
//
// `fm` defaults to the module constant so callers can pass a custom value
// for testing without altering the module-level default.
// -----------------------------------------------------------------------
export function deriveLinkState({ sseConnected, lastBroadcastAt, now, freshnessMs: fm = freshnessMs }) {
    if (sseConnected) return 'connected';
    if (lastBroadcastAt !== null && (now - lastBroadcastAt) < fm) return 'local';
    return 'stale';
}

// -----------------------------------------------------------------------
// resolveCompId: read a competition's id regardless of shape.
//
// competitions[] entries carry their id at different depths depending on the
// source: top-level `id` for the viewer aggregate, nested under `config` for
// the detail endpoint (and a legacy capital-`Id` arm is tolerated). Returns ''
// for a null/primitive entry or one with no id, so callers can `if (!id)`-skip.
// -----------------------------------------------------------------------
export function resolveCompId(c) {
    if (!c || typeof c !== 'object') return '';
    return c.id || (c.config && (c.config.id || c.config.Id)) || '';
}

// -----------------------------------------------------------------------
// applyPatchToTree: tree-level adapter.
//
// Takes the full in-memory `tournament` (with `tournament.competitions[]`)
// plus one broadcast `msg` (type: 'patch', compId, payload), locates the
// competition by `compId`, delegates the single-competition merge to the
// existing `applyPatch` from patch.jsx, and returns a new tournament object.
//
// Only the touched competition changes reference; all other competitions
// and the surrounding tournament fields are preserved by shallow spread.
// Returns the original `tournament` reference when nothing matched.
// -----------------------------------------------------------------------
// staleRunningOverlay: whether a running patch from this device (the
// operator's board, publishBoardOverlay) is older than the copy a display
// holds that is NOT running: a finished or not-started match whose server
// scoreVersion is at least the board's (stage 3 review 11, Y1). The editor
// keeps publishing its running board until it closes, and a board answered
// after the finish landed would otherwise show the match running again. A
// newer server copy (a reopen) still shows; a copy or a patch with no version
// cannot be weighed and the patch applies as it always did.
export function staleRunningOverlay(existing, result) {
    if (!existing || !result || result.status !== 'running' || existing.status === 'running') return false;
    const held = Number(existing.scoreVersion) || 0;
    const board = Number(result.scoreVersion) || 0;
    return held > 0 && board > 0 && held >= board;
}

export function applyPatchToTree(tournament, msg) {
    // A patch with no payload is a no-op: bail before the findIndex scan and
    // before the patch can dereference a missing payload.
    if (!tournament || !msg || msg.type !== 'patch' || !msg.payload) return tournament;
    const competitions = tournament.competitions;
    if (!competitions || competitions.length === 0) return tournament;

    const compId = msg.compId;
    const idx = compId
        ? competitions.findIndex(c => resolveCompId(c) === compId)
        : -1;
    if (idx === -1) return tournament;

    const prevComp = competitions[idx];
    const results = Array.isArray(msg.payload.results) ? msg.payload.results : [msg.payload.result];
    // A patch that names the status it moves from (`ifStatus`: a send back and
    // its put-back, bc-stpf) applies only while this display holds the match
    // in that status: one it already holds finished, from another device's
    // finish, stays finished. A match it does not hold applies as before.
    const ifStatus = msg.payload.ifStatus;
    if (ifStatus && results.some((r) => {
        const held = r && r.id != null ? _matchInComp(prevComp, r.id) : null;
        return held && held.status !== ifStatus;
    })) {
        return tournament;
    }
    // A running board never puts back to running a match this display holds
    // finished or not started, unless the board is of a newer server copy
    // (staleRunningOverlay, stage 3 review 11, Y1).
    if (results.length > 0 && results.every((r) => staleRunningOverlay(r && r.id != null ? _matchInComp(prevComp, r.id) : null, r))) {
        return tournament;
    }
    // Wrap the broadcast payload in a synthetic SSE match_updated envelope
    // so applyPatch (which expects {type, data}) can process it directly.
    const syntheticEvent = {
        type: 'match_updated',
        data: msg.payload,
    };
    // patch.jsx's applyPatch filters null/id-less result entries before
    // building its lookup Map, so a payload like results:[null] no longer
    // throws. This try/catch is kept as defense-in-depth: it runs inside a
    // React state updater (deferred, outside any handler try/catch) fed by
    // arbitrary same-origin BroadcastChannel messages, so any future throw
    // path in the patch is still absorbed rather than crashing the display
    // board into the error boundary.
    // The tree holds every competition the court's queue counts, so a patch
    // that moves a match into or out of the queue re-stamps the court-wide
    // numbers (applyPatchToCompetitions).
    let nextCompetitions;
    try {
        nextCompetitions = _applyPatchToCompetitions(competitions, compId, syntheticEvent, resolveCompId);
    } catch (err) {
        console.error('court_bridge: applyPatch failed for a malformed payload:', err);
        return tournament;
    }
    if (nextCompetitions === competitions) return tournament;
    return { ...tournament, competitions: nextCompetitions };
}

// -----------------------------------------------------------------------
// mergeSnapshotIntoTree: fold an operator-tab court snapshot into the
// display's in-memory tournament. Pure (and exported) so the merge policy
// is unit-testable apart from the React effect that drives it.
//
//   prev          : current tournament (null/undefined before the first load)
//   incomingComps : the snapshot payload (expected: array of competitions)
//   connected     : whether the SSE feed is currently up
//
// Policy:
//   - non-array payload          -> prev unchanged
//   - no prev (cold start)       -> bootstrap a minimal tournament wrapper.
//                                   UNCONDITIONAL by design: an outage AT LOAD
//                                   must not be starved of the snapshot even
//                                   when `connected` is still its optimistic
//                                   mount default.
//   - prev present AND connected -> prev unchanged: the server feed is
//                                   authoritative, so a late snapshot is dropped
//                                   rather than allowed to overwrite server data.
//   - prev present AND offline   -> replace existing competitions by id, append
//                                   any new ones (the operator tab is the court
//                                   authority during an outage; the reconnect
//                                   full-refetch later restores server truth).
//
// Null/primitive elements and elements with no resolvable id are skipped.
// -----------------------------------------------------------------------
export function mergeSnapshotIntoTree(prev, incomingComps, { connected } = {}) {
    if (!Array.isArray(incomingComps)) return prev;
    // Keep only well-formed competitions (a resolvable id) up front, so BOTH the
    // cold-start and the offline-merge paths are protected: a malformed or rogue
    // same-origin snapshot can never inject null/primitive/id-less values into
    // tournament.competitions.
    const valid = incomingComps.filter(c => resolveCompId(c));
    if (!prev) {
        return { name: '', courts: [], competitions: valid };
    }
    if (connected) return prev;
    const existing = prev.competitions || [];
    const merged = existing.slice();
    for (const comp of valid) {
        const id = resolveCompId(comp);
        const idx = merged.findIndex(c => resolveCompId(c) === id);
        if (idx === -1) {
            merged.push(comp);
        } else {
            merged[idx] = comp;
        }
    }
    return { ...prev, competitions: merged };
}

// -----------------------------------------------------------------------
// Module-level recency tracker. Shared across all bridge instances in this
// tab (there should only ever be one open bridge per tab in normal use).
// Updated on every outbound patch/snapshot AND every inbound patch/snapshot,
// so the display tab's recency clock also advances when it receives a reply.
// -----------------------------------------------------------------------
let _lastBroadcastAt = null;

// -----------------------------------------------------------------------
// Court-scope filter for inbound recency tracking (FIX 2).
//
// When a display court is set, only patches/snapshots for that court
// advance the recency clock. This prevents a court-B patch from making
// the court-A display dot falsely show 'local'.
// When null (operator tabs, no specific display court), all inbound
// messages advance the clock.
// -----------------------------------------------------------------------
let _displayCourt = null;

export function setDisplayCourt(c) {
    const next = c || null;
    // Reset the recency clock when the court actually changes so the dot does
    // not inherit the previous court's freshness: after a ?court= switch the
    // new court starts 'stale' until its own operator broadcasts.
    if (_displayCourt !== next) _lastBroadcastAt = null;
    _displayCourt = next;
}

// getLastBroadcastAt: read the module-level recency timestamp.
// Called by app.jsx in its linkState-derivation tick.
export function getLastBroadcastAt() {
    return _lastBroadcastAt;
}

// -----------------------------------------------------------------------
// maskSnapshotForBridge: the competitions slice an operator tab hands to a
// display tab, with every competitor of a competition whose public pages hide
// names (competitionHidesNames) carried by number and id alone (bc-hnam).
//
// The operator tab holds the organiser's copy, names and all; the display tab
// is a PUBLIC surface, so a same-origin snapshot must not carry what the
// server's masked payload would not. A competitor record (an object carrying a
// number, an id the roster maps to one, or a name and a dojo) gets its name, display name and
// zekken name set to its number and is stamped `nameHidden`; a match side, a
// winner or a representative named by a bare string is replaced by the number
// of the competitor its id names (else of the one roster name that is unique),
// never by a name. A name no roster entry can place reads "?", as the server's
// maskView.sideReading reads it; only an empty side and the draw's placeholders
// ("Winner of ...", "Pool A-1st") stay as they are. The competition is
// stamped `namesHidden`, as a server payload built for a hidden reader is.
// A team competition keeps its team names and carries its team members the
// way the server does (see maskTeamNode). A competition that shows names, or
// any competition of a self-run tournament, passes through untouched. A match
// PATCH is masked the same way before it is published (maskPatchForBridge).
//
// Pure and exported so vitest can pin it.
// -----------------------------------------------------------------------
const MASK_STRING_SIDES = [
    ['sideA', ['sideAId', 'sideAID']],
    ['sideB', ['sideBId', 'sideBID']],
    ['winner', ['winnerId', 'winnerID']],
    ['repPlayerA', []],
    ['repPlayerB', []],
];
const MASK_NAME_FIELDS = ['displayName', 'zekkenName', 'zekken'];
// The draw's two placeholder shapes, which are not competitors (helper.
// IsWinnerOfPlaceholder and IsPoolFinalistPlaceholder). The shared table
// public_names_side_cases.json pins this reading to the server's.
const DRAW_PLACEHOLDER_RE = /^(Winner of r\d+-m\d+|Pool .+-\d+(st|nd|rd|th))$/;

function hasCompetitorNumber(node) {
    return node.number != null && String(node.number).trim() !== '';
}

// A roster record is a node with a name and a number. In an individual
// competition (failClosed) a record with a name, a dojo and NO number is one
// too, and reads "?" as the server's mask does for a competitor it cannot
// place, so a competitor the draw has not numbered is never carried by name.
function collectNumbers(node, ctx, failClosed) {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) {
        node.forEach((n) => collectNumbers(n, ctx, failClosed));
        return;
    }
    const numbered = hasCompetitorNumber(node);
    if (typeof node.name === 'string' && (numbered || (failClosed && 'dojo' in node))) {
        const num = numbered ? String(node.number).trim() : '?';
        if (node.id != null && node.id !== '') {
            ctx.byId.set(String(node.id), num);
            ctx.nameById.set(String(node.id), node.name);
        }
        if (ctx.byName.has(node.name) && ctx.byName.get(node.name) !== num) ctx.clashing.add(node.name);
        ctx.byName.set(node.name, num);
    }
    for (const v of Object.values(node)) collectNumbers(v, ctx, failClosed);
}

function maskNode(node, ctx) {
    if (Array.isArray(node)) return node.map((n) => maskNode(n, ctx));
    if (!node || typeof node !== 'object') return node;
    const out = {};
    for (const [k, v] of Object.entries(node)) out[k] = maskNode(v, ctx);
    const own = hasCompetitorNumber(node) ? String(node.number).trim() : '';
    const mapped = node.id != null && node.id !== '' ? ctx.byId.get(String(node.id)) : undefined;
    // A competitor record (a name and a dojo) the roster cannot place, a
    // legacy row with no id and no number among them, reads "?", as
    // collectNumbers reads it: never its name.
    const record = typeof node.name === 'string' && 'dojo' in node;
    const num = own || mapped || (record ? '?' : '');
    if (num && typeof node.name === 'string') {
        out.name = num;
        out.nameHidden = true;
        for (const f of MASK_NAME_FIELDS) {
            if (typeof node[f] === 'string' && node[f]) out[f] = num;
        }
    }
    for (const [f, idKeys] of MASK_STRING_SIDES) {
        const name = node[f];
        if (typeof name !== 'string' || !name) continue;
        let found = '';
        for (const ik of idKeys) {
            if (node[ik] != null && node[ik] !== '' && ctx.byId.has(String(node[ik]))) {
                found = ctx.byId.get(String(node[ik]));
                break;
            }
        }
        if (!found && ctx.byName.has(name)) {
            found = ctx.clashing.has(name) ? '?' : ctx.byName.get(name);
        }
        if (found) out[f] = found;
        else if (!DRAW_PLACEHOLDER_RE.test(name)) out[f] = '?';
    }
    return out;
}

// A team competition keeps its team names (the teams are the competitors) and
// hides its team MEMBERS, as the server's masked payload does (maskView.bouts,
// memberByName, squads): a member reads as their label ("T10.1"), or "?" when
// no label can be made. A team record loses its metadata, and the dan grade
// read from it, as maskView.player clears Metadata: a legacy roster held the
// members' names there. The members come from the competition's own
// teamMembers/squads map (team id to its members); a bout row names its
// fighters by member id, a representative bout by name.
const MEMBER_MAPS = ['teamMembers', 'squads'];

function isPlainObject(v) {
    return !!v && typeof v === 'object' && !Array.isArray(v);
}

function collectMembers(node, ctx) {
    if (!node || typeof node !== 'object') return;
    if (Array.isArray(node)) {
        node.forEach((n) => collectMembers(n, ctx));
        return;
    }
    for (const [k, v] of Object.entries(node)) {
        if (MEMBER_MAPS.includes(k) && isPlainObject(v)) {
            for (const [teamId, members] of Object.entries(v)) {
                if (!Array.isArray(members)) continue;
                const names = ctx.namesByTeam.get(teamId) || new Map();
                for (const m of members) {
                    if (!m || m.id == null || m.id === '') continue;
                    ctx.memberTeam.set(String(m.id), teamId);
                    ctx.memberLabel.set(String(m.id), squadMemberLabel(ctx.byId.get(teamId), m.index) || '?');
                    if (typeof m.name === 'string' && m.name) names.set(m.name, names.has(m.name) ? null : String(m.id));
                }
                ctx.namesByTeam.set(teamId, names);
            }
        } else {
            collectMembers(v, ctx);
        }
    }
}

// maskMember: what a team member's name reads as. A team's own name stays
// (a quick-score row names the two teams).
function maskMember(ctx, memberId, name, teamName) {
    if (!name) return name;
    if (teamName && name === teamName) return name;
    if (memberId == null || memberId === '') return '?';
    return ctx.memberLabel.get(String(memberId)) || '?';
}

// A patch names its sides by id only, so the team's name comes from the roster.
function sideIdentity(side, id, ctx) {
    const own = id != null && id !== '' ? id : (side && typeof side === 'object' ? side.id : '');
    const ownId = own == null ? '' : String(own);
    const name = typeof side === 'string' ? side : (side && side.name) || ctx.nameById.get(ownId) || '';
    return { name, id: ownId };
}

function maskTeamMatch(node, out, ctx) {
    const A = sideIdentity(node.sideA, node.sideAId ?? node.sideAID, ctx);
    const B = sideIdentity(node.sideB, node.sideBId ?? node.sideBID, ctx);
    out.subResults = node.subResults.map((row) => {
        if (!row || typeof row !== 'object') return row;
        const masked = { ...row };
        masked.sideA = maskMember(ctx, row.sideAMemberId, row.sideA, A.name);
        masked.sideB = maskMember(ctx, row.sideBMemberId, row.sideB, B.name);
        const w = row.winner;
        if (w && w !== A.name && w !== B.name) {
            if (row.winnerMemberId) masked.winner = maskMember(ctx, row.winnerMemberId, w, '');
            else if (w === row.sideA) masked.winner = masked.sideA;
            else if (w === row.sideB) masked.winner = masked.sideB;
            else masked.winner = '?';
        }
        return masked;
    });
    // A representative bout's fighter is recorded by name alone: it reads as
    // the one member of that team the name names, else "?".
    const rep = (name, teamId) => {
        if (typeof name !== 'string' || !name) return name;
        const id = (ctx.namesByTeam.get(teamId) || new Map()).get(name);
        return maskMember(ctx, id, name, '');
    };
    if (node.repPlayerA) out.repPlayerA = rep(node.repPlayerA, A.id);
    if (node.repPlayerB) out.repPlayerB = rep(node.repPlayerB, B.id);
}

function maskTeamNode(node, ctx) {
    if (Array.isArray(node)) return node.map((n) => maskTeamNode(n, ctx));
    if (!node || typeof node !== 'object') return node;
    const out = {};
    for (const [k, v] of Object.entries(node)) {
        if (MEMBER_MAPS.includes(k) && isPlainObject(v)) {
            const masked = {};
            for (const [teamId, members] of Object.entries(v)) {
                masked[teamId] = Array.isArray(members)
                    ? members.map((m) => (m && typeof m === 'object' ? { ...m, name: maskMember(ctx, m.id, m.name, '') } : m))
                    : members;
            }
            out[k] = masked;
        } else {
            out[k] = maskTeamNode(v, ctx);
        }
    }
    if (typeof node.name === 'string') {
        if ('metadata' in node) out.metadata = [];
        if ('danGrade' in node) out.danGrade = '';
    }
    if (Array.isArray(node.subResults)) maskTeamMatch(node, out, ctx);
    return out;
}

// maskAwards: a fighting spirit award names its recipient by name and dojo, and
// the server reads it as the number or team member label of the competitor it
// finds (maskView.awardRecipient), "?" when none. The bridge reads the one
// competitor or member holding that name, "?" when none or several do.
function maskAwards(comp, ctx, team) {
    if (!Array.isArray(comp.fightingSpiritAwards)) return comp;
    const reading = (name) => {
        if (!name) return name;
        if (!team) return ctx.byName.has(name) && !ctx.clashing.has(name) ? ctx.byName.get(name) : '?';
        const ids = [];
        for (const names of ctx.namesByTeam.values()) if (names.has(name)) ids.push(names.get(name));
        return ids.length === 1 && ids[0] ? ctx.memberLabel.get(String(ids[0])) || '?' : '?';
    };
    return {
        ...comp,
        fightingSpiritAwards: comp.fightingSpiritAwards.map((a) => (
            a && typeof a === 'object' && typeof a.recipientName === 'string'
                ? { ...a, recipientName: reading(a.recipientName) }
                : a)),
    };
}

// `tournament` is the operator tab's tournament: a self-run one shows names
// whatever a competition's setting says (competitionHidesNames), as the server
// does. Without it the tournament is read as an officiated one.
export function maskSnapshotForBridge(comps, tournament = null) {
    if (!Array.isArray(comps)) return comps;
    const hides = (comp) => !!comp && typeof comp === 'object' && competitionHidesNames(tournament, comp);
    // The slice itself comes back when no competition in it hides its names.
    if (!comps.some(hides)) return comps;
    return comps.map((comp) => {
        if (!hides(comp)) return comp;
        const ctx = maskContext(comp);
        const team = isTeamCompetition(comp);
        const masked = team ? maskTeamNode(comp, ctx) : maskNode(comp, ctx);
        return { ...maskAwards(masked, ctx, team), namesHidden: true };
    });
}

function maskContext(comp) {
    const team = isTeamCompetition(comp);
    const ctx = {
        byId: new Map(), byName: new Map(), clashing: new Set(), nameById: new Map(),
        memberTeam: new Map(), memberLabel: new Map(), namesByTeam: new Map(),
    };
    collectNumbers(comp, ctx, !team);
    if (team) collectMembers(comp, ctx);
    return ctx;
}

// maskPatchForBridge: a match patch (`payload.result`, or `payload.results`)
// about competition `comp`, with its competitors carried as maskSnapshotForBridge
// carries them: a winner or a representative named by a bare string, and a team
// bout's fighters, read as the number or label the roster gives them, "?" when
// no roster entry can place them. A competition that shows its names, or one of a self-run tournament, passes
// through by reference. Pure and exported so vitest can pin it.
export function maskPatchForBridge(payload, comp, tournament = null) {
    if (!payload || !comp || !competitionHidesNames(tournament, comp)) return payload;
    const ctx = maskContext(comp);
    const team = isTeamCompetition(comp);
    const maskResult = (r) => {
        if (!r || typeof r !== 'object') return r;
        if (team) {
            const hasRows = Array.isArray(r.subResults);
            const out = maskTeamNode(hasRows ? r : { ...r, subResults: [] }, ctx);
            if (!hasRows) delete out.subResults;
            return out;
        }
        return maskNode(r, ctx);
    };
    const out = { ...payload };
    if (payload.result) out.result = maskResult(payload.result);
    if (Array.isArray(payload.results)) out.results = payload.results.map(maskResult);
    return out;
}

// Module-level snapshot provider registry.
//
// The operator tab calls setSnapshotProvider(fn) after loading its
// competition data. When a snapshot-req arrives, openBridge replies
// automatically using this provider. Only one provider is registered at a
// time; re-registering replaces the previous one (correct because one
// operator tab drives one court).
//
// fn signature: (court: string) => competitions[] | null
//   Return null when the tab has no data for the requested court.
// tournamentFn (optional): () => the operator tab's tournament, which the
//   snapshot's name mask asks whether names are shown (a self-run tournament
//   always shows them).
// -----------------------------------------------------------------------
let _snapshotProvider = null;
let _snapshotTournament = null;

// maskOutgoingPatch: the patch this tab publishes for compId, masked as the
// snapshot is when the competition hides its names. The competition comes from
// the registered provider's slice for the court; a tab with no provider holds
// no tournament, and the patch goes as it is. With a provider, a competition it
// cannot place gives null: the patch is not sent, since it could carry names,
// and the display takes the server's copy at its next refresh.
function maskOutgoingPatch(payload, court, compId) {
    if (!_snapshotProvider || !payload) return payload;
    const comp = (_snapshotProvider(court) || []).find((c) => resolveCompId(c) === compId);
    if (!comp) return null;
    return maskPatchForBridge(payload, comp, _snapshotTournament ? _snapshotTournament() : null);
}

export function setSnapshotProvider(fn, tournamentFn) {
    _snapshotProvider = typeof fn === 'function' ? fn : null;
    _snapshotTournament = typeof tournamentFn === 'function' ? tournamentFn : null;
}

// Testing only: reset all module-level state so test suites that assert on
// _lastBroadcastAt, _displayCourt, or _snapshotProvider do not depend on
// test-execution order. Call in beforeEach of any suite that touches these.
export function _resetModuleStateForTests() {
    _lastBroadcastAt = null;
    _displayCourt = null;
    _snapshotProvider = null;
    _snapshotTournament = null;
}

// -----------------------------------------------------------------------
// openBridge: create a BroadcastChannel handle.
//
// Returns a no-op object when BroadcastChannel is unavailable OR when
// constructing it throws (e.g. a SecurityError in a sandboxed/blocked
// context), so all callers work without guards. This must not throw: the
// module-level `bridge` singleton is created at import time, so a throw here
// would crash the whole app instead of degrading to the SSE-only path.
// -----------------------------------------------------------------------
const _noopBridge = {
    publish: () => {},
    onMessage: () => () => {},
    close: () => {},
};

export function openBridge() {
    if (typeof BroadcastChannel === 'undefined') {
        return _noopBridge;
    }

    let channel;
    try {
        channel = new BroadcastChannel(CHANNEL_NAME);
    } catch (err) {
        console.error('court_bridge: BroadcastChannel unavailable, degrading to SSE-only:', err);
        return _noopBridge;
    }
    const handlers = new Set();

    channel.onmessage = (evt) => {
        const msg = evt.data;
        if (!msg || msg.v !== 1) return;
        // Self-echo suppression.
        if (msg.origin === _tabId) return;

        // Update recency tracker on inbound patch or snapshot data.
        // When a display court is set, only messages for that court advance
        // the clock; when null (operator tabs) all inbound messages advance it.
        if ((msg.type === 'patch' || msg.type === 'snapshot') &&
            (!_displayCourt || msg.court === _displayCourt)) {
            _lastBroadcastAt = Date.now();
        }

        // snapshot-req is always fully handled here (never forwarded to
        // subscriber handlers below), whether or not this tab happens to have
        // a provider registered: an unanswered snapshot-req has no meaning to
        // any current subscriber (they only act on 'patch'/'snapshot'), so
        // there is nothing to gain by passing it through.
        if (msg.type === 'snapshot-req') {
            if (_snapshotProvider) {
                const court = msg.court || '';
                // One try/catch covers both the provider call and postMessage:
                // either failing just logs and continues. A dropped reply is
                // recovered the next time the display publishes snapshot-req
                // (on its effect mount or a court change); there is no
                // periodic re-request.
                try {
                    const slice = _snapshotProvider(court);
                    if (slice && slice.length > 0) {
                        // Reply with one aggregate snapshot (compId empty, payload
                        // = the court's competitions slice) so the display
                        // bootstraps its whole court view in one message.
                        channel.postMessage({
                            v: 1,
                            type: 'snapshot',
                            origin: _tabId,
                            court,
                            compId: '',
                            payload: maskSnapshotForBridge(slice, _snapshotTournament ? _snapshotTournament() : null),
                        });
                        _lastBroadcastAt = Date.now();
                    }
                } catch (err) {
                    console.error('court_bridge: snapshot-req responder failed:', err);
                }
            }
            return;
        }

        for (const h of handlers) {
            try { h(msg); } catch (err) { console.error('court_bridge handler error:', err); }
        }
    };

    // Surface structured-clone deserialization failures (otherwise the browser
    // swallows them silently) so a bad cross-tab message is visible in DevTools.
    channel.onmessageerror = (evt) => {
        console.error('court_bridge: message deserialization error:', evt);
    };

    const bridge = {
        // publish: send a versioned message on the channel.
        //   type   : 'patch' | 'snapshot-req' | 'snapshot'
        //   court  : court label e.g. "A"
        //   compId : competition id (may be '' for snapshot-req)
        //   payload: message body
        publish(type, court, compId, payload) {
            try {
                const outgoing = type === 'patch' ? maskOutgoingPatch(payload, court, compId) : payload;
                // A patch about a competition this tab cannot place, when it
                // holds the tournament, is not sent: it could carry names.
                if (type === 'patch' && payload && outgoing === null) return;
                channel.postMessage({
                    v: 1,
                    type,
                    origin: _tabId,
                    court: court || '',
                    compId: compId || '',
                    payload: outgoing || null,
                });
                if (type === 'patch' || type === 'snapshot') {
                    _lastBroadcastAt = Date.now();
                }
            } catch (err) {
                console.error('court_bridge: postMessage failed:', err);
            }
        },

        // onMessage: register an inbound message handler.
        // Returns an unsubscribe function.
        onMessage(handler) {
            handlers.add(handler);
            return () => handlers.delete(handler);
        },

        close() {
            handlers.clear();
            channel.close();
        },
    };

    return bridge;
}

// -----------------------------------------------------------------------
// Singleton bridge: one shared BroadcastChannel handle per tab.
// Both api_client_overlays.jsx (publisher) and app.jsx (display subscriber) import
// this same reference so only one BroadcastChannel is opened per tab.
// The no-op-degrade path inside openBridge() runs exactly once at module
// load time.
// -----------------------------------------------------------------------
export const bridge = openBridge();

// -----------------------------------------------------------------------
// The operator's board on a display of the same device (bc-evts stage 3,
// operator S1 a). A display in another tab of this device (or on its HDMI
// output) shows exactly what the operator's individual score editor shows,
// pending taps included: the editor publishes its board, the server's last
// copy with this device's taps folded on, as a patch on every tap and every
// outcome. It carries the server copy's scoreVersion, so a display already
// holding a newer server copy keeps that one (mergeMatchPatch). A display on
// ANOTHER device shows a tap once the server has it (its push), one round
// trip later: the known limit of a board kept by the server.
// -----------------------------------------------------------------------
// flagsA/flagsB are an engi board's (bc-evts stage 4); an individual board
// carries none, so a display of it gains none. subResults and teamResult are a
// team board's (stage 5): its bout rows, and teamResult stated null so the
// display works IV and PW out of those rows rather than keep the totals of the
// server's last push.
const OVERLAY_FIELDS = ['status', 'ipponsA', 'ipponsB', 'hansokuA', 'hansokuB', 'flagsA', 'flagsB', 'markIds', 'scoreVersion', 'modifiedAt', 'repPlayerA', 'repPlayerB', 'subResults', 'teamResult'];

// boardOverlayResult: the match_updated `result` a board publishes, or null
// for a match no display can place (no id or no court).
export function boardOverlayResult(match) {
    if (!match || match.id == null || !match.court) return null;
    const result = { id: match.id };
    if (match.sideA && match.sideA.id) result.sideAId = match.sideA.id;
    if (match.sideB && match.sideB.id) result.sideBId = match.sideB.id;
    for (const f of OVERLAY_FIELDS) {
        if (match[f] !== undefined) result[f] = match[f];
    }
    // No overtime is said, not left out, so a period taken back clears it.
    result.encho = match.encho && match.encho.periodCount > 0 ? { periodCount: match.encho.periodCount } : null;
    return result;
}

// publishBoardOverlay sends the board to this device's displays of its court.
export function publishBoardOverlay(match) {
    const result = boardOverlayResult(match);
    if (!result) return;
    bridge.publish('patch', match.court, match.compId || '', { result });
}
