// viewer_competition.jsx: page-level competition and overview components.
// Extracted from viewer.jsx (mp-pxxc step 9). Pure split, no behavior change.

import { TermV, competitionKindLabel, poolLabel } from './viewer_utils.jsx';
import { matchParticipantIds, matchParticipantNames, isFollowedPlayer, isPlayerWatched, entryKey, resolveWatchedPlayers, findPrimaryEntry, buildPrimaryNextMatch, buildRoster, useWatchlist } from './viewer_watchlist_core.jsx';
import { MatchDetailCard, VSchedItem, MatchViewerModal } from './viewer_match.jsx';
import { WinnerBadge, SwissStandingsViewer, PoolsViewer } from './viewer_standings.jsx';
import { AwardsView } from './viewer_awards.jsx';
import { usePrimaryWatch } from './viewer_schedule.jsx';

const { useState, useMemo, useRef: useRefV } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const EmptyState = window.EmptyState;

// Lazy callable: window.hasBothSides is set by admin_helpers.js which loads
// AFTER viewer scripts. By the time any React render runs, it is defined.
const hasBothSides = (m) => window.hasBothSides(m);
// Pool daihyosen matches carry '-DH-' in their id.
const isPoolDaihyosenID = id => id.includes('-DH-');

// mp-tidg: activeTab + onTabChange are controlled props: app.jsx owns the
// tab state so browser back/forward across tabs works (each tab switch is a
// history push in the URL-sync effect). Do not add internal tab state here.
export function ViewerCompetition({ tournament, competition, pools, poolMatches, standings, bracket, onBack, authed, onEditCompetition, tweaks, activeTab, onTabChange }) {
  const c = competition;

  const allMatches = useMemo(() => {
    const out = [];
    if (pools) {
        pools.forEach((p) => {
            const matches = poolMatches ? poolMatches.filter(m => m.id.startsWith(p.poolName + "-")) : [];
            matches.forEach((m) => {
                const isDH = isPoolDaihyosenID(m.id || "");
                out.push({ ...m, phase: "pool", phaseName: p.poolName, poolName: p.poolName, compFormat: c.format, compId: c.id, compName: c.name, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
            });
        });
    }
    // mp-9dz/mp-8jbo: a preview bracket (bracket.preview === true) on a mixed
    // (Pools + Knockout) competition carries pool-origin TBD placeholders
    // ("Pool A-1st", "Pool B-2nd", …). These must NOT flow into allMatches
    // because allMatches feeds Up next / upcoming / recent / watchlist, and
    // spectators would see meaningless placeholder entries. The Bracket tab
    // renders `bracket`/`derivedBracket` directly and is NOT affected.
    if (bracket && bracket.rounds && !bracket.preview) {
        bracket.rounds.forEach((round, ri) => {
            round.forEach((m) => out.push({ ...m, phase: "bracket", round: window.roundLabel(ri, bracket.rounds.length), phaseName: window.roundLabel(ri, bracket.rounds.length), roundIndex: ri, compId: c.id, compName: c.name, compKind: c.kind, teamSize: c.teamSize }));
        });
    }
    return out;
  }, [pools, poolMatches, bracket, c.id, c.name, c.kind, c.teamSize, c.format]);

  // mp-xhaa: the schedule filter and bracket/pool highlight are driven by the
  // unified watchlist (resolved to flat players, so dojo entries expand to
  // current members). ALL watched players are highlighted via highlightPlayers
  // (a Set of ids+names); myPlayer (the primary, when it's a single player)
  // still feeds the "your next match" opponent-side logic.
  const [watchlist, setWatchlist] = useWatchlist();
  const [primaryKey, setPrimaryKey] = usePrimaryWatch();
  const compRoster = useMemo(() => buildRoster([c]), [c]);
  const rosterById = useMemo(() => new Map(compRoster.map((p) => [p.id, p])), [compRoster]);
  // Restrict the filter bar chips to entries that are relevant to THIS competition:
  // player entries only shown when the player is in compRoster; dojo entries only
  // when at least one roster player belongs to that dojo.
  const compDojos = useMemo(() => new Set(compRoster.map((p) => p.dojo).filter(Boolean)), [compRoster]);
  const compWatchlist = useMemo(
    () => watchlist.filter((e) => e.type === "dojo" ? compDojos.has(e.dojo) : rosterById.has(e.id)),
    [watchlist, compDojos, rosterById],
  );
  const resolvedWatched = useMemo(() => resolveWatchedPlayers(compWatchlist, compRoster), [compWatchlist, compRoster]);
  const watchedIds = useMemo(() => new Set(resolvedWatched.map((p) => String(p.id))), [resolvedWatched]);
  const watchedNames = useMemo(() => new Set(resolvedWatched.map((p) => (p.name || "").trim().toLowerCase()).filter(Boolean)), [resolvedWatched]);
  const hasActiveFilter = compWatchlist.length > 0;

  const primaryEntry = useMemo(() => findPrimaryEntry(watchlist, primaryKey), [watchlist, primaryKey]);
  const myPlayer = useMemo(() => {
    if (!primaryEntry || primaryEntry.type !== "player") return null;
    const rec = compRoster.find((p) => p.id === primaryEntry.id);
    return { id: primaryEntry.id, name: (rec && rec.name) || primaryEntry.name || "" };
  }, [primaryEntry, compRoster]);
  const myUpcoming = useMemo(() => buildPrimaryNextMatch(primaryEntry, compRoster, allMatches.filter(hasBothSides)), [primaryEntry, compRoster, allMatches]);

  // mp-xhaa: the highlight set covers ALL watched players (dojo entries
  // expanded to members), keyed by id AND lowercased name so the
  // bracket/pool/schedule highlight matches by either. This is the upgrade
  // over the old single-followed-player highlight.
  const highlightPlayers = useMemo(() => {
    const s = new Set();
    resolvedWatched.forEach((p) => {
      if (p.id) s.add(String(p.id));
      const n = (p.name || "").trim().toLowerCase();
      if (n) s.add(n);
    });
    return s;
  }, [resolvedWatched]);

  const { runningMatches, upcomingMatches, recentMatches } = useMemo(() => {
    const matchInvolvesWatched = (m) => {
      if (!hasActiveFilter) return true;
      const [aId, bId] = matchParticipantIds(m);
      if ((aId && watchedIds.has(aId)) || (bId && watchedIds.has(bId))) return true;
      if (watchedNames.size > 0) {
        const [aName, bName] = matchParticipantNames(m);
        const aN = aName ? aName.trim().toLowerCase() : "";
        const bN = bName ? bName.trim().toLowerCase() : "";
        if ((aN && watchedNames.has(aN)) || (bN && watchedNames.has(bN))) return true;
      }
      return false;
    };
    const running = allMatches.filter((m) => m.status === "running" && hasBothSides(m) && matchInvolvesWatched(m));
    const upcoming = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m) && matchInvolvesWatched(m))
      .sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"))
      .slice(0, hasActiveFilter ? 20 : 3);
    // Reverse-chronological by scheduled time. allMatches arrives in
    // pool-then-bracket order (not time order), so a bare slice(-N).reverse()
    // produced a jumbled "Recent results" list: sort by scheduledAt desc,
    // then take the most recent N. Missing times sort last (oldest).
    const recent = allMatches
      .filter((m) => m.status === "completed" && m.winner && matchInvolvesWatched(m))
      .sort((a, b) => (b.scheduledAt || "00:00").localeCompare(a.scheduledAt || "00:00"))
      .slice(0, hasActiveFilter ? 20 : 5);
    return { runningMatches: running, upcomingMatches: upcoming, recentMatches: recent };
  }, [allMatches, watchedIds, watchedNames, hasActiveFilter]);

  const filterLabel = useMemo(() => {
    if (!hasActiveFilter) return null;
    const n = watchedIds.size;
    if (myPlayer && myPlayer.name && n === 1) return myPlayer.name;
    return `${n} watched`;
  }, [myPlayer, watchedIds, hasActiveFilter]);

  // A mixed competition always carries a real bracket payload from the server
  // (pool-origin placeholder leaves like "Pool A-1st" while pools are running,
  // each replaced by the real finisher as that pool completes). No need for a
  // client-side placeholder fallback: just use what the server sends.
  const derivedBracket = useMemo(() => {
    if (bracket && bracket.rounds && bracket.rounds.length > 0) return bracket;
    return null;
  }, [bracket]);

  // draw-ready is NOT pre-start for the purposes of showing pool/bracket
  // structure: the draw has been generated and the payload already includes
  // pools + bracket data returned unconditionally by handlers_viewer.go.
  // Only setup (no draw yet) is treated as pre-start.
  const isPreStart = !c.status || c.status === "setup";
  const hasPools = !isPreStart && !!pools && pools.length > 0;
  const hasBracket = !isPreStart && !!derivedBracket && derivedBracket.rounds && derivedBracket.rounds.length > 0;
  // T192 (FR-050e): Swiss competitions surface a dedicated standings
  // tab in place of pools/bracket. The standings tab fetches its own
  // data via /swiss/standings (it's not part of the competition-detail
  // payload: see api_client.jsx).
  const isSwiss = c.format === "swiss";
  const isLeague = c.format === "league";
  const tabs = [
    { id: "overview", label: "Overview" },
    isSwiss ? { id: "swiss", label: "Standings" } : null,
    hasPools && !isSwiss ? { id: "pools", label: isLeague ? "League" : "Pools" } : null,
    hasBracket && !isSwiss ? { id: "bracket", label: "Bracket" } : null,
    c.status === "completed" ? { id: "results", label: "Awards" } : null,
  ].filter(Boolean);

  // Clamp the incoming activeTab to the set of tabs that are actually
  // rendered for this competition: a tab that was valid on a previous
  // comp (e.g. "bracket") may not exist yet on draw-ready or setup.
  const effectiveTab = tabs.some(t => t.id === activeTab) ? activeTab : "overview";

  // Controlled-tab writer. Maps the default tab to null so App keeps the
  // canonical bare /competition/:id URL (the rest of the app stores null,
  // not "overview", for the default), and tolerates a missing handler via
  // optional chaining: some unit tests mount ViewerCompetition without an
  // onTabChange just to assert tab presence. (mp-tidg / PR #307 review)
  const selectTab = (id, replace = false) => onTabChange?.(id === "overview" ? null : id, replace);

  // mp-tidg: when the requested tab isn't available (deep-link to /bracket
  // before a draw, or a draw_discarded SSE while sitting on it) effectiveTab
  // falls back to overview for display. Propagate that back to app state so
  // the URL stops claiming a tab the user isn't actually on: otherwise a
  // shared permalink would render Overview while still reading /…/bracket.
  // Guard on a truthy activeTab: when no specific tab was requested (the
  // default/nullish state) there is nothing to correct: the URL is already
  // canonical.
  React.useEffect(() => {
    // replace=true: this is a correction, not a navigation: rewrite the
    // invalid tab URL in place so Back doesn't return to it (PR #307 review).
    if (activeTab && effectiveTab !== activeTab) selectTab(effectiveTab, true);
  }, [effectiveTab, activeTab, onTabChange]);

  const currentMatch = useMemo(() => {
    if (runningMatches.length > 0) return runningMatches[0];
    return upcomingMatches[0] || null;
  }, [runningMatches, upcomingMatches]);

  const [bracketScrollTarget, setBracketScrollTarget] = useState(null);
  const bracketScrollRef = useRefV(null);
  const [selectedMatch, setSelectedMatch] = useState(null);
  const [bracketOverflowRight, setBracketOverflowRight] = useState(false);

  React.useEffect(() => {
    if (effectiveTab === "bracket" && currentMatch) {
      setBracketScrollTarget(currentMatch.id + "::" + Date.now());
    }
  }, [effectiveTab, currentMatch?.id]);

  const hasBracketEl = effectiveTab === "bracket" && !!derivedBracket;
  React.useEffect(() => {
    if (!hasBracketEl) return;
    const el = bracketScrollRef.current;
    if (!el) return;
    const check = () => { const next = el.scrollLeft + el.clientWidth < el.scrollWidth - 4; setBracketOverflowRight((cur) => (cur === next ? cur : next)); };
    check();
    el.addEventListener("scroll", check, { passive: true });
    const ro = new ResizeObserver(check);
    ro.observe(el);
    // Also observe the inner canvas so bracket content width changes (e.g.
    // new bracket payload rendered into the same tab) trigger a recheck.
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    return () => { el.removeEventListener("scroll", check); ro.disconnect(); };
  }, [hasBracketEl]);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button type="button" className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">
              {c.date && <span style={{ fontWeight: 600 }}>{formatDate(c.date)}</span>}
              {c.date && c.startTime && " at "}
              {c.startTime} · {c.courts.join(", ")}
            </div>
            <div className="viewer__title">{c.name}</div>
            <div className="viewer__sub">{competitionKindLabel(c)}</div>
          </div>
          {/* Suppress the "League" badge during pools: the active tab already
              reads "League", so the badge is pure redundancy. Other statuses
              (Playoffs / Completed) still carry information, so keep those. */}
          {!(c.status === "pools" && c.format === "league") && (
            <StatusBadge status={c.status} showRunningDot format={c.format} />
          )}
          {authed && onEditCompetition && (
            <button type="button" className="viewer__admin-pill" onClick={() => onEditCompetition(c.id)}>✎ Edit</button>
          )}
        </div>
        <div className="viewer__tabs">
          {tabs.map((tb) => (
            <button type="button" key={tb.id} className={`viewer__tab ${effectiveTab === tb.id ? "is-active" : ""}`} onClick={() => selectTab(tb.id)}>
              {tb.label}
            </button>
          ))}
        </div>
        {hasActiveFilter && (
          <div className="viewer__filter-bar">
            <span className="viewer__filter-label">Filter:</span>
            {compWatchlist.map((entry) => {
              const k = entryKey(entry);
              if (entry.type === "dojo") {
                return (
                  <span key={k} className="pmf__chip pmf__chip--dojo">
                    <span className="pmf__chip-icon" aria-hidden="true">⌂</span>
                    {entry.dojo}
                    <button type="button" onClick={() => { setWatchlist(prev => prev.filter((e) => entryKey(e) !== k)); if (primaryKey === k) setPrimaryKey(""); }} aria-label={`Remove ${entry.dojo}`}>×</button>
                  </span>
                );
              }
              const pRecord = rosterById.get(entry.id);
              const name = (pRecord && pRecord.name) || entry.name || "(unknown)";
              const number = pRecord?.number || "";
              return (
                <span key={k} className="pmf__chip">
                  {number && <span className="num-prefix">{number}</span>}
                  {name}
                  <button type="button" onClick={() => { setWatchlist(prev => prev.filter((e) => entryKey(e) !== k)); if (primaryKey === k) setPrimaryKey(""); }} aria-label={`Remove ${name}`}>×</button>
                </span>
              );
            })}
            {compWatchlist.length > 1 && (
              <button type="button" className="viewer__filter-clear" onClick={() => { const ks = new Set(compWatchlist.map(entryKey)); setWatchlist(prev => prev.filter((e) => !ks.has(entryKey(e)))); if (ks.has(primaryKey)) setPrimaryKey(""); }}>Clear all</button>
            )}
          </div>
        )}
        <div className="viewer__body">
          {effectiveTab === "overview" && (
            <ViewerOverview
              c={c}
              myPlayer={myPlayer}
              myUpcoming={myUpcoming}
              currentMatch={currentMatch}
              runningMatches={runningMatches}
              upcomingMatches={upcomingMatches}
              recentMatches={recentMatches}
              tweaks={tweaks}
              tournament={tournament}
              compId={c.id}
              standings={standings}
              pools={pools}
              poolMatches={poolMatches}
              onSwitchTab={selectTab}
              hasActiveFilter={hasActiveFilter}
              filterLabel={filterLabel}
              highlightPlayers={highlightPlayers}
            />
          )}
          {effectiveTab === "bracket" && derivedBracket && (
            <div className={`viewer-bracket-bleed${bracketOverflowRight ? " viewer-bracket-bleed--overflow-right" : ""}`}>
              <div ref={bracketScrollRef} className="bracket-canvas" style={{ borderRadius: 0, borderLeft: 0, borderRight: 0 }}>
                <div className="bracket-canvas__inner" style={{ padding: 18 }}>
                  <window.BracketTree
                    rounds={derivedBracket.rounds}
                    variant={tweaks.cardVariant}
                    showDojo={tweaks.showDojo}
                    highlightedMatchId={currentMatch?.id}
                    autoScrollMatchId={bracketScrollTarget}
                    scrollContainerRef={bracketScrollRef}
                    highlightPlayers={highlightPlayers}
                    onMatchClick={(m, ri, _mi, total) => {
                      const label = window.roundLabel(ri, total ?? derivedBracket.rounds.length);
                      // m.roundIndex is the backend round array index, stamped by
                      // buildDisplayModel (meta mode) or the raw rounds[ri] position
                      // (legacy mode where ri equals the backend index). Prefer it
                      // over the display-column index so lineup fetches use the right
                      // round when phantom leading rounds shift the display column.
                      setSelectedMatch({ ...m, phase: "bracket", round: label, phaseName: label, roundIndex: m.roundIndex ?? ri, compId: c.id, compName: c.name, compKind: c.kind, teamSize: c.teamSize });
                    }}
                  />
                </div>
              </div>
            </div>
          )}
          {effectiveTab === "pools" && hasPools && (
            <PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches} tweaks={tweaks} competition={c} onMatchClick={setSelectedMatch} highlightPlayers={highlightPlayers} />
          )}
          {effectiveTab === "swiss" && isSwiss && (
            <SwissStandingsViewer competition={c} poolMatches={poolMatches} tweaks={tweaks} />
          )}
          {effectiveTab === "results" && c.status === "completed" && (
            // Pass the *real* server bracket (not derivedBracket): the latter
            // is a TBD placeholder for visualization only and carries no
            // winner data. Using real server data ensures deriveAwards sees
            // actual winners; when the final has no winner yet, deriveAwards
            // explicitly falls through to the standings-based path rather
            // than short-circuiting.
            <AwardsView c={c} bracket={bracket} standings={standings} pools={pools} players={c.players} />
          )}
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} compId={c.id} />}
    </div>
  );
}

export function ViewerOverview({ c, myPlayer, myUpcoming, currentMatch, runningMatches, upcomingMatches, recentMatches, tweaks, tournament, compId, standings, pools, poolMatches, onSwitchTab, hasActiveFilter, filterLabel, highlightPlayers }) {
  const [expandedMatchId, setExpandedMatchId] = useState(null);
  const [selectedMatch, setSelectedMatch] = useState(null);
  const isSelfRun = tournament && tournament.mode === "self-run";

  const isLeague = c.format === "league";
  const isTeam = c.kind === "team" || c.teamSize > 0;
  const leaguePoolName = isLeague && pools && pools[0] ? pools[0].poolName : null;
  const leagueStandings = leaguePoolName && standings ? (standings[leaguePoolName] || []) : [];
  const allMatchesComplete = isLeague && (() => {
    const all = poolMatches || [];
    return all.length > 0 && all.every(m => m.status === "completed");
  })();
  const leagueWinner = allMatchesComplete && leagueStandings.length > 0 ? leagueStandings[0] : null;

  // setup: no draw yet: plain "not started" message.
  if (!c.status || c.status === "setup") {
    return (
      <EmptyState icon="⏳" title="Not started yet" message={`Starts at ${c.startTime}. Check back when the competition begins.`} style={{ padding: 32 }} />
    );
  }

  // draw-ready: the draw is published but no match has been called yet.
  // The comp is not running, so the Overview has no running/recent matches to
  // show: point spectators to the now-available tabs. Swiss comps render a
  // Standings tab instead of Pools/Bracket (same isSwiss signal as the tab
  // logic in ViewerCompetition), so the pointer text must match.
  if (c.status === "draw-ready") {
    let tabHint = "Browse the Pools and Bracket tabs to see the draw.";
    if (c.format === "swiss") tabHint = "Check the Standings tab to follow the rounds.";
    else if (isLeague) tabHint = "Browse the League tab to see the draw.";
    return (
      <EmptyState
        icon="📋"
        title="Draw is ready"
        message={`Starts at ${c.startTime}. ${tabHint}`}
        style={{ padding: 32 }}
      />
    );
  }

  const handleMatchClick = (m) => {
    if (isSelfRun) {
      setSelectedMatch(m);
    } else {
      setExpandedMatchId(prev => prev === m.id ? null : m.id);
    }
  };

  return (
    <div>
      {hasActiveFilter && (
        <div className="viewer-filter-bar">
          <span className="viewer-filter-bar__icon" aria-hidden="true">👤</span>
          <span>Showing matches for <strong>{filterLabel}</strong></span>
        </div>
      )}
      {myUpcoming && myPlayer ? (
        <div className="my-match">
          <div className="my-match__lbl">Your next match</div>
          <div className="my-match__name">{myPlayer.name}</div>
          <div className="my-match__round">
            {myUpcoming.phase === "pool" ? poolLabel(myUpcoming) : myUpcoming.round}
            {myUpcoming.status === "running" ? " · NOW" : ""}
          </div>
          <div className="my-match__row">
            <div className="my-match__chip">
              <span className="l">Court</span>
              <span className="v"><TermV name="shiaijo">Shiaijo</TermV> {myUpcoming.court}</span>
            </div>
            <div className="my-match__chip">
              <span className="l">Time</span>
              <span className="v">{myUpcoming.scheduledAt || "TBA"}</span>
            </div>
          </div>
          {(() => {
            const opp = isFollowedPlayer(myUpcoming.sideA, myPlayer) ? myUpcoming.sideB : myUpcoming.sideA;
            return opp ? (
              <div className="my-match__opp">
                <div className="l">vs Opponent</div>
                <div className="n">{opp.name}</div>
                {tweaks.showDojo ? <div className="d">{opp.dojo}</div> : null}
              </div>
            ) : null;
          })()}
        </div>
      ) : null}

      {/* League standings summary (mp-ldnr) */}
      {isLeague && leagueStandings.length > 0 && (c.status === "pools" || c.status === "playoffs" || c.status === "completed") && (
        <div className="pool pool--overview-summary" data-testid="league-overview-standings">
          {leagueWinner && <WinnerBadge name={leagueWinner.player?.name || ""} testId="league-overview-winner" marginBottom={12} />}
          <div className="pool__head">
            <div className="pool__name">{allMatchesComplete ? "Final standings" : "Standings"}</div>
            {poolMatches && (
              <div className="pool__match-count">
                {poolMatches.filter(m => m.status === "completed").length}/{poolMatches.length} matches
              </div>
            )}
          </div>
          <table className="pool__table">
            <thead>
              {isTeam ? (
                <tr><th>#</th><th>Team</th><th className="num">W</th><th className="num">L</th><th className="num">T</th><th className="num">IV</th><th className="num">IL</th><th className="num">PW</th><th className="num">PL</th></tr>
              ) : (
                <tr><th>#</th><th>Player</th><th className="num">W</th><th className="num">L</th><th className="num">D</th><th className="num">PW</th><th className="num">PL</th></tr>
              )}
            </thead>
            <tbody>
              {leagueStandings.slice(0, 5).map((s, i) => (
                <tr key={s.player?.id || s.player?.name || i} className={s.tied ? "pool__row--tied" : undefined}>
                  {/* Rank-ordered summary: "#" is the authoritative standing rank
                      (s.rank), not the row index: DRY with the full standings and
                      the backend tiebreak/override logic. */}
                  <td className={`pool-standings__draw-pos${s.isOverridden ? " pool-standings__draw-pos--override" : ""}`}>{s.rank || i + 1}{s.isOverridden ? "*" : ""}</td>
                  <td>
                    <div className="pool__player-name">
                      {s.player?.number ? <span className="num-prefix">{s.player.number}</span> : null}
                      {s.player?.name || ""}
                      {/* No rank badge here: this summary is rank-sorted, so the
                          "#" column already IS the rank: a badge would just echo
                          it. The rank badge only carries information when rows are
                          in draw order (non-league pools), where rank ≠ position. */}
                    </div>
                    {tweaks?.showDojo ? <div className="pool__dojo-name">{s.player?.dojo || ""}</div> : null}
                  </td>
                  <td className="num">{s.wins || 0}</td>
                  <td className="num">{s.losses || 0}</td>
                  <td className="num">{s.draws || 0}</td>
                  {isTeam && <td className="num">{s.individualWins || 0}</td>}
                  {isTeam && <td className="num">{s.individualLosses || 0}</td>}
                  <td className="num">{isTeam ? (s.pointsWon || 0) : (s.ipponsGiven || 0)}</td>
                  <td className="num">{isTeam ? (s.pointsLost || 0) : (s.ipponsTaken || 0)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {leagueStandings.length > 5 && (
            <div className="hint--sm pool__overflow-note">
              Showing top 5 of {leagueStandings.length}
            </div>
          )}
          {onSwitchTab && (
            <button type="button"
              className="btn btn--link pool__view-all-btn"
              onClick={() => onSwitchTab("pools")}
              data-testid="league-overview-view-all"
            >
              View full standings →
            </button>
          )}
        </div>
      )}

      {/* Current match: shown inline, before Up Next */}
      {currentMatch && currentMatch.status === "running" && (
        <div
          style={{ marginBottom: 12, cursor: isSelfRun ? "pointer" : undefined }}
          role={isSelfRun ? "button" : undefined}
          aria-label={isSelfRun ? "View current match details" : undefined}
          tabIndex={isSelfRun ? 0 : undefined}
          onClick={isSelfRun ? () => handleMatchClick(currentMatch) : undefined}
          onKeyDown={isSelfRun ? (e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              handleMatchClick(currentMatch);
            }
          } : undefined}
        >
          <div className="section-title section-title--running">
            <span className="dot dot--running"></span> ON NOW
          </div>
          <MatchDetailCard match={currentMatch} />
        </div>
      )}

      {/* Running matches beyond the single current match */}
      {runningMatches.length > 1 && (
        <>
          <div className="section-title section-title--running">
            <span className="dot dot--running"></span> NOW · {runningMatches.length}
          </div>
          <div className="vsched">
            {runningMatches.filter(m => !currentMatch || m.id !== currentMatch.id).map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isPlayerWatched(m.sideA, highlightPlayers) || isPlayerWatched(m.sideB, highlightPlayers)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* Up next */}
      {upcomingMatches.length > 0 && (
        <>
          <div className="section-title">Up next · {upcomingMatches.length}</div>
          <div className="vsched">
            {upcomingMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isPlayerWatched(m.sideA, highlightPlayers) || isPlayerWatched(m.sideB, highlightPlayers)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}

      {/* No running or upcoming */}
      {!currentMatch && upcomingMatches.length === 0 && runningMatches.length === 0 && (
        <EmptyState title="Nothing scheduled" style={{ padding: 20 }} />
      )}

      {recentMatches.length > 0 && (
        <>
          <div className="section-title">Recent results</div>
          <div className="vsched">
            {recentMatches.map((m) => (
              <React.Fragment key={m.id}>
                <VSchedItem m={m} tweaks={tweaks} onClick={() => handleMatchClick(m)} highlight={isPlayerWatched(m.sideA, highlightPlayers) || isPlayerWatched(m.sideB, highlightPlayers)} />
                {!isSelfRun && expandedMatchId === m.id && <MatchDetailCard match={m} onClose={() => setExpandedMatchId(null)} />}
              </React.Fragment>
            ))}
          </div>
        </>
      )}
      {isSelfRun && selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={tournament} compId={compId} />}
    </div>
  );
}

// window.ViewerCompetition is set in viewer.jsx (which re-exports this symbol
// and owns the full window.* assignment surface for backward compatibility).
