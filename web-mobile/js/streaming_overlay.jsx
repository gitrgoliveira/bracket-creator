// streaming_overlay.jsx: OBS/vMix streaming overlay (StreamingOverlay).
// Transparent-background lower-third for broadcast integrations. T066, T067, mp-13y.

import { findRunningOnCourt, sideLabel, TermD, StreamingQR } from './display_helpers.jsx';
import { useTeamLineups, teamIVPW } from './match_scoreboard.jsx';
import { pickFromLineup } from './lineup_resolver.jsx';

const { useEffect: useED, useMemo: useMD } = React;

// overlayPositionLabel: FIK position label for the current bout, used as the
// fallback when no per-match lineup pins a player name. Mirrors
// positionLabelFor in admin_scoring_modal.jsx (module-local copy; display.jsx
// is a separate ES module). Senpo/Jiho/... for 5-person teams, "Daihyosen"
// for the rep bout (position === -1), else the bare bout number ("1", "2", …).
// NEVER the team name.
const OVL_POS_LABELS_5 = ["Senpo", "Jiho", "Chuken", "Fukusho", "Taisho"];
function overlayPositionLabel(teamSize, index, sub) {
    if (sub && sub.position === -1) return "Daihyosen";
    if (sub && typeof sub.position === "string" && sub.position.length > 0 && /[a-z]/i.test(sub.position)) return sub.position;
    // FIK named positions exist ONLY for 5-person teams. For 3/7/11/13/15 and
    // kachinuki the app uses numeric positions "1".."N" everywhere
    // (domain.PositionNumbered, admin_lineup positionsForSize): so the
    // overlay falls back to the bare bout number, which scales to any size.
    if (teamSize === 5 && index >= 0 && index < 5) return OVL_POS_LABELS_5[index];
    return String(index + 1);
}

// findCurrentBoutIndex: returns the 0-based index of the bout that is
// currently being fought (the first UNSCORED regular bout, position != -1).
// Falls back to 0 on an empty subResults. Used by StreamingOverlay to pick
// which bout names and score to show.
function findCurrentBoutIndex(subResults) {
    if (!subResults || !subResults.length) return 0;
    // A bout is scored if it has real ippons, a hansoku, a hantei decision, an
    // explicit winner/decision (quick-score and forfeit-style outcomes set
    // these without ippon letters), or a hikiwake. This aligns with
    // TeamScoreboard's isScored logic. When all regular bouts are complete,
    // returns regular.length (= subResults.length excluding any DH row at
    // position -1): the caller treats that as the "DH/done" signal.
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

// <StreamingOverlay court="A" position="bottom">: transparent-background
// lower-third for OBS / vMix browser sources (T066, T067).
//
// CRITICAL: the page background MUST be transparent so the kendo broadcast
// video shows through. We set this via useEffect on mount and restore on
// unmount so navigating back to a normal view doesn't leave the body
// transparent.
//
// T067: keep the overlay DOM mounted regardless of running state so the
// opacity transition can run. Toggle opacity + pointerEvents only.
//
// mp-13y: team match lower-third. for team matches the centre holds a
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

    const running = useMD(() => findRunningOnCourt(competitions, court), [competitions, court]);
    const hasRunning = !!running;
    const comp = running?.competition;
    const zekken = !!(comp && comp.withZekkenName);

    // mp-13y: team match detection.
    const isTeamMatch = !!(comp && (comp.kind === "team" || (comp.teamSize || 0) > 0));
    const teamSizeOvl = (comp && comp.teamSize) || 0;

    // mp-13y: per-match lineups for team overlay.
    const { lineupA: ovlLineupA, lineupB: ovlLineupB } = useTeamLineups(
        isTeamMatch && hasRunning ? running.match : null,
        isTeamMatch && hasRunning ? comp : null,
        hasRunning ? running.roundIndex : undefined
    );

    // Current bout for the overlay (last active sub-result, index 0 fallback).
    const ovlSubResults = (hasRunning && running.match.subResults) || [];
    const currentBoutIdx = useMD(() => findCurrentBoutIndex(ovlSubResults), [ovlSubResults]);
    const currentSub = ovlSubResults[currentBoutIdx] || null;

    // mp-13y #10: running IV/PW aggregate per side. teamIVPW excludes the
    // Daihyosen (position -1) row. sideB = shiro, sideA = aka.
    const ovlSideA = hasRunning ? (running.match.sideA?.name || (typeof running.match.sideA === "string" ? running.match.sideA : "")) : "";
    const ovlSideB = hasRunning ? (running.match.sideB?.name || (typeof running.match.sideB === "string" ? running.match.sideB : "")) : "";
    const ovlIV = isTeamMatch ? teamIVPW(ovlSubResults, ovlSideA, ovlSideB) : { ivShiro: 0, ivAka: 0, pwShiro: 0, pwAka: 0 };

    // DH-pending: all regular bouts are scored, the match is tied (equal IV
    // and PW), but no DH sub-result has been created yet. In that case
    // findCurrentBoutIndex returns subResults.length and currentSub is null;
    // the overlay would otherwise read blank. Show "Daihyosen" on both sides
    // so spectators know the rep bout is about to start.
    const regularSubsOvl = ovlSubResults.filter(s => s.position !== -1);
    const dhPending = isTeamMatch && !currentSub && regularSubsOvl.length > 0
        && ovlIV.ivShiro === ovlIV.ivAka && ovlIV.pwShiro === ovlIV.pwAka
        && !ovlSubResults.some(s => s.position === -1);

    // Competitor for the current bout: pinned lineup name, else the per-bout
    // competitor stored on the sub (kachinuki), else the FIK POSITION label
    // (Senpo/Jiho/...), else "Daihyosen" when the rep bout is pending; never
    // the team name (that flanks the QR above).
    const subSideName = (v) => (v && v.name) || (typeof v === "string" ? v : "");
    const boutPosLabel = currentSub ? overlayPositionLabel(teamSizeOvl, currentBoutIdx, currentSub) : (dhPending ? 'Daihyosen' : '');
    const boutShiroName = isTeamMatch && currentSub
        ? (pickFromLineup(ovlLineupB, currentBoutIdx, teamSizeOvl) || subSideName(currentSub.sideB) || boutPosLabel)
        : (dhPending ? boutPosLabel : '');
    const boutAkaName = isTeamMatch && currentSub
        ? (pickFromLineup(ovlLineupA, currentBoutIdx, teamSizeOvl) || subSideName(currentSub.sideA) || boutPosLabel)
        : (dhPending ? boutPosLabel : '');

    // Bout score for the current sub: ippon letters, "-" (not "0") for an
    // empty side so a kendo score never reads "M – 0".
    const boutIpponsB = currentSub ? ((currentSub.ipponsB || []).filter(x => x && x !== "•").join('') || '-') : '-';
    const boutIpponsA = currentSub ? ((currentSub.ipponsA || []).filter(x => x && x !== "•").join('') || '-') : '-';

    // Team names (outer flanks of QR in team mode).
    const shiroTeamName = hasRunning ? sideLabel(running.match.sideB, zekken) : '';
    const akaTeamName = hasRunning ? sideLabel(running.match.sideA, zekken) : '';

    // Individual match data (non-team).
    const shiro = hasRunning && !isTeamMatch ? sideLabel(running.match.sideB, zekken) : '';
    const aka = hasRunning && !isTeamMatch ? sideLabel(running.match.sideA, zekken) : '';
    const ipponsB = hasRunning && !isTeamMatch ? ((running.match.ipponsB || []).filter(x => x && x !== "•").join('') || '0') : '';
    const ipponsA = hasRunning && !isTeamMatch ? ((running.match.ipponsA || []).filter(x => x && x !== "•").join('') || '0') : '';
    // T097: Kiken/Fus./DH/(E) suffix on the OBS lower-third. Computed off
    // the running match so it disappears the moment the overlay fades out.
    const decSfx = hasRunning && !isTeamMatch && window.decisionSuffix ? window.decisionSuffix(running.match) : '';
    const compName = comp?.name || '';

    // QR target URL: the tournament viewer home page (NOT a per-match deep
    // link; viewers land on the schedule and navigate themselves). Only
    // emitted on team matches so the lower-third doesn't crowd the
    // individual-match layout. Uses the current page origin so the QR
    // works on the local network.
    const qrUrl = hasRunning && isTeamMatch
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
            // T067: fade in/out; keep the DOM mounted so the transition runs.
            // 300ms sits in the middle of the A-6 200–400ms band.
            opacity: hasRunning ? 1 : 0,
            transition: 'opacity 300ms ease-in-out',
            pointerEvents: hasRunning ? 'auto' : 'none',
            // The overlay can be aria-hidden when not visible so screen
            // readers don't announce stale match data during the fade.
            visibility: hasRunning ? 'visible' : 'hidden',
            transitionProperty: 'opacity, visibility',
            transitionDelay: hasRunning ? '0s, 0s' : '0s, 300ms',
        }} aria-hidden={!hasRunning}>

            {isTeamMatch ? (
                /* mp-13y: team match lower-third.
                   Layout: [Shiro team/bout] [QR] [Aka …]
                   Per side, TWO rows:
                     • Team row: the TEAM NAME + the running IV/PW aggregate
                       (the team-level result belongs on the team row).
                     • Bout row: the current bout's competitor/position + that
                       competitor's ippon score, so the result lines up with the
                       individual bout it belongs to. */
                <>
                    {/* Shiro: left side (white) */}
                    <div style={{ flex: 1, minWidth: 0 }} data-testid="overlay-shiro">
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '1vw' }}>
                            <span style={{ fontWeight: 700, fontSize: '2.6vh', color: '#ffffff', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{shiroTeamName}</span>
                            <span data-testid="overlay-shiro-ivpw" style={{ flexShrink: 0, fontSize: '1.8vh', color: '#ffffff', fontFamily: 'var(--font-mono, monospace)', fontWeight: 700 }}>IV {ovlIV.ivShiro} · PW {ovlIV.pwShiro}</span>
                        </div>
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '1vw', marginTop: '0.4vh', opacity: 0.85 }}>
                            <span style={{ fontSize: '1.9vh', color: '#ffffff', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{boutShiroName}</span>
                            <span data-testid="overlay-shiro-bout" style={{ flexShrink: 0, fontSize: '2vh', color: '#ffffff', fontFamily: 'var(--font-mono, monospace)', fontWeight: 700 }}>{boutIpponsB}</span>
                        </div>
                    </div>

                    {/* QR centre */}
                    <div style={{ flexShrink: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.6vh', padding: '0 2vw' }}>
                        {qrUrl && <StreamingQR url={qrUrl} label="scan for results" />}
                    </div>

                    {/* Aka: right side (red) */}
                    <div style={{ flex: 1, minWidth: 0, textAlign: 'right' }} data-testid="overlay-aka">
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '1vw' }}>
                            <span data-testid="overlay-aka-ivpw" style={{ flexShrink: 0, fontSize: '1.8vh', color: '#fda4af', fontFamily: 'var(--font-mono, monospace)', fontWeight: 700 }}>PW {ovlIV.pwAka} · IV {ovlIV.ivAka}</span>
                            <span style={{ fontWeight: 700, fontSize: '2.6vh', color: '#fda4af', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{akaTeamName}</span>
                        </div>
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '1vw', marginTop: '0.4vh', opacity: 0.85 }}>
                            <span data-testid="overlay-aka-bout" style={{ flexShrink: 0, fontSize: '2vh', color: '#fda4af', fontFamily: 'var(--font-mono, monospace)', fontWeight: 700 }}>{boutIpponsA}</span>
                            <span style={{ fontSize: '1.9vh', color: '#fda4af', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{boutAkaName}</span>
                        </div>
                    </div>
                </>
            ) : (
                /* Individual match lower-third: unchanged from original. */
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

export { StreamingOverlay, StreamingQR, overlayPositionLabel, findCurrentBoutIndex };
