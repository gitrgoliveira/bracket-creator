// The court console's queue: queue ordering and grouping, the queue row and
// group, the match number and score cell, and the tie-breaker reason with its
// shared standings. Moved out of admin_shiaijo.jsx.

import { compareCourtDayQueue } from './court_queue.jsx';
import { SideCell, WinnerTick } from './side_cell.jsx';
import { swissRoundLabel, scoreRowMatchLabel, isSupplementaryBout, poolNameOf } from './pool_ids.jsx';
// rankOrdinal: "1st", "2nd"... for the place a tie-breaker bout decides.
import { rankOrdinal, poolMatchesSignature } from './viewer_standings.jsx';
// NumberedName: single owner of the number-chip-on-the-outer-side rule
// (bc-dnst); see that file's header for why this stays an ES import.
import { NumberedName } from './numbered_name.jsx';
// mp-jnvl: the recency rule is shared with the public viewer's Recent
// results, so it lives in its own leaf rather than in this page.
import { resultRecencyDesc } from './result_recency.jsx';
import { isBarredMatch, barredSides } from './ineligible_match.jsx';
// Straight from its own leaf, NOT admin_scoring_shared.jsx: that module also
// imports bracket.jsx (for sideMarks), and admin_shiaijo.jsx's render suite
// stubs window.BracketTree before importing this file -- routing through
// admin_scoring_shared.jsx pulled bracket.jsx's module body in ahead of that
// stub taking effect and silently overwrote it. See barred_match_notice.jsx's
// header. The same rule is why winnerSideLR and teamMatchMarks come from the
// side_marks.jsx leaf below, not from bracket.jsx.
import { BarredMatchNotice } from './barred_match_notice.jsx';
// The winner cue and a withdrawn team's mark, imported from their leaf (its one
// import is competitor_identity.jsx), never read off `window`: a missing global
// used to paint nothing, with no error to say why.
import { winnerSideLR, teamMatchMarks } from './side_marks.jsx';
// bc-tmwn: team match marks (Kiken, Fus.) are placed beside the withdrawn
// side's name via teamNameMark.
import { teamNameMark } from './match_scoreboard.jsx';
import { matchKey } from './admin_shiaijo_feed.jsx';

const { useState, useEffect } = React;

const CourtPicker = window.CourtPicker;

// Pure ordering/partition helpers (exported for unit tests). Every group is
// ordered by the court-day queue comparator, queue rows first, then
// completed, then any other status.
export function sortShiaijoMatches(matches) {
    const groupRank = (s) => (s === "running" || s === "scheduled" ? 0 : s === "completed" ? 1 : 2);

    const toEntry = (m) => ({
        competitionId: m.competitionId || m.compId || "",
        competitionStartTime: m.competitionStartTime || "",
        date: m.date || "",
        status: m.status || "",
        scheduledAt: m.scheduledAt || "",
        drawIndex: m.drawIndex ?? 0,
    });

    return [...matches].sort((a, b) => {
        const ra = groupRank(a.status);
        const rb = groupRank(b.status);
        if (ra !== rb) return ra - rb;
        return compareCourtDayQueue(toEntry(a), toEntry(b));
    });
}

// Completed bouts read in the order they were PLAYED, oldest first, which is
// only the schedule order on a court that ran to schedule (mp-jnvl). That makes
// the newest result the TAIL, so the context strip's anchor and the Completed
// preview are both a plain tail again: the preview stays a contiguous window
// and "Show all" extends the list instead of inserting rows into the middle of
// it (operator ruling 2026-09-19). Running and scheduled keep schedule order,
// which is the order they will be fought in.
export function partitionShiaijoMatches(matches) {
    const sorted = sortShiaijoMatches(matches);
    const running = [], scheduled = [], completed = [];
    for (const m of sorted) {
        if (m.status === "running") running.push(m);
        else if (m.status === "scheduled") scheduled.push(m);
        else if (m.status === "completed") completed.push(m);
    }
    // Oldest write first: resultRecencyDesc is newest-first, so reverse it. An
    // all-unstamped list falls back to scheduled time and keeps exactly the
    // order it had before the stamp existed.
    completed.sort((a, b) => resultRecencyDesc(b, a));
    return { sorted, running, scheduled, completed };
}

// How many of the most-recent completed bouts the Completed section shows
// before the "Show all N" toggle. Keeps the live queue + standings above the
// fold on a full-day court without hiding the recent record.
export const COMPLETED_PREVIEW = 8;

// A team encounter (vs an individual bout): team matches carry a lineup the
// operator can set before the bout starts. Exported for unit tests (it gates
// the "Enter lineup" affordance).
export const isTeamMatch = (m) => !!m && (m.compKind === "team" || m.teamSize > 0);

// (addMinuteHHMM and deferTimeFor removed: queue reordering now works by
// swapping scheduledAt between adjacent rows via moveMatch, which calls
// updateMatchTime for both the moved match and its neighbour.)

// shiaijoMatchNo: the match's identity on the Up next card and a queue row,
// after the time and the competition. A pool bout keeps the console's own
// "Match N of M". A knockout match is named by the owner the Scores list and
// the refusal dialogs use (scoreRowMatchLabel -> matchLabel), so the 3rd-place
// match, which has no number, reads "the 3rd-place match" (bc-bzlb, operator
// decision 2026-10-11) rather than nothing.
export function shiaijoMatchNo(m) {
    if (m.phase === "pool") {
        return m.poolPosition > 0 && m.poolCount > 0 ? ` · Match ${m.poolPosition} of ${m.poolCount}` : "";
    }
    if (m.phase !== "bracket") return "";
    const label = scoreRowMatchLabel(m);
    return label ? ` · ${label}` : "";
}

// shiaijoScoreCell: decide what the queue row's middle score column shows.
// Exported so the team-vs-individual routing is unit-testable. A team
// encounter's headline number is Individual Victories (IV); it must always
// carry the IV label and never appear as a bare figure (which could be read
// as wins or points). Engi (flag-count scoring) is the ONLY competition type
// where the headline figure is a number at all; it also carries an explicit
// label so it isn't mistaken for an ippon count. Every other individual bout
// shows the self-explanatory ippon LETTERS, never digits.
// Returns one of: {kind:"team",iv} | {kind:"engi",flags} | {kind:"ippon",ippon} | {kind:"vs"} | {kind:"none"}.
export function shiaijoScoreCell(m) {
    if (!m) return { kind: "none" };
    if (m.status === "scheduled") return { kind: "vs" };
    if (m.status !== "completed" && m.status !== "running") return { kind: "none" };
    const flags = window.engiFlagScore ? window.engiFlagScore(m) : null;
    if (flags) return { kind: "engi", flags };
    const isTeam = isTeamMatch(m);
    if (isTeam) {
        const iv = window.teamIVPWScore ? window.teamIVPWScore(m) : (window.teamIVScore ? window.teamIVScore(m) : null);
        return iv ? { kind: "team", iv } : { kind: "none" };
    }
    const ipponsA = m.ipponsA || [];
    const ipponsB = m.ipponsB || [];
    const s = window.formatIpponsScore
        ? window.formatIpponsScore(ipponsB, ipponsA, m.score, m.decision, m.encho, m.decidedByHantei,
            winnerSideLR(m))
        : "";
    return s ? { kind: "ippon", ippon: s } : { kind: "none" };
}

// A queued match row: plain info (the court reassign and reorder controls
// work) but the row itself can't be opened in the scoring panel: you start a
// match from the Up Next card, not by clicking it. The console never shows a
// running match in these groups (the running bout is officiated in the scoring
// panel on the right), so only scheduled/completed states render here.
//
// `scheduled` is the filtered (selected-competition) scheduled list: the same
// list `moveMatch` reorders against: used to derive first/last position for
// disabling the ↑/↓ buttons. `onMove(m, direction)` swaps scheduledAt with the
// adjacent same-competition row via two updateMatchTime calls.
// Group an Upcoming slice for display so the operator sees what they'll be
// scoring: pool matches by pool ("Pool A", "Pool B"), knockout matches by round
// ("Final", "Round 16"). League is a single round-robin table and needs no
// grouping → returns null, and the caller renders a flat list. Groups keep
// first-appearance (i.e. scheduled-time) order; pools are seeded contiguously
// per court, so each group stays a contiguous time block.
export function groupQueueMatches(matches) {
    if (!matches.length || matches.every((m) => m.compFormat === "league")) return null;
    const order = [];
    const byKey = new Map();
    for (const m of matches) {
        let key, label;
        if (m.phase === "bracket") {
            // Key on the LABEL, not roundIndex: m.round is the EFFECTIVE round
            // (bracketRoundLabel), and one backend round can hold two of them
            // when a bye collapses a round, while two backend rounds can share
            // one. Keying on roundIndex therefore both put a quarterfinal under
            // a "Semifinals" heading and split one round name across two
            // identically-titled groups. Keying on the displayed string keeps
            // the invariant the operator relies on: the heading describes every
            // match under it. Same cross-competition behaviour as before, since
            // a shared roundIndex already merged those.
            key = "round:" + (m.round || "");
            label = m.round || "Knockout";
        } else if (m.phase === "pool") {
            key = "pool:" + (m.poolName || "");
            // Swiss rounds piggyback on the pool pipeline with a synthetic
            // "Swiss-RN" pool name (mp-pglr); show the operator "Round N".
            label = m.compFormat === "swiss"
                ? swissRoundLabel(m.poolName)
                : (m.poolName || "Pool");
        } else {
            key = "other";
            label = null;
        }
        if (!byKey.has(key)) { byKey.set(key, { key, label, matches: [] }); order.push(key); }
        byKey.get(key).matches.push(m);
    }
    return order.map((k) => byKey.get(k));
}

export function ShiaijoQueueGroup({ label, matches, subGroup, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, noticeFor, onCall, callingKey, isCalled, startingKey, password }) {
    const renderRow = (m) => (
        <ShiaijoQueueRow
            key={matchKey(m)} m={m}
            notice={noticeFor ? noticeFor(m) : null}
            scheduled={scheduled}
            courts={courts} onMoveCourt={onMoveCourt} onMove={onMove} onEnterLineup={onEnterLineup} onPick={onPick} onCorrect={onCorrect}
            onCall={onCall} callingKey={callingKey} isCalled={isCalled} startingKey={startingKey}
            password={password}
        />
    );
    const groups = subGroup ? groupQueueMatches(matches) : null;
    return (
        <div className="shiaijo-group">
            {label && <div className="section-title">{label}</div>}
            {groups
                ? groups.map((g) => (
                    <div className="shiaijo-subgroup" key={g.key}>
                        {g.label && <div className="shiaijo-subgroup__title">{g.label}</div>}
                        <div className="score-editor__list">
                            {g.matches.map(renderRow)}
                        </div>
                    </div>
                ))
                : (
                    <div className="score-editor__list">
                        {matches.map(renderRow)}
                    </div>
                )}
        </div>
    );
}

// tieBreakerReasonLine: why a tie-breaker bout is on the court, in the
// operator's words, naming the place it decides. `rows` are the standings of
// the bout's own pool (a league has one table). The two sides are found by
// participant id only; the teams or players level on points with them form
// the tied group, whose best place is the row with the lowest rank. A group of
// more than two names its whole range as the tie this bout is part of. The group
// is the run the two sides share (`tieRun` on the rows), so a later round over
// only some of a bigger tie (RunTiebreakerRound) names the places of that run
// alone. Without standings (still loading, or none for this pool), or when the
// two sides are no longer level or sit in different runs, the line still says
// the bout is a tie-breaker and why, but names no place it cannot know.
export function tieBreakerReasonLine(m, rows) {
    // Both sides withdrew: the bout cannot be fought, so the line says what to
    // do instead of claiming the bout separates them (the barred note under it
    // says neither can fight).
    const barred = barredSides(m);
    const bothBarred = !!(barred.a && barred.b);
    // What the operator does with a bout nobody can fight. Inside the band a tie
    // is broken in, the drawn bout then waits for the organiser's choice on the
    // Pools tab: fight it again or enter the lots, neither forced (operator
    // ruling 2026-11-16). A pair level below it (`tieOutOfBand` on its rows)
    // shares its place and owes neither. Without the standings the line cannot
    // say which, so it names the Pools tab only as the place that asks.
    const neither = (outOfBand) => outOfBand === undefined
        ? "Neither can fight: record it as drawn. If a place depends on it, the Pools tab then offers to fight it again or enter the lots."
        : outOfBand
            ? "Neither can fight: record it as drawn. They share this place, so no lots are owed."
            : "Neither can fight: record it as drawn. The Pools tab then offers to fight it again or enter the lots.";
    const generic = bothBarred
        ? `Tie-breaker bout. ${neither()}`
        : "Tie-breaker bout. These two finished level.";
    const list = Array.isArray(rows) ? rows : [];
    const rowOf = (id) => (id ? list.find((s) => s.player && s.player.id === id) : undefined);
    const a = rowOf(m.sideAId || (m.sideA && m.sideA.id));
    const b = rowOf(m.sideBId || (m.sideB && m.sideB.id));
    const runOf = (s) => s.tieRun || 0;
    if (!a || !b || a.points !== b.points || runOf(a) !== runOf(b)) return generic;
    const group = list.filter((s) => s.points === a.points && runOf(s) === runOf(a));
    const lo = Math.min(...group.map((s) => s.rank));
    const outOfBand = group.every((s) => s.tieOutOfBand);
    if (group.length === 2) {
        return `Tie-breaker for ${rankOrdinal(lo)} place. ${bothBarred ? neither(outOfBand) : "They finished level, so this bout separates them."}`;
    }
    return `Part of the tie-breaker for ${rankOrdinal(lo)} to ${rankOrdinal(lo + group.length - 1)} place. ${bothBarred ? neither(outOfBand) : "These two finished level, so this bout helps separate them."}`;
}

// The standings every unfinished tie-breaker row reads (the league table, or
// the competition detail a pool's rows come from), shared per competition: the
// rows of one competition ask for the same table, so they make one request, and
// the court feed's refresh (forgetTieBreakerStandings) makes a competition's
// rows read it again only when that competition's pool results changed
// (poolMatchesSignature, which a running bout's score ticks do not move),
// because the results are what move a table. A failed read is not kept, so the
// next refresh asks again.
const sharedStandings = new Map();

const lastPoolSignatures = new Map();

const standingsListeners = new Map();

export function forgetTieBreakerStandings(competitions) {
    (competitions || []).forEach((c) => {
        const signature = poolMatchesSignature(c.poolMatches);
        if (lastPoolSignatures.get(c.id) === signature) return;
        lastPoolSignatures.set(c.id, signature);
        sharedStandings.delete(c.id);
        (standingsListeners.get(c.id) || []).forEach((read) => read());
    });
}

function readSharedStandings(compId, isLeague) {
    if (!sharedStandings.has(compId)) {
        const ask = isLeague ? window.API.leagueStandings : window.API.fetchCompetitionDetails;
        const read = ask.call(window.API, compId);
        sharedStandings.set(compId, read);
        read.catch(() => { if (sharedStandings.get(compId) === read) sharedStandings.delete(compId); });
    }
    return sharedStandings.get(compId);
}

// TieBreakerReason: the line under a tie-breaker bout's row on the court
// console. It reads the competition's standings (the league table, or the pool's
// rows from the competition detail) for a bout not yet fought, and renders
// nothing for any other bout.
export function TieBreakerReason({ m, className }) {
    const applies = !!m && isSupplementaryBout(m.id) && m.status !== "completed";
    const isLeague = !!m && m.compFormat === "league";
    const compId = m && m.compId;
    const matchId = m && m.id;
    const matchStatus = m && m.status;
    const [rows, setRows] = useState(null);
    const [reads, setReads] = useState(0);
    const canRead = !!window.API && typeof (isLeague ? window.API.leagueStandings : window.API.fetchCompetitionDetails) === "function";
    useEffect(() => {
        if (!applies || !compId || !canRead) return undefined;
        const again = () => setReads((n) => n + 1);
        if (!standingsListeners.has(compId)) standingsListeners.set(compId, new Set());
        standingsListeners.get(compId).add(again);
        return () => { standingsListeners.get(compId).delete(again); };
    }, [applies, compId, canRead]);
    useEffect(() => {
        if (!applies || !compId || !canRead) {
            setRows(null);
            return undefined;
        }
        let cancelled = false;
        readSharedStandings(compId, isLeague)
            .then((d) => {
                if (cancelled) return;
                setRows(isLeague ? d : (d && d.standings ? d.standings[poolNameOf(matchId)] : null));
            })
            .catch(() => { if (!cancelled) setRows(null); });
        return () => { cancelled = true; };
    }, [applies, isLeague, compId, matchId, matchStatus, canRead, reads]);
    if (!applies) return null;
    return (
        <div className={className} data-testid="tiebreaker-reason">{tieBreakerReasonLine(m, rows)}</div>
    );
}

export function ShiaijoQueueRow({ m, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, onCall, callingKey, isCalled, startingKey, pending, slotLabel, emptyNames, password, notice }) {
    const isComplete = m.status === "completed";
    // bc-cse: a scheduled match a competitor is barred from. `pending`
    // placeholder finals are excluded on purpose: their sides are still
    // feeder placeholders, not a resolved competitor the stamp could name.
    const barred = !pending && isBarredMatch(m);
    // Slot text through the one shared rule (bracket.jsx). Actionable rows never
    // hold a placeholder (hasBothSides filtered them out), so this only ever
    // changes the `pending` "Later" rows; the fallbacks keep any other caller
    // from printing a raw "Winner of rX-mY" if it ever does.
    const slotName = slotLabel || window.slotDisplayName || ((n) => n);
    // `emptyNames` ({a, b}) names an EMPTY side: only the 3rd-place match's
    // "Later" row passes it (bronzeSlotLabels), since its sides have no slot value.
    const aName = slotName(m.sideA?.name || "", (m.feeders || [])[0]) || (emptyNames ? emptyNames.a : "");
    const bName = slotName(m.sideB?.name || "", (m.feeders || [])[1]) || (emptyNames ? emptyNames.b : "");
    const scoreCell = shiaijoScoreCell(m);
    // bc-tmwn: for completed rows, determine which side won and get team marks.
    const winnerSide = isComplete ? winnerSideLR(m) : null;
    // teamMatchMarks itself returns no marks for a non-team row or one not completed.
    const { shiro: teamShiroMark, aka: teamAkaMark } = teamMatchMarks(m, isTeamMatch(m));
    // Derive position in the full scheduled list to know when to disable ↑/↓.
    // `scheduled` is the court's complete scheduled array (including Up Next);
    // the row may be in the Upcoming slice but we disable based on absolute pos.
    const scheduledIdx = onMove ? (scheduled || []).findIndex((x) => matchKey(x) === matchKey(m)) : -1;
    const isFirst = scheduledIdx === 0;
    const isLast = scheduledIdx >= 0 && scheduledIdx === (scheduled || []).length - 1;
    // Stacked card layout (not the wide score-editor grid): the matchup gets a
    // full-width line so names stay READABLE in the narrow queue column; the
    // controls live on their own line below so they never crowd the names.
    // A `pending` placeholder final is NEVER actionable (its sides are still
    // "Winner of rX-mY" feeders): suppress every control regardless of which
    // handlers were passed, so the row can only ever be informational.
    const showActions = !pending && m.status === "scheduled" && (
        (onMoveCourt && courts.length > 1) ||
        (onEnterLineup && isTeamMatch(m)) || onPick || onMove
    );
    return (
        <div className={`shiaijo-qrow ${isComplete ? "shiaijo-qrow--complete" : ""}`}>
            <div className="shiaijo-qrow__top">
                <span className="shiaijo-qrow__time">
                    {m.scheduledAt || "-"} · {m.compName}
                    {shiaijoMatchNo(m)}
                    {/* DH-only label (not "-TB-"): see the Up Next card note above. */}
                    {window.isPoolDaihyosenBout && window.isPoolDaihyosenBout(m.id) && (
                        <span className="tag-badge" style={{ marginLeft: 4 }}>
                            {window.Term ? React.createElement(window.Term, { name: "daihyosen" }, "DH") : "DH"}
                        </span>
                    )}
                </span>
                {/* No "Final" for a completed row (operator ruling 2026-09-20,
                    bc-sccl): "Final" names the LAST MATCH OF A KNOCKOUT, never a
                    finished bout. The row already says it two other ways -- the
                    score on its own line below and the "Completed" group heading
                    above -- plus a "Correct" button when the console wires
                    onCorrect (there is no "Score" button on this row; that
                    contrast belongs to the scores list). "Waiting" stays: a
                    blocked row has no other marker. */}
                <span className="shiaijo-qrow__state">
                    {pending && <span className="shiaijo-qrow__waiting">Waiting</span>}
                </span>
            </div>
            <div className="shiaijo-qrow__match">
                {/* Side by CELL TINT, not a SHIRO/AKA badge (operator decision
                    2026-09-20, bc-sccl). The text channel is an sr-only span, the
                    same one every other converted surface took.
                    It is NOT an aria-label on this div, which is what these cells
                    carried until 2026-09-20: a bare div is role=generic, and ARIA
                    prohibits author naming there, so that label was announced by
                    nothing. Removing the badge on top of it would have left the
                    side carried by colour alone on the one surface where reading
                    it wrong mis-scores a bout. */}
                <SideCell side="shiro" className={`shiaijo-qrow__side shiaijo-qrow__side--shiro ${winnerSide === "left" ? "shiaijo-qrow__side--win" : ""}`}>
                    {winnerSide === "left" && <WinnerTick />}
                    <span className={`shiaijo-qrow__name${teamShiroMark ? " msb-name--labelled" : ""}`}>
                        {teamNameMark("shiro", teamShiroMark, <NumberedName side="shiro" name={bName} number={m.sideB?.number} clip />)}
                    </span>
                </SideCell>
                <span className="shiaijo-qrow__vs">vs</span>
                <SideCell side="aka" className={`shiaijo-qrow__side shiaijo-qrow__side--aka ${winnerSide === "right" ? "shiaijo-qrow__side--win" : ""}`}>
                    {winnerSide === "right" && <WinnerTick />}
                    <span className={`shiaijo-qrow__name${teamAkaMark ? " msb-name--labelled" : ""}`}>
                        {teamNameMark("aka", teamAkaMark, <NumberedName side="aka" name={aName} number={m.sideA?.number} clip />)}
                    </span>
                </SideCell>
            </div>
            <TieBreakerReason m={m} className="shiaijo-qrow__reason" />
            {/* bc-cse: a barred match cannot be started (the server refuses it),
                so the row shows why and the one-tap resolution here instead of a
                dead Start button. BarredMatchNotice (admin_scoring_shared.jsx) is
                the one component: same note/action/reinstate on every surface. */}
            {barred && <BarredMatchNotice match={m} password={password} />}
            {/* Completed result on its own centred line BELOW the names: the
                canonical "marks in the centre" position, but stacked so the
                (often long) names keep the full-width line and never crowd. The
                ippon string is shiro: aka, matching the Shiro-left/Aka-right
                names above. */}
            {isComplete && (scoreCell.kind === "ippon" || scoreCell.kind === "team" || scoreCell.kind === "engi") && (
                <div className="shiaijo-qrow__result">
                    {/* scoreCell.iv already self-labels ("IV s-a · PW s-a"), so no separate IV abbr prefix. */}
                    {scoreCell.kind === "team" && <span className="shiaijo-row__teamscore">{scoreCell.iv}</span>}
                    {scoreCell.kind === "engi" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Total flags received">Flags</abbr>{scoreCell.flags}</span>}
                    {scoreCell.kind === "ippon" && scoreCell.ippon}
                </div>
            )}
            {/* Correct: reopen/fix a completed match in place (parity with the
                Scores page). A pending placeholder final has no real result to
                correct, so it never gets the button. */}
            {!pending && isComplete && onCorrect && (
                <div className="shiaijo-qrow__actions" onClick={(e) => e.stopPropagation()}>
                    <button type="button" className="btn btn--ghost btn--sm shiaijo-row__correct" onClick={() => onCorrect(m)} title="Reopen or fix this completed match">Correct</button>
                </div>
            )}
            {notice && isComplete && <div className="shiaijo-upnext__error" role="alert">{notice}</div>}
            {showActions && (
                <div className="shiaijo-qrow__actions" onClick={(e) => e.stopPropagation()}>
                    {onMoveCourt && courts.length > 1 && (
                        <CourtPicker
                            value={m.court} courts={courts}
                            onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
                            btnClassName="score-edit-row__court score-edit-row__court--btn"
                        />
                    )}
                    {/* bc-cse: never offered on a barred row -- the server
                        would just refuse the Start this exists to prepare
                        for, and BarredMatchNotice above already owns the
                        one-tap resolution instead. */}
                    {onEnterLineup && isTeamMatch(m) && !barred && (
                        <button type="button" className="btn btn--ghost btn--sm" onClick={() => onEnterLineup(m)} title="Set the team lineup before starting">Lineup</button>
                    )}
                    {onMove && (
                        <>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move up" disabled={isFirst} onClick={() => onMove(m, "up")} title="Move earlier in the queue">↑</button>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move down" disabled={isLast} onClick={() => onMove(m, "down")} title="Move later in the queue">↓</button>
                        </>
                    )}
                    {/* Optional announce: mirrors the Up Next card so any queued match is a
                        complete view: call it to the floor, or start it directly.
                        bc-cse: never offered on a barred row -- there is nobody to
                        call to the court for a match the server would refuse to
                        start. */}
                    {onCall && window.API && typeof window.API.sendAnnouncement === "function" && !barred && (
                        <button type="button" className="btn btn--ghost btn--sm" disabled={callingKey === matchKey(m)} onClick={(ev) => onCall(m, ev)} title="Announce this match to spectators and competitors">
                            {callingKey === matchKey(m) ? "Calling…" : (isCalled && isCalled(matchKey(m)) ? "Call again" : "Call to court")}
                        </button>
                    )}
                    {/* Start match is the primary per-row action: pushed to the end (the "go" slot).
                        Same pickMatch path as the Up Next card: refused while a correction is
                        open (the notice below the actions), a running bout goes back to the
                        queue keeping its score (bc-sbq), then this match starts for scoring.
                        bc-cse: never offered on a barred row -- BarredMatchNotice above owns its
                        one-tap resolution instead, since the server would just refuse a Start. */}
                    {onPick && !barred && <button type="button" className="btn btn--primary btn--sm shiaijo-row__pick" disabled={startingKey === matchKey(m)} onClick={(ev) => onPick(m, ev)} title="Start this match now and begin scoring">{startingKey === matchKey(m) ? "Starting…" : "Start match"}</button>}
                </div>
            )}
            {notice && !isComplete && <div className="shiaijo-upnext__error" role="alert">{notice}</div>}
        </div>
    );
}

export function MatchSides({ m, large }) {
    return (
        <div className={`shiaijo-sides ${large ? "shiaijo-sides--lg" : ""}`}>
            {/* Side by CELL TINT, not a SHIRO/AKA badge (bc-sccl); an sr-only
                span carries the side in text, as on the queue row above. */}
            <SideCell side="shiro" className="shiaijo-sides__side shiaijo-sides__side--shiro">
                <div className="name">
                    <NumberedName side="shiro" name={m.sideB?.name} number={m.sideB?.number} clip />
                </div>
                <div className="dojo">{m.sideB?.dojo}</div>
            </SideCell>
            <div className="shiaijo-sides__vs">vs</div>
            <SideCell side="aka" className="shiaijo-sides__side shiaijo-sides__side--aka" style={{ textAlign: "right" }}>
                <div className="name">
                    <NumberedName side="aka" name={m.sideA?.name} number={m.sideA?.number} clip />
                </div>
                <div className="dojo">{m.sideA?.dojo}</div>
            </SideCell>
        </div>
    );
}
