// match_history_view.jsx: the score editors' "History" disclosure (bc-mrgc,
// operator ruling 2026-10-03: "Nothing should be dropped. All events must be
// ordered.").
//
// The server merges a match write group by group and keeps every write that
// reached the match in the match's history, with the values of any change it
// did not apply because a newer change to the same thing was recorded first
// (GET /api/competitions/:id/matches/:mid/history, oldest change first). This
// is where an operator reads it: closed by default, fetched when opened (and
// again when the match changes while it is open), one line per write: when the
// change was made, what made it, what it changed, and, for a change kept
// rather than applied, what it would have set.
//
// It holds no state an editor's dirty check reads and blocks nothing. The
// route is the organiser's, so a self-run participant's editor does not show
// it (the editors pass `hidden`).
//
// Readable values come from the owners every other surface uses: a scoreline
// through formatIpponsScore (bracket.jsx: letters, never digits; the middle
// is its closed set), a withdrawal through withdrawalLabel, a side through
// sideWord, a group's name through groupLabel.

import { formatIpponsScore } from './bracket.jsx';
import { hanteiDecided } from './result_slot.jsx';
import { withdrawalLabel } from './admin_scoring_shared.jsx';
import { sideWord } from './side_cell.jsx';
import { groupLabel, parseBoutGroup, GROUP_POINTS, GROUP_RESULT, GROUP_ENCHO, GROUP_FLAGS, GROUP_REP } from './match_groups.jsx';

const { useState: useStateH, useEffect: useEffectH, useRef: useRefH } = React;

// What made each kind of write, in the operator's words. The keys are the
// server's door names (engine/match_history.go).
const DOOR_WORDS = {
    'score': 'Score sheet saved',
    'bulk-score': 'Results entered together',
    'quick-score': 'Quick result',
    'decision': 'Decision recorded',
    'daihyosen-add': 'Representative bout added',
    'daihyosen-remove': 'Representative bout removed',
    'engine': 'Updated by the app',
    'reopen': 'Reopened',
    'requeue': 'Sent back to the queue',
    'override-winner': 'Winner set by hand',
    'engi': 'Engi result recorded',
    'kachinuki-advance': 'Next kachinuki bout added',
    'kachinuki-remove-bout': 'Kachinuki bout removed',
    'downstream-reopen': 'Reopened by a correction to an earlier match',
};

export function historyDoorWords(door) {
    return DOOR_WORDS[door] || 'Updated';
}

const pad2 = (n) => String(n).padStart(2, '0');

// historyTime: when the change was made, HH:MM:SS on this device's clock. A
// write that carried no time of its own reads at the time the server took it.
export function historyTime(entry) {
    const ms = entry && entry.stamp > 0 ? entry.stamp : (entry && entry.receivedAt) || 0;
    if (!ms) return '';
    const d = new Date(ms);
    return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
}

// A scoreline in the score-string form, Shiro left and Aka right.
function scorelineText(v) {
    const s = formatIpponsScore(v.ipponsB, v.ipponsA, null, v.decision || '', v.encho || null, hanteiDecided(v), null);
    return s || 'no points';
}

function foulsText(v) {
    const a = Number(v.hansokuA) || 0, b = Number(v.hansokuB) || 0;
    return a || b ? `, fouls Shiro ${b}, Aka ${a}` : '';
}

const DEFAULT_WINS = ['kiken', 'kiken-voluntary', 'kiken-injury', 'fusenpai', 'fusensho'];

function resultText(v) {
    if (!v) return 'no result';
    if (DEFAULT_WINS.includes(v.decision)) {
        const label = withdrawalLabel(v.decision);
        if (!v.decisionBy) return label;
        // decisionBy names the side that withdrew or did not appear; a
        // fusensho is the win AGAINST that side.
        return `${label} ${v.decision === 'fusensho' ? 'against' : 'by'} ${sideWord(v.decisionBy)}`;
    }
    if (v.decision === 'hikiwake') return 'draw';
    if (v.winner) return `${v.winner} won`;
    if (v.status === 'running') return 'running';
    if (v.status === 'scheduled') return 'not started';
    return 'finished with no winner';
}

// heldValueText: a short readable form of a change kept in the history.
export function heldValueText(group, value) {
    const v = value && typeof value === 'object' ? value : null;
    switch (group) {
        case GROUP_POINTS:
            return v ? `${scorelineText(v)}${foulsText(v)}` : 'no points';
        case GROUP_RESULT:
            return resultText(v);
        case GROUP_ENCHO:
            // (E) is always bare: the period count is recorded, never shown.
            return v && v.periodCount > 0 ? 'overtime' : 'no overtime';
        case GROUP_FLAGS:
            return v ? `flags Shiro ${Number(v.flagsB) || 0}, Aka ${Number(v.flagsA) || 0}` : 'no flags';
        case GROUP_REP:
            return v ? `Shiro ${v.repPlayerB || 'not picked'}, Aka ${v.repPlayerA || 'not picked'}` : 'not picked';
        default:
            if (parseBoutGroup(group) === null) return '';
            return v ? `${scorelineText(v)}${foulsText(v)}` : 'no bout';
    }
}

// historyEntryView: one history entry as the lines the disclosure shows.
export function historyEntryView(entry) {
    const changed = (entry.changed || []).map(groupLabel).filter(Boolean);
    const held = Object.keys(entry.held || {}).map((g) => ({
        group: g,
        text: `Kept in history: ${groupLabel(g)}, ${heldValueText(g, entry.held[g])}`,
    }));
    const cleared = entry.clearedWithdrawal
        ? `Withdrawal cleared by later scoring: ${resultText(entry.clearedWithdrawal)}`
        : null;
    return {
        time: historyTime(entry),
        door: historyDoorWords(entry.door),
        changed: changed.length ? `changed ${changed.join(', ')}` : 'changed nothing',
        held,
        cleared,
    };
}

export function MatchHistoryDisclosure({ match, password, hidden = false }) {
    const [open, setOpen] = useStateH(false);
    const [state, setState] = useStateH({ key: '', loading: false, error: '', entries: null });
    const mountedRef = useRefH(true);
    useEffectH(() => () => { mountedRef.current = false; }, []);
    const compId = match && match.compId;
    const matchId = match && match.id;
    const modifiedAt = match && match.modifiedAt;
    // Fetched when opened, and again when the match changes while it is open.
    useEffectH(() => {
        if (!open || !compId || !matchId || hidden) return undefined;
        const api = window.API;
        if (!api || typeof api.fetchMatchHistory !== 'function') return undefined;
        let cancelled = false;
        const key = `${compId}\u0000${matchId}`;
        // Another match's list never stands in while this one's loads.
        setState((s) => ({ key, loading: true, error: '', entries: s.key === key ? s.entries : null }));
        api.fetchMatchHistory(compId, matchId, password).then((entries) => {
            if (cancelled || !mountedRef.current) return;
            setState({ key, loading: false, error: '', entries: Array.isArray(entries) ? entries : [] });
        }, () => {
            if (cancelled || !mountedRef.current) return;
            setState((s) => ({ ...s, loading: false, error: "Couldn't load the history. Hide it, then open it to try again." }));
        });
        return () => { cancelled = true; };
    }, [open, compId, matchId, modifiedAt, password, hidden]);
    if (hidden || !compId || !matchId) return null;
    const { loading, error, entries } = state;
    return (
        <div className="match-history" data-testid="match-history">
            <button
                type="button"
                className="btn btn--ghost btn--sm match-history__toggle"
                aria-expanded={open ? 'true' : 'false'}
                data-testid="match-history-toggle"
                onClick={() => setOpen((o) => !o)}
            >
                {open ? 'Hide history' : 'History'}
            </button>
            {open && (
                <div className="match-history__body" data-testid="match-history-body">
                    {error && <div className="match-history__note" role="alert">{error}</div>}
                    {!error && loading && !entries && <div className="match-history__note">Loading…</div>}
                    {!error && entries && entries.length === 0 && (
                        <div className="match-history__note">Nothing recorded for this match yet.</div>
                    )}
                    {!error && entries && entries.length > 0 && (
                        <ol className="match-history__list">
                            {entries.map((e) => {
                                const v = historyEntryView(e);
                                // An entry has no id: when the server took it, when the
                                // change was made, and what it was, tell them apart.
                                const key = `${e.receivedAt}:${e.stamp}:${e.door}:${e.session || ''}:${(e.changed || []).join(',')}`;
                                return (
                                    <li key={key} className="match-history__entry" data-testid="match-history-entry">
                                        <span className="match-history__time">{v.time}</span>
                                        {' '}<span className="match-history__door">{v.door}</span>
                                        {', '}<span className="match-history__changed">{v.changed}</span>
                                        {v.held.map((h) => (
                                            <div key={h.group} className="match-history__held" data-testid="match-history-held">{h.text}</div>
                                        ))}
                                        {v.cleared && <div className="match-history__held">{v.cleared}</div>}
                                    </li>
                                );
                            })}
                        </ol>
                    )}
                </div>
            )}
        </div>
    );
}
