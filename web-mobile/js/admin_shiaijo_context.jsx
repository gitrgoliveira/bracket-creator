// The court console's context panel: the standings of the pool, or the bracket
// fragment, beside the match being scored. Moved out of admin_shiaijo.jsx.

import { poolMatchesSignature } from './viewer_standings.jsx';
import { Icon } from './icon.jsx';

const { useState, useMemo, useEffect } = React;

const BracketTree = window.BracketTree;

// Standings ordering is decided by competition FORMAT, not by surface
// (mp-ahu6/mp-pglr): swiss renders cumulative standings from the dedicated
// /swiss/standings endpoint (window.SwissStandingsViewer), leagues are always
// rank-ordered (window.LeagueStandingsViewer), pools are always draw-ordered
// with rank as a badge (window.PoolsViewer) - the same three-way split as the
// public viewer (viewer_competition.jsx) and admin Pools tab. Swiss must NOT
// fall through to the pool path: Swiss piggybacks on pool-matches.csv with a
// synthetic pool name ("Swiss-R1") but never writes pools.csv, so the pool
// path's detail.pools lookup finds nothing and the panel sticks on "Loading
// standings…" forever. Exported so the format->viewer decision is
// independently testable and this invariant cannot silently regress again.
export function shiaijoStandingsKind(match) {
    if (!match) return "pool";
    if (match.compFormat === "swiss") return "swiss";
    return match.compFormat === "league" ? "league" : "pool";
}

// Collapsible context for the match being scored:
//   • pool phase  → live standings + results for the current pool, routed by
//     shiaijoStandingsKind. Pools also show which pool is next on this
//     court; leagues have no "next pool" concept.
//   • bracket phase → a bracket fragment with the anchored match highlighted.
// `lead` replaces the heading's leading word when the anchored match is not the
// live bout: "Up next" for a bout not yet fought, "Just played" for a finished
// one the panel fell back to, "Correcting" for one the operator opened to fix.
// Without it the highlight reads as the bout now being fought (mp-jnvl). With
// no lead the word names the panel's CONTENT - standings for a pool, the
// bracket fragment for a knockout; "Context" said nothing and was dropped
// (operator ruling 2026-09-19).
export function ShiaijoContext({ match, competitions, court, nextPoolName, tweaks, lead, open, onToggle }) {
    const comp = (competitions || []).find((c) => c.id === match.compId);
    const bracket = comp && (comp.bracket || (Array.isArray(comp.rounds) ? { rounds: comp.rounds } : null));
    const isPool = match.phase === "pool";
    const standingsKind = shiaijoStandingsKind(match);
    const isLeagueComp = standingsKind === "league";
    const isSwissComp = standingsKind === "swiss";
    // leagueAwareLabel (viewer_utils.jsx) already folds the swiss case in via
    // swissRoundLabel, so the isSwissComp ternary that used to sit here is
    // redundant (mp-dej2).
    const phaseLabel = isPool
        ? window.leagueAwareLabel(match.compFormat, match.poolName, "Pool")
        : (match.round || "Elimination");
    const PoolsViewer = window.PoolsViewer;
    const LeagueStandingsViewer = window.LeagueStandingsViewer;
    const SwissStandingsViewer = window.SwissStandingsViewer;

    // Pools/standings aren't on the console's competition list payload: fetch
    // the competition detail on demand. Refetch whenever this comp's pool
    // matches change (a scored bout), so standings stay current. poolSig is the
    // change key (poolMatchesSignature, the one owner of "did the pool results
    // change", so a correction that keeps the winner counts); it's cheap and
    // keyed only to this comp. Leagues skip this
    // fetch entirely (Copilot, PR #333): LeagueStandingsViewer fetches its own
    // standings via window.API.leagueStandings(c.id) and only needs
    // `comp`/`comp.poolMatches`, both already on the court feed - waiting on
    // fetchCompetitionDetails here would be an unneeded dependency that could
    // stall or blank the league panel if that endpoint (unlike the dedicated
    // league-standings one) has a transient failure.
    const poolSig = useMemo(() => poolMatchesSignature(comp && comp.poolMatches), [comp]);
    const [detail, setDetail] = useState(null);
    const [detailErr, setDetailErr] = useState(false);
    useEffect(() => {
        // Swiss and league both render self-fetching viewers off `comp` alone
        // (dedicated standings endpoints), so the competition-detail fetch is
        // only needed for the pool path.
        if (!isPool || isLeagueComp || isSwissComp || !match.compId || !window.API || typeof window.API.fetchCompetitionDetails !== "function") {
            setDetail(null);
            return;
        }
        let cancelled = false;
        setDetailErr(false);
        window.API.fetchCompetitionDetails(match.compId)
            .then((d) => { if (!cancelled) setDetail(d); })
            .catch(() => { if (!cancelled) { setDetail(null); setDetailErr(true); } });
        return () => { cancelled = true; };
    }, [match.compId, isPool, isLeagueComp, isSwissComp, poolSig]);

    const currentPool = detail && Array.isArray(detail.pools)
        ? detail.pools.find((p) => p.poolName === match.poolName)
        : null;

    // Shared loading placeholder for the self-fetching standings viewers
    // (swiss + league branches below).
    const standingsLoader = (
        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
            Loading standings…
        </p>
    );

    return (
        <div className="shiaijo-context">
            <button type="button" className="section-title shiaijo-context__toggle" aria-expanded={open} onClick={onToggle}>
                <Icon name={open ? "chevron-down" : "chevron-right"} size={14} className="icon-lead" />{lead || (isPool ? "Standings" : "Bracket")}, {match.compName}, {phaseLabel}
            </button>
            {open && (
                <div className="shiaijo-context__body">
                    {/* mp-gmcg: the scorer's live IV/PW tally and this standings
                        table disagreed on screen (critique P1) — standings only
                        count RECORDED encounters, so a running match reads 0.
                        Name the gap instead of leaving two contradictory numbers. */}
                    {isPool && match.status === "running" && (
                        <p className="shiaijo-context__live-note" style={{ fontSize: 12, color: "var(--ink-2)", margin: "0 0 8px" }}>
                            This match is still in progress, so its result isn’t in the standings below yet. They update when it’s recorded.
                        </p>
                    )}
                    {isPool ? (
                        isSwissComp ? (
                            // Swiss standings come from the dedicated
                            // /swiss/standings endpoint; SwissStandingsViewer
                            // fetches its own data (like LeagueStandingsViewer)
                            // and needs only `comp` from the court feed. The
                            // poolSig key remounts it after any scored bout so
                            // the cumulative table stays current mid-round
                            // (its own refetch deps only cover round changes).
                            // No "next pool" banner: Swiss rounds are not
                            // pools, the next round doesn't exist until the
                            // operator generates it.
                            SwissStandingsViewer && comp ? (
                                <div className="shiaijo-context__pools">
                                    <SwissStandingsViewer
                                        key={poolSig}
                                        competition={comp}
                                        poolMatches={comp.poolMatches}
                                        tweaks={tweaks || { showDojo: true }}
                                    />
                                </div>
                            ) : standingsLoader
                        ) : isLeagueComp ? (
                            // Leagues are always RANK-ordered (mp-ahu6): never fall
                            // through to the draw-order PoolsViewer here. Renders off
                            // `comp` alone (see the poolSig effect above for why) -
                            // LeagueStandingsViewer fetches its own standings.
                            LeagueStandingsViewer && comp ? (
                                <div className="shiaijo-context__pools">
                                    <LeagueStandingsViewer
                                        competition={comp}
                                        poolMatches={comp.poolMatches}
                                        tweaks={tweaks || { showDojo: true }}
                                        onMatchClick={null}
                                        highlightPlayers={[]}
                                        showDataIssues
                                    />
                                </div>
                            ) : standingsLoader
                        ) : (
                            <>
                                <div className="shiaijo-context__next">
                                    {nextPoolName
                                        ? <><span className="shiaijo-context__next-label">Next pool on Shiaijo {court}:</span> <strong>{nextPoolName}</strong></>
                                        : <span className="shiaijo-context__next-label">Last pool on Shiaijo {court}.</span>}
                                </div>
                                {PoolsViewer && currentPool ? (
                                    <div className="shiaijo-context__pools">
                                        <PoolsViewer
                                            pools={[currentPool]}
                                            standings={detail.standings}
                                            poolMatches={detail.poolMatches}
                                            competition={comp || detail}
                                            tweaks={tweaks || { showDojo: true }}
                                            onMatchClick={null}
                                            highlightPlayers={[]}
                                            showDataIssues
                                        />
                                    </div>
                                ) : (
                                    <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                                        {detailErr
                                            ? "Couldn't load standings: they'll appear once the connection recovers."
                                            : "Loading standings…"}
                                    </p>
                                )}
                            </>
                        )
                    ) : (match.phase === "bracket" && BracketTree && bracket && bracket.rounds) ? (
                        <div className="shiaijo-context__bracket">
                            <BracketTree rounds={bracket.rounds} highlightedMatchId={match.id} isEngi={!!match.compEngi} />
                        </div>
                    ) : (
                        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                            {match.compName}: {phaseLabel}. Bracket context appears here for elimination matches.
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}
