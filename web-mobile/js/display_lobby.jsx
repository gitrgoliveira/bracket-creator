// display_lobby.jsx — lobby / schedule TV (LobbyDisplay).
// Multi-court cross-court table for venue lobby screens. T064, T065, mp-13y.

import { findRunningOnCourt, findUpcomingOnCourt, findActiveCourts, phaseLabel, phaseProgressOnCourt } from './display_helpers.jsx';
import { IndividualScore } from './match_scoreboard.jsx';

const { useState: useSD, useEffect: useED, useMemo: useMD } = React;

// LobbyDisplay pagination tunables. PAGE_SIZE = 2 courts per page so
// the table layout (2 court columns) fills a single screen; CYCLE_MS =
// 10 s is the cadence the spec recommends for auto-cycle. Tweaking
// these here flows to both the page-indicator and the timer below.
const LOBBY_PAGE_SIZE = 2;
const LOBBY_CYCLE_MS = 10000;

// Colour tokens for the cross-court table — defined once so the
// per-cell renderers stay readable. These mirror the mockup's :root
// vars but expressed as inline-style strings (display.jsx convention).
// mp-13y: light palette (white redesign by analogy with the per-court board).
const LOBBY_COLORS = {
    bg:         '#ffffff',
    ink:        '#111111',
    surface:    'rgba(0,0,0,0.03)',
    inkDim:     'rgba(0,0,0,0.55)',
    inkMuted:   'rgba(0,0,0,0.35)',
    line:       'rgba(0,0,0,0.10)',
    lineStrong: 'rgba(0,0,0,0.20)',
    // NOW row: navy accent — emphasis is on the live match.
    nowBg:      'var(--accent-soft, #e7eaf3)',
    nowBorder:  'var(--accent, #1d3557)',
    // NEXT row: quiet neutral — visible but clearly subordinate to NOW.
    nextBg:     'rgba(0,0,0,0.02)',
    nextBorder: 'rgba(0,0,0,0.10)',
    schedBg:    'rgba(0,0,0,0.02)',
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
// Auto-promote semantics (T062): when there is no running match the first
// scheduled match is promoted to slot 0 ("Now") with a slight style
// difference (no score shown in the vs column). The remaining
// upcoming matches fill slots 1 – (LOBBY_ROWS.length - 1).
//
// Returns an array of exactly LOBBY_ROWS.length elements; missing
// slots are null (rendered as an empty "—" cell).
function buildCourtSlots(competitions, court) {
    const totalSlots = LOBBY_ROWS.length;
    const running = findRunningOnCourt(competitions, court);
    // Request enough upcoming matches to fill the queue rows. When there
    // is no running match we need one extra (it will promote to slot 0).
    const upcoming = findUpcomingOnCourt(competitions, court, running ? totalSlots - 1 : totalSlots);

    const slots = new Array(totalSlots).fill(null);

    if (running) {
        slots[0] = { kind: 'running', match: running.match, competition: running.competition,
                     isBracket: running.isBracket, roundIndex: running.roundIndex,
                     totalRounds: running.totalRounds };
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
    // If no running and no upcoming, slots stay null → empty cells.
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
    const sfx = (kind === 'running' && window.decisionSuffix) ? window.decisionSuffix(match) : '';

    return (
        <td style={{ padding: '4px 8px', verticalAlign: 'top' }}>
            <div style={{
                background: cellBg,
                borderRadius: 8, padding: '10px 14px',
                minHeight: 54,
                border: `1px solid ${cellBorder}`,
            }}>
                {compMeta && (
                    <div style={{ fontSize: 10, color: LOBBY_COLORS.inkMuted, marginBottom: 4, letterSpacing: '0.02em', display: 'flex', justifyContent: 'space-between', gap: 6 }}>
                        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{compMeta}</span>
                        {sfx && <span style={{ flexShrink: 0, fontWeight: 700, color: LOBBY_COLORS.ink }}>{sfx}</span>}
                    </div>
                )}
                {/* One matchup = one IndividualScore row (same component the
                    per-court board and viewer card use). Owns names, ippon
                    slots, hansoku ▲ on the offending side, hantei / decision
                    marks — attribution is positional, not color-only. For
                    scheduled rows the match has no ippons, so the slots
                    render empty (next to each name) which reads as "upcoming"
                    consistently with the running case's progression. */}
                <IndividualScore match={match} showNames withZekkenName={zekken} />
            </div>
        </td>
    );
}

// <LobbyDisplay> — multi-court cross-court table for venue lobby screens.
//
// T064: shows all *active* courts (courts with at least one running or
// scheduled match) in a 2-column table. Each column is one court;
// rows are queue positions (Now, Next, #3–#6). Auto-promote semantics
// from TvDisplay/LobbyCard (T062) are preserved: when no running match
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

    // Trim queue rows: always show Now (slot 0) and Next (slot 1) as anchors;
    // only include deeper rows (#3–#6) when at least one visible court has a
    // non-null slot at that index. This avoids a table half-filled with "—"
    // placeholders when the queue is short.
    const visibleRows = LOBBY_ROWS.filter(row =>
        row.slot < 2 || courtSlots.some(slots => slots[row.slot] != null)
    );

    // Page label: "Shiaijo A–B · 1 / 2" or just "Shiaijo A" for single.
    const pageCourtLabel = visible.length === 2
        ? `Shiaijo ${visible[0]}–${visible[1]}`
        : visible.length === 1
            ? `Shiaijo ${visible[0]}`
            : '';

    return (
        <div className="lobby" data-testid="lobby-root" style={{
            position: 'fixed', inset: 0,
            background: LOBBY_COLORS.bg, color: LOBBY_COLORS.ink,
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
                <div style={{ fontWeight: 700, fontSize: '2.4vh', letterSpacing: '0.1em' }}>
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
                                    aria-current={i === page ? 'page' : undefined}
                                    style={{
                                        width: 7, height: 7, borderRadius: '50%',
                                        background: i === page ? LOBBY_COLORS.ink : LOBBY_COLORS.inkMuted,
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
                            color: '#b45309', fontWeight: 700,
                            display: 'inline-flex', alignItems: 'center', gap: 6,
                        }}>
                            <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#b45309', display: 'inline-block' }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {/* ── Cycle progress bar ─────────────────────────── */}
            {totalPages > 1 && (
                <div style={{ height: 2, background: 'rgba(0,0,0,0.06)', position: 'relative', overflow: 'hidden' }}>
                    <div
                        key={cycleKey}
                        style={{
                            position: 'absolute', top: 0, left: 0, height: '100%',
                            background: 'linear-gradient(90deg, transparent, rgba(0,0,0,0.30))',
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
                                {/* Row-label column header — labels the queue-position column (Now/Next/#3…) */}
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
                                    // Derive subtitle from the already-built courtSlots so
                                    // the header and the table body always agree on which
                                    // match is "current" (same auto-promote logic, no rescan).
                                    const firstSlot = courtSlots[ci] && courtSlots[ci][0];
                                    const compName = firstSlot ? (firstSlot.competition?.name || '') : '';
                                    const phase = firstSlot ? phaseLabel(firstSlot.match, firstSlot.isBracket, firstSlot.roundIndex, firstSlot.totalRounds) : '';
                                    const progress = firstSlot ? phaseProgressOnCourt({
                                        competition: firstSlot.competition,
                                        isBracket: firstSlot.isBracket,
                                        roundIndex: firstSlot.roundIndex,
                                        match: firstSlot.match,
                                    }, cc) : null;
                                    const subtitle = [compName, phase, progress ? `${progress.done} / ${progress.total}` : null].filter(Boolean).join(' · ');
                                    return (
                                        <React.Fragment key={cc}>
                                            <th scope="col" style={{
                                                textAlign: 'center',
                                                padding: '14px 12px 12px',
                                                fontSize: 18, fontWeight: 700, letterSpacing: '0.1em',
                                                textTransform: 'uppercase',
                                                borderBottom: `2px solid ${LOBBY_COLORS.lineStrong}`,
                                                background: LOBBY_COLORS.bg,
                                            }}>
                                                Shiaijo {cc}
                                                {subtitle && (
                                                    <div data-testid={`lobby-shiaijo-subtitle-${cc}`} style={{ fontSize: 11, fontWeight: 400, color: LOBBY_COLORS.inkMuted, marginTop: 4, letterSpacing: '0.02em', textTransform: 'none' }}>
                                                        {subtitle}
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
                            {visibleRows.map((row) => {
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
                fontSize: '1.6vh', color: LOBBY_COLORS.inkMuted,
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

            {/* mp-c38: sponsor strip — non-interactive on lobby (no input
                hardware to click; touchscreen lobbies should not focus-trap). */}
            {window.SponsorStrip && <window.SponsorStrip sponsors={tournament && tournament.sponsors} variant="lobby" />}

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

export { LobbyDisplay, LobbyMatchCell, LOBBY_COLORS, buildCourtSlots, LOBBY_PAGE_SIZE, LOBBY_CYCLE_MS, LOBBY_ROWS };
