// display_scoreboard.jsx — per-court TV scoreboard (TvDisplay).
// Fullscreen white board shown on Shiaijo-dedicated screens.
// T061, T062, T063, mp-13y.

import { findRunningOnCourt, findUpcomingOnCourt, countCourtMatches, sideLabel, phaseLabel, TermD, poolNameOf, StreamingQR } from './display_helpers.jsx';
import { TeamScoreboard, IndividualScore, useTeamLineups, teamIVPW } from './match_scoreboard.jsx';

const { useMemo: useMD } = React;

// emptyStateHeadline — headline text for the TvDisplay empty state, by sub-state.
// The third case ("No match in progress") is a defensive fallback that is
// UNREACHABLE under the current promote logic: countCourtMatches and
// findUpcomingOnCourt/findRunningOnCourt share the same bracketSidesReady
// predicate, so any running/scheduled match the counts see is also
// auto-promoted — and the empty state only renders when nothing is promoted
// (so counts.running and counts.scheduled are both 0 here). It is kept so that
// if that invariant is ever broken the screen degrades to a correct message
// rather than a wrong one. Exported + unit-tested per the PR #274 review (mp-s99q).
function emptyStateHeadline(allCompleted, noMatches) {
    if (allCompleted) return "All matches completed";
    if (noMatches) return "No matches scheduled";
    return "No match in progress";
}

// TvWhiteTeamBoard — mp-13y: white scoreboard for a running TEAM match
// (per the agreed mockup). Replaces the dark aka/shiro half-panels for the
// team case with a light board: court header + black rule, team name row
// (Shiro black left / Aka red right, NO top IV score), then the per-bout
// grid (done / in-progress amber / queued grey), optional Daihyosen banner,
// and a single "Next" line. Individual matches, empty states and the lobby
// keep the existing dark surface (no mockup for those).

function TvWhiteBoard({ tournament, court, connected, promoted, isTeamMatch, subResults, lineupA, lineupB, teamSize, showDH, queueMatches, zekken }) {
    const shiroTeam = sideLabel(promoted.match.sideB, zekken);
    const akaTeam = sideLabel(promoted.match.sideA, zekken);
    const next = queueMatches && queueMatches.length ? queueMatches[0] : null;
    const sfx = (window.decisionSuffix && window.decisionSuffix(promoted.match)) || "";
    // The shared scoreboard below carries the score (IV/PW summary for teams,
    // ippon slots for individuals), so the team-name row centre is just "vs"
    // (+ any decision suffix).
    const nameCentre = <div style={{ fontSize: "2.4vh", color: "#9ca3af", fontWeight: 700 }}>vs{sfx ? <span style={{ marginLeft: "1vw", color: "#374151" }}>{sfx}</span> : null}</div>;

    return (
        <div className="tvd tvd--white" data-testid="tv-display-root" style={{
            position: "fixed", inset: 0, background: "#ffffff", color: "#111",
            display: "flex", flexDirection: "column", padding: "4vh 5vw",
        }}>
            {/* Court header + black rule */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", borderBottom: "3px solid #111", paddingBottom: "1.4vh", marginBottom: "2.4vh" }}>
                <div style={{ fontSize: "2.6vh", fontWeight: 700, letterSpacing: "0.08em" }}>
                    {tournament?.name ? tournament.name + " · " : ""}SHIAIJO {court}
                </div>
                <div style={{ display: "flex", gap: "1.5vw", alignItems: "center", fontSize: "2.2vh", color: "#6b7280" }}>
                    <span>{promoted.competition?.name} · {phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds)}</span>
                    {/* mp-13y #9: no "UP NEXT" badge — the promoted match is shown
                        plainly (the NEXT line below still lists what follows). */}
                    {!connected && (
                        <span data-testid="display-reconnect" role="status" aria-label="Reconnecting"
                            style={{ display: "inline-flex", alignItems: "center", gap: "0.6vw", background: "#fef3c7", color: "#b45309", padding: "0.4vh 1vw", borderRadius: "0.4vw", fontSize: "1.6vh", fontWeight: 700 }}>
                            <span style={{ width: "1.2vh", height: "1.2vh", borderRadius: "50%", background: "#b45309", display: "inline-block", animation: "pulse 1.4s ease-in-out infinite" }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {/* Team name row — Shiro black (left), Aka red (right), no top score */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr auto 1fr", alignItems: "center", gap: "2vw", marginBottom: "2vh" }}>
                <div style={{ minWidth: 0 }}>
                    <div style={{ fontFamily: "var(--font-impact)", fontSize: "2.2vh", letterSpacing: "0.14em", color: "#6b7280" }}><TermD name="shiro">SHIRO</TermD></div>
                    <div style={{ fontSize: "5vh", fontWeight: 800, color: "#111", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{shiroTeam}</div>
                </div>
                <div style={{ display: "flex", justifyContent: "center" }}>{nameCentre}</div>
                <div style={{ minWidth: 0, textAlign: "right" }}>
                    <div style={{ fontFamily: "var(--font-impact)", fontSize: "2.2vh", letterSpacing: "0.14em", color: "#b91c1c" }}><TermD name="aka">AKA</TermD></div>
                    <div style={{ fontSize: "5vh", fontWeight: 800, color: "#b91c1c", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{akaTeam}</div>
                </div>
            </div>

            {/* Shared FIK scoreboard (match_scoreboard.jsx) — the SAME component
                the viewer card uses; variant="tv" only scales it up. Up-next
                matches have no bouts yet: TeamScoreboard renders numbered/roster
                rows (mp-13y #6) so the board reads as a real scoreboard rather
                than an empty grid. */}
            {isTeamMatch ? (
                <div style={{ flex: 1 }} data-testid="tvd-team-bouts">
                    {/* tri-review #1: thread the team names so the Daihyosen
                        win-mark lands on the winning side when the backend
                        persists the winner as the team name (mirrors viewer
                        MatchDetailCard). */}
                    <TeamScoreboard subResults={subResults} lineupA={lineupA} lineupB={lineupB}
                        teamSize={teamSize} showDH={showDH} variant="tv"
                        shiroName={shiroTeam} akaName={akaTeam}
                        matchSideA={promoted.match.sideA?.name || (typeof promoted.match.sideA === "string" ? promoted.match.sideA : "")}
                        matchSideB={promoted.match.sideB?.name || (typeof promoted.match.sideB === "string" ? promoted.match.sideB : "")} />
                </div>
            ) : (
                <div style={{ flex: 1, display: "flex", alignItems: "flex-start", justifyContent: "center", paddingTop: "2vh" }}>
                    {/* tri-review #2: thread withZekkenName so IndividualScore
                        renders the zekken display name on zekken-mode comps. */}
                    <IndividualScore match={promoted.match} variant="tv" withZekkenName={zekken} />
                </div>
            )}

            {/* Next line */}
            {next && (
                <div style={{ display: "flex", alignItems: "center", gap: "1.5vw", borderTop: "1px dashed #d1d5db", paddingTop: "1.6vh", marginTop: "1.6vh" }}>
                    <span style={{ fontSize: "1.8vh", letterSpacing: "0.12em", color: "#6b7280", fontWeight: 700 }}>NEXT</span>
                    <span style={{ flex: 1, display: "flex", justifyContent: "space-between", fontSize: "2.6vh" }}>
                        <span style={{ color: "#111", fontWeight: 600 }}>{sideLabel(next.sideB, next._comp?.withZekkenName)}</span>
                        <span style={{ color: "#9ca3af", fontSize: "2vh", padding: "0 1vw" }}>vs</span>
                        <span style={{ color: "#b91c1c", fontWeight: 600 }}>{sideLabel(next.sideA, next._comp?.withZekkenName)}</span>
                    </span>
                </div>
            )}
            {window.SponsorStrip && <window.SponsorStrip sponsors={tournament && tournament.sponsors} variant="tv" />}
        </div>
    );
}

// gatherIndividualGroup — the sibling matches for an individual TV board:
// every match in the same POOL (pool phase) or the same ROUND (knockout) as
// the promoted match, **on the same court**. The TV display is per-court, so
// bracket rounds that span multiple courts must not leak cross-court matches.
// Returns them ordered completed-first with the CURRENT (running, or the
// promoted up-next) match LAST, so the feed keeps the active match at the
// bottom of the visible list and older results scroll off the top.
// Not-yet-started matches other than the promoted one are omitted (feed model).
function gatherIndividualGroup(promoted, court) {
    if (!promoted || !promoted.competition || !promoted.match) return [];
    const comp = promoted.competition;
    const cur = promoted.match;
    const matchCourt = court || cur.court || "";
    let group;
    if (promoted.isBracket) {
        const rounds = (comp.bracket && comp.bracket.rounds) || [];
        group = rounds[promoted.roundIndex] || [];
    } else {
        const pool = poolNameOf(cur.id);
        group = (comp.poolMatches || []).filter(m => poolNameOf(m.id) === pool);
    }
    // Filter to the same court — bracket rounds can span multiple courts.
    const onCourt = group.filter(m => (m.court || "") === matchCourt);
    const isCurrent = m => m.id === cur.id || m.status === "running";
    const shown = onCourt.filter(m => m.status === "completed" || isCurrent(m));
    return shown.slice().sort((a, b) => {
        const d = (isCurrent(a) ? 1 : 0) - (isCurrent(b) ? 1 : 0); // current sinks to bottom
        if (d !== 0) return d;
        return String(a.scheduledAt || a.id).localeCompare(String(b.scheduledAt || b.id));
    });
}

// At variant=tv each row is roughly 6vh tall and the body has ~80vh of room,
// so ~10 rows fit comfortably. Cap at TV_INDIV_MAX_VISIBLE and take the TAIL
// of the gathered group so the current match (already sorted to the end by
// gatherIndividualGroup) is always shown — the oldest completed rows drop off
// the top first. The visible rows then render top-anchored on the panel so
// there's no empty space above when the pool is small.
const TV_INDIV_MAX_VISIBLE = 10;

// TvIndividualBoard — mp-13y: white TV board for INDIVIDUAL competitions. The
// body lists the whole pool's matches (pool phase) or the whole round's matches
// (knockout) as a feed: each row is one match (Shiro name · ippon slots · Aka
// name, via the shared IndividualScore). Layout: TOP-anchored — the rows fill
// from the top of the panel; the running match stays at the bottom of the
// visible list because gatherIndividualGroup sorts current LAST. When the
// group exceeds TV_INDIV_MAX_VISIBLE, the oldest completed rows drop off the
// top so the current match stays on screen (no animation). FIK §263.
function TvIndividualBoard({ tournament, court, connected, promoted, queueMatches, zekken }) {
    const all = gatherIndividualGroup(promoted, court);
    const dropped = Math.max(0, all.length - TV_INDIV_MAX_VISIBLE);
    const rows = dropped > 0 ? all.slice(dropped) : all;
    const groupLabel = phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds);
    const next = queueMatches && queueMatches.length ? queueMatches[0] : null;
    return (
        <div className="tvd tvd--white" data-testid="tv-display-root" style={{
            position: "fixed", inset: 0, background: "#ffffff", color: "#111",
            display: "flex", flexDirection: "column", padding: "4vh 5vw",
        }}>
            {/* Court header + black rule */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", borderBottom: "3px solid #111", paddingBottom: "1.4vh", marginBottom: "2.4vh" }}>
                <div style={{ fontSize: "2.6vh", fontWeight: 700, letterSpacing: "0.08em" }}>
                    {tournament?.name ? tournament.name + " · " : ""}SHIAIJO {court}
                </div>
                <div style={{ display: "flex", gap: "1.5vw", alignItems: "center", fontSize: "2.2vh", color: "#6b7280" }}>
                    <span>{promoted.competition?.name}{groupLabel ? " · " + groupLabel : ""}</span>
                    {!connected && (
                        <span data-testid="display-reconnect" role="status" aria-label="Reconnecting"
                            style={{ display: "inline-flex", alignItems: "center", gap: "0.6vw", background: "#fef3c7", color: "#b45309", padding: "0.4vh 1vw", borderRadius: "0.4vw", fontSize: "1.6vh", fontWeight: 700 }}>
                            <span style={{ width: "1.2vh", height: "1.2vh", borderRadius: "50%", background: "#b45309", display: "inline-block", animation: "pulse 1.4s ease-in-out infinite" }} />
                            RECONNECTING
                        </span>
                    )}
                </div>
            </div>

            {/* Top-anchored match feed: the visible rows fill from the top of
                the panel; gatherIndividualGroup sorts the current match LAST
                so it stays at the bottom of the visible list. When the group
                exceeds TV_INDIV_MAX_VISIBLE, we slice the tail above so the
                oldest completed rows drop off the top first. */}
            <div data-testid="tvd-indiv-group" data-dropped={dropped} style={{ flex: 1, display: "flex", flexDirection: "column", justifyContent: "flex-start", gap: "1vh", overflow: "hidden" }}>
                {rows.map(m => {
                    const isNow = m.id === promoted.match.id || m.status === "running";
                    const isDone = !isNow && m.status === "completed";
                    return (
                        <div key={m.id} data-testid={isNow ? "tvd-indiv-row-now" : "tvd-indiv-row"}
                            className={"tvd-indiv-row" + (isNow ? " tvd-indiv-row--now" : "") + (isDone ? " tvd-indiv-row--done" : "")}
                            style={{
                                padding: "1vh 1.5vw", borderRadius: "0.6vw",
                                background: isNow ? "var(--accent-soft)" : isDone ? "#f9fafb" : "transparent",
                                borderLeft: isNow ? "0.8vw solid var(--accent)" : undefined,
                                opacity: isNow ? 1 : isDone ? 0.88 : 0.78,
                            }}>
                            <IndividualScore match={m} variant="tv" showNames withZekkenName={zekken} />
                        </div>
                    );
                })}
                {rows.length === 0 && (
                    <div style={{ textAlign: "center", color: "#9ca3af", fontSize: "3vh", padding: "4vh 0" }}>No matches yet</div>
                )}
            </div>

            {/* Next line */}
            {next && (
                <div style={{ display: "flex", alignItems: "center", gap: "1.5vw", borderTop: "1px dashed #d1d5db", paddingTop: "1.6vh", marginTop: "1.6vh" }}>
                    <span style={{ fontSize: "1.8vh", letterSpacing: "0.12em", color: "#6b7280", fontWeight: 700 }}>NEXT</span>
                    <span style={{ flex: 1, display: "flex", justifyContent: "space-between", fontSize: "2.6vh" }}>
                        <span style={{ color: "#111", fontWeight: 600 }}>{sideLabel(next.sideB, next._comp?.withZekkenName ?? zekken)}</span>
                        <span style={{ color: "#9ca3af", fontSize: "2vh", padding: "0 1vw" }}>vs</span>
                        <span style={{ color: "#b91c1c", fontWeight: 600 }}>{sideLabel(next.sideA, next._comp?.withZekkenName ?? zekken)}</span>
                    </span>
                </div>
            )}
            {window.SponsorStrip && <window.SponsorStrip sponsors={tournament && tournament.sponsors} variant="tv" />}
        </div>
    );
}

// <TvDisplay court="A"> — fullscreen per-court board.
//
// Implements T061 (visual treatment), T062 (auto-promote first scheduled
// when no running match + "all completed" / "no matches" empty states), and
// T063 (SSE reconnect indicator). Reads the data model that lives on the
// `competitions` prop (an array of normalised competitions; each carries
// poolMatches / bracket / withZekkenName) and the `tournament` prop (for
// venue branding / court labels).
//
// Auto-promote semantics (T062 / FR-011 scenario 3): if there's no running
// match on the court, the first scheduled match takes over the "current"
// slot, labelled UP NEXT instead of NOW so spectators understand it
// hasn't actually started. The queue beneath shifts up by one so we
// don't double-render the promoted match.
//
// Empty states (T062 / FR-011 scenarios 4–5):
//   - All matches completed → "All matches completed on Shiaijo {court}"
//   - Nothing has ever been scheduled here → "No matches scheduled"
// These are mutually exclusive: completed > 0 AND no running/scheduled →
// "completed"; otherwise zero matches at all → "nothing".
//
// Reconnect indicator (T063 / FR-011 scenario 4): the `connected` prop
// (defaults to true) is wired from app.jsx which owns the SSE
// EventSource. When it flips false we render a small amber pill so the
// venue knows the screen has gone stale; the rest of the layout stays
// put so reconnect doesn't flash the layout.
function TvDisplay({ court, tournament, competitions, withZekkenName, connected = true }) {
    const running = useMD(() => findRunningOnCourt(competitions, court), [competitions, court]);
    const upcoming = useMD(() => findUpcomingOnCourt(competitions, court, running ? 2 : 3), [competitions, court, running]);
    const counts = useMD(() => countCourtMatches(competitions, court), [competitions, court]);

    if (!competitions || competitions.length === 0) {
        return <div className="tvd tvd--empty" style={{
            position: 'fixed', inset: 0,
            background: '#0b0d12', color: '#fff',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: '4vh', opacity: 0.6,
        }}>Loading…</div>;
    }

    // Auto-promote the first scheduled match when no running match (T062).
    // promoted = the match we'll show in the "current" slot.
    // queueMatches = matches we'll show in the queue list beneath.
    // When promoting, drop the first scheduled from the queue to avoid
    // double-rendering the same card.
    let promoted = null;
    let queueMatches = upcoming;
    let promotedKind = null; // "running" | "upnext" | null
    if (running) {
        promoted = { kind: "running", match: running.match, competition: running.competition, isBracket: running.isBracket, roundIndex: running.roundIndex, totalRounds: running.totalRounds };
        promotedKind = "running";
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
    // "no matches" so a finished court reads clearly. counts.running === 0
    // is already guaranteed when promoted is null, but check it
    // explicitly for symmetry.
    const allCompleted = !promoted && counts.running === 0 && counts.scheduled === 0 && counts.completed > 0;
    const noMatches = !promoted && counts.completed === 0;

    const zekken = withZekkenName !== undefined
        ? withZekkenName
        : !!(promoted && promoted.competition && promoted.competition.withZekkenName);

    // mp-13y: team match detection for the running promoted slot.
    // competition.kind === "team" OR competition.teamSize > 0.
    const isTeamMatch = !!(promoted && promoted.competition &&
        (promoted.competition.kind === "team" || (promoted.competition.teamSize || 0) > 0));
    const teamSize = (promoted && promoted.competition && promoted.competition.teamSize) || 0;

    // mp-13y: fetch lineups for the running team match. useTeamLineups
    // degrades gracefully (returns null/null) when the promoted slot is
    // not a team match or when window.API is unavailable.
    const { lineupA, lineupB } = useTeamLineups(
        isTeamMatch && promoted && promoted.match ? promoted.match : null,
        isTeamMatch && promoted ? promoted.competition : null,
        promoted ? promoted.roundIndex : undefined
    );

    // mp-13y: DH (Daihyosen) row gating — shown when:
    //   1. All regular bouts are complete (every sub has ippons, hantei, or draw).
    //   2. IV (Individual Victories) are tied.
    //   3. PW (Points Won) are also tied.
    //   4. It is a knockout phase (not a pool match).
    // The DH sub-result (position === -1) may or may not exist yet; when absent,
    // TeamScoreboard renders a "Daihyosen pending" placeholder.
    const subResults = (promoted && promoted.match && promoted.match.subResults) || [];
    const isKnockoutPhase = !!(promoted && promoted.isBracket) ||
        !!(promoted && promoted.match && promoted.match.phase === "bracket");
    // Extract stable string primitives from promoted.match.sideA/B so the
    // useMemo below can dep on values rather than the promoted object literal
    // (which is recreated on every render, defeating memoisation).
    const promotedSideA = promoted?.match?.sideA?.name || (typeof promoted?.match?.sideA === "string" ? promoted.match.sideA : "");
    const promotedSideB = promoted?.match?.sideB?.name || (typeof promoted?.match?.sideB === "string" ? promoted.match.sideB : "");
    const showDH = useMD(() => {
        if (!isTeamMatch || !isKnockoutPhase) return false;
        const regularSubs = subResults.filter(s => s.position !== -1);
        if (regularSubs.length === 0) return false;
        const allDone = regularSubs.every(s => {
            const aIp = (s.ipponsA || []).filter(x => x && x !== "•");
            const bIp = (s.ipponsB || []).filter(x => x && x !== "•");
            // Mirror the shared scoreboard's "scored" test: a bout can also be
            // decided with no ippon letters — fusensho/kiken (winner + decision),
            // a hansoku award, or an explicit winner. Without these, a tied
            // knockout closed by forfeit would never satisfy allDone and the
            // Daihyosen row would be suppressed forever.
            return aIp.length > 0 || bIp.length > 0 || s.decidedByHantei ||
                !!s.winner || (typeof s.decision === "string" && s.decision !== "") ||
                s.hansokuA > 0 || s.hansokuB > 0 ||
                (typeof window.isHikiwake === "function" &&
                    (window.isHikiwake(s.score?.type) || window.isHikiwake(s.decision)));
        });
        if (!allDone) return false;
        // The match is tied (→ show DH) when IV and PW are level per side.
        // teamIVPW already prefers an explicit `sub.winner` (which the server
        // guarantees equals sideA/sideB), so a hantei-decided 0-0 bout is
        // counted as an IV for its winner there — no extra hantei loop needed.
        const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults, promotedSideA, promotedSideB);
        return ivShiro === ivAka && pwShiro === pwAka;
    }, [subResults, isTeamMatch, isKnockoutPhase, promotedSideA, promotedSideB]);

    // White scoreboard for any promoted match.
    // Team → TvWhiteBoard (IV/PW summary + bout grid). Individual → grouped
    // board listing every match in the same POOL (pool phase) or ROUND
    // (knockout), bottom-anchored with the current match at the bottom.
    if (promoted) {
        if (isTeamMatch) {
            return <TvWhiteBoard
                tournament={tournament} court={court} connected={connected}
                promoted={promoted} promotedKind={promotedKind} isTeamMatch={isTeamMatch}
                subResults={subResults} lineupA={lineupA} lineupB={lineupB} teamSize={teamSize}
                showDH={showDH} queueMatches={queueMatches} zekken={zekken}
            />;
        }
        return <TvIndividualBoard
            tournament={tournament} court={court} connected={connected}
            promoted={promoted} queueMatches={queueMatches} zekken={zekken}
        />;
    }

    // ─── Empty-state redesign (mp-s99q) ──────────────────────────────────────
    // No promoted match → white board with high-contrast empty state.
    // Three sub-states: allCompleted / noMatches / between-matches.
    // Headline uses var(--ink-1) (~10:1 on white) instead of the prior
    // #9ca3af (2.5:1) which failed WCAG AA — critical for wall screens in
    // bright halls.
    // The completed check-badge colours mirror the playoffs/completed status
    // palette used across the app: #ecfdf5 bg / #065f46 ink / #a7f3d0 border.
    // Active-courts wayfinding strip shows sibling courts with running or
    // scheduled matches so operators can redirect spectators.
    // ─────────────────────────────────────────────────────────────────────────
    const qrUrl = typeof window !== 'undefined' ? window.location.origin + '/viewer' : '';
    const otherCourts = useMD(
        () => (tournament?.courts || []).filter(c => {
            if (c === court) return false;
            const cts = countCourtMatches(competitions, c);
            return cts.running + cts.scheduled > 0;
        }),
        [tournament, competitions, court]
    );

    return (
        <div className="tvd tvd--white" data-testid="tv-display-root" style={{
            position: "fixed", inset: 0, background: "#ffffff", color: "#111",
            display: "flex", flexDirection: "column", padding: "4vh 5vw",
        }}>
            {/* Court header + black rule */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", borderBottom: "3px solid #111", paddingBottom: "1.4vh", marginBottom: "2.4vh", fontSize: "2.6vh", fontWeight: 700, letterSpacing: "0.08em" }}>
                <div>{tournament?.name ? tournament.name + " · " : ""}SHIAIJO {court}</div>
                {!connected && (
                    <span data-testid="display-reconnect" role="status" aria-label="Reconnecting"
                        style={{ display: "inline-flex", alignItems: "center", gap: "0.6vw", background: "#fef3c7", color: "#b45309", padding: "0.4vh 1vw", borderRadius: "0.4vw", fontSize: "1.6vh", fontWeight: 700 }}>
                        <span style={{ width: "1.2vh", height: "1.2vh", borderRadius: "50%", background: "#b45309", display: "inline-block", animation: "pulse 1.4s ease-in-out infinite" }} />
                        RECONNECTING
                    </span>
                )}
            </div>

            {/* Centre block — anchored slightly above dead-centre */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", paddingBottom: "8vh", gap: "3vh" }}>

                {/* a) Status icon + headline */}
                <div data-testid={allCompleted ? "display-all-completed" : (noMatches ? "display-no-matches" : "display-between-matches")}
                    style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "2vh", textAlign: "center", maxWidth: "60vw" }}>
                    {allCompleted && (
                        /* Drawn SVG checkmark — NOT the raw ✓ Unicode glyph */
                        <div style={{
                            width: "8vh", height: "8vh", borderRadius: "50%",
                            /* completed-status palette: mirrors playoffs/completed used across the app */
                            background: "#ecfdf5", border: "2px solid #a7f3d0",
                            display: "flex", alignItems: "center", justifyContent: "center",
                            flexShrink: 0,
                        }}>
                            <svg viewBox="0 0 24 24" width="4.5vh" height="4.5vh" fill="none"
                                stroke="#065f46" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
                                aria-hidden="true">
                                <polyline points="20 6 9 17 4 12" />
                            </svg>
                        </div>
                    )}
                    <div style={{ fontSize: "5vh", fontWeight: 700, color: "var(--ink-1)", textWrap: "balance", lineHeight: 1.15 }}>
                        {emptyStateHeadline(allCompleted, noMatches)}
                    </div>
                </div>

                {/* b) QR affordance — "Scan for results" */}
                {qrUrl && (
                    <div style={{ display: "inline-flex", alignItems: "center", gap: "1.5vw", marginTop: "1vh" }}>
                        <StreamingQR url={qrUrl} />
                        <span style={{ fontSize: "2vh", color: "var(--ink-2)", fontWeight: 500 }}>Scan for results</span>
                    </div>
                )}

                {/* c) IN PROGRESS wayfinding strip — other active courts */}
                {otherCourts.length > 0 && (
                    <div data-testid="tvd-active-courts" style={{ display: "inline-flex", alignItems: "center", gap: "1.2vw", flexWrap: "wrap", justifyContent: "center", marginTop: "1vh" }}>
                        <span style={{ fontSize: "1.6vh", letterSpacing: "0.12em", color: "var(--ink-3)", fontWeight: 700 }}>IN PROGRESS</span>
                        {otherCourts.map(c => (
                            <span key={c} data-court={c} style={{
                                display: "inline-flex", alignItems: "center", gap: "0.5vw",
                                background: "var(--accent-soft)", color: "var(--accent)",
                                borderRadius: "0.6vw", padding: "0.5vh 1.2vw",
                                fontWeight: 700, fontSize: "1.8vh",
                            }}>
                                {/* Static navy dot — wayfinding only, NOT pulsing */}
                                <span style={{ width: "0.9vh", height: "0.9vh", borderRadius: "50%", background: "var(--accent)", display: "inline-block", flexShrink: 0 }} />
                                Shiaijo {c}
                            </span>
                        ))}
                    </div>
                )}
            </div>

            {window.SponsorStrip && <window.SponsorStrip sponsors={tournament && tournament.sponsors} variant="tv" />}
        </div>
    );
}

export { TvDisplay, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, emptyStateHeadline };
