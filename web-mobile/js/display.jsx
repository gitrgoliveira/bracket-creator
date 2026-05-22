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

// Queue-position label per T068. We mirror the contract from VSchedItem in
// viewer.jsx: position 1 → "Next up"; position N → "(N-1) before yours".
// Falls back to scheduledAt time if no queue position is present (pre-T046
// payloads or unannotated matches). Keep this synchronised with the
// equivalent helper in viewer.jsx so the two surfaces agree.
function queueLabel(m) {
    const qp = m.queuePosition;
    if (qp && qp > 0) {
        if (qp === 1) return "Next up";
        return `${qp - 1} before yours`;
    }
    if (m.scheduledAt) return `Scheduled ${m.scheduledAt}`;
    return "";
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
                        {promotedKind === "live" && (
                            <span style={{ color: '#ff3b30', fontWeight: 800, fontSize: '2.4vh', letterSpacing: '0.1em' }}>● LIVE</span>
                        )}
                        {promotedKind === "upnext" && (
                            <span style={{ color: '#ffd166', fontWeight: 800, fontSize: '2.4vh', letterSpacing: '0.1em' }}>UP NEXT</span>
                        )}
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '8vh', fontWeight: 700 }}>
                        <div style={{ flex: 1, minWidth: 0 }}>
                            <div style={{ fontSize: '2.5vh', opacity: 0.6 }}><TermD name="shiro">SHIRO</TermD></div>
                            <div style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{sideLabel(promoted.match.sideB, zekken)}</div>
                            {promoted.match.sideB?.dojo && (
                                <div style={{ fontSize: '2.2vh', opacity: 0.55, fontWeight: 400, marginTop: '0.5vh' }}>
                                    {promoted.match.sideB.dojo}
                                </div>
                            )}
                        </div>
                        <div style={{
                            flexShrink: 0,
                            padding: '0 4vw',
                            fontFamily: 'var(--font-mono, monospace)',
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            gap: '1vh',
                        }}>
                            {promotedKind === "live" ? (
                                <>
                                    <div>
                                        <span>{(promoted.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                        <span style={{ opacity: 0.4 }}> - </span>
                                        <span style={{ color: '#ff5b54' }}>{(promoted.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                    </div>
                                    {/* T097: decision suffix on the TV's live block. Hand-rolled
                                        score above keeps the SHIRO/AKA colour split, so we add the
                                        Kiken/Fus./DH/(E) label as its own line beneath the digits
                                        rather than re-using formatIpponsScore (which collapses
                                        both sides into one string). */}
                                    {window.decisionSuffix && window.decisionSuffix(promoted.match) && (
                                        <div style={{ fontSize: '2.2vh', opacity: 0.8, fontWeight: 700, letterSpacing: '0.05em' }}>
                                            {window.decisionSuffix(promoted.match)}
                                        </div>
                                    )}
                                    {((promoted.match.hansokuA || 0) + (promoted.match.hansokuB || 0)) > 0 && (
                                        <div style={{ fontSize: '2.2vh', opacity: 0.65, fontWeight: 500 }}>
                                            Fouls {promoted.match.hansokuB || 0} – {promoted.match.hansokuA || 0}
                                        </div>
                                    )}
                                </>
                            ) : (
                                <div style={{ fontSize: '6vh', opacity: 0.35 }}>vs</div>
                            )}
                            {promoted.match.scheduledAt && (
                                <div style={{ fontSize: '2vh', opacity: 0.6, fontWeight: 500 }}>
                                    {promoted.match.scheduledAt}
                                </div>
                            )}
                        </div>
                        <div style={{ flex: 1, textAlign: 'right', minWidth: 0 }}>
                            <div style={{ fontSize: '2.5vh', opacity: 0.7, color: '#e63946' }}><TermD name="aka">AKA</TermD></div>
                            <div style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{sideLabel(promoted.match.sideA, zekken)}</div>
                            {promoted.match.sideA?.dojo && (
                                <div style={{ fontSize: '2.2vh', opacity: 0.55, fontWeight: 400, marginTop: '0.5vh' }}>
                                    {promoted.match.sideA.dojo}
                                </div>
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
                        const qp = m.queuePosition;
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

// LobbyDisplay pagination tunables. PAGE_SIZE = 4 lines up with the
// "≤ 4 courts fit on one screen" assumption from T065; CYCLE_MS = 10s
// is the cadence the spec recommends for auto-cycle. Tweaking these
// here flows to both the page-indicator and the timer below.
const LOBBY_PAGE_SIZE = 4;
const LOBBY_CYCLE_MS = 10000;

// <LobbyDisplay> — multi-court grid for venue lobby screens.
//
// T064: one card per *active* court (i.e. courts with at least one live
// or scheduled match). Each card mirrors TvDisplay's structure at
// reduced scale: court header, current/up-next match, and two queue
// items beneath. CSS grid auto-fit makes the layout responsive to the
// court count up to one screenful.
//
// T065: when there are more active courts than fit on one screen
// (PAGE_SIZE = 4), cards auto-cycle every 10 seconds. The page counter
// (1/3 etc.) is rendered in the header so spectators understand the
// rotation. Below the threshold the timer is a no-op and all cards
// render at once.
//
// Reconnect indicator (T063) is mirrored here so the lobby screen also
// shows when SSE is in a reconnect window.
function LobbyDisplay({ tournament, competitions, connected = true }) {
    const courts = useMD(() => findActiveCourts(tournament, competitions), [tournament, competitions]);
    const totalPages = Math.max(1, Math.ceil(courts.length / LOBBY_PAGE_SIZE));
    const [page, setPage] = useSD(0);

    // T065 auto-cycle. Only arm the timer when there are more courts
    // than fit. Reset to page 0 if the court count drops below the
    // threshold mid-cycle (e.g. a court finishes and falls off).
    useED(() => {
        if (courts.length <= LOBBY_PAGE_SIZE) {
            if (page !== 0) setPage(0);
            return undefined;
        }
        if (page >= totalPages) {
            setPage(0);
            return undefined;
        }
        const t = setTimeout(() => setPage(p => (p + 1) % totalPages), LOBBY_CYCLE_MS);
        return () => clearTimeout(t);
    }, [courts.length, page, totalPages]);

    const start = page * LOBBY_PAGE_SIZE;
    const visible = courts.length > LOBBY_PAGE_SIZE
        ? courts.slice(start, start + LOBBY_PAGE_SIZE)
        : courts;

    return (
        <div className="lobby" style={{
            position: 'fixed', inset: 0,
            background: '#0b0d12', color: '#fff',
            display: 'flex', flexDirection: 'column',
            padding: 24, gap: 16,
        }}>
            <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                fontSize: 14, opacity: 0.7, letterSpacing: '0.06em', textTransform: 'uppercase',
            }}>
                <div>{tournament?.name || ''}</div>
                <div style={{ display: 'flex', gap: 16, alignItems: 'center' }}>
                    <span>{courts.length} active court{courts.length === 1 ? '' : 's'}</span>
                    {totalPages > 1 && (
                        <span data-testid="lobby-page-indicator">
                            Page {page + 1} / {totalPages}
                        </span>
                    )}
                    {!connected && (
                        <span data-testid="display-reconnect" role="status" aria-label="Reconnecting" style={{
                            color: '#ffb400',
                            fontWeight: 700,
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 6,
                        }}>
                            <span style={{
                                width: 8, height: 8, borderRadius: '50%',
                                background: '#ffb400', display: 'inline-block',
                            }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {courts.length === 0 ? (
                <div data-testid="lobby-empty" style={{
                    flex: 1,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 28, opacity: 0.55,
                }}>
                    No active courts
                </div>
            ) : (
                <div data-testid="lobby-display-grid" style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))',
                    gap: 16,
                    flex: 1,
                    overflow: 'hidden',
                }}>
                    {visible.map(cc => <LobbyCard key={cc} court={cc} tournament={tournament} competitions={competitions} />)}
                </div>
            )}
        </div>
    );
}

// One court's card inside LobbyDisplay. Same auto-promote semantics as
// TvDisplay but rendered at lobby-card scale (px-based instead of vw-
// based so multiple cards on one screen stay legible regardless of
// court count). Shows current match + up to 2 queue items.
function LobbyCard({ court, tournament: _tournament, competitions }) {
    const live = useMD(() => findLiveOnCourt(competitions, court), [competitions, court]);
    const upcoming = useMD(() => findUpcomingOnCourt(competitions, court, live ? 2 : 3), [competitions, court, live]);
    const counts = useMD(() => countCourtMatches(competitions, court), [competitions, court]);

    let promoted = null;
    let queueMatches = upcoming;
    let promotedKind = null;
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

    const allCompleted = !promoted && counts.live === 0 && counts.scheduled === 0 && counts.completed > 0;
    const zekken = !!(promoted && promoted.competition && promoted.competition.withZekkenName);

    return (
        <div className="lobby__card" style={{
            background: 'rgba(255,255,255,0.04)',
            borderRadius: 12, padding: 20,
            display: 'flex', flexDirection: 'column',
            gap: 12,
            border: '1px solid rgba(255,255,255,0.06)',
            minWidth: 0,
            overflow: 'hidden',
        }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: '0.06em' }}>SHIAIJO {court}</div>
                {promotedKind === "live" && (
                    <span style={{ color: '#ff3b30', fontWeight: 800, fontSize: 12 }}>● LIVE</span>
                )}
                {promotedKind === "upnext" && (
                    <span style={{ color: '#ffd166', fontWeight: 700, fontSize: 12 }}>UP NEXT</span>
                )}
            </div>

            {promoted ? (
                <>
                    <div style={{ fontSize: 13, opacity: 0.7 }}>
                        {promoted.competition?.name}
                        {' · '}
                        {phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds)}
                        {promoted.match.scheduledAt ? ` · ${promoted.match.scheduledAt}` : ''}
                    </div>
                    <div style={{
                        display: 'grid',
                        gridTemplateColumns: '1fr auto 1fr',
                        gap: 12,
                        alignItems: 'center',
                        fontSize: 22, fontWeight: 600,
                    }}>
                        <div style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            <div style={{ fontSize: 10, opacity: 0.55, letterSpacing: '0.2em' }}><TermD name="shiro">SHIRO</TermD></div>
                            <div style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{sideLabel(promoted.match.sideB, zekken)}</div>
                        </div>
                        <div style={{
                            fontFamily: 'var(--font-mono, monospace)', fontWeight: 700,
                            opacity: 0.85, fontSize: 22, minWidth: 80, textAlign: 'center',
                        }}>
                            {promotedKind === "live" ? (
                                <>
                                    <span>{(promoted.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                    <span style={{ opacity: 0.4 }}> - </span>
                                    <span style={{ color: '#ff5b54' }}>{(promoted.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0'}</span>
                                    {/* T097: lobby grid is dense, so the suffix rides inline
                                        on the same row as the digits rather than a separate
                                        line. Empty string when no decision/encho applies. */}
                                    {window.decisionSuffix && window.decisionSuffix(promoted.match) && (
                                        <span style={{ marginLeft: 6, fontSize: 12, opacity: 0.8, fontWeight: 600 }}>
                                            {window.decisionSuffix(promoted.match)}
                                        </span>
                                    )}
                                </>
                            ) : (
                                <span style={{ opacity: 0.4 }}>vs</span>
                            )}
                        </div>
                        <div style={{ textAlign: 'right', color: '#ff8a86', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                            <div style={{ fontSize: 10, opacity: 0.55, letterSpacing: '0.2em', color: '#e63946' }}><TermD name="aka">AKA</TermD></div>
                            <div style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{sideLabel(promoted.match.sideA, zekken)}</div>
                        </div>
                    </div>
                </>
            ) : allCompleted ? (
                <div style={{ opacity: 0.5, fontSize: 15, padding: '12px 0' }}>All matches completed</div>
            ) : (
                <div style={{ opacity: 0.5, fontSize: 15, padding: '12px 0' }}>No matches</div>
            )}

            {queueMatches.length > 0 && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 }}>
                    <div style={{ fontSize: 10, opacity: 0.5, letterSpacing: '0.2em', textTransform: 'uppercase' }}>Queue</div>
                    {queueMatches.map((m) => {
                        const compZekken = m._comp?.withZekkenName;
                        return (
                            <div key={(m._comp?.id || '') + m.id} style={{
                                display: 'grid',
                                gridTemplateColumns: '1fr auto 1fr',
                                alignItems: 'center',
                                gap: 8,
                                fontSize: 13,
                                opacity: 0.85,
                            }}>
                                <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                    {sideLabel(m.sideB, compZekken)}
                                </div>
                                <div style={{ opacity: 0.5, fontSize: 11 }}>
                                    {m.scheduledAt || 'vs'}
                                </div>
                                <div style={{ textAlign: 'right', color: '#ff8a86', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                    {sideLabel(m.sideA, compZekken)}
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}
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
                    background: '#fff',
                    color: '#000',
                    fontSize: '1.4vh',
                    fontWeight: 700,
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
                    background: '#e63946',
                    color: '#fff',
                    fontSize: '1.4vh',
                    fontWeight: 700,
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
    LOBBY_PAGE_SIZE,
    LOBBY_CYCLE_MS,
};

if (typeof window !== 'undefined') {
    window.TvDisplay = TvDisplay;
    window.LobbyDisplay = LobbyDisplay;
    window.StreamingOverlay = StreamingOverlay;
    window.DisplayRoute = DisplayRoute;
}
