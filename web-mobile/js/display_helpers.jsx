// display_helpers.jsx — shared data/label layer for display surfaces.
// Pure functions, the TermD wrapper, and StreamingQR used by TvDisplay,
// LobbyDisplay, and StreamingOverlay. No module-level side-effects.

import { withNumber } from './match_scoreboard.jsx';

// NOTE: this module is the shared *leaf* in the display graph — presentation
// modules (scoreboard, lobby, streaming) import from here, not vice versa.
// Within this file, hooks are NOT destructured off `React` at module-eval time;
// StreamingQR and TermD call React.* lazily at render time instead.
// The import of `withNumber` from `match_scoreboard.jsx` is a transitive
// dependency on `React` being global, which every display surface already
// requires before mounting — this is not a new constraint introduced here.

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

// sideLabel — thin delegate to the shared `withNumber` helper from
// match_scoreboard.jsx so display.jsx and the OBS overlay agree on what to
// render with no risk of the two implementations drifting. Kept as a named
// export for the TV/lobby/overlay call sites and for test imports
// (display_white_board.test.jsx asserts on `sideLabel`).
function sideLabel(side, withZekkenName) {
    return withNumber(side, withZekkenName);
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
// Returns null when no running match. Used by TvDisplay and StreamingOverlay.
function findRunningOnCourt(competitions, court) {
    if (!competitions || !court) return null;
    for (const c of competitions) {
        if (!c) continue;
        const poolMatches = c.poolMatches || [];
        for (const m of poolMatches) {
            if ((m.court || "") !== court) continue;
            if (m.status === "running" && bracketSidesReady(m)) {
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
// "2 before yours" labels under the running match in TvDisplay.
function findUpcomingOnCourt(competitions, court, limit = 2) {
    const out = [];
    if (!competitions || !court) return out;
    for (const c of competitions) {
        if (!c) continue;
        const poolMatches = c.poolMatches || [];
        for (const m of poolMatches) {
            if ((m.court || "") !== court) continue;
            if (m.status !== "scheduled") continue;
            if (!bracketSidesReady(m)) continue;
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
// states: "All matches completed" requires completed > 0 + no running and
// no scheduled; "No matches scheduled" requires zero matches in total.
// Counts only the matches that have two real sides (not bye / TBD /
// "Winner of rX-mY" placeholders) so a half-resolved bracket doesn't
// flip the empty-state heuristic prematurely.
function countCourtMatches(competitions, court) {
    let running = 0, scheduled = 0, completed = 0;
    if (!competitions || !court) return { running, scheduled, completed };
    // Count only matches with two real sides — reject "Winner of rX-mY" feeders
    // AND "Pool A-1st" pool-origin leaves — so a half-resolved bracket doesn't
    // inflate the "scheduled" count and prevent the "All matches completed"
    // empty state from firing. (bracketSidesReady mirrors admin_helpers.hasBothSides
    // without a module-eval window dependency.)
    const hasBoth = bracketSidesReady;
    const bump = (m) => {
        if (!hasBoth(m)) return;
        if (m.status === "running") running++;
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
    return { running, scheduled, completed };
}

// Active courts = courts with at least one running or scheduled match.
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

// poolNameOf — derive the pool name from a pool-match id shaped "<Pool>-<idx>"
// (e.g. "Pool A-0" → "Pool A"). Returns "" when the id isn't pool-shaped.
function poolNameOf(id) {
    if (typeof id !== "string") return "";
    const cut = id.lastIndexOf("-");
    return (cut > 0 && /^\d+$/.test(id.slice(cut + 1))) ? id.slice(0, cut) : "";
}

// StreamingQR — minimal canvas QR code for the streaming overlay and TV
// empty-state. Renders using renderQR from qr.jsx when available via
// window.renderQR. Falls back gracefully (blank canvas) if QR rendering
// is unavailable. Moved here from streaming_overlay.jsx (mp-s99q) so both
// StreamingOverlay and TvDisplay can share the same component.
function StreamingQR({ url, label }) {
    // Use a stable object as the ref container so the useEffect dependency
    // doesn't change on every render. The canvas element is stored in
    // canvasHolder.el after the JSX ref callback fires.
    const canvasHolder = React.useMemo(() => ({ el: null }), []);

    React.useEffect(() => {
        const canvas = canvasHolder.el;
        if (!canvas || !url) return undefined;
        // renderQR may be available on window if qr.jsx has been imported
        // by another module (e.g. admin_shell.jsx exposes it). If not, skip.
        const fn = (typeof window !== 'undefined' && window.renderQR) || null;
        if (!fn) return undefined;
        try { fn(canvas, url, { moduleSize: 2, quietZone: 2 }); } catch (_e) { /* skip */ }
        return undefined;
    }, [url]);

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.4vh' }}>
            <canvas
                data-testid="qr-canvas"
                ref={(el) => { canvasHolder.el = el; }}
                style={{ display: 'block', imageRendering: 'pixelated', borderRadius: 4 }}
            />
            {label && <div style={{ fontSize: '1.2vh', opacity: 0.75, textAlign: 'center' }}>{label}</div>}
        </div>
    );
}

export {
    DISPLAY_PLACEHOLDER_RE,
    bracketSidesReady,
    findRunningOnCourt,
    findUpcomingOnCourt,
    countCourtMatches,
    findActiveCourts,
    queueLabel,
    queueLabelCompact,
    phaseLabel,
    poolNameOf,
    sideLabel,
    TermD,
    StreamingQR,
};
