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

import { pickFromLineup } from './lineup_resolver.jsx';
import { TeamScoreboard, IndividualScore, useTeamLineups, teamIVPW } from './match_scoreboard.jsx';

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

// Reject a bracket side that is still a placeholder rather than a resolved
// competitor: a "Winner of rX-mY" feeder OR a pool-origin "Pool A-1st" leaf.
// The latter matters since mp-turx: a mixed comp's bracket.preview strip lifts
// the moment ANY single pool resolves, so the aggregate /api/viewer payload then
// exposes a partially-resolved bracket whose un-finished pools are still
// placeholders. Without this filter the TV/lobby surfaces render phantom
// "Pool C-1st vs Pool D-1st" bouts and mark idle courts active. Mirrors
// admin_helpers.hasBothSides without a module-eval window dependency.
const DISPLAY_PLACEHOLDER_RE = /^(Winner of r\d+-m\d+|Pool .+-\d+(st|nd|rd|th))$/;
function bracketSidesReady(m) {
    if (!m || !m.sideA || !m.sideB) return false;
    const aName = typeof m.sideA === "string" ? m.sideA : (m.sideA.name || "");
    const bName = typeof m.sideB === "string" ? m.sideB : (m.sideB.name || "");
    if (!aName || !bName) return false;
    if (DISPLAY_PLACEHOLDER_RE.test(aName) || DISPLAY_PLACEHOLDER_RE.test(bName)) return false;
    return true;
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
                if (m.status === "running" && bracketSidesReady(m)) {
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
            if (!bracketSidesReady(m)) return; // skip "Pool X-Nth" / "Winner of …" placeholders
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
    // Count only matches with two real sides — reject "Winner of rX-mY" feeders
    // AND "Pool A-1st" pool-origin leaves — so a half-resolved bracket doesn't
    // inflate the "scheduled" count and prevent the "All matches completed"
    // empty state from firing. (bracketSidesReady mirrors admin_helpers.hasBothSides
    // without a module-eval window dependency.)
    const hasBoth = bracketSidesReady;
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
                if (!bracketSidesReady(m)) continue; // a placeholder bout doesn't make a court "active"
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
    // Pool matches reach the display feed with round === -1 (a sentinel, not a
    // real round) and no poolName, but their id is shaped "<PoolName>-<index>".
    // Derive the pool name from the id rather than rendering the bare "-1".
    if (typeof m.round === "number" && m.round < 0) {
        const id = typeof m.id === "string" ? m.id : "";
        const cut = id.lastIndexOf("-");
        if (cut > 0 && /^\d+$/.test(id.slice(cut + 1))) return id.slice(0, cut);
        return "";
    }
    // Render a numeric round explicitly so a 0-based round index (round === 0)
    // is not swallowed by the falsy-`||` fallback into an empty label.
    if (typeof m.round === "number") return String(m.round);
    return m.round || "";
}

// overlayPositionLabel — FIK position label for the current bout, used as the
// fallback when no per-match lineup pins a player name. Mirrors
// positionLabelFor in admin_scoring_modal.jsx (module-local copy; display.jsx
// is a separate ES module). Senpo/Jiho/... for 5-person teams, "Daihyosen"
// for the rep bout (position === -1), else "Match N". NEVER the team name.
const OVL_POS_LABELS_5 = ["Senpo", "Jiho", "Chuken", "Fukusho", "Taisho"];
function overlayPositionLabel(teamSize, index, sub) {
    if (sub && sub.position === -1) return "Daihyosen";
    if (sub && typeof sub.position === "string" && sub.position.length > 0 && /[a-z]/i.test(sub.position)) return sub.position;
    // FIK named positions exist ONLY for 5-person teams. For 3/7/11/13/15 and
    // kachinuki the app uses numeric positions "1".."N" everywhere
    // (domain.PositionNumbered, admin_lineup positionsForSize) — so the
    // overlay falls back to the bare bout number, which scales to any size.
    if (teamSize === 5 && index >= 0 && index < 5) return OVL_POS_LABELS_5[index];
    return String(index + 1);
}

// findCurrentBoutIndex — returns the 0-based index of the bout that is
// currently being fought (last non-empty subResult). Falls back to 0.
// Used by StreamingOverlay to pick which bout names and score to show.
function findCurrentBoutIndex(subResults) {
    if (!subResults || !subResults.length) return 0;
    // Return the first UNSCORED regular bout — the bout currently in progress.
    // A bout is scored if it has real ippons, a hansoku, a hantei decision, an
    // explicit winner/decision (quick-score and forfeit-style outcomes set
    // these without ippon letters), or a hikiwake. This aligns with
    // TeamScoreboard's isScored logic. When all regular bouts are complete,
    // returns subResults.length (signals DH/done).
    const regular = subResults.filter(s => s.position !== -1);
    for (let i = 0; i < regular.length; i++) {
        const s = regular[i];
        const hasIppon = (s.ipponsA && s.ipponsA.some(x => x && x !== "•")) ||
            (s.ipponsB && s.ipponsB.some(x => x && x !== "•"));
        const hasFoul = s.hansokuA || s.hansokuB;
        const hasHantei = s.decidedByHantei;
        const hasOutcome = !!s.winner || (typeof s.decision === "string" && s.decision !== "");
        const isDraw = typeof window.isHikiwake === "function" &&
            (window.isHikiwake(s.score?.type) || window.isHikiwake(s.decision));
        if (!hasIppon && !hasFoul && !hasHantei && !hasOutcome && !isDraw) return i;
    }
    return regular.length;
}


// TvWhiteTeamBoard — mp-13y: white scoreboard for a live TEAM match
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
                    <TeamScoreboard subResults={subResults} lineupA={lineupA} lineupB={lineupB}
                        teamSize={teamSize} showDH={showDH} variant="tv" />
                </div>
            ) : (
                <div style={{ flex: 1, display: "flex", alignItems: "flex-start", justifyContent: "center", paddingTop: "2vh" }}>
                    <IndividualScore match={promoted.match} variant="tv" />
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

    // mp-13y: team match detection for the live promoted slot.
    // competition.kind === "team" OR competition.teamSize > 0.
    const isTeamMatch = !!(promoted && promoted.competition &&
        (promoted.competition.kind === "team" || (promoted.competition.teamSize || 0) > 0));
    const teamSize = (promoted && promoted.competition && promoted.competition.teamSize) || 0;

    // mp-13y: fetch lineups for the live team match. useTeamLineups
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
        const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults);
        return ivShiro === ivAka && pwShiro === pwAka;
    }, [subResults, isTeamMatch, isKnockoutPhase]);

    // White scoreboard for any promoted match — team (bout grid) or individual
    // (ippon score), live or up-next.
    if (promoted) {
        return <TvWhiteBoard
            tournament={tournament} court={court} connected={connected}
            promoted={promoted} promotedKind={promotedKind} isTeamMatch={isTeamMatch}
            subResults={subResults} lineupA={lineupA} lineupB={lineupB} teamSize={teamSize}
            showDH={showDH} queueMatches={queueMatches} zekken={zekken}
        />;
    }

    // No promoted match → white empty state (matches the white board chrome).
    return (
        <div className="tvd tvd--white" data-testid="tv-display-root" style={{
            position: "fixed", inset: 0, background: "#ffffff", color: "#111",
            display: "flex", flexDirection: "column", padding: "4vh 5vw",
        }}>
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
            <div data-testid={allCompleted ? "display-all-completed" : "display-no-matches"}
                style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", flexDirection: "column", gap: "2vh", fontSize: "5vh", color: "#9ca3af", textAlign: "center", padding: "0 5vw" }}>
                {allCompleted ? (
                    <>
                        <span style={{ fontSize: "6vh", color: "#16a34a" }}>✓</span>
                        <span>All matches completed on <TermD name="shiaijo">Shiaijo</TermD> {court}</span>
                    </>
                ) : noMatches ? (
                    <span>No matches scheduled</span>
                ) : (
                    <span>No match in progress on <TermD name="shiaijo">Shiaijo</TermD> {court}</span>
                )}
            </div>
            {window.SponsorStrip && <window.SponsorStrip sponsors={tournament && tournament.sponsors} variant="tv" />}
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
// mp-13y: light palette (white redesign by analogy with the per-court board).
const LOBBY_COLORS = {
    bg:         '#ffffff',
    ink:        '#111111',
    surface:    'rgba(0,0,0,0.03)',
    inkDim:     'rgba(0,0,0,0.55)',
    inkMuted:   'rgba(0,0,0,0.35)',
    line:       'rgba(0,0,0,0.10)',
    lineStrong: 'rgba(0,0,0,0.20)',
    nowBg:      'rgba(0,0,0,0.04)',
    nowBorder:  'rgba(0,0,0,0.14)',
    nextBg:     '#fef3c7',
    nextBorder: 'rgba(180,83,9,0.30)',
    nextAccent: '#b45309',
    schedBg:    'rgba(0,0,0,0.02)',
    akaSoft:    '#c0392b',
    akaVivid:   '#b91c1c',
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
            <span style={{ fontFamily: 'var(--font-mono, monospace)', fontWeight: 700, fontSize: 14, color: LOBBY_COLORS.ink }}>
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
                                    const cts = countCourtMatches(competitions, cc);
                                    const remaining = cts.live + cts.scheduled;
                                    const firstSlot = courtSlots[ci] && courtSlots[ci][0];
                                    const compName = firstSlot ? (firstSlot.competition?.name || '') : '';
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

// StreamingQR — minimal canvas QR code for the streaming overlay.
// Renders using renderQR from qr.jsx when available via window.renderQR.
// Falls back gracefully (blank canvas) if QR rendering is unavailable.
function StreamingQR({ url, label }) {
    // Use a stable object as the ref container so the useEffect dependency
    // doesn't change on every render. The canvas element is stored in
    // canvasHolder.el after the JSX ref callback fires.
    const canvasHolder = useMD(() => ({ el: null }), []);

    useED(() => {
        const canvas = canvasHolder.el;
        if (!canvas || !url) return undefined;
        // renderQR may be available on window if qr.jsx has been imported
        // by another module (e.g. admin_shell.jsx exposes it). If not, skip.
        const fn = window.renderQR || null;
        if (!fn) return undefined;
        try { fn(canvas, url, { moduleSize: 2, quietZone: 2 }); } catch (_e) { /* skip */ }
        return undefined;
    }, [url]);

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.4vh' }}>
            <canvas
                data-testid="overlay-qr"
                ref={(el) => { canvasHolder.el = el; }}
                style={{ display: 'block', imageRendering: 'pixelated', borderRadius: 4 }}
            />
            {label && <div style={{ fontSize: '1.2vh', opacity: 0.75, textAlign: 'center' }}>{label}</div>}
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
//
// mp-13y: team match lower-third — for team matches the centre holds a
// QR code ("scan for results") flanked by the team names; the current
// bout's competitor names appear on the outer sides, with a running IV/PW
// aggregate per side beneath them (mp-13y #10).
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

    // mp-13y: team match detection.
    const isTeamMatch = !!(comp && (comp.kind === "team" || (comp.teamSize || 0) > 0));
    const teamSizeOvl = (comp && comp.teamSize) || 0;

    // mp-13y: per-match lineups for team overlay.
    const { lineupA: ovlLineupA, lineupB: ovlLineupB } = useTeamLineups(
        isTeamMatch && hasLive ? live.match : null,
        isTeamMatch && hasLive ? comp : null,
        hasLive ? live.roundIndex : undefined
    );

    // Current bout for the overlay (last active sub-result, index 0 fallback).
    const ovlSubResults = (hasLive && live.match.subResults) || [];
    const currentBoutIdx = useMD(() => findCurrentBoutIndex(ovlSubResults), [ovlSubResults]);
    const currentSub = ovlSubResults[currentBoutIdx] || null;

    // Competitor for the current bout: pinned lineup name, else the FIK
    // POSITION label (Senpo/Jiho/...) — never the team name (that flanks the
    // QR above). "position or name", per product direction.
    const boutPosLabel = currentSub ? overlayPositionLabel(teamSizeOvl, currentBoutIdx, currentSub) : '';
    const boutShiroName = isTeamMatch && currentSub
        ? (pickFromLineup(ovlLineupB, currentBoutIdx, teamSizeOvl) || boutPosLabel)
        : '';
    const boutAkaName = isTeamMatch && currentSub
        ? (pickFromLineup(ovlLineupA, currentBoutIdx, teamSizeOvl) || boutPosLabel)
        : '';

    // Bout score for the current sub — ippon letters, "—" (not "0") for an
    // empty side so a kendo score never reads "M – 0".
    const boutIpponsB = currentSub ? ((currentSub.ipponsB || []).filter(x => x && x !== "•").join('') || '—') : '—';
    const boutIpponsA = currentSub ? ((currentSub.ipponsA || []).filter(x => x && x !== "•").join('') || '—') : '—';

    // mp-13y #10: running IV/PW aggregate per side, so the lower-third shows
    // the team-match standing (not just the current bout). teamIVPW excludes
    // the Daihyosen (position -1) row. sideB = shiro, sideA = aka.
    const ovlIV = isTeamMatch ? teamIVPW(ovlSubResults) : { ivShiro: 0, ivAka: 0, pwShiro: 0, pwAka: 0 };

    // Team names (outer flanks of QR in team mode).
    const shiroTeamName = hasLive ? sideLabel(live.match.sideB, zekken) : '';
    const akaTeamName = hasLive ? sideLabel(live.match.sideA, zekken) : '';

    // Individual match data (non-team).
    const shiro = hasLive && !isTeamMatch ? sideLabel(live.match.sideB, zekken) : '';
    const aka = hasLive && !isTeamMatch ? sideLabel(live.match.sideA, zekken) : '';
    const ipponsB = hasLive && !isTeamMatch ? ((live.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0') : '';
    const ipponsA = hasLive && !isTeamMatch ? ((live.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0') : '';
    // T097: Kiken/Fus./DH/(E) suffix on the OBS lower-third. Computed off
    // the live match so it disappears the moment the overlay fades out.
    const decSfx = hasLive && !isTeamMatch && window.decisionSuffix ? window.decisionSuffix(live.match) : '';
    const compName = comp?.name || '';

    // QR target URL: the per-match results page for team matches.
    // Uses the current page origin so the QR works on the local network.
    const qrUrl = hasLive && isTeamMatch
        ? `${typeof window !== 'undefined' ? window.location.origin : ''}/viewer`
        : '';

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

            {isTeamMatch ? (
                /* mp-13y: team match lower-third.
                   Layout: [Shiro team/bout/IV·PW] [QR + bout score] [Aka …]
                   Each side shows the team name, the current bout competitor,
                   and a running IV/PW aggregate (mp-13y #10). */
                <>
                    {/* Shiro bout competitor — left side (white) */}
                    <div style={{ flex: 1, minWidth: 0 }} data-testid="overlay-shiro-bout">
                        <div style={{ fontSize: '1.4vh', opacity: 0.7, marginBottom: 2, color: '#ffffff' }}>{shiroTeamName}</div>
                        <div style={{ fontWeight: 700, fontSize: '2.6vh', color: '#ffffff', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{boutShiroName}</div>
                        <div data-testid="overlay-shiro-ivpw" style={{ fontSize: '1.5vh', opacity: 0.8, marginTop: '0.4vh', color: '#ffffff', fontFamily: 'var(--font-mono, monospace)' }}>IV {ovlIV.ivShiro} · PW {ovlIV.pwShiro}</div>
                    </div>

                    {/* QR + score centre */}
                    <div style={{ flexShrink: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.6vh', padding: '0 2vw' }}>
                        {qrUrl && <StreamingQR url={qrUrl} label="scan for results" />}
                        <div
                            data-testid="overlay-bout-score"
                            style={{ fontFamily: 'var(--font-mono, monospace)', fontWeight: 700, fontSize: '2.5vh' }}
                        >
                            {boutIpponsB} – {boutIpponsA}
                        </div>
                    </div>

                    {/* Aka bout competitor — right side (red) */}
                    <div style={{ flex: 1, minWidth: 0, textAlign: 'right' }} data-testid="overlay-aka-bout">
                        <div style={{ fontSize: '1.4vh', opacity: 0.85, marginBottom: 2, color: '#fda4af' }}>{akaTeamName}</div>
                        <div style={{ fontWeight: 700, fontSize: '2.6vh', color: '#fda4af', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{boutAkaName}</div>
                        <div data-testid="overlay-aka-ivpw" style={{ fontSize: '1.5vh', opacity: 0.85, marginTop: '0.4vh', color: '#fda4af', fontFamily: 'var(--font-mono, monospace)' }}>PW {ovlIV.pwAka} · IV {ovlIV.ivAka}</div>
                    </div>
                </>
            ) : (
                /* Individual match lower-third — unchanged from original. */
                <>
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
                </>
            )}

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
    // mp-13y: helpers exported for vitest.
    findCurrentBoutIndex,
    overlayPositionLabel,
    TvWhiteBoard,
};

if (typeof window !== 'undefined') {
    window.TvDisplay = TvDisplay;
    window.LobbyDisplay = LobbyDisplay;
    window.StreamingOverlay = StreamingOverlay;
    window.DisplayRoute = DisplayRoute;
    window.queueLabel = queueLabel;
    window.queueLabelCompact = queueLabelCompact;
}
