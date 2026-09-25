// display_scoreboard.jsx: per-court TV scoreboard (TvDisplay).
// Fullscreen white board shown on Shiaijo-dedicated screens.
// T061, T062, T063, mp-13y.

// TermD is gone from this import: the board's only glossary terms were the
// SHIRO/AKA headings, dropped with bc-sccl.
import { findRunningOnCourt, findUpcomingOnCourt, countCourtMatches, sideLabelParts, phaseLabel, poolNameOf, isSupplementaryBout, phaseProgressOnCourt, bracketRoundSiblings, StreamingQR } from './display_helpers.jsx';
import { NumberedName } from './numbered_name.jsx';
import { teamMatchTypeFor, DAIHYOSEN_POSITION } from './pool_ids.jsx';
import { TeamScoreboard, IndividualScore, useTeamLineups, teamIVPW, teamIVPWFrom, teamNameMark } from './match_scoreboard.jsx';
import { isBarredMatch } from './ineligible_match.jsx';
import { subBoutHasResult } from './team_default_credit.jsx';

const { useMemo: useMD } = React;

// The Shiro-vs-Aka name pair the NEXT line and the UP NEXT bout list share:
// Shiro dark on the left, Aka in the board's red on the right, "vs" between.
// Layout (size, weight, wrapping) is the caller's, via the outer span's props.
function NextPair({ shiro, aka, ...rest }) {
    return (
        <span {...rest}>
            <span style={{ color: "var(--ink-1)" }}>{shiro}</span>
            <span style={{ color: "var(--ink-3)", fontWeight: 600, fontSize: "2vh", padding: "0 1vw" }}>vs</span>
            <span style={{ color: "var(--red)" }}>{aka}</span>
        </span>
    );
}

// emptyStateHeadline: headline text for the TvDisplay empty state, by sub-state.
// The third case ("No match in progress") is a defensive fallback that is
// UNREACHABLE under the current promote logic: countCourtMatches and
// findUpcomingOnCourt/findRunningOnCourt share the same bracketSidesReady
// predicate, so any running/scheduled match the counts see is also
// auto-promoted: and the empty state only renders when nothing is promoted
// (so counts.running and counts.scheduled are both 0 here). It is kept so that
// if that invariant is ever broken the screen degrades to a correct message
// rather than a wrong one. Exported + unit-tested per the PR #274 review (mp-s99q).
function emptyStateHeadline(allCompleted, noMatches) {
    if (allCompleted) return "All matches completed";
    if (noMatches) return "No matches scheduled";
    return "No match in progress";
}

// TvWhiteTeamBoard: mp-13y: white scoreboard for a running TEAM match
// (per the agreed mockup). Replaces the dark aka/shiro half-panels for the
// team case with a light board: court header + black rule, team name row
// (Shiro black left / Aka red right, each carrying its own IV/PW readout as
// of the bc-lbty pass below), then the per-bout grid (done / in-progress
// amber / queued grey), optional Daihyosen banner, and a single "Next" line.
// Individual matches, empty states and the lobby keep the existing dark
// surface (no mockup for those).

// LinkDot: 3-state connection indicator (mp-9ukk Phase 2).
// 'connected' renders an INVISIBLE span (visibility:hidden + transparent), not
// nothing: it still reserves the dot's layout space so the header does not
// reflow when the state changes, but a healthy, server-fresh board shows no
// visible dot. 'local' → amber dot (operator broadcast fresh, server down);
// 'stale' → red dot WITH a dark ring (no feed). A small static circle keeps
// the board uncluttered; the two degraded states are told apart by treatment
// (the ring), not hue alone, and each carries an aria-label for assistive tech.
function LinkDot({ linkState }) {
    const connected = linkState === 'connected';
    const isStale = linkState === 'stale';
    // 'local' uses var(--warn); 'stale' uses var(--danger).
    // Per DESIGN.md principle 2, the two degraded states are distinguished by
    // TREATMENT, not hue alone (so the meaning survives projector glare and
    // colour-blindness): 'stale' carries a dark ring, 'local' is a plain dot.
    // When connected we keep the element but make it invisible (visibility
    // hidden, aria-hidden) so the header reserves the dot's space and the
    // subtitle does not shift horizontally as the state toggles.
    const bg = isStale ? 'var(--danger)' : 'var(--warn)';
    const label = isStale ? 'No data feed' : 'Operator broadcast (server offline)';
    return (
        <span data-testid="display-link-dot" data-link-state={linkState}
            role={connected ? undefined : 'status'} aria-label={connected ? undefined : label}
            aria-hidden={connected ? 'true' : undefined}
            style={{ width: '1.4vh', height: '1.4vh', borderRadius: '50%', background: connected ? 'transparent' : bg, display: 'inline-block', flexShrink: 0, boxShadow: isStale ? '0 0 0 0.3vh var(--ink-1)' : 'none', visibility: connected ? 'hidden' : 'visible' }} />
    );
}

function TvWhiteBoard({ tournament, court, linkState = 'connected', promoted, isTeamMatch, subResults, lineupA, lineupB, squadA, squadB, teamSize, showDH, queueMatches, zekken }) {
    // Every cell on this board that shows a team name ELLIPSISES -- the two
    // headline cells, their rep sub-lines, and the summary row inside
    // TeamScoreboard. sideLabel's string form puts Aka's number LAST, and an
    // ellipsis truncates the END of a run, so a long team name took Aka's
    // number with it while Shiro's leading number always survived (measured at
    // 1920x1080: an 811px headline cell at 54px, so ~28 characters; and 85px
    // on the 402px viewer card, which a perfectly ordinary name overflows).
    // So this board passes PARTS and lets NumberedName's clip mode keep the
    // chip out of the ellipsised run. NextPair's rows below do not clip (they
    // were measured at the same viewport and have no ellipsis to protect
    // against), but as of 2026-09-19 they render the SAME NumberedName chip in
    // its plain (non-clip) form off sideLabelParts too, for visual consistency
    // with the row above (operator decision, bc-lbty): see sideLabelParts'
    // own header in display_helpers.jsx for the full clip-vs-string rule.
    const shiroTeamParts = sideLabelParts(promoted.match.sideB, zekken);
    const akaTeamParts = sideLabelParts(promoted.match.sideA, zekken);
    // Daihyosen / tiebreaker rep bout (mp-62vr): SideA/SideB are TEAM names, but
    // the actual fighters are the rep players the operator records. When set,
    // show the rep player as the headline name with the team as a sub-label.
    // Shiro = sideB → repPlayerB; Aka = sideA → repPlayerA. Empty for every
    // non-supplementary match, so this is a no-op for regular team/individual
    // boards.
    const repShiro = (promoted.match.repPlayerB || "").trim();
    const repAka = (promoted.match.repPlayerA || "").trim();
    const next = queueMatches && queueMatches.length ? queueMatches[0] : null;
    // Hoisted out of the TeamScoreboard call below (bc-lbty) so this row's own
    // IV/PW readout can derive from the exact same values via teamIVPWFrom:
    // one computation, never two chances to disagree.
    const matchSideA = promoted.match.sideA?.name || (typeof promoted.match.sideA === "string" ? promoted.match.sideA : "");
    const matchSideB = promoted.match.sideB?.name || (typeof promoted.match.sideB === "string" ? promoted.match.sideB : "");
    const isKachinuki = teamMatchTypeFor(promoted.competition) === "kachinuki";
    // bc-tmfn: the match-level default-win credit context, threaded into
    // BOTH the headline's own teamIVPWFrom call and TeamScoreboard below (see
    // team_default_credit.jsx for the whole rule). Every field is OPTIONAL on
    // TeamScoreboard/teamIVPWFrom, so this is a no-op wherever the ruling is
    // not in force.
    const matchCtx = { status: promoted.match?.status, decision: promoted.match?.decision, decisionBy: promoted.match?.decisionBy, kachinuki: isKachinuki };
    const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPWFrom(promoted.match?.teamResult, subResults, matchSideA, matchSideB, matchCtx);
    // bc-tmfn: the match-level Kiken/Fus. mark for a team a default-win
    // decision withdrew/barred, placed beside the headline name -- the SAME
    // pattern MatchCard (bracket.jsx) uses (sameCompetitor + sideMarks +
    // placeMarks). bc-cse: reached through the ONE shared composition of
    // that pattern, window.teamMatchMarks (bracket.jsx), rather than
    // re-derived inline here -- this board reaches bracket.jsx's primitives
    // through window globals like every other viewer/display module (see
    // result_slot.jsx's header). isTeamMatch is this board's OWN team-match
    // signal (competition format), passed through as the override
    // teamMatchMarks accepts. Computed ONCE here and threaded into
    // TeamScoreboard too, so the headline and the (suppressed, on this
    // variant) summary row can never disagree about which side carries the
    // mark.
    const { shiro: teamShiroMark, aka: teamAkaMark } = window.teamMatchMarks
        ? window.teamMatchMarks(promoted.match, isTeamMatch)
        : { shiro: "", aka: "" };
    // NO middle mark here (operator ruling): the FIK row centre rendered by the
    // shared scoreboard below is the mark's ONE home, and this header chip
    // duplicating X/(E)/(DH) ~10cm above it was an error; the plain "vs" stays.
    // (The OBS streaming overlay keeps its chip: it renders no scoreboard row,
    // so there the chip IS the one home.) Threading the match-level mark into
    // the team summary spacer instead was tried and reverted - two (E) marks on
    // one board, stacked. Do not re-add either. Where each mark DOES land is
    // stated once in CLAUDE.md § Match Decision Types; this file renders none of
    // those rows, so it does not restate the rule.
    // Header subtitle: competition name + phase, joined only when both exist
    // (phaseLabel is "" for league, so no dangling " · ").
    const compName = promoted.competition?.name || "";
    const compPhase = phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds, promoted.competition?.format);
    const headerSubtitle = [compName, compPhase].filter(Boolean).join(" · ");
    // The centre is a CLOSED SET (vs / X / (E) / (DH), operator ruling) and
    // never carries a result belonging to one competitor, so it stays a bare
    // "vs" regardless of what either side's cell shows. It used to be bare
    // because the score lived only in the shared scoreboard below; as of
    // bc-lbty (2026-09-19) a TEAM match's IV/PW now ALSO renders per side in
    // this row's own Shiro/Aka cells (see below), and the shared scoreboard's
    // own §277 summary row is suppressed on the TV board to avoid repeating
    // the team name a second time (TeamScoreboard, match_scoreboard.jsx) - but
    // that score still belongs to a SIDE, never to this shared centre cell.
    const nameCentre = <div style={{ fontSize: "2.4vh", color: "var(--ink-3)", fontWeight: 700 }}>vs</div>;

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
                <div style={{ display: "flex", gap: "1.5vw", alignItems: "center", fontSize: "2.2vh", color: "var(--ink-3)" }}>
                    <span>{headerSubtitle}</span>
                    {/* mp-9ukk Phase 2: 3-state dot. Hidden when connected;
                        amber when operator-broadcast is fresh; red when stale.
                        mp-13y #9: no "UP NEXT" badge: the promoted match is shown
                        plainly (the NEXT line below still lists what follows). */}
                    <LinkDot linkState={linkState} />
                </div>
            </div>

            {/* Team name row: Shiro black (left), Aka red (right). A TEAM match
                (bc-lbty, operator decision 2026-09-19) also carries each side's
                own IV/PW readout in its cell, now that TeamScoreboard's §277
                summary row below is suppressed on the TV board: the pairing and
                the score used to be shown twice (once here, once there); this
                is the one home for both now. Each side's figures take that
                side's OWN colour (var(--ink-1) / var(--red)), which is what the summary
                row they replace did via .msb-slot and .msb-slot--aka: a neutral
                slate here would put grey figures under a red name, the same
                name-vs-number colour mismatch this pass exists to remove. */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr auto 1fr", alignItems: "center", gap: "2vw", marginBottom: "2vh" }}>
                {/* No SHIRO/AKA heading (operator ruling 2026-09-20, bc-sccl). On a
                    board read from across a hall the side is already carried by the
                    two cues that survive at that distance: POSITION (Shiro left, Aka
                    right, fixed by kendo convention and never swapped) and COLOUR
                    (the name and its IV/PW in --ink-1 vs --red). The word added a
                    third statement of the same fact and cost a line of vertical
                    space on the surface with the least of it. No sr-only stand-in
                    here: this is a projector/OBS surface with no screen-reader
                    audience, unlike the operator consoles that did take one. */}
                <div style={{ minWidth: 0 }}>
                    {/* bc-cse: msb-name--labelled -- see viewer_match.jsx's
                        VSchedItem comment for why a mark riding beside a
                        clip-mode name needs its own flex row: without it a
                        long team name clips the Kiken/Fus. mark off the end.
                        No-op (harmless) when repShiro renders instead, since
                        that branch carries no mark at all. */}
                    <div className="msb-name--labelled" style={{ fontSize: "5vh", fontWeight: 800, color: "var(--ink-1)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{repShiro || teamNameMark("shiro", teamShiroMark, <NumberedName side="shiro" clip {...shiroTeamParts} />)}</div>
                    {repShiro && <div data-testid="rep-shiro-team" style={{ fontSize: "2.4vh", fontWeight: 600, color: "var(--ink-3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}><NumberedName side="shiro" clip {...shiroTeamParts} /></div>}
                    {isTeamMatch && (
                        <div data-testid="headline-ivpw-shiro" style={{ display: "flex", gap: "1.4vw", fontSize: "2vh", fontWeight: 700, color: "var(--ink-1)", marginTop: "0.6vh" }}>
                            <span><abbr className="msb-lab" title="Individual Victories" style={{ display: "inline", fontSize: "0.7em" }}>IV</abbr> {ivShiro}</span>
                            <span><abbr className="msb-lab" title="Points Won" style={{ display: "inline", fontSize: "0.7em" }}>PW</abbr> {pwShiro}</span>
                        </div>
                    )}
                </div>
                <div style={{ display: "flex", justifyContent: "center" }}>{nameCentre}</div>
                <div style={{ minWidth: 0, textAlign: "right" }}>
                    {/* bc-cse: msb-name--labelled + msb-name--aka (flex row,
                        justify-content flex-end so it still reads to the
                        right once .name-labelled overrides the parent's
                        textAlign, which no longer reaches a flex child). */}
                    <div className="msb-name--labelled msb-name--aka" style={{ fontSize: "5vh", fontWeight: 800, color: "var(--red)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{repAka || teamNameMark("aka", teamAkaMark, <NumberedName side="aka" clip {...akaTeamParts} />)}</div>
                    {repAka && <div data-testid="rep-aka-team" style={{ fontSize: "2.4vh", fontWeight: 600, color: "var(--ink-3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}><NumberedName side="aka" clip {...akaTeamParts} /></div>}
                    {isTeamMatch && (
                        <div data-testid="headline-ivpw-aka" style={{ display: "flex", gap: "1.4vw", justifyContent: "flex-end", fontSize: "2vh", fontWeight: 700, color: "var(--red)", marginTop: "0.6vh" }}>
                            <span><abbr className="msb-lab" title="Points Won" style={{ display: "inline", fontSize: "0.7em" }}>PW</abbr> {pwAka}</span>
                            <span><abbr className="msb-lab" title="Individual Victories" style={{ display: "inline", fontSize: "0.7em" }}>IV</abbr> {ivAka}</span>
                        </div>
                    )}
                </div>
            </div>

            {/* Shared FIK scoreboard (match_scoreboard.jsx): the SAME component
                the viewer card uses. On the card (viewer) variant="tv" only
                scales it up; on THIS board (variant="tv") it also suppresses
                its own §277 summary row, because the team-name row above now
                carries the pairing and each side's IV/PW (bc-lbty operator
                decision 2026-09-19: the two rows duplicated the team name, and
                the summary row was the redundant one, not the headline). Up-next
                matches have no bouts yet: TeamScoreboard renders numbered/roster
                rows (mp-13y #6) so the board reads as a real scoreboard rather
                than an empty grid. */}
            {isTeamMatch ? (
                <div style={{ flex: 1 }} data-testid="tvd-team-bouts">
                    {/* tri-review #1: thread the team names so the Daihyosen
                        win-mark lands on the winning side when the backend
                        persists the winner as the team name (mirrors viewer
                        MatchDetailCard). */}
                    <TeamScoreboard subResults={subResults} teamResult={promoted.match?.teamResult} lineupA={lineupA} lineupB={lineupB}
                        teamSize={teamSize} showDH={showDH} variant="tv"
                        isRunning={promoted.match?.status === "running"}
                        shiroName={shiroTeamParts.name} akaName={akaTeamParts.name}
                        matchSideA={matchSideA} matchSideB={matchSideB}
                        squadA={squadA} squadB={squadB}
                        numberA={promoted.match.sideA?.number || ""} numberB={promoted.match.sideB?.number || ""}
                        kachinuki={isKachinuki}
                        decision={promoted.match?.decision} decisionBy={promoted.match?.decisionBy} status={promoted.match?.status}
                        shiroMark={teamShiroMark} akaMark={teamAkaMark} />
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
                    <span style={{ fontSize: "1.8vh", letterSpacing: "0.12em", color: "var(--ink-3)", fontWeight: 700 }}>NEXT</span>
                    <NextPair
                        shiro={<NumberedName side="shiro" {...sideLabelParts(next.sideB, next._comp?.withZekkenName)} />}
                        aka={<NumberedName side="aka" {...sideLabelParts(next.sideA, next._comp?.withZekkenName)} />}
                        style={{ flex: 1, display: "flex", justifyContent: "space-between", fontSize: "2.6vh", fontWeight: 600 }}
                    />
                </div>
            )}
        </div>
    );
}

// poolMatchIndex: numeric trailing index of a pool-match id ("Pool A-10" → 10,
// "Pool A-DH-0" → 0); large fallback when none. Used as a final ordering
// tiebreaker so ids don't sort LEXICOGRAPHICALLY ("Pool A-10" before "Pool A-2").
function poolMatchIndex(id) {
    const m = /(\d+)$/.exec(typeof id === "string" ? id : "");
    return m ? Number(m[1]) : 9999;
}

// compareByRunOrder: canonical per-court run order: queue position, then
// scheduled time, then the numeric match index. Mirrors findUpcomingOnCourt and
// survives untimed schedules (blank scheduledAt) without the lexicographic-id
// pitfall of comparing ids as strings.
function compareByRunOrder(a, b) {
    const qa = Number(a.queuePosition) || 9999;
    const qb = Number(b.queuePosition) || 9999;
    if (qa !== qb) return qa - qb;
    const ta = a.scheduledAt || "99:99";
    const tb = b.scheduledAt || "99:99";
    if (ta !== tb) return ta.localeCompare(tb);
    return poolMatchIndex(a.id) - poolMatchIndex(b.id);
}

// gatherIndividualGroup: the sibling matches for an individual TV board:
// every match in the same POOL (pool phase) or the same ROUND (knockout) as
// the promoted match, **on the same court**. The TV display is per-court, so
// bracket rounds that span multiple courts must not leak cross-court matches.
//
// POOL phase: returns ALL pool matches on this court regardless of status, so
// spectators see the pool's full progression (completed → live → upcoming) on
// one screen, not just a feed of what's already happened.
// BRACKET phase: keeps the feed model (completed + current only), because
// scheduled bracket bouts often still carry placeholder sides like
// "Winner of r1-m0" that wouldn't read as real matches yet.
//
// Sort order: completed first (oldest → newest), then the CURRENT (running /
// promoted up-next) match, then scheduled (by scheduledAt), so the row order
// reads top-to-bottom as past → present → future.
function gatherIndividualGroup(promoted, court) {
    if (!promoted || !promoted.competition || !promoted.match) return [];
    const comp = promoted.competition;
    const cur = promoted.match;
    const matchCourt = court || cur.court || "";
    let group;
    if (promoted.isBracket) {
        // Effective round, matching the phaseLabel heading above these rows:
        // the raw round can mix two effective rounds when a bye collapses one.
        const rounds = (comp.bracket && comp.bracket.rounds) || [];
        group = bracketRoundSiblings(rounds, cur, promoted.roundIndex);
    } else {
        const pool = poolNameOf(cur.id);
        if (!pool) return []; // non-pool-shaped id → don't collect every other non-pool match
        group = (comp.poolMatches || []).filter(m => poolNameOf(m.id) === pool);
    }
    // Filter to the same court: bracket rounds can span multiple courts.
    const onCourt = group.filter(m => (m.court || "") === matchCourt);
    const isCurrent = m => m.id === cur.id || m.status === "running";
    const shown = promoted.isBracket
        ? onCourt.filter(m => m.status === "completed" || isCurrent(m))
        : onCourt; // POOL: include scheduled too
    // statusOrder: 0=completed (top), 1=current, 2=scheduled (bottom).
    const statusOrder = m => isCurrent(m) ? 1 : (m.status === "completed" ? 0 : 2);
    return shown.slice().sort((a, b) => (statusOrder(a) - statusOrder(b)) || compareByRunOrder(a, b));
}

// findNextPoolOnCourt: for the per-court pool-phase board, the next POOL that
// will play on this court after the current one finishes, plus its bouts as
// Shiro/Aka pairs in run order (so the spectator knows which bouts are coming
// and can decide whether to stay). Looks across the SAME competition (court
// routing is per-comp). Returns { name, bouts: [{ id, shiro, aka }] } or null.
//
// The pool with the lowest queue position (then earliest scheduledAt, then
// pool name) plays next: matching findUpcomingOnCourt. Bouts = ALL of that
// pool's matches in the comp (not just on this court): a pool's bouts are
// fixed, the courts list is just routing. For team competitions, sides carry
// team names so the pairs surface team names.
function findNextPoolOnCourt(competition, currentPoolName, court) {
    if (!competition || !competition.poolMatches) return null;
    const onCourt = competition.poolMatches.filter(m => (m.court || "") === court);
    // Candidate pools routed to this court (excluding the current one), tracked
    // by their FIRST match's (queuePosition, scheduledAt). Mirror
    // findUpcomingOnCourt's ordering: queuePosition first, scheduledAt as a
    // tiebreaker: so the UP NEXT pool matches the actual queue even in untimed
    // schedules. The (qp, ts) pair is taken from the SAME match (lexicographic
    // minimum) so we never synthesise a pair from two different matches.
    const meta = new Map();
    for (const m of onCourt) {
        const p = poolNameOf(m.id);
        if (!p || p === currentPoolName) continue;
        const qp = Number(m.queuePosition) || 9999;
        const ts = m.scheduledAt || "99:99";
        const cur = meta.get(p);
        if (!cur || qp < cur.qp || (qp === cur.qp && ts < cur.ts)) meta.set(p, { qp, ts });
    }
    if (meta.size === 0) return null;
    // A pool counts as already started if ANY of its matches is running or
    // completed on ANY court: matches can be moved between courts, so a pool
    // begun elsewhere is not a "future" pool here. (Scanning the whole comp,
    // not just this court, is what makes that cross-court check correct.)
    const started = new Set();
    for (const m of competition.poolMatches) {
        if (m.status === "running" || m.status === "completed") {
            const p = poolNameOf(m.id);
            if (p) started.add(p);
        }
    }
    const future = [...meta.entries()].filter(([p]) => !started.has(p));
    if (future.length === 0) return null;
    const nextName = future
        .sort(([na, a], [nb, b]) => (a.qp - b.qp) || a.ts.localeCompare(b.ts) || na.localeCompare(nb))[0][0];
    // The pool's bouts in run order, as Shiro/Aka pairs: that IS what comes
    // next on this court, so the strip shows the group of bouts rather than a
    // roster of names (operator ruling 2026-09-14, bc-dnst: a roster coloured
    // by each name's first side did not say which bouts were coming). All of
    // the pool's matches in the comp, not just this court's: a pool's bouts
    // are fixed, the courts list is just routing. Labels render as NumberedName
    // chips off sideLabelParts (number on the outer side + zekken displayName),
    // matching the main scoreboard row above (operator decision 2026-09-19,
    // bc-lbty: the NEXT strip now chips its number everywhere for visual
    // consistency, rather than the bare-string form this used to call sideLabel
    // for); a side still to be resolved reads "TBD".
    const zekken = !!competition.withZekkenName;
    const bouts = competition.poolMatches
        .filter(m => poolNameOf(m.id) === nextName)
        // A barred bout (ineligible_match.jsx) cannot be fought as scheduled,
        // so it is not a real "up next" bout for this strip: same picker rule
        // as findUpcomingOnCourt.
        .filter(m => !isBarredMatch(m))
        .sort(compareByRunOrder)
        .map(m => ({
            id: m.id,
            shiro: <NumberedName side="shiro" {...sideLabelParts(m.sideB, zekken)} />,
            aka: <NumberedName side="aka" {...sideLabelParts(m.sideA, zekken)} />,
        }));
    return { name: nextName, bouts };
}

// At variant=tv each row is roughly 6vh tall and the body has ~80vh of room,
// so ~10 rows fit comfortably. When the gathered group exceeds this, we take a
// WINDOW anchored on the current match (see windowAroundCurrent) rather than a
// fixed tail: the pool-phase sort is completed → current → scheduled, so a
// blind tail slice could drop the running row when there are many upcoming
// matches. The visible rows are distributed space-evenly to fill the panel.
const TV_INDIV_MAX_VISIBLE = 10;

// windowAroundCurrent: pick at most `max` consecutive rows from `all` that are
// guaranteed to include the anchored current match. A couple of completed rows
// are kept above the current one for context; the rest of the window fills with
// upcoming matches below it. Returns { rows, dropped } where `dropped` is the
// number removed from the HEAD (used for the data-dropped attribute).
function windowAroundCurrent(all, currentIdx, max) {
    if (all.length <= max) return { rows: all, dropped: 0 };
    // No current match found → show the FIRST max (upcoming), not the tail.
    const anchor = currentIdx < 0 ? 0 : currentIdx;
    const LOOKBACK = 2; // completed rows shown above the current match
    let start = Math.max(0, anchor - LOOKBACK);
    // Don't run past the end: pull the window back so it stays `max` tall.
    // (start is already ≥ 0 here: all.length > max, so all.length - max > 0.)
    if (start + max > all.length) start = all.length - max;
    return { rows: all.slice(start, start + max), dropped: start };
}

// TvIndividualBoard: mp-13y: white TV board for INDIVIDUAL competitions. The
// body lists the whole pool's matches (pool phase) or the whole round's matches
// (knockout) as a feed: each row is one match (Shiro name · ippon slots · Aka
// name, via the shared IndividualScore). Layout: rows are distributed
// space-evenly down the panel to fill the screen. gatherIndividualGroup orders
// pool phase as completed → current → scheduled, so the current match sits
// among its upcoming matches; windowAroundCurrent keeps it on screen when the
// group overflows the visible cap (no animation). FIK §263.
function TvIndividualBoard({ tournament, court, linkState = 'connected', promoted, queueMatches, zekken }) {
    const all = gatherIndividualGroup(promoted, court);
    const currentIdx = all.findIndex(m => m.id === promoted.match.id || m.status === "running");
    // A league is one big round-robin table: showing its whole match list would
    // cram the board, so cap it at a readable window around the current match.
    // Pools/brackets keep showing their whole group (the pool must be visible in
    // full: earlier product requirement), bounded only by TV_INDIV_MAX_VISIBLE.
    const maxVisible = promoted.competition?.format === "league" ? 6 : TV_INDIV_MAX_VISIBLE;
    const { rows, dropped } = windowAroundCurrent(all, currentIdx, maxVisible);
    const groupLabel = phaseLabel(promoted.match, promoted.isBracket, promoted.roundIndex, promoted.totalRounds, promoted.competition?.format);
    // Suppress the bottom NEXT line when its match is already visible in the
    // body: in pool phase the whole pool's queue is now in the feed, so the
    // line would otherwise duplicate a row immediately above it.
    const shownIds = new Set(rows.map(r => r.id));
    const firstQueued = queueMatches && queueMatches.length ? queueMatches[0] : null;
    const next = (firstQueued && !shownIds.has(firstQueued.id)) ? firstQueued : null;
    // Pool-phase only: surface the next pool on this court + its roster so
    // spectators know what's coming after the current pool finishes here.
    // Bracket phase has no pools so this is null. Gated to the "mixed" format:
    // the only one with MULTIPLE pools. Swiss matches also live in poolMatches
    // and poolNameOf would treat "Swiss-R1-2" as a pool, so without this gate a
    // Swiss board would flood the strip with the whole next round's roster.
    const currentPoolName = !promoted.isBracket ? poolNameOf(promoted.match.id) : "";
    const nextPool = (!promoted.isBracket && currentPoolName && promoted.competition?.format === "mixed")
        ? findNextPoolOnCourt(promoted.competition, currentPoolName, court)
        : null;
    // Text scale adapts to how much vertical room each row gets: fewer rows →
    // bigger glyphs (~7 rows ≈ 1×, a 4-match pool ≈ 1.5×); a packed group
    // shrinks to a 0.85 floor. Capped at 1.5 on every board (operator ruling
    // 2026-09-14, bc-dnst: at the old 2.4 / 2.0 caps a 3-bout pool rendered
    // 7vh names that read as too large and truncated to "M. SAN…"); the rows
    // are still distributed down the panel, they just no longer inflate to
    // fill it.
    const maxScale = 1.5;
    const rowScale = Math.min(maxScale, Math.max(0.85, 7 / Math.max(1, rows.length)));
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
                <div style={{ display: "flex", gap: "1.5vw", alignItems: "center", fontSize: "2.2vh", color: "var(--ink-3)" }}>
                    <span>{promoted.competition?.name || ""}</span>
                    {/* mp-9ukk Phase 2: 3-state dot. Hidden when connected;
                        amber when operator-broadcast is fresh; red when stale. */}
                    <LinkDot linkState={linkState} />
                </div>
            </div>

            {/* Phase strip: prominent phase label + per-court progress counter.
                Replaces the now-removed groupLabel from the top-right subtitle.
                Uses the same dashed divider style as the UP NEXT strip below
                for visual consistency. */}
            {(() => {
                const progress = phaseProgressOnCourt(promoted, court);
                if (!groupLabel && !progress) return null;
                return (
                    <div data-testid="tvd-phase-strip" style={{
                        display: "flex", alignItems: "baseline", gap: "1.5vw",
                        paddingBottom: "1.6vh", marginBottom: "2vh",
                        borderBottom: "1px dashed #d1d5db",
                    }}>
                        {groupLabel && (
                            <span style={{ fontSize: "3.2vh", fontWeight: 800, color: "#111", textTransform: "uppercase", letterSpacing: "0.05em" }}>
                                {groupLabel}
                            </span>
                        )}
                        {groupLabel && progress && (
                            <span style={{ fontSize: "2.4vh", color: "var(--ink-3)" }}>·</span>
                        )}
                        {progress && (
                            <>
                                <span data-testid="tvd-phase-progress" style={{ fontSize: "2.8vh", color: "#111", fontFamily: "var(--font-mono)", fontWeight: 700 }}>
                                    {progress.done} / {progress.total}
                                </span>
                                <span style={{ fontSize: "1.8vh", color: "var(--ink-3)", letterSpacing: "0.12em", fontWeight: 700 }}>MATCHES</span>
                            </>
                        )}
                    </div>
                );
            })()}

            {/* Match feed distributed space-evenly down the panel so the rows
                fill the screen (justifyContent below). Pool phase is ordered
                completed → current → scheduled, so the current match sits among
                its upcoming matches; when the group exceeds the visible cap,
                windowAroundCurrent keeps the running row visible (dropping
                oldest completed from the head). */}
            {/* --msb-scale flows into the shared .msb--tv CSS rules so the
                IndividualScore inside each row sizes itself to fit the available
                room. All rows render at the SAME size; the live row is signalled
                only by a quiet bg tint (no spine, no transform, no pulse). */}
            <div data-testid="tvd-indiv-group" data-dropped={dropped} style={{ "--msb-scale": rowScale, flex: 1, display: "flex", flexDirection: "column", justifyContent: "space-evenly", gap: "1vh", overflow: "hidden" }}>
                {rows.map(m => {
                    const isNow = m.id === promoted.match.id || m.status === "running";
                    const isDone = !isNow && m.status === "completed";
                    return (
                        <div key={m.id} data-testid={isNow ? "tvd-indiv-row-now" : "tvd-indiv-row"}
                            className={"tvd-indiv-row" + (isNow ? " tvd-indiv-row--now" : "") + (isDone ? " tvd-indiv-row--done" : "")}
                            style={{
                                padding: "1.2vh 1.5vw", borderRadius: "0.6vw",
                                background: isNow ? "var(--accent-soft)" : isDone ? "#f9fafb" : "transparent",
                                opacity: isNow ? 1 : isDone ? 0.88 : 0.78,
                            }}>
                            <IndividualScore match={m} variant="tv" showNames withZekkenName={zekken} showBarredChip />
                        </div>
                    );
                })}
                {rows.length === 0 && (
                    <div style={{ textAlign: "center", color: "var(--ink-3)", fontSize: "3vh", padding: "4vh 0" }}>No matches yet</div>
                )}
            </div>

            {/* Next pool on this court: its name and its bouts as Shiro vs Aka
                pairs in run order (team names for team competitions). Tells
                the spectator which bouts are coming, as a group, after the
                current pool finishes here. */}
            {nextPool && (
                <div data-testid="tvd-next-pool" style={{ borderTop: "1px dashed #d1d5db", paddingTop: "1.6vh", marginTop: "1.6vh" }}>
                    {/* Row 1: label + pool name + bout count */}
                    <div style={{ display: "flex", alignItems: "baseline", gap: "1.5vw", marginBottom: "0.8vh" }}>
                        <span style={{ fontSize: "1.6vh", letterSpacing: "0.12em", color: "var(--ink-3)", fontWeight: 700, flexShrink: 0 }}>UP NEXT</span>
                        <span style={{ fontSize: "2.6vh", color: "#111", fontWeight: 700 }}>{nextPool.name}</span>
                        <span style={{ fontSize: "1.8vh", color: "var(--ink-3)" }}>{`${nextPool.bouts.length} ${nextPool.bouts.length === 1 ? "bout" : "bouts"}`}</span>
                    </div>
                    {/* Row 2: the bouts, each an unbreakable Shiro vs Aka pair,
                        wrapping as a group. Same left-dark / right-red reading
                        as the rows above. */}
                    <div style={{ display: "flex", flexWrap: "wrap", gap: "0.6vh 3vw" }}>
                        {nextPool.bouts.map((b) => (
                            <NextPair
                                key={b.id}
                                shiro={b.shiro}
                                aka={b.aka}
                                data-testid="tvd-next-bout"
                                style={{ fontSize: "2.8vh", fontWeight: 700, whiteSpace: "nowrap" }}
                            />
                        ))}
                    </div>
                </div>
            )}

            {/* Next match line: shown only when there's a queued match NOT
                already in the body and the UP NEXT pool strip isn't already
                shown (that strip's next pool subsumes this matchup). In
                multi-comp / multi-pool-on-one-court setups it surfaces the very
                next match the operator will run here. */}
            {next && !nextPool && (
                <div style={{ display: "flex", alignItems: "center", gap: "1.5vw", borderTop: "1px dashed #d1d5db", paddingTop: "1.6vh", marginTop: "1.6vh" }}>
                    <span style={{ fontSize: "1.8vh", letterSpacing: "0.12em", color: "var(--ink-3)", fontWeight: 700 }}>NEXT</span>
                    <NextPair
                        shiro={<NumberedName side="shiro" {...sideLabelParts(next.sideB, next._comp?.withZekkenName ?? zekken)} />}
                        aka={<NumberedName side="aka" {...sideLabelParts(next.sideA, next._comp?.withZekkenName ?? zekken)} />}
                        style={{ flex: 1, display: "flex", justifyContent: "space-between", fontSize: "2.6vh", fontWeight: 600 }}
                    />
                </div>
            )}
        </div>
    );
}

// <TvDisplay court="A">: fullscreen per-court board.
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
// Link indicator (mp-9ukk): the `linkState` prop ('connected' | 'local' |
// 'stale'), wired from app.jsx which owns both the SSE EventSource and the
// court broadcast recency, drives the discreet LinkDot in the header. The dot
// is hidden when connected, amber when court-local (server down but the
// operator broadcast is fresh), and red when stale. The rest of the layout
// stays put so a state change doesn't reflow the board.
function TvDisplay({ court, tournament, competitions, withZekkenName, linkState = 'connected' }) {
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
    if (running) {
        promoted = { kind: "running", match: running.match, competition: running.competition, isBracket: running.isBracket, roundIndex: running.roundIndex, totalRounds: running.totalRounds };
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
    // competition.kind === "team" OR competition.teamSize > 0: EXCEPT a pool
    // daihyosen / tiebreaker rep bout ("Pool X-DH-N" / "Pool X-TB-N"), which is
    // a single INDIVIDUAL ippon-shobu even in a team competition. Mirrors the
    // admin scorer's compKind override (admin_pools.jsx): without it the rep
    // bout would render as an empty 5-person team grid since its score lives at
    // the match top level, not in subResults.
    const supplementaryBout = isSupplementaryBout(promoted && promoted.match && promoted.match.id);
    const isTeamComp = !!(promoted && promoted.competition &&
        (promoted.competition.kind === "team" || (promoted.competition.teamSize || 0) > 0));
    const isTeamMatch = isTeamComp && !supplementaryBout;
    const teamSize = (promoted && promoted.competition && promoted.competition.teamSize) || 0;

    // mp-13y: fetch lineups for the running team match. useTeamLineups
    // degrades gracefully (returns null/null/[]/[]) when the promoted slot is
    // not a team match or when window.API is unavailable. bc-pnum: squadA/
    // squadB ride the same fetch, resolved off promoted.competition.squads
    // (the aggregate item this surface consumes, per api_client.jsx's
    // normalizeViewerCompItem).
    const { lineupA, lineupB, squadA, squadB } = useTeamLineups(
        isTeamMatch && promoted && promoted.match ? promoted.match : null,
        isTeamMatch && promoted ? promoted.competition : null,
        promoted ? promoted.roundIndex : undefined
    );

    // mp-13y: DH (Daihyosen) row gating: shown when:
    //   1. All regular bouts are complete (every sub has ippons, hantei, or draw).
    //   2. IV (Individual Victories) are tied.
    //   3. PW (Points Won) are also tied.
    //   4. It is a knockout phase (not a pool match).
    // The DH sub-result (position === DAIHYOSEN_POSITION) may or may not exist
    // yet; when absent, TeamScoreboard renders a "Daihyosen pending" placeholder.
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
        const regularSubs = subResults.filter(s => s.position > DAIHYOSEN_POSITION);
        if (regularSubs.length === 0) return false;
        // bc-cse: subBoutHasResult (team_default_credit.jsx) is THE wire-level
        // "has a result" test -- a winner, a decision, a struck point, the
        // hantei mark, a hansoku award, or an overtime -- exactly what this
        // used to hand-roll here. Without one of those, a tied knockout closed
        // by forfeit would never satisfy allDone and the Daihyosen row would
        // be suppressed forever. The isHikiwake(score.type) arm is the one
        // legacy/client-only branch it does not cover (a bare quick-score
        // synth shape), same as TeamScoreboard's isScored (match_scoreboard.jsx)
        // this mirrors.
        const allDone = regularSubs.every(s => subBoutHasResult(s) ||
            (typeof window.isHikiwake === "function" && window.isHikiwake(s.score?.type)));
        if (!allDone) return false;
        // The match is tied (→ show DH) when IV and PW are level per side.
        // teamIVPW already prefers an explicit `sub.winner` (which the server
        // guarantees equals sideA/sideB), so a hantei-decided 0-0 bout is
        // counted as an IV for its winner there: no extra hantei loop needed.
        const { ivShiro, ivAka, pwShiro, pwAka } = teamIVPW(subResults, promotedSideA, promotedSideB);
        return ivShiro === ivAka && pwShiro === pwAka;
    }, [subResults, isTeamMatch, isKnockoutPhase, promotedSideA, promotedSideB]);

    // White scoreboard for any promoted match.
    // Team → TvWhiteBoard (IV/PW summary + bout grid). Individual → grouped
    // board listing every match in the same POOL (pool phase) or ROUND
    // (knockout), ordered completed → current → scheduled and distributed
    // space-evenly; when the group overflows, windowAroundCurrent keeps a
    // window centred on the current match so it's never scrolled off.
    if (promoted) {
        if (isTeamMatch) {
            return <TvWhiteBoard
                tournament={tournament} court={court} linkState={linkState}
                promoted={promoted} isTeamMatch={isTeamMatch}
                subResults={subResults} lineupA={lineupA} lineupB={lineupB} squadA={squadA} squadB={squadB} teamSize={teamSize}
                showDH={showDH} queueMatches={queueMatches} zekken={zekken}
            />;
        }
        // A pool DH/TB rep bout in a TEAM competition is individual, but it must
        // NOT fall into TvIndividualBoard's whole-pool feed (which would render
        // the sibling team encounters as individual rows). Show it as a single
        // scoreboard via TvWhiteBoard's individual branch (big Shiro/Aka names +
        // the rep bout's ippon slots).
        if (supplementaryBout && isTeamComp) {
            return <TvWhiteBoard
                tournament={tournament} court={court} linkState={linkState}
                promoted={promoted} isTeamMatch={false}
                subResults={subResults} lineupA={lineupA} lineupB={lineupB} teamSize={teamSize}
                showDH={false} queueMatches={queueMatches} zekken={zekken}
            />;
        }
        return <TvIndividualBoard
            tournament={tournament} court={court} linkState={linkState}
            promoted={promoted} queueMatches={queueMatches} zekken={zekken}
        />;
    }

    // ─── Empty-state redesign (mp-s99q) ──────────────────────────────────────
    // No promoted match → white board with high-contrast empty state.
    // Three sub-states: allCompleted / noMatches / between-matches.
    // Headline uses var(--ink-1) (~10:1 on white) instead of the prior
    // #9ca3af (2.5:1) which failed WCAG AA: critical for wall screens in
    // bright halls.
    // The completed check-badge colours mirror the knockout/completed status
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
                {/* mp-9ukk Phase 2: 3-state dot on empty-state board. Hidden
                    when connected; amber when operator-broadcast fresh; red stale. */}
                <LinkDot linkState={linkState} />
            </div>

            {/* Centre block: anchored slightly above dead-centre */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", paddingBottom: "8vh", gap: "3vh" }}>

                {/* a) Status icon + headline */}
                <div data-testid={allCompleted ? "display-all-completed" : (noMatches ? "display-no-matches" : "display-between-matches")}
                    style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "2vh", textAlign: "center", maxWidth: "60vw" }}>
                    {allCompleted && (
                        /* Drawn SVG checkmark: NOT the raw ✓ Unicode glyph */
                        <div style={{
                            width: "8vh", height: "8vh", borderRadius: "50%",
                            /* completed-status palette: mirrors knockout/completed used across the app */
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

                {/* b) QR affordance: "Scan for results" */}
                {qrUrl && (
                    <div style={{ display: "inline-flex", alignItems: "center", gap: "1.5vw", marginTop: "1vh" }}>
                        <StreamingQR url={qrUrl} />
                        <span style={{ fontSize: "2vh", color: "var(--ink-2)", fontWeight: 500 }}>Scan for results</span>
                    </div>
                )}

                {/* c) IN PROGRESS wayfinding strip: other active courts */}
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
                                {/* Static navy wayfinding dot */}
                                <span style={{ width: "0.9vh", height: "0.9vh", borderRadius: "50%", background: "var(--accent)", display: "inline-block", flexShrink: 0 }} />
                                Shiaijo {c}
                            </span>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

export { TvDisplay, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, findNextPoolOnCourt, emptyStateHeadline, LinkDot };
