// match_groups.jsx: the ONE client owner of the groups a match write is merged
// by (bc-mrgc, operator ruling 2026-10-03: "Nothing should be dropped. All
// events must be ordered.").
//
// The server merges a score write group by group (internal/state/
// match_groups.go): points, result, encho, flags, rep, and one group per bout
// row, "bout:<position>". A write names the groups it changes in `changed`;
// each named group applies only when the write is not older than that group's
// stored change, a group the write does not name is never overwritten, and a
// change that is not applied is kept in the match's history. So a client write
// must name exactly what it changed: naming too little loses the operator's
// edit, naming too much can put back a value another device changed since.
//
// This file owns the names (pinned against the server's by the shared table
// internal/state/testdata/match_groups.json, loaded by a Go test and a JS
// test), the comparison that decides which groups a write changed
// (changedGroups), the union two coalesced writes carry (unionChanged), and
// the plain words a group is named by to the operator (groupLabel).
//
// A near leaf with two imports, both leaves: result_slot.jsx for the
// placeholder token, so the "what is an empty slot" rule keeps one definition,
// and write_result.jsx for the answer predicates keptInHistoryNote reads.
// The serializer that builds the wire shapes compared here is
// api_serializers.jsx's (matchWire, changedGroupsFor), and the hook that keeps
// an editor's baselines is admin_scoring_autosave.jsx's useChangedGroups.

import { IPPON_PLACEHOLDER } from './result_slot.jsx';
import {
    NEEDS_WINNER_NOTE, DEFAULT_WIN_STANDS_NOTE, writeNeedsWinner, writeDefaultWinStands,
    writePartlyHeld, writeHeldGroups, writeDisplacedGroups,
} from './write_result.jsx';

export const GROUP_POINTS = 'points';
export const GROUP_RESULT = 'result';
export const GROUP_ENCHO = 'encho';
export const GROUP_FLAGS = 'flags';
export const GROUP_REP = 'rep';

// The scalar groups in the server's fixed order (state.ScalarGroups).
export const SCALAR_GROUPS = [GROUP_POINTS, GROUP_RESULT, GROUP_ENCHO, GROUP_FLAGS, GROUP_REP];

const BOUT_PREFIX = 'bout:';

// boutGroup names the group of the bout row at position (1..n, or -1 for the
// representative bout).
export function boutGroup(position) {
    return BOUT_PREFIX + position;
}

// parseBoutGroup returns the bout position a group names, or null.
export function parseBoutGroup(group) {
    if (typeof group !== 'string' || !group.startsWith(BOUT_PREFIX)) return null;
    const rest = group.slice(BOUT_PREFIX.length);
    if (!/^[+-]?\d+$/.test(rest)) return null;
    return Number(rest);
}

// isValidGroup: a scalar group or a bout row (state.ValidGroup).
export function isValidGroup(group) {
    return SCALAR_GROUPS.includes(group) || parseBoutGroup(group) !== null;
}

// unionChanged: the groups a write that replaces another queued write for the
// same match must claim, every group either changed. An absent list means
// "every group the payload carries" (a write from an older build, or one that
// never named its groups), and that never narrows to a partial list: the
// union is then absent too.
export function unionChanged(a, b) {
    if (!Array.isArray(a) || !Array.isArray(b)) return undefined;
    const out = [...a];
    for (const g of b) if (!out.includes(g)) out.push(g);
    return out;
}

// groupLabel: a group in the operator's words, for the note naming what was
// kept in the match's history and for the history list itself.
export function groupLabel(group) {
    switch (group) {
        case GROUP_POINTS: return 'points';
        case GROUP_RESULT: return 'the result';
        case GROUP_ENCHO: return 'overtime';
        case GROUP_FLAGS: return 'flags';
        case GROUP_REP: return 'the representative players';
        default: {
            const pos = parseBoutGroup(group);
            if (pos === null) return String(group || '');
            return pos < 0 ? 'the representative bout' : `bout ${pos}`;
        }
    }
}

// groupsLabel joins several groups for a sentence: "points and bout 2".
export function groupsLabel(groups) {
    const labels = (groups || []).map(groupLabel).filter(Boolean);
    if (labels.length <= 1) return labels[0] || '';
    return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`;
}

// heldGroupsNote: the quiet note a score editor shows when the server applied
// a write in part and kept the rest in the match's history (write_result.jsx
// writePartlyHeld), naming what was kept; null when nothing was. `needsWinner`
// when they were held because applying them would leave the finished match
// without a winner (writeNeedsWinner): the note then says to correct the
// result with a winner instead of naming a newer change.
export function heldGroupsNote(groups, needsWinner = false, defaultWinStands = false) {
    const words = groupsLabel(groups);
    if (!words) return null;
    const why = needsWinner ? NEEDS_WINNER_NOTE : defaultWinStands ? DEFAULT_WIN_STANDS_NOTE : 'A newer change to the same thing was recorded first.';
    return `Kept in the match's history, not applied: ${words}. ${why}`;
}

// displacedGroupsNote: the note for a write that WAS recorded and moved a
// later change of the match to its history, because that change would have
// left the finished match without a winner (write_result.jsx
// writeDisplacedGroups). null when nothing was moved.
export function displacedGroupsNote(groups) {
    const words = groupsLabel(groups);
    if (!words) return null;
    return `Saved. A later change to ${words} would have left the finished match without a winner, so it was moved to the match's history.`;
}

// keptInHistoryNote: the ONE note a score editor (useKeptInHistoryNote) or
// its closing host (admin.jsx's toast) shows for what a write's answer kept
// in the match's history: this write's own groups held (applied in part, or
// superseded because the finished match needs a winner), and/or later
// changes it moved there. null when the answer kept nothing.
export function keptInHistoryNote(res) {
    const parts = [];
    if (writePartlyHeld(res) || writeNeedsWinner(res) || writeDefaultWinStands(res)) {
        parts.push(heldGroupsNote(writeHeldGroups(res), writeNeedsWinner(res), writeDefaultWinStands(res)));
    }
    parts.push(displacedGroupsNote(writeDisplacedGroups(res)));
    const text = parts.filter(Boolean).join(' ');
    return text || null;
}

// ---------------------------------------------------------------------------
// The comparison. Every value compared here is a WIRE shape (what
// toBackendMatchResult sends), for the write AND for each baseline, so two
// copies of the same match that only differ in how they are held on the
// client (a winner object or a name, an empty slot written as a placeholder)
// read the same.
// ---------------------------------------------------------------------------

const has = (o, k) => o != null && Object.prototype.hasOwnProperty.call(o, k) && o[k] !== undefined;

// The marks a side holds, without empty slots: [M, •] and [M] are the same
// scoreline.
const marks = (arr) => (Array.isArray(arr) ? arr : []).filter((x) => x && x !== IPPON_PLACEHOLDER);
const enchoCount = (e) => (e && e.periodCount > 0 ? e.periodCount : 0);
const str = (v) => (v == null ? '' : String(v));
const num = (v) => Number(v) || 0;

function rowAt(wire, position) {
    const subs = wire && Array.isArray(wire.subResults) ? wire.subResults : [];
    return subs.find((s) => s && num(s.position) === position) || null;
}

// What a bout row's fields compare as. Only the keys the WRITE's row states
// are compared (a key the row leaves out is one this writer knows nothing
// about, e.g. a member id or the ippons of a representative bout it has not
// touched), plus its overtime, whose absence means none.
function rowProjection(row, keys) {
    const r = row || {};
    const out = {};
    for (const k of keys) {
        if (k === 'ipponsA' || k === 'ipponsB') out[k] = marks(r[k]);
        else if (k === 'position' || k === 'hansokuA' || k === 'hansokuB') out[k] = num(r[k]);
        else if (k === 'encho') out[k] = enchoCount(r.encho);
        else if (k === 'decidedByHantei') continue;
        else out[k] = str(r[k]);
    }
    return out;
}

// statedGroups: the groups a write's payload says anything about, in the
// server's order. A group none of whose fields the payload carries is not
// changed by it (a start sends no scoreline; only an engi write sends flags).
function statedGroups(next) {
    const out = [];
    if (['ipponsA', 'ipponsB', 'hansokuA', 'hansokuB'].some((k) => has(next, k))) out.push(GROUP_POINTS);
    if (has(next, 'status')) out.push(GROUP_RESULT);
    // Always stated: an absent encho is the write saying "no overtime".
    out.push(GROUP_ENCHO);
    if (has(next, 'flagsA') || has(next, 'flagsB')) out.push(GROUP_FLAGS);
    if (has(next, 'repPlayerA') || has(next, 'repPlayerB')) out.push(GROUP_REP);
    if (Array.isArray(next.subResults)) {
        for (const s of next.subResults) {
            const g = s ? boutGroup(num(s.position)) : null;
            if (g && !out.includes(g)) out.push(g);
        }
    }
    return out;
}

// groupKey: the comparable value of `group` on `wire`, read through the keys
// `next` (the write) states, as a string.
function groupKey(wire, group, next) {
    const w = wire || {};
    switch (group) {
        case GROUP_POINTS:
            return JSON.stringify([marks(w.ipponsA), marks(w.ipponsB), num(w.hansokuA), num(w.hansokuB)]);
        case GROUP_RESULT:
            return JSON.stringify([
                str(w.status), str(w.winner), str(w.decision),
                has(next, 'winnerId') ? str(w.winnerId) : null,
                // An omitted reason is inherited from the stored match by the
                // server, so only a stated one can change.
                has(next, 'correctionReason') ? str(w.correctionReason) : null,
            ]);
        case GROUP_ENCHO:
            return String(enchoCount(w.encho));
        case GROUP_FLAGS:
            return JSON.stringify([num(w.flagsA), num(w.flagsB)]);
        case GROUP_REP:
            // The serializer leaves an unset pick off the wire and the server
            // keeps a stored pick over an absent one, so only a stated side
            // can change.
            return JSON.stringify([
                has(next, 'repPlayerA') ? str(w.repPlayerA) : null,
                has(next, 'repPlayerB') ? str(w.repPlayerB) : null,
            ]);
        default: {
            const pos = parseBoutGroup(group);
            const own = rowAt(next, pos);
            const keys = new Set(Object.keys(own || {}).filter((k) => own[k] !== undefined));
            keys.add('encho');
            keys.add('position');
            // A row the baseline does not hold reads as an empty one, so an
            // untouched blank bout is not a change.
            return JSON.stringify(rowProjection(rowAt(w, pos) || { position: pos }, [...keys].sort()));
        }
    }
}

// changedGroups: the groups `next` (the write, wire shape) changes against
// each baseline in `bases`: a group counts when its value differs from ANY of
// them. A baseline is a wire shape, or a function of the group returning one
// (an editor keeps a baseline per group, see useChangedGroups); a null
// baseline is skipped. `clearWithdrawal` always changes the result: it is the
// write saying the recorded ruling goes.
//
// The order is the server's: the scalar groups, then the bout rows in the
// write's own row order.
export function changedGroups(next, ...bases) {
    if (!next) return [];
    const out = [];
    const groups = statedGroups(next);
    if (next.clearWithdrawal === true && !groups.includes(GROUP_RESULT)) groups.splice(groups.indexOf(GROUP_ENCHO), 0, GROUP_RESULT);
    for (const g of groups) {
        if (g === GROUP_RESULT && next.clearWithdrawal === true) { out.push(g); continue; }
        const n = groupKey(next, g, next);
        const differs = (base) => {
            const b = typeof base === 'function' ? base(g) : base;
            return !!b && groupKey(b, g, next) !== n;
        };
        if (bases.some(differs)) out.push(g);
    }
    return out;
}

// groupMatches: does `group` read the same on `a` and `b`, through the keys
// `next` states? The hook that keeps an editor's baselines asks it to learn
// when the editor's own state has caught up with the server for a group.
export function groupMatches(group, a, b, next) {
    return groupKey(a, group, next) === groupKey(b, group, next);
}

// statedGroupsOf: statedGroups, exported for that same hook.
export function statedGroupsOf(next) {
    return next ? statedGroups(next) : [];
}
