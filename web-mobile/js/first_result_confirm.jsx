// first_result_confirm.jsx: the ONE owner of the question asked before the first
// result of a drawn competition (bc-drev, operator decision D4 b, 2026-11-25:
// "This warning really must be on the shiaijo operator view").
//
// A competition whose draw is generated (status draw-ready) starts with its FIRST
// RECORDED RESULT: a match left running or completed (the server's result gate,
// recordedResultIn, mirrored by competitionHasRecordedResult in
// recorded_result.jsx). From then on the draw is fixed: it can no longer be
// discarded and no competitor can be swapped. A tired operator who taps Start
// on the wrong court, or a Pools (preview) tab they took for the live one, must
// be told before that happens, so every write that would record that first
// result asks here first.
//
// The question is the browser's alone: the server never gates on it, so a write
// queued offline after the operator agreed is never refused when it replays.
//
// What it knows about a competition comes from the surfaces that hold it
// (noteFirstResultCompetitions): the court console notes its court feed, and an
// admin host notes the tournament it shows. A competition no surface has noted
// is not asked about; nothing is fetched to find out.
//
// Every door asks through confirmFirstResult, the one question:
// attemptScoreWrite (write_result.jsx), which every editor's start, finish and
// decision on every host goes through, is the last one. The others ask EARLIER,
// before something a Not yet must leave as it was: the court console's Start
// (before it sends a running bout back, admin_shiaijo_start.jsx), the editors'
// Start match and the kachinuki sheet's Record bout on a queued encounter
// (before the owner shows the start), the team sheet's representative-bout add
// on a queued match (before the sheet is saved), a decision (before the unsaved
// entries' question, makeSubmitDecision) and the barred-match notice's Record
// fusensho (barred_match_notice.jsx). The editors reach it through
// confirmFirstResultFor (admin_scoring_shared.jsx), which names the match the
// way the operator's rows do. Once the operator agrees for a competition, no
// door asks again for it, so a start queued offline and the finish that follows
// it ask once; the agreement is forgotten when the competition is seen back in
// setup (its draw discarded), so a draw generated again asks again.
//
// The question opens through the injected confirmDialog (ui.jsx DialogHost),
// which already moves focus in and back, traps Tab, cancels on Escape and takes
// the bounce of the tap that opened it (useOpenedTapGuard), so a double tap on
// Start never answers it.
//
// Import-only (never script-tagged), like the leaves it reads.

import { competitionHasRecordedResult } from './recorded_result.jsx';

// The competitions noted, by id: { name, awaits } where `awaits` is true while
// the competition is drawn and nothing is recorded in it yet.
const noted = new Map();
// The competitions whose first result the operator has agreed to record.
const agreed = new Set();

// competitionAwaitsFirstResult: the competition is drawn (draw-ready) and no
// result is recorded in it yet, judged on the competition as the page holds it
// (normalizeCompetitionDetail's shape, which recorded_result.jsx reads).
export function competitionAwaitsFirstResult(comp) {
    return !!comp && comp.status === 'draw-ready'
        && !competitionHasRecordedResult(comp.poolMatches, comp.bracket);
}

// noteFirstResultCompetitions: remember what `comps` say about each
// competition's first result. Called with every list a surface renders, so the
// latest list read wins. A competition seen in setup has no draw: an agreement
// given for its earlier draw is forgotten.
export function noteFirstResultCompetitions(comps) {
    for (const c of comps || []) {
        if (!c || !c.id) continue;
        noted.set(c.id, { name: c.name || '', awaits: competitionAwaitsFirstResult(c) });
        if (!c.status || c.status === 'setup') agreed.delete(c.id);
    }
}

// writeRecordsResult: whether a write would record a result, so start a drawn
// competition: a score write that leaves the match running or completed, or a
// decision (kiken, fusenpai, fusensho, hantei, a representative bout), which
// always completes it. A write that leaves the match scheduled records nothing.
export function writeRecordsResult(result) {
    if (!result || typeof result !== 'object') return false;
    if (result.decision) return true;
    return result.status === 'running' || result.status === 'completed';
}

// firstResultAsks: whether a write to competition `compId` asks first: it is
// noted as drawn with nothing recorded, and the operator has not agreed yet.
export function firstResultAsks(compId) {
    const n = noted.get(compId);
    return !!n && n.awaits && !agreed.has(compId);
}

// What the question says. `name` is the competition's, `label` the match's as
// the operator's rows name it, and `starts` tells a Start from a result.
export function firstResultConfirm({ name, label, starts }) {
    const comp = name || 'This competition';
    const match = label || 'this match';
    const what = starts ? `Starting ${match}` : `Recording a result for ${match}`;
    return {
        title: `Start ${name || 'the competition'}?`,
        message: `${comp} has not started yet. ${what} starts it. After that the draw is fixed: competitors can no longer be swapped, and the draw can no longer be discarded.`,
        confirmLabel: 'Start the competition',
        cancelLabel: 'Not yet',
    };
}

// The sentence a write the operator kept back answers with (the error
// attemptScoreWrite throws, marked firstResultCancelled).
export const FIRST_RESULT_CANCELLED = 'Not recorded: the competition was not started.';

// confirmFirstResult: ask, when the write asks (firstResultAsks), and answer
// whether to go on. True with no question when it does not ask; true when the
// operator agrees, which is remembered for the competition; false when they
// keep it back or the dialog could not open.
export async function confirmFirstResult({ confirmDialog, compId, label = '', starts = false }) {
    if (!firstResultAsks(compId)) return true;
    let ok = false;
    try {
        ok = typeof confirmDialog === 'function'
            && !!(await confirmDialog(firstResultConfirm({ name: noted.get(compId).name, label, starts })));
    } catch {
        ok = false;
    }
    if (ok) agreed.add(compId);
    return ok;
}

// For tests: forget every competition noted and every agreement.
export function resetFirstResultConfirm() {
    noted.clear();
    agreed.clear();
}
