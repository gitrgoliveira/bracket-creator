// Display surfaces — public, no-auth views for tournament screens, lobbies,
// and streaming integrations.
//
// Three components live here:
//   <TvDisplay court="A"> — fullscreen per-court board (T061, T068)
//   <LobbyDisplay>        — multi-court grid for lobby screens (T064)
//   <StreamingOverlay>    — transparent-background lower-third for OBS / vMix (T066, T067)
//
// All three are designed to be linked-to from `/display?court=A` etc., and
// each subscribes to the existing SSE stream via the centralised patch.jsx
// (T059). The viewer-home "Display modes" section (T069, in viewer.jsx) is
// the user-facing entry point.
//
// Coordination note: this file's components were scaffolded with T066/T067/
// T068 (StreamingOverlay + queue-position-aware upcoming-match render in
// TvDisplay) in mind. The sibling agent owns the broader visual treatment
// and SSE wiring under T059/T061–T065. The interaction details below should
// remain stable when those land — only the layout primitives around them
// are expected to evolve.

const { useState: useSD, useEffect: useED, useMemo: useMD } = React;

// TermD — kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and display.jsx doesn't matter.
// On TV/lobby surfaces (public, fullscreen) the popover provides a
// gloss but operators rarely interact with these screens — the wrap
// is more about consistency than discoverability. U1 / glossary.md.
function TermD(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

// Resolve a side's rendered label respecting competition.withZekkenName.
// When the competition has zekken/display-name mode on, players use a short
// stage name; otherwise we fall back to the canonical full name. Mirrors
// the same predicate used by buildSide in handlers_display.go so the OBS
// overlay and the TV display agree on what to render.
function sideLabel(side, withZekkenName) {
    if (!side) return "TBD";
    if (withZekkenName && side.displayName) return side.displayName;
    return side.name || "TBD";
}

// Find the running match on a court from a tournament + competitions blob.
// Returns null when no live match. Used by TvDisplay and StreamingOverlay.
function findLiveOnCourt(competitions, court) {
    if (!competitions || !court) return null;
    for (const c of competitions) {
        if (!c) continue;
        const poolMatches = c.poolMatches || [];
        for (const m of poolMatches) {
            if ((m.court || "") !== court) continue;
            if (m.status === "running" && m.sideA && m.sideB) {
                return { match: m, competition: c };
            }
        }
        // Bracket matches stored in c.bracket?.rounds may also be running.
        const rounds = (c.bracket && c.bracket.rounds) || [];
        for (let ri = 0; ri < rounds.length; ri++) {
            for (const m of rounds[ri]) {
                if ((m.court || "") !== court) continue;
                if (m.status === "running" && m.sideA && m.sideB) {
                    return { match: m, competition: c, isBracket: true, roundIndex: ri, totalRounds: rounds.length };
                }
            }
        }
    }
    return null;
}

// Collect upcoming (scheduled) matches on a court, sorted by queue position
// (asc), then scheduledAt. Caps at `limit`. Used by T068 to render
// "2 before yours" labels under the live match in TvDisplay.
function findUpcomingOnCourt(competitions, court, limit = 2) {
    const out = [];
    if (!competitions || !court) return out;
    for (const c of competitions) {
        if (!c) continue;
        const poolMatches = c.poolMatches || [];
        for (const m of poolMatches) {
            if ((m.court || "") !== court) continue;
            if (m.status !== "scheduled") continue;
            if (!m.sideA || !m.sideB) continue;
            out.push({ ...m, _comp: c });
        }
        const rounds = (c.bracket && c.bracket.rounds) || [];
        rounds.forEach((round, ri) => round.forEach((m) => {
            if ((m.court || "") !== court) return;
            if (m.status !== "scheduled") return;
            if (!m.sideA || !m.sideB) return;
            out.push({ ...m, _comp: c, _isBracket: true, _roundIndex: ri, _totalRounds: rounds.length });
        }));
    }
    out.sort((a, b) => {
        const qa = a.queuePosition || 9999;
        const qb = b.queuePosition || 9999;
        if (qa !== qb) return qa - qb;
        return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
    });
    return out.slice(0, limit);
}

// Counts of matches on a court grouped by status. Drives T062/T063 empty
// states: "All matches completed" requires completed > 0 + no live and
// no scheduled; "No matches scheduled" requires zero matches in total.
// Counts only the matches that have two real sides (not bye / TBD /
// "Winner of rX-mY" placeholders) so a half-resolved bracket doesn't
// flip the empty-state heuristic prematurely.
function countCourtMatches(competitions, court) {
    let live = 0, scheduled = 0, completed = 0;
    if (!competitions || !court) return { live, scheduled, completed };
    const hasBoth = (m) => {
        // Mirror admin_helpers.hasBothSides without taking a window
        // dependency at module-eval time. Bracket placeholders ("Winner
        // of r0-m1") are non-empty strings on raw payloads — reject them
        // explicitly so a half-resolved bracket doesn't inflate the
        // "scheduled" count and prevent the "All matches completed"
        // empty state from firing.
        if (!m || !m.sideA || !m.sideB) return false;
        const aName = typeof m.sideA === "string" ? m.sideA : (m.sideA.name || "");
        const bName = typeof m.sideB === "string" ? m.sideB : (m.sideB.name || "");
        if (!aName || !bName) return false;
        if (/^Winner of r\d+-m\d+$/.test(aName) || /^Winner of r\d+-m\d+$/.test(bName)) return false;
        return true;
    };
    const bump = (m) => {
        if (!hasBoth(m)) return;
        if (m.status === "running") live++;
        else if (m.status === "scheduled") scheduled++;
        else if (m.status === "completed") completed++;
    };
    for (const c of competitions) {
        if (!c) continue;
        for (const m of (c.poolMatches || [])) {
            if ((m.court || "") !== court) continue;
            bump(m);
        }
        for (const round of ((c.bracket && c.bracket.rounds) || [])) {
            for (const m of round) {
                if ((m.court || "") !== court) continue;
                bump(m);
            }
        }
    }
    return { live, scheduled, completed };
}

// Active courts = courts with at least one live or scheduled match.
// Filters out idle courts so LobbyDisplay doesn't waste real estate on
// "Shiaijo D — no matches" cards. Preserves the tournament's declared
// court order (A, B, C, …) instead of an arbitrary iteration order.
function findActiveCourts(tournament, competitions) {
    const all = (tournament && tournament.courts) || [];
    if (!competitions || competitions.length === 0) return [];
    const inUse = new Set();
    for (const c of competitions) {
        if (!c) continue;
        for (const m of (c.poolMatches || [])) {
            if (!m.court) continue;
            if (m.status === "running" || m.status === "scheduled") inUse.add(m.court);
        }
        for (const round of ((c.bracket && c.bracket.rounds) || [])) {
            for (const m of round) {
                if (!m.court) continue;
                if (m.status === "running" || m.status === "scheduled") inUse.add(m.court);
            }
        }
    }
    return all.filter(cc => inUse.has(cc));
}

// Queue-position label per T068, and the canonical source of truth for all
// viewer surfaces (display.jsx, VSchedItem in viewer.jsx, etc.).
//
// Contract (evaluated in this order):
//   1. status !== "scheduled"           → "" (no queue label on running /
//      completed / cancelled rows, even if queuePosition or scheduledAt
//      is set — this gate takes precedence over everything below).
//   2. status === "scheduled" + qp ===1 → "Next up".
//   3. status === "scheduled" + qp > 1  → "(qp - 1) before yours".
//   4. status === "scheduled" + falsy qp + scheduledAt → "Scheduled hh:mm".
//   5. anything else                    → "".
//
// queuePosition is coerced with Number() so JSON-string values ("1", "2")
// work the same as numeric ones.
//
// Wording is consolidated on "Next up" across all surfaces (bead mp-e3k).
// If you need a denser pill form for a tight row, use queueLabelCompact.
function queueLabel(m) {
    if (!m) return "";
    if (m.status !== "scheduled") return "";
    const qp = Number(m.queuePosition);
    if (Number.isFinite(qp) && qp > 0) {
        if (qp === 1) return "Next up";
        return `${qp - 1} before yours`;
    }
    if (m.scheduledAt) return `Scheduled ${m.scheduledAt}`;
    return "";
}

// Compact pill form of the queue-position label, for dense rows like the
// per-court TWMatch tiles on the tournament-wide viewer. Same canonical
// "Next up" wording for qp=1 so all surfaces agree (bead mp-e3k); other
// positions render as "#N". Returns null when there's nothing to show so
// callers can conditionally render the pill.
function queueLabelCompact(m) {
    if (!m) return null;
    if (m.status !== "scheduled") return null;
    const qp = Number(m.queuePosition);
    // Use Number.isFinite so Infinity/-Infinity are rejected alongside NaN —
    // matches queueLabel's guard. isNaN alone would let Infinity through and
    // render "#Infinity".
    if (!Number.isFinite(qp) || qp <= 0) return null;
    if (qp === 1) return "Next up";
    return `#${qp}`;
}

// Compute a phase label for either a pool or a bracket match.
function phaseLabel(m, isBracket, roundIndex, totalRounds) {
    if (m.phaseName) return m.phaseName;
    if (m.poolName) return m.poolName;
    if (isBracket && typeof roundIndex === "number" && window.roundLabel) {
        return window.roundLabel(roundIndex, totalRounds);
    }
    return m.round || "";
}

// <TvDisplay court="A"> — fullscreen per-court board.
//
// Implements T061 (visual treatment), T062 (auto-promote first scheduled
// when no live match + "all completed" / "no matches" empty states), and
// T063 (SSE reconnect indicator). Reads the data model that lives on the
// `competitions` prop (an array of normalised competitions; each carries
// poolMatches / bracket / withZekkenName) and the `tournament` prop (for
// venue branding / court labels).
//
// Auto-promote semantics (T062 / FR-011 scenario 3): if there's no live
// match on the court, the first scheduled match takes over the "current"
// slot, labelled UP NEXT instead of LIVE so spectators understand it
// hasn't actually started. The queue beneath shifts up by one so we
// don't double-render the promoted match.
//
// Empty states (T062 / FR-011 scenarios 4–5):
//   - All matches completed → "All matches completed on Shiaijo {court}"
//   - Nothing has ever been scheduled here → "No matches scheduled"
// These are mutually exclusive: completed > 0 AND no live/scheduled →
// "completed"; otherwise zero matches at all → "nothing".
//
// Reconnect indicator (T063 / FR-011 scenario 4): the `connected` prop
// (defaults to true) is wired from app.jsx which owns the SSE
// EventSource. When it flips false we render a small amber pill so the
// venue knows the screen has gone stale; the rest of the layout stays
// put so reconnect doesn't flash the layout.
function TvDisplay({ court, tournament, competitions, withZekkenName, connected = true }) {
    const live = useMD(() => findLiveOnCourt(competitions, court), [competitions, court]);
    const upcoming = useMD(() => findUpcomingOnCourt(competitions, court, live ? 2 : 3), [competitions, court, live]);
    const counts = useMD(() => countCourtMatches(competitions, court), [competitions, court]);

    if (!competitions || competitions.length === 0) {
        return <div className="tvd tvd--empty" style={{
            position: 'fixed', inset: 0,
            background: '#0b0d12', color: '#fff',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: '4vh', opacity: 0.6,
        }}>Loading…</div>;
    }

    // Auto-promote the first scheduled match when no live match (T062).
    // promoted = the match we'll show in the "current" slot.
    // queueMatches = matches we'll show in the queue list beneath.
    // When promoting, drop the first scheduled from the queue to avoid
    // double-rendering the same card.
    let promoted = null;
    let queueMatches = upcoming;
    let promotedKind = null; // "live" | "upnext" | null
    if (live) {
        promoted = { kind: "live", match: live.match, competition: live.competition, isBracket: live.isBracket, roundIndex: live.roundIndex, totalRounds: live.totalRounds };
        promotedKind = "live";
    } else if (upcoming.length > 0) {
        const first = upcoming[0];
        promoted = {
            kind: "upnext",
            match: first,
            competition: first._comp,
            isBracket: first._isBracket,
            roundIndex: first._roundIndex,
            totalRounds: first._totalRounds,
        };
        promotedKind = "upnext";
        queueMatches = upcoming.slice(1, 3);
    } else {
        queueMatches = [];
    }

    // Empty-state decisions (T062). "all completed" takes precedence over
    // "no matches" so a finished court reads clearly. counts.live === 0
    // is already guaranteed when promoted is null, but check it
    // explicitly for symmetry.
    const allCompleted = !promoted && counts.live === 0 && counts.scheduled === 0 && counts.completed > 0;
    const noMatches = !promoted && counts.completed === 0;

    const zekken = withZekkenName !== undefined
        ? withZekkenName
        : !!(promoted && promoted.competition && promoted.competition.withZekkenName);

    return (
        <div className="tvd" data-testid="tv-display-root" style={{
            position: 'fixed', inset: 0,
            background: '#0b0d12', color: '#fff',
            display: 'flex', flexDirection: 'column',
            padding: '4vh 5vw',
        }}>
            <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                fontSize: '2.5vh', opacity: 0.7, marginBottom: '2vh',
            }}>
                <div>{tournament?.name || ''} · SHIAIJO {court}</div>
                <div style={{ display: 'flex', gap: '1.5vw', alignItems: 'center' }}>
                    {/* T063: SSE reconnect indicator. Hidden while connected
                        so the chrome stays calm during normal operation. */}
                    {!connected && (
                        <span
                            data-testid="display-reconnect"
                            role="status"
                            aria-label="Reconnecting"
                            style={{
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: '0.6vw',
                                background: 'rgba(255,180,0,0.18)',
                                color: '#ffb400',
                                padding: '0.4vh 1vw',
                                borderRadius: '0.4vw',
                                fontSize: '1.6vh',
                                fontWeight: 700,
                                letterSpacing: '0.04em',
                            }}
                        >
                            <span style={{
                                width: '1.2vh', height: '1.2vh',
                                borderRadius: '50%',
                                background: '#ffb400',
                                display: 'inline-block',
                                animation: 'pulse 1.4s ease-in-out infinite',
                            }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {promoted ? (
                <div style={{ flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
                    <div style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                        fontSize: '3vh', opacity: 0.85, marginBottom: '2vh',
                    }}>
                        <div>
                            {promoted.competition?.name}
                            {' · '}
                            {phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds)}
                        </div>
                        {/* LIVE is the default promoted state — no badge (the board IS
                            the live match). UP NEXT is the exception, shown as a small
                            muted note rather than a loud badge. */}
                        {promotedKind === "upnext" && (
                            <span className="tvd-upnext-note" data-testid="tvd-upnext">↑ up next</span>
                        )}
                    </div>
                    {/* Aka/Shiro colored half-panels (DESIGN.md §4). SHIRO (sideB)
                        left, AKA (sideA) right; centre carries the real per-side
                        waza score (white–red), decision suffix + fouls. */}
                    <div className="tvd-board">
                        <div className="tvd-side tvd-side--shiro">
                            <div className="tvd-side__bar"></div>
                            <div className="tvd-side__lab"><TermD name="shiro">SHIRO</TermD></div>
                            <div className="tvd-side__name">{sideLabel(promoted.match.sideB, zekken)}</div>
                            {promoted.match.sideB?.dojo && (
                                <div className="tvd-side__dojo">{promoted.match.sideB.dojo}</div>
                            )}
                        </div>
                        <div className="tvd-centre">
                            {promotedKind === "live" ? (
                                <>
                                    <div className="tvd-score">
                                        <span className="tvd-score__s">{(promoted.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                        <span className="tvd-score__d">–</span>
                                        <span className="tvd-score__a">{(promoted.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                    </div>
                                    {/* T097: Kiken/Fus./DH/(E) suffix. Kept separate from the
                                        per-side digits (which carry the SHIRO/AKA colour split)
                                        rather than re-using formatIpponsScore. */}
                                    {window.decisionSuffix && window.decisionSuffix(promoted.match) && (
                                        <div className="tvd-centre__sfx">{window.decisionSuffix(promoted.match)}</div>
                                    )}
                                    {((promoted.match.hansokuA || 0) + (promoted.match.hansokuB || 0)) > 0 && (
                                        <div className="tvd-centre__fouls">
                                            Fouls {promoted.match.hansokuB || 0} – {promoted.match.hansokuA || 0}
                                        </div>
                                    )}
                                </>
                            ) : (
                                <div className="tvd-centre__vs">VS</div>
                            )}
                            {promoted.match.scheduledAt && (
                                <div className="tvd-side__dojo" style={{ opacity: 0.6 }}>{promoted.match.scheduledAt}</div>
                            )}
                        </div>
                        <div className="tvd-side tvd-side--aka">
                            <div className="tvd-side__bar"></div>
                            <div className="tvd-side__lab"><TermD name="aka">AKA</TermD></div>
                            <div className="tvd-side__name">{sideLabel(promoted.match.sideA, zekken)}</div>
                            {promoted.match.sideA?.dojo && (
                                <div className="tvd-side__dojo">{promoted.match.sideA.dojo}</div>
                            )}
                        </div>
                    </div>
                </div>
            ) : (
                <div data-testid={allCompleted ? 'display-all-completed' : 'display-no-matches'}
                    style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', gap: '2vh', fontSize: '5vh', opacity: 0.6, textAlign: 'center', padding: '0 5vw' }}>
                    {allCompleted ? (
                        <>
                            <span style={{ fontSize: '6vh' }}>✓</span>
                            <span>All matches completed on <TermD name="shiaijo">Shiaijo</TermD> {court}</span>
                        </>
                    ) : noMatches ? (
                        <span>No matches scheduled</span>
                    ) : (
                        <span>No live match on <TermD name="shiaijo">Shiaijo</TermD> {court}</span>
                    )}
                </div>
            )}

            {/* T068: upcoming-match list with queue-position labels.
                Pulls m.queuePosition (1-indexed, populated by handlers_viewer
                annotateQueuePositions). Position 1 → "Next up"; position N
                (>1) → "(N-1) before yours" to match VSchedItem's wording.
                T061: capped at 2 entries when live, falls back to up to 2
                additional cards when auto-promoting (so we still show a
                "next two upcoming" peek beyond the promoted match). */}
            {queueMatches.length > 0 && (
                <div className="tvd__upcoming" style={{
                    display: 'flex', gap: '2vw',
                    marginTop: '4vh',
                }}>
                    {queueMatches.map((m) => {
                        const compZekken = m._comp?.withZekkenName;
                        const label = queueLabel(m);
                        // Coerce to number so the qp===1 highlight matches even if
                        // server emits a string (consistent with TWMatch/VSchedItem).
                        const qp = Number(m.queuePosition);
                        return (
                            <div key={(m._comp?.id || '') + m.id} className="tvd__upcoming-card" style={{
                                flex: 1,
                                background: 'rgba(255,255,255,0.06)',
                                borderRadius: 8,
                                padding: '2vh 2vw',
                            }}>
                                <div style={{ fontSize: '1.8vh', opacity: 0.7, marginBottom: 6 }}>
                                    {m._comp?.name} · {phaseLabel(m, m._isBracket, m._roundIndex, m._totalRounds)}
                                </div>
                                <div style={{ fontSize: '2.5vh', fontWeight: 600 }}>
                                    {sideLabel(m.sideB, compZekken)} <span style={{ opacity: 0.4 }}>vs</span> {sideLabel(m.sideA, compZekken)}
                                </div>
                                {label && (
                                    <div style={{
                                        marginTop: 6,
                                        fontSize: '1.6vh',
                                        fontWeight: 700,
                                        color: qp === 1 ? '#ffd166' : 'rgba(255,255,255,0.7)',
                                        textTransform: 'uppercase',
                                        letterSpacing: '0.04em',
                                    }}>
                                        {label}
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

// LobbyDisplay pagination tunables. PAGE_SIZE = 2 courts per page so
// the table layout (2 court columns) fills a single screen; CYCLE_MS =
// 10 s is the cadence the spec recommends for auto-cycle. Tweaking
// these here flows to both the page-indicator and the timer below.
const LOBBY_PAGE_SIZE = 2;
const LOBBY_CYCLE_MS = 10000;

// Colour tokens for the cross-court table — defined once so the
// per-cell renderers stay readable. These mirror the mockup's :root
// vars but expressed as inline-style strings (display.jsx convention).
const LOBBY_COLORS = {
    bg:         '#080a0f',
    surface:    'rgba(255,255,255,0.025)',
    inkDim:     'rgba(255,255,255,0.45)',
    inkMuted:   'rgba(255,255,255,0.25)',
    line:       'rgba(255,255,255,0.06)',
    lineStrong: 'rgba(255,255,255,0.12)',
    nowBg:      'rgba(255,255,255,0.05)',
    nowBorder:  'rgba(255,255,255,0.10)',
    nextBg:     'rgba(255,209,102,0.06)',
    nextBorder: 'rgba(255,209,102,0.14)',
    nextAccent: '#ffd166',
    schedBg:    'rgba(255,255,255,0.02)',
    akaSoft:    '#ff9a95',
    akaVivid:   '#ff5b54',
};

// Row descriptor array — drives both the row-label column and the
// slot index pulled from the per-court slot arrays.
const LOBBY_ROWS = [
    { label: 'Now',  slot: 0 },
    { label: 'Next', slot: 1 },
    { label: '#3',   slot: 2 },
    { label: '#4',   slot: 3 },
    { label: '#5',   slot: 4 },
    { label: '#6',   slot: 5 },
];

// Build the display slots for a single court — one per LOBBY_ROWS entry.
//
// Auto-promote semantics (T062): when there is no live match the first
// scheduled match is promoted to slot 0 ("Now") with a slight style
// difference (no score shown in the vs column). The remaining
// upcoming matches fill slots 1 – (LOBBY_ROWS.length - 1).
//
// Returns an array of exactly LOBBY_ROWS.length elements; missing
// slots are null (rendered as an empty "—" cell).
function buildCourtSlots(competitions, court) {
    const totalSlots = LOBBY_ROWS.length;
    const live = findLiveOnCourt(competitions, court);
    // Request enough upcoming matches to fill the queue rows. When there
    // is no live match we need one extra (it will promote to slot 0).
    const upcoming = findUpcomingOnCourt(competitions, court, live ? totalSlots - 1 : totalSlots);

    const slots = new Array(totalSlots).fill(null);

    if (live) {
        slots[0] = { kind: 'live', match: live.match, competition: live.competition,
                     isBracket: live.isBracket, roundIndex: live.roundIndex,
                     totalRounds: live.totalRounds };
        for (let i = 0; i < upcoming.length && i + 1 < totalSlots; i++) {
            const m = upcoming[i];
            slots[i + 1] = { kind: 'scheduled', match: m, competition: m._comp,
                             isBracket: m._isBracket, roundIndex: m._roundIndex,
                             totalRounds: m._totalRounds };
        }
    } else if (upcoming.length > 0) {
        // Auto-promote first scheduled to "Now" slot.
        const first = upcoming[0];
        slots[0] = { kind: 'upnext', match: first, competition: first._comp,
                     isBracket: first._isBracket, roundIndex: first._roundIndex,
                     totalRounds: first._totalRounds };
        for (let i = 1; i < upcoming.length && i < totalSlots; i++) {
            const m = upcoming[i];
            slots[i] = { kind: 'scheduled', match: m, competition: m._comp,
                         isBracket: m._isBracket, roundIndex: m._roundIndex,
                         totalRounds: m._totalRounds };
        }
    }
    // If no live and no upcoming, slots stay null → empty cells.
    return slots;
}

// Render one match cell (td > .match-cell div) for the cross-court table.
// rowKind: 'now' | 'next' | 'queue' — determines the background/border.
// slot: the buildCourtSlots entry for this cell (null → empty cell).
function LobbyMatchCell({ slot, rowKind }) {
    if (!slot) {
        return (
            <td style={{ padding: '4px 8px', verticalAlign: 'top' }}>
                <div style={{
                    background: 'none',
                    borderRadius: 8, padding: '10px 14px',
                    minHeight: 54,
                    border: '1px solid transparent',
                    opacity: 0.12,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 18,
                }}>—</div>
            </td>
        );
    }

    const { kind, match, competition, isBracket, roundIndex, totalRounds } = slot;
    const zekken = !!(competition && competition.withZekkenName);

    let cellBg = LOBBY_COLORS.schedBg;
    let cellBorder = 'transparent';
    if (rowKind === 'now') {
        cellBg = LOBBY_COLORS.nowBg;
        cellBorder = LOBBY_COLORS.nowBorder;
    } else if (rowKind === 'next') {
        cellBg = LOBBY_COLORS.nextBg;
        cellBorder = LOBBY_COLORS.nextBorder;
    }

    const phase = phaseLabel(match, isBracket, roundIndex, totalRounds);
    const compMeta = [competition?.name, phase, match.scheduledAt].filter(Boolean).join(' · ');

    // Score column: live → actual scores; upnext/scheduled → "vs"
    let vsContent;
    if (kind === 'live') {
        const shiroScore = (match.ipponsB || []).filter(x => x && x !== '•').join('') || '0';
        const akaScore   = (match.ipponsA || []).filter(x => x && x !== '•').join('') || '0';
        const sfx = window.decisionSuffix ? window.decisionSuffix(match) : '';
        vsContent = (
            <span style={{ fontFamily: 'var(--font-mono, monospace)', fontWeight: 700, fontSize: 14, color: '#e8eaed' }}>
                {shiroScore}
                <span style={{ opacity: 0.45 }}> - </span>
                <span style={{ color: LOBBY_COLORS.akaVivid }}>{akaScore}</span>
                {sfx ? <span style={{ marginLeft: 4, fontSize: 11, opacity: 0.85 }}>{sfx}</span> : null}
            </span>
        );
    } else {
        vsContent = (
            <span style={{ fontFamily: 'var(--font-mono, monospace)', fontWeight: 500, fontSize: 14, color: LOBBY_COLORS.inkMuted }}>vs</span>
        );
    }

    return (
        <td style={{ padding: '4px 8px', verticalAlign: 'top' }}>
            <div style={{
                background: cellBg,
                borderRadius: 8, padding: '10px 14px',
                minHeight: 54,
                border: `1px solid ${cellBorder}`,
            }}>
                {compMeta && (
                    <div style={{ fontSize: 10, color: LOBBY_COLORS.inkMuted, marginBottom: 4, letterSpacing: '0.02em' }}>
                        {compMeta}
                    </div>
                )}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8, fontSize: 16, fontWeight: 600 }}>
                    {/* Shiro — white text, left side */}
                    <div style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {sideLabel(match.sideB, zekken)}
                    </div>
                    {/* Score / vs */}
                    <div style={{ flexShrink: 0, minWidth: 64, textAlign: 'center' }}>
                        {vsContent}
                    </div>
                    {/* Aka — pink/red text, right side */}
                    <div style={{ flex: 1, minWidth: 0, textAlign: 'right', color: LOBBY_COLORS.akaSoft, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {sideLabel(match.sideA, zekken)}
                    </div>
                </div>
            </div>
        </td>
    );
}

// <LobbyDisplay> — multi-court cross-court table for venue lobby screens.
//
// T064: shows all *active* courts (courts with at least one live or
// scheduled match) in a 2-column table. Each column is one court;
// rows are queue positions (Now, Next, #3–#6). Auto-promote semantics
// from TvDisplay/LobbyCard (T062) are preserved: when no live match
// exists the first scheduled promotes to "Now".
//
// T065: 2 courts per page; auto-cycles every 10 s when there are more
// courts than fit. A progress bar at the top and clickable dot
// pagination make the cycle visible to spectators.
//
// Reconnect indicator (T063) is preserved in the top bar.
function LobbyDisplay({ tournament, competitions, connected = true }) {
    const courts = useMD(() => findActiveCourts(tournament, competitions), [tournament, competitions]);
    const totalPages = Math.max(1, Math.ceil(courts.length / LOBBY_PAGE_SIZE));
    const [page, setPage] = useSD(0);
    // cycleKey increments on every page flip so the CSS animation on the
    // progress bar restarts from zero each time.
    const [cycleKey, setCycleKey] = useSD(0);

    // T065 auto-cycle. Only arm the timer when there is more than one
    // page. Reset to page 0 if the court count drops below the
    // threshold mid-cycle.
    //
    // All branches that change the page must also bump cycleKey so the
    // progress bar animation restarts — including guard resets, not
    // only the regular auto-cycle tick.
    useED(() => {
        if (totalPages <= 1) {
            if (page !== 0) { setPage(0); setCycleKey(k => k + 1); }
            return undefined;
        }
        if (page >= totalPages) {
            setPage(0);
            setCycleKey(k => k + 1);
            return undefined;
        }
        const t = setTimeout(() => {
            setPage(p => (p + 1) % totalPages);
            setCycleKey(k => k + 1);
        }, LOBBY_CYCLE_MS);
        return () => clearTimeout(t);
    }, [totalPages, page]);

    const start = page * LOBBY_PAGE_SIZE;
    const visible = courts.slice(start, start + LOBBY_PAGE_SIZE);

    // Build per-court slot arrays for the visible courts. Computed
    // outside JSX so each cell renderer receives a plain object.
    const courtSlots = visible.map(cc => buildCourtSlots(competitions, cc));

    // Page label: "Shiaijo A–B · 1 / 2" or just "Shiaijo A" for single.
    const pageCourtLabel = visible.length === 2
        ? `Shiaijo ${visible[0]}–${visible[1]}`
        : visible.length === 1
            ? `Shiaijo ${visible[0]}`
            : '';

    return (
        <div className="lobby" data-testid="lobby-root" style={{
            position: 'fixed', inset: 0,
            background: LOBBY_COLORS.bg, color: '#e8eaed',
            display: 'flex', flexDirection: 'column',
            fontFamily: 'var(--font-body, system-ui, sans-serif)',
            WebkitFontSmoothing: 'antialiased',
            overflow: 'hidden',
        }}>
            {/* ── Top bar ────────────────────────────────────── */}
            <div style={{
                display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                padding: '20px 36px 16px',
                fontSize: 13, color: LOBBY_COLORS.inkDim,
                letterSpacing: '0.08em', textTransform: 'uppercase',
                borderBottom: `1px solid ${LOBBY_COLORS.line}`,
            }}>
                <div style={{ fontWeight: 700, fontSize: 14, letterSpacing: '0.1em' }}>
                    {tournament?.name || ''}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
                    {/* Dot pagination — clickable to jump between pages */}
                    {totalPages > 1 && (
                        <div style={{ display: 'flex', gap: 7, alignItems: 'center' }}>
                            {Array.from({ length: totalPages }, (_, i) => (
                                <button
                                    type="button"
                                    key={i}
                                    data-testid={`lobby-page-dot-${i}`}
                                    onClick={() => { setPage(i); setCycleKey(k => k + 1); }}
                                    aria-label={`Page ${i + 1}`}
                                    style={{
                                        width: 7, height: 7, borderRadius: '50%',
                                        background: i === page ? '#e8eaed' : LOBBY_COLORS.inkMuted,
                                        transform: i === page ? 'scale(1.4)' : 'scale(1)',
                                        transition: 'background 0.4s, transform 0.4s',
                                        border: 'none', cursor: 'pointer', padding: 0,
                                    }}
                                />
                            ))}
                        </div>
                    )}
                    {totalPages > 1 && (
                        <span data-testid="lobby-page-indicator" style={{ fontSize: 11, color: LOBBY_COLORS.inkMuted, letterSpacing: '0.06em', fontWeight: 400 }}>
                            {pageCourtLabel} · {page + 1} / {totalPages}
                        </span>
                    )}
                    {/* T063: SSE reconnect indicator */}
                    {!connected && (
                        <span data-testid="display-reconnect" role="status" aria-label="Reconnecting" style={{
                            color: '#ffb400', fontWeight: 700,
                            display: 'inline-flex', alignItems: 'center', gap: 6,
                        }}>
                            <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#ffb400', display: 'inline-block' }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {/* ── Cycle progress bar ─────────────────────────── */}
            {totalPages > 1 && (
                <div style={{ height: 2, background: 'rgba(255,255,255,0.03)', position: 'relative', overflow: 'hidden' }}>
                    <div
                        key={cycleKey}
                        style={{
                            position: 'absolute', top: 0, left: 0, height: '100%',
                            background: 'linear-gradient(90deg, transparent, rgba(255,255,255,0.18))',
                            width: '100%',
                            transformOrigin: 'left',
                            animation: `lobby-cycle-fill ${LOBBY_CYCLE_MS}ms linear`,
                        }}
                    />
                </div>
            )}

            {/* ── Main content area ──────────────────────────── */}
            {courts.length === 0 ? (
                <div data-testid="lobby-empty" style={{
                    flex: 1,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 28, opacity: 0.55,
                }}>
                    No active courts
                </div>
            ) : (
                <div data-testid="lobby-display-grid" style={{ flex: 1, padding: '12px 36px 16px', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
                    <table style={{ width: '100%', borderCollapse: 'separate', borderSpacing: 0, tableLayout: 'fixed' }}>
                        <thead>
                            <tr>
                                {/* Row-label stub — presentational spacer, not a data header */}
                                <th
                                    aria-label="Queue position"
                                    scope="col"
                                    style={{
                                        width: 72, minWidth: 72,
                                        borderBottom: `2px solid ${LOBBY_COLORS.lineStrong}`,
                                        background: LOBBY_COLORS.bg,
                                    }}
                                />
                                {visible.map((cc, ci) => {
                                    // Derive subtitle from existing helpers so the
                                    // predicate stays consistent with the slots.
                                    const cts = countCourtMatches(competitions, cc);
                                    const remaining = cts.live + cts.scheduled;
                                    const liveHit = findLiveOnCourt(competitions, cc);
                                    const compName = liveHit
                                        ? (liveHit.competition?.name || '')
                                        : (() => {
                                            const first = findUpcomingOnCourt(competitions, cc, 1);
                                            return first.length > 0 ? (first[0]._comp?.name || '') : '';
                                        })();
                                    return (
                                        <React.Fragment key={cc}>
                                            <th style={{
                                                textAlign: 'center',
                                                padding: '14px 12px 12px',
                                                fontSize: 18, fontWeight: 700, letterSpacing: '0.1em',
                                                textTransform: 'uppercase',
                                                borderBottom: `2px solid ${LOBBY_COLORS.lineStrong}`,
                                                background: LOBBY_COLORS.bg,
                                            }}>
                                                Shiaijo {cc}
                                                {compName && (
                                                    <div style={{ fontSize: 11, fontWeight: 400, color: LOBBY_COLORS.inkMuted, marginTop: 4, letterSpacing: '0.02em', textTransform: 'none' }}>
                                                        {compName}{remaining > 0 ? ` · ${remaining} match${remaining === 1 ? '' : 'es'}` : ''}
                                                    </div>
                                                )}
                                            </th>
                                            {/* Thin separator between courts — decorative, hidden from AT */}
                                            {ci < visible.length - 1 && (
                                                <th aria-hidden="true" style={{ width: 1, padding: 0, background: 'transparent', borderBottom: 'none' }} />
                                            )}
                                        </React.Fragment>
                                    );
                                })}
                            </tr>
                        </thead>
                        <tbody>
                            {LOBBY_ROWS.map((row) => {
                                const rowKind = row.slot === 0 ? 'now' : row.slot === 1 ? 'next' : 'queue';
                                return (
                                    <tr key={row.label}>
                                        {/* Row label — <th scope="row"> so AT associates it with its cells */}
                                        <th scope="row" style={{
                                            textAlign: 'right', paddingRight: 16,
                                            fontSize: 10, fontWeight: 700, letterSpacing: '0.14em',
                                            textTransform: 'uppercase', color: LOBBY_COLORS.inkMuted,
                                            verticalAlign: 'top', paddingTop: 16,
                                            borderRight: `1px solid ${LOBBY_COLORS.line}`,
                                        }}>
                                            {row.label}
                                        </th>
                                        {visible.map((cc, ci) => (
                                            <React.Fragment key={cc}>
                                                <LobbyMatchCell
                                                    slot={courtSlots[ci][row.slot]}
                                                    rowKind={rowKind}
                                                />
                                                {/* Thin vertical separator between courts — decorative, hidden from AT */}
                                                {ci < visible.length - 1 && (
                                                    <td aria-hidden="true" style={{ width: 1, padding: 0, background: LOBBY_COLORS.line }} />
                                                )}
                                            </React.Fragment>
                                        ))}
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}

            {/* ── Bottom bar ─────────────────────────────────── */}
            <div style={{
                padding: '10px 36px',
                display: 'flex', justifyContent: 'center',
                fontSize: 10, color: LOBBY_COLORS.inkMuted,
                letterSpacing: '0.06em',
                borderTop: `1px solid ${LOBBY_COLORS.line}`,
            }}>
                {totalPages > 1
                    ? `Auto-cycling every ${LOBBY_CYCLE_MS / 1000} seconds`
                    : courts.length === 0
                        ? ''
                        : courts.length === 1
                            ? `Shiaijo ${courts[0]}`
                            : `Shiaijo ${courts.join(' · ')}`
                }
            </div>

            {/* Keyframe for the cycle progress bar animation. Injected via a
                <style> tag because inline styles cannot express @keyframes.
                Re-rendered with the component but content is static/idempotent. */}
            <style>{`
                @keyframes lobby-cycle-fill {
                    0%   { transform: scaleX(0); }
                    100% { transform: scaleX(1); }
                }
            `}</style>
        </div>
    );
}

// <StreamingOverlay court="A" position="bottom"> — transparent-background
// lower-third for OBS / vMix browser sources (T066, T067).
//
// CRITICAL: the page background MUST be transparent so the kendo broadcast
// video shows through. We set this via useEffect on mount and restore on
// unmount so navigating back to a normal view doesn't leave the body
// transparent.
//
// T067: keep the overlay DOM mounted regardless of live state so the
// opacity transition can run. Toggle opacity + pointerEvents only.
function StreamingOverlay({ court, position, competitions }) {
    const pos = position === 'top' ? 'top' : 'bottom';

    // T066: set page background to transparent so the OBS browser source
    // renders the overlay over the broadcast video without a backdrop.
    // Restore the original on unmount.
    useED(() => {
        const prev = document.body.style.background;
        document.body.style.background = 'transparent';
        const prevHtml = document.documentElement.style.background;
        document.documentElement.style.background = 'transparent';
        return () => {
            document.body.style.background = prev;
            document.documentElement.style.background = prevHtml;
        };
    }, []);

    const live = useMD(() => findLiveOnCourt(competitions, court), [competitions, court]);
    const hasLive = !!live;
    const comp = live?.competition;
    const zekken = !!(comp && comp.withZekkenName);

    const shiro = hasLive ? sideLabel(live.match.sideB, zekken) : '';
    const aka = hasLive ? sideLabel(live.match.sideA, zekken) : '';
    const ipponsB = hasLive ? ((live.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0') : '';
    const ipponsA = hasLive ? ((live.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0') : '';
    // T097: Kiken/Fus./DH/(E) suffix on the OBS lower-third. Computed off
    // the live match so it disappears the moment the overlay fades out.
    const decSfx = hasLive && window.decisionSuffix ? window.decisionSuffix(live.match) : '';
    const compName = comp?.name || '';

    return (
        <div className="streaming-overlay" data-testid="streaming-overlay-root" style={{
            position: 'fixed',
            left: '6%', right: '6%',
            bottom: pos === 'top' ? 'auto' : '6%',
            top: pos === 'top' ? '6%' : 'auto',
            background: 'rgba(0,0,0,0.85)',
            color: 'white',
            padding: '2vh 3vw',
            borderRadius: 8,
            fontSize: '3vh',
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            // T067: fade in/out — keep the DOM mounted so the transition runs.
            // 300ms sits in the middle of the A-6 200–400ms band.
            opacity: hasLive ? 1 : 0,
            transition: 'opacity 300ms ease-in-out',
            pointerEvents: hasLive ? 'auto' : 'none',
            // The overlay can be aria-hidden when not visible so screen
            // readers don't announce stale match data during the fade.
            visibility: hasLive ? 'visible' : 'hidden',
            transitionProperty: 'opacity, visibility',
            transitionDelay: hasLive ? '0s, 0s' : '0s, 300ms',
        }} aria-hidden={!hasLive}>
            <div style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                <span style={{
                    display: 'inline-block',
                    // Framed-white Shiro chip (DESIGN.md §4): white fill + navy
                    // frame + navy text, matching the framed-white badges across
                    // the app. Kept as a chip (not a flood) so the transparent
                    // lower-third still lets broadcast video show through.
                    background: '#fff',
                    color: 'var(--accent, #1d3557)',
                    border: '1px solid var(--accent, #1d3557)',
                    fontSize: '1.4vh',
                    fontWeight: 800,
                    letterSpacing: '0.06em',
                    padding: '2px 6px',
                    borderRadius: 4,
                    marginRight: 8,
                    verticalAlign: 'middle',
                }}><TermD name="shiro">SHIRO</TermD></span>
                <span style={{ fontWeight: 600, verticalAlign: 'middle' }}>{shiro}</span>
            </div>
            <div style={{
                flexShrink: 0,
                padding: '0 2vw',
                fontFamily: 'var(--font-mono, monospace)',
                fontWeight: 700,
                fontSize: '3.5vh',
            }}>
                {ipponsB} - {ipponsA}
                {decSfx && (
                    <span style={{ marginLeft: '1vw', fontSize: '2.4vh', opacity: 0.85 }}>{decSfx}</span>
                )}
            </div>
            <div style={{ flex: 1, minWidth: 0, textAlign: 'right', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                <span style={{ fontWeight: 600, verticalAlign: 'middle' }}>{aka}</span>
                <span style={{
                    display: 'inline-block',
                    // Solid-red Aka chip (DESIGN.md §4): --red fill, matching the
                    // app's Aka badges. e63946 → --red for token consistency.
                    background: 'var(--red, #c1121f)',
                    color: '#fff',
                    fontSize: '1.4vh',
                    fontWeight: 800,
                    letterSpacing: '0.06em',
                    padding: '2px 6px',
                    borderRadius: 4,
                    marginLeft: 8,
                    verticalAlign: 'middle',
                }}><TermD name="aka">AKA</TermD></span>
            </div>
            {compName && (
                <div style={{
                    position: 'absolute',
                    bottom: pos === 'top' ? 'auto' : '100%',
                    top: pos === 'top' ? '100%' : 'auto',
                    left: 0,
                    fontSize: '1.6vh',
                    color: 'rgba(255,255,255,0.85)',
                    padding: '4px 8px',
                    background: 'rgba(0,0,0,0.6)',
                    borderRadius: 4,
                    marginBottom: pos === 'top' ? 0 : 4,
                    marginTop: pos === 'top' ? 4 : 0,
                }}>{compName}</div>
            )}
        </div>
    );
}

// Wrapper that picks the right display component based on URL query.
// Used by the router when the path is /display. Reads `?court=A`,
// `?court=all`, `?overlay=true|1`, and `?position=top|bottom` off the
// current URL via useQuery() — see web-mobile/js/router.jsx.
//
// Defaults: missing court → "A", missing overlay → false, missing
// position → "bottom". Both "true" and "1" toggle overlay mode so the
// OBS muscle-memory ?overlay=1 form works alongside ?overlay=true.
//
// `connected` is forwarded so the per-court / lobby surfaces can show
// the SSE reconnect indicator. app.jsx owns the EventSource lifecycle
// and feeds the boolean down through the AppRouter props.
function DisplayRoute({ tournament, competitions, connected = true }) {
    const useQuery = window.AppRouter?.useQuery;
    const query = useQuery ? useQuery() : (() => {
        if (typeof window === 'undefined') return {};
        const s = window.location.search || '';
        const out = {};
        if (s.length < 2) return out;
        const trimmed = s.startsWith('?') ? s.slice(1) : s;
        for (const pair of trimmed.split('&')) {
            if (!pair) continue;
            const eq = pair.indexOf('=');
            if (eq === -1) out[decodeURIComponent(pair)] = '';
            else out[decodeURIComponent(pair.slice(0, eq))] = decodeURIComponent(pair.slice(eq + 1));
        }
        return out;
    })();
    const courtRaw = (query.court || 'A');
    const overlay = query.overlay === 'true' || query.overlay === '1';
    const position = query.position === 'top' ? 'top' : 'bottom';
    // Normalise "all" case-insensitively; keep single-court labels in
    // their declared case because the venue uses "A", "B", … not "a".
    const court = courtRaw.toLowerCase() === 'all' ? 'ALL' : courtRaw.toUpperCase();

    if (overlay) {
        return <StreamingOverlay court={court} position={position} competitions={competitions} />;
    }
    if (court === 'ALL') {
        return <LobbyDisplay tournament={tournament} competitions={competitions} connected={connected} />;
    }
    return <TvDisplay court={court} tournament={tournament} competitions={competitions} connected={connected} />;
}

export {
    TvDisplay,
    LobbyDisplay,
    StreamingOverlay,
    DisplayRoute,
    sideLabel,
    findLiveOnCourt,
    findUpcomingOnCourt,
    findActiveCourts,
    countCourtMatches,
    queueLabel,
    queueLabelCompact,
    LOBBY_PAGE_SIZE,
    LOBBY_CYCLE_MS,
    LOBBY_ROWS,
    buildCourtSlots,
};

if (typeof window !== 'undefined') {
    window.TvDisplay = TvDisplay;
    window.LobbyDisplay = LobbyDisplay;
    window.StreamingOverlay = StreamingOverlay;
    window.DisplayRoute = DisplayRoute;
    window.queueLabel = queueLabel;
    window.queueLabelCompact = queueLabelCompact;
}
