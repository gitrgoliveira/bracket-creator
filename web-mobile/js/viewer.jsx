// Viewer side — mobile-first. Single tournament. Shows competitions as the home;
// each competition opens to its own Overview/Bracket/Pools/Schedule/Results.

import { withNumber } from './match_scoreboard.jsx';
// Re-export the shared scoreboard primitives so existing tests that import them
// from '../viewer.jsx' keep working (the canonical defs now live in match_scoreboard.jsx).
export { BoutSubRow, boutHansokuMark } from './match_scoreboard.jsx';
import { TermV, competitionKindLabel, poolLabel, compMatches, tournamentMatches, currentMatchOf, linkBase, isNonPublicOrigin, TournamentInfo } from './viewer_utils.jsx';
import { matchParticipantIds, matchParticipantNames, isFollowedPlayer, isPlayerWatched, WATCHLIST_MAX, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, addPlayerToWatchlist, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, buildRoster, useWatchlist } from './viewer_watchlist_core.jsx';
import { NOTIF_SYNC_EVENT, runOnce, notifEnable, notifDisable, useChimeMuted, isFollowedMatchOnDeck, useFollowedMatchAlert, computeSecondaryAlert, useSecondaryWatchAlert, MyMatchAlertBanner } from './viewer_alerts.jsx';
import { notificationSupported, AnnBellBtn, AnnouncementCard, AnnouncementBanner } from './viewer_notifications.jsx';
import { mymatchQueueLabel, MatchDetailCard, VSchedItem, MatchViewerModal } from './viewer_match.jsx';
import { isSwissFinalStandings, swissStandingsHeading, WinnerBadge, SwissStandingsViewer, LeagueMatrix, PoolNumberedMatchRow, PoolsViewer } from './viewer_standings.jsx';
import { deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, AwardsView, FightingSpiritSection, buildAllWinnersPublic, AllWinnersView } from './viewer_awards.jsx';
import { PlayerMultiFilter, applyFilters, matchHighlightedBy, buildPlayerMatchHighlight, buildWatchlistUpcoming, ScheduleViewer, ViewerSchedule, TWMatch, usePrimaryWatch, LS_WATCH_PRIMARY, WATCHED_UPCOMING_MAX, WATCHED_UPCOMING_LIST_MAX } from './viewer_schedule.jsx';

const { useState, useMemo, useRef: useRefV, useEffect } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const formatLabel = window.formatLabel;
const formatViewerHeaderEyebrow = window.formatViewerHeaderEyebrow;

// shouldShowRegister returns true when a "Register for this competition" button
// should be shown on a competition card. Extracted for unit testability (mp-e5j).
function shouldShowRegister(tournament, competition, hasHandler) {
  return !!(hasHandler &&
    tournament && tournament.mode === "self-run" &&
    competition && competition.kind !== "team" &&
    (!competition.status || competition.status === "setup"));
}

// TermV — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.
// Canonical "match has both sides for real participants" predicate.
// Replaces the local `m.sideA && m.sideB` shorthand that treated the
// `{id:"",name:""}` placeholder from normalizeMatch as a real side —
// see admin_helpers.jsx for the full bug shape and rationale.
//
// Wrapped as a lazy callable rather than `const x = window.hasBothSides`
// because index.html loads viewer.js BEFORE admin_helpers.js (viewer
// is reachable pre-auth; admin helpers load later). At module-eval
// time, window.hasBothSides is undefined; by the time any React render
// runs, admin_helpers.js has executed and set the global, so deferring
// the lookup to call time is safe.
const hasBothSides = (m) => window.hasBothSides(m);
// Lazy callable for the same load-order reason as hasBothSides above.
// Canonical date format is DD-MM-YYYY, which doesn't lex-sort
// chronologically — use compareDmy as the sort comparator everywhere
// dates are ordered.
const compareDmy = (a, b) => window.compareDmy(a, b);

// competitionKindLabel, poolLabel, compMatches, tournamentMatches, currentMatchOf
// — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.

const pluralize = window.pluralize;
const isPoolDaihyosenID = id => id.includes('-DH-');
// window.poolLabel is set by viewer_utils.jsx at module-eval time (it loads first).

// --- Slice 4 helpers: "Find my matches" + Watchlist (FR-020 / FR-022 / FR-024) ---
// matchParticipantIds, matchParticipantNames, isFollowedPlayer, isPlayerWatched,
// WATCHLIST_MAX, entryKey, normalizeWatchlistEntry, normalizeWatchlist,
// migrateWatchlistOnLoad, addPlayerToWatchlist, resolveEntryPlayerIds,
// resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry,
// buildPrimaryNextMatch, buildRoster, useWatchlist
// — moved to viewer_watchlist_core.jsx (mp-pxxc step 2). Imported above.

// buildPlayerMatchHighlight — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// LS_WATCH_PRIMARY, WATCHED_UPCOMING_MAX, WATCHED_UPCOMING_LIST_MAX,
// usePrimaryWatch, buildWatchlistUpcoming
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// NOTIF_SYNC_EVENT, dispatchNotif, runOnce, notifEnable, notifDisable,
// useChimeMuted, isFollowedMatchOnDeck, useFollowedMatchAlert,
// computeSecondaryAlert, useSecondaryWatchAlert, MyMatchAlertBanner
// — moved to viewer_alerts.jsx (mp-pxxc step 3). Imported above.
// The export keyword is preserved via re-export in the export{} block below.

// _permSubscribed, _permGaveUp, subscribePermissionChanges
// — moved to viewer_notifications.jsx (mp-pxxc step 8). Imported above via AnnBellBtn.

// isHttpURL, linkBase, isNonPublicOrigin, TournamentInfo
// — moved to viewer_utils.jsx (mp-pxxc step 1). Imported above.
// The `export` keyword is preserved via re-export in the export{} block below.

// Pure helper: resolve a ?player= / ?playerNumber= / ?name= deep link against
// the participant roster. Resolution order:
//   1. ?player= as exact id (UUID) match
//   2. ?playerNumber= as exact number match (mp-yin4 tag QR)
//   3. ?name= (or ?player= as backward-compatible fallback) as case-insensitive
//      name substring — allows legacy links that used ?player=<name> to keep working
// Returns null when no participant matches, else { player: {id,name} }.
export function resolveDeepLink(searchString, roster) {
  const params = new URLSearchParams(searchString || "");
  const qpPlayer = (params.get("player") || "").trim();
  const qpNumber = (params.get("playerNumber") || "").trim();
  const qpName = (params.get("name") || "").trim();
  if (!qpPlayer && !qpNumber && !qpName) return null;
  let hit = qpPlayer ? roster.find((p) => p.id === qpPlayer) : null;
  if (!hit && qpNumber) {
    hit = roster.find((p) => (p.number || "") === qpNumber);
  }
  if (!hit) {
    const needle = (qpName || qpPlayer).toLowerCase();
    if (needle) hit = roster.find((p) => (p.name || "").toLowerCase().includes(needle));
  }
  if (!hit) return null;
  return { player: { id: hit.id, name: hit.name } };
}

function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule, onRegister, onOpenResults, sseConnected = true }) {
  // WatchlistPanel lives in viewer_watchlist.jsx; read it at render time so the
  // viewer.jsx <-> viewer_watchlist.jsx runtime cycle is load-order independent.
  // Fall back to a no-op if viewer_watchlist.js fails to load so the rest of
  // the page still renders rather than crashing on an invalid element type.
  const WatchlistPanel = window.WatchlistPanel ?? (() => null);
  const t = tournament;
  const comps = t.competitions || [];
  const completedCount = comps.filter((c) => c.status === "completed").length;
  const compsByDate = useMemo(() => {
    const map = {};
    comps.forEach((c) => {
      const d = c.date || t.date || "";
      if (!map[d]) map[d] = [];
      map[d].push(c);
    });
    return map;
  }, [comps, t.date]);
  const dates = Object.keys(compsByDate).sort(compareDmy);

  const displayEntriesByDate = useMemo(() => {
    const result = {};
    Object.keys(compsByDate).forEach(d => {
      result[d] = compsByDate[d].map(c => ({ kind: "single", comp: c }));
    });
    return result;
  }, [compsByDate]);

  const [courtFilter, setCourtFilter] = useState("all");
  const [selectedMatch, setSelectedMatch] = useState(null);

  // mp-xhaa: per-viewer personalisation is now a single unified watchlist of
  // up to 50 entities — individual players OR whole dojos. Exactly one entity
  // is "primary" (implicitly when there is one, or by an explicit pin when ≥2);
  // the primary gets the hero card and the loud on-deck chime. The legacy
  // single "followed player" is migrated into the list once (see useWatchlist).
  const [watchlist, setWatchlist] = useWatchlist();
  const [primaryKey, setPrimaryKey] = usePrimaryWatch();
  const roster = useMemo(() => buildRoster(t.competitions), [t.competitions]);

  // Add a single player to the watchlist (dedup by id). Used by the deep link.
  const addWatchPlayer = (p) => setWatchlist(prev => addPlayerToWatchlist(prev, p));

  // T114 / mp-xhaa: parse `?player=<uuid>` (and optionally `?name=<name>`) deep
  // links from QR codes exactly once. Adding to the watchlist is
  // non-destructive (unlike the old single-follow overwrite), so we just add
  // the resolved player — they become the implicit primary when they land as
  // the sole entry.
  const deepLinkApplied = useRefV(false);
  React.useEffect(() => {
    if (deepLinkApplied.current) return;
    if (typeof window === "undefined" || !window.location) return;
    if (roster.length === 0) return; // wait until participants are loaded
    const result = resolveDeepLink(window.location.search, roster);
    deepLinkApplied.current = true;
    if (result && result.player) addWatchPlayer(result.player);
  }, [roster, watchlist]);

  // global "across-all-competitions" lists for the home page
  const allMatches = useMemo(() => tournamentMatches(t), [t]);
  const bothSidesMatches = useMemo(() => allMatches.filter(hasBothSides), [allMatches]);
  // Running-comp set: gates the home NOW / Up-next strips and the running dot.
  // BOTH setup and draw-ready are excluded — a draw-ready comp has a published
  // draw but no match has been called, so it is NOT running. (The competition
  // detail view still shows its Pools/Bracket tabs; that is governed separately
  // by isPreStart in ViewerCompetition.)
  const runningCompIds = useMemo(() => new Set((t.competitions || []).filter(c => c.status && c.status !== "setup" && c.status !== "draw-ready").map(c => c.id)), [t.competitions]);
  // Apply hasBothSides here too — pre-fix, a bracket match marked
  // `running` while one side was still an unresolved "Winner of rX-mY"
  // placeholder would appear in the public NOW strip, even though
  // the upcoming list / cards / detail view all reject placeholder
  // sides. Mirrors the upNext filter below.
  const running = allMatches.filter((m) => m.status === "running" && hasBothSides(m) && runningCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  let upNext = allMatches.filter((m) => m.status === "scheduled" && hasBothSides(m) && runningCompIds.has(m.compId) && (courtFilter === "all" || m.court === courtFilter));
  if (courtFilter === "all") upNext = upNext.slice(0, 3);

  // mp-xhaa: resolve the watchlist to a flat player set (dojo entries expand to
  // current members), then derive the primary entity and its hero match.
  const resolvedWatched = useMemo(() => resolveWatchedPlayers(watchlist, roster), [watchlist, roster]);
  const primaryEntry = useMemo(() => findPrimaryEntry(watchlist, primaryKey), [watchlist, primaryKey]);
  const primaryIds = useMemo(() => new Set(resolveEntryPlayerIds(primaryEntry, roster)), [primaryEntry, roster]);
  const primaryNextMatch = useMemo(() => buildPrimaryNextMatch(primaryEntry, roster, bothSidesMatches), [primaryEntry, roster, bothSidesMatches]);

  // Compact list of running and upcoming watched matches — shown when ≥2 entities
  // are watched (coach multi-watch). Includes running matches so they can be
  // excluded from the global-NOW hero section (mp-42rg de-dup). Bounded so it
  // stays glanceable on a phone.
  const watchedUpcoming = useMemo(
    () => buildWatchlistUpcoming(resolvedWatched, bothSidesMatches, WATCHED_UPCOMING_LIST_MAX),
    [resolvedWatched, bothSidesMatches]
  );

  // mp-42rg: de-duplicate the global NOW section — exclude matches already
  // visible in the watched-upcoming list so the same card doesn't appear
  // twice on a phone viewport. When every running match is tracked, the
  // hero-running section disappears entirely (hybrid approach).
  const globalRunning = useMemo(() => {
    const watchedIds = new Set(watchedUpcoming.map((m) => `${m.compId}:${m.id}`));
    return running.filter((m) => !watchedIds.has(`${m.compId}:${m.id}`));
  }, [running, watchedUpcoming]);

  // On-deck matches for NON-primary watched players (the quiet, rate-limited
  // banner path). A match that involves the primary is handled by the loud
  // path, so it is excluded here.
  const secondaryOnDeck = useMemo(() => {
    if (resolvedWatched.length === 0) return [];
    const watchedIds = new Set(resolvedWatched.map((p) => p.id));
    const watchedNames = new Set(resolvedWatched.map((p) => (p.name || "").trim().toLowerCase()).filter(Boolean));
    return bothSidesMatches.filter((m) => {
      if (!isFollowedMatchOnDeck(m)) return false;
      const [a, b] = matchParticipantIds(m);
      if ((a && primaryIds.has(a)) || (b && primaryIds.has(b))) return false;
      if ((a && watchedIds.has(a)) || (b && watchedIds.has(b))) return true;
      if (watchedNames.size > 0) {
        const [aN, bN] = matchParticipantNames(m);
        if ((aN && watchedNames.has(aN.trim().toLowerCase())) || (bN && watchedNames.has(bN.trim().toLowerCase()))) return true;
      }
      return false;
    });
  }, [bothSidesMatches, resolvedWatched, primaryIds]);

  // mp-xhaa: primary loud alert (chime + title flash + banner) + secondary
  // quiet, rate-limited banner.
  const [chimeMuted, toggleChimeMuted, setChimeMuted] = useChimeMuted();
  const bellToggleInFlight = useRefV(false);
  // Bell button: toggle chime and keep browser-notification opt-in in sync.
  const handleBellToggle = () => runOnce(bellToggleInFlight, async () => {
    const willEnable = chimeMuted; // currently muted → about to enable
    toggleChimeMuted(); // optimistic flip
    if (!willEnable) {
      // Muting: disable browser notifications too so they don't keep firing.
      // notifDisable() is a pure LS+event op; runs even when the Notification
      // API is absent (e.g. bare http) so the opt-in flag stays in sync.
      notifDisable();
      return;
    }
    if (!notificationSupported()) return; // enabling path requires the API
    // Delegate to notifEnable() which handles permission request, LS write, and
    // NOTIF_SYNC_EVENT dispatch. Revert the optimistic chime flip only when the
    // user dismissed the prompt or LS write failed ("off"); on "denied" the
    // chime stays enabled (chime-only mode) and on "on" everything is in sync.
    const outcome = await notifEnable();
    if (outcome === "off") {
      setChimeMuted(true); // revert optimistic flip — point-in-time snapshot avoids
      // a second toggle() call which could land on the wrong value if another
      // instance dispatched CHIME_SYNC_EVENT during the async permission dialog.
    }
  });
  const [alertMatch, setAlertMatch] = useState(null);
  const [alertDismissed, setAlertDismissed] = useState(false);
  const [secondaryAlert, setSecondaryAlert] = useState(null);
  const [secondaryDismissed, setSecondaryDismissed] = useState(false);
  // Reset dismissal when the primary or its match changes.
  useEffect(() => {
    setAlertDismissed(false);
  }, [primaryKey, primaryNextMatch && primaryNextMatch.id]);
  useFollowedMatchAlert(primaryNextMatch, {
    chimeMuted,
    onAlert: (m) => { setAlertMatch(m); setAlertDismissed(false); },
  });
  useSecondaryWatchAlert(secondaryOnDeck, {
    onSecondaryAlert: (m) => { setSecondaryAlert(m); setSecondaryDismissed(false); },
  });
  const showAlertBanner = alertMatch && !alertDismissed && isFollowedMatchOnDeck(primaryNextMatch);
  const showSecondaryBanner = secondaryAlert && !secondaryDismissed
    && secondaryOnDeck.some((m) => m.id === secondaryAlert.id);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head viewer__head--hero">
          <img src="/api/branding/logo" onError={(e) => { e.target.onerror = null; e.target.src = "/logo.jpeg"; }} alt="Tournament logo" className="topbar__logo viewer__logo" decoding="async" />
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">
              {formatViewerHeaderEyebrow(formatDate(t.date), t.venue)}
            </div>
            <div className="viewer__title viewer__title--lg">{t.name}</div>
          </div>
          <button className="viewer__admin-pill" onClick={onAdminClick}>
            <svg width="10" height="12" viewBox="0 0 10 12" fill="none" aria-hidden="true">
              <rect x="1" y="5" width="8" height="7" rx="1.5" fill="currentColor"/>
              <path d="M3 5V3.5a2 2 0 0 1 4 0V5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
            </svg>
            Admin
          </button>
        </div>

        <div className="viewer__body">
          {/* T063: SSE connection indicator, visible only when the connection drops. */}
          {!sseConnected && (
            <div className="sse-offline-banner" role="status" aria-live="polite">
              <span className="sse-offline-banner__dot" aria-hidden="true" />
              Connection interrupted. Reconnecting… Scores may be out of date.
            </div>
          )}
          <TournamentInfo tournament={t} />
          <div className="viewer__court-filter">
             <select className="input" value={courtFilter} onChange={(e) => setCourtFilter(e.target.value)}>
               <option value="all">All Shiaijo</option>
               {(t.courts || ["A"]).map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
             </select>
          </div>

          {/* mp-4fd / mp-xhaa: loud on-deck alert for the PRIMARY watched
              entity — chime + title flash already fired by the hook; this is
              the dismissible banner. */}
          {showAlertBanner && (
            <MyMatchAlertBanner
              match={alertMatch}
              onView={(m) => { setSelectedMatch(m); setAlertDismissed(true); }}
              onDismiss={() => setAlertDismissed(true)}
            />
          )}

          {/* mp-xhaa: QUIET banner for a non-primary watched player going
              on-deck — no chime, no title flash, rate-limited by the hook. */}
          {showSecondaryBanner && (
            <MyMatchAlertBanner
              match={secondaryAlert}
              onView={(m) => { setSelectedMatch(m); setSecondaryDismissed(true); }}
              onDismiss={() => setSecondaryDismissed(true)}
            />
          )}

          {/* mp-xhaa: unified Watchlist panel. Rendered up top so a competitor
              or coach who opens the viewer mid-tournament lands on their next
              fight without scrolling past the competition list. Absorbs the
              former "Find my matches" hero + the multi-player watchlist. */}
          <WatchlistPanel
            roster={roster}
            watchlist={watchlist}
            setWatchlist={setWatchlist}
            primaryKey={primaryKey}
            setPrimaryKey={setPrimaryKey}
            primaryEntry={primaryEntry}
            primaryNextMatch={primaryNextMatch}
            upcoming={watchedUpcoming}
            onMatchClick={setSelectedMatch}
            chimeMuted={chimeMuted}
            onBellToggle={handleBellToggle}
            onFirstAdd={chimeMuted ? handleBellToggle : undefined}
          />

          {globalRunning.length > 0 && (
            <div className="hero-running">
              <div className="hero-running__lbl"><span className="dot dot--running"></span> NOW · {pluralize(globalRunning.length, "match", "matches")}</div>
              <div className="vsched hero-running__vsched">
                {globalRunning.slice(0, 3).map((m) => <VSchedItem key={`${m.compId}:${m.id}`} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
              </div>
            </div>
          )}

          <div className="viewer-nav-row">
            <button className="viewer-nav-card" onClick={onOpenSchedule}>
              <span className="viewer-nav-card__icon">🗓</span>
              <div className="viewer-nav-card__text">
                <div className="viewer-nav-card__title">Full schedule</div>
                <div className="viewer-nav-card__sub">{pluralize(bothSidesMatches.length, "match", "matches")} · {pluralize((tournament.courts || []).length, "court", "courts")}</div>
              </div>
              <span className="viewer-nav-card__chev">→</span>
            </button>

            {/* mp-koqh: Results summary — only shown when at least one comp has completed. */}
            {onOpenResults && completedCount > 0 && (
              <button className="viewer-nav-card" onClick={onOpenResults} data-testid="open-results-btn">
                <span className="viewer-nav-card__icon">🏅</span>
                <div className="viewer-nav-card__text">
                  <div className="viewer-nav-card__title">Results</div>
                  <div className="viewer-nav-card__sub">All placings</div>
                </div>
                <span className="viewer__results-badge" aria-label={`${completedCount} completed`}>{completedCount}</span>
                <span className="viewer-nav-card__chev">→</span>
              </button>
            )}
          </div>

          {dates.length === 0 ? (
            <>
              <div className="section-title">Competitions</div>
              <div className="vlist">
                <div className="empty">
                  <div className="icon">⏳</div>
                  <h3>No competitions yet</h3>
                  <div className="hint--md">Check back soon for the tournament schedule and updates.</div>
                </div>
              </div>
            </>
          ) : dates.map((d) => (
            <div key={d}>
              <div className="section-title">{formatDate(d)}</div>
              <div className="vlist">
                {(displayEntriesByDate[d] || []).map((entry) => {
                  const c = entry.comp;
                  const matches = compMatches(c).filter(hasBothSides);
                  const total = matches.length;
                  const done = matches.filter((m) => m.status === "completed").length;
                  const runningCount = matches.filter((m) => m.status === "running").length;
                  const pct = total ? Math.round((done / total) * 100) : 0;
                  const showRegister = shouldShowRegister(t, c, !!onRegister);
                  return (
                    <div key={c.id} className="comp-item">
                      <button className="vlist-item vlist-item--comp" onClick={() => onSelectCompetition(c.id)}>
                        <div className="comp-item__header">
                          <div className="comp-item__body">
                            <div className="vlist-item__eyebrow">{competitionKindLabel(c)}{c.teamSize > 1 ? ` · ${c.teamSize}-person` : ""}</div>
                            <div className="vlist-item__name">{c.name}</div>
                            <div className="vlist-item__meta">
                              {c.players.length} {c.kind === "team" ? "teams" : "players"} · {formatLabel(c.format)} · Starts {c.startTime}
                            </div>
                          </div>
                          <StatusBadge status={c.status} showRunningDot format={c.format} />
                        </div>
                        {c.status && c.status !== "setup" && c.status !== "draw-ready" && total > 0 && (
                          <div className="vlist-item__progress">
                            <div className="vlist-item__bar"><div style={{ width: pct + "%" }}></div></div>
                            <div className="vlist-item__pct">
                              {runningCount > 0 ? <span className="bc-running-count">● {runningCount} now</span> : pluralize(done, "match", "matches") + " / " + total}
                            </div>
                          </div>
                        )}
                      </button>
                      {showRegister && (
                        <div className="vlist-item--row-padded">
                          <button
                            className="btn btn--primary btn--sm btn--full"
                            onClick={(e) => {
                              e.stopPropagation();
                              onRegister(c.id);
                            }}
                          >
                            Register for this competition
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          ))}

          {upNext.length > 0 && (
            <>
              <div className="section-title viewer__upnext-title">Up next · {upNext.length}</div>
              <div className="vsched">
                {upNext.map((m) => <VSchedItem key={`${m.compId}:${m.id}`} m={m} tweaks={{ showDojo: true }} showCompetition onClick={() => setSelectedMatch(m)} />)}
              </div>
            </>
          )}

          {/* mp-c38: sponsor logos. Hidden when none configured. */}
          {window.SponsorStrip && <window.SponsorStrip sponsors={t && t.sponsors} variant="viewer" />}

          {/* U1: link to the kendo glossary so volunteers (and
              spectators new to kendo) can browse the term register
              that the inline tooltips draw from. */}
          <div className="vlist viewer__glossary-vlist">
            <a
              className="vlist-item vlist-item--row"
              href="/glossary"
              onClick={(e) => {
                e.preventDefault();
                if (window.AppRouter && window.AppRouter.route) window.AppRouter.route("/glossary");
                else window.location.href = "/glossary";
              }}
            >
              <span className="vlist-item__icon">📖</span>
              <div className="vlist-item__rowbody">
                <div className="vlist-item__rowtitle">Kendo glossary</div>
                <div className="vlist-item__rowsub">Quick reference for scoring-table terms (Kiken, Hikiwake, Encho, etc.)</div>
              </div>
              <span className="vlist-item__rowchev">→</span>
            </a>
          </div>

          {/* T069 / FR-016: "Display modes" — links into the public
              /display routes for TV screens, lobby boards, and OBS browser
              sources. Each link opens in a new tab so the host page
              (viewer) keeps its tab and stays interactive. Collapsed by
              default (mp-mjaq) since most viewers are spectators; only
              tournament operators need these links. */}

          <DisplayModes tournament={t} />
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
      {selectedMatch && <MatchViewerModal match={selectedMatch} onClose={() => setSelectedMatch(null)} tournament={t} />}
    </div>
  );
}

// mymatchQueueLabel, subBoutLabel — moved to viewer_match.jsx (mp-pxxc step 4).
// Imported above; re-exported in the big export {} block below.

// DisplayModes — viewer-home section linking to the public /display routes.
// Collapsed by default inside a <details> element. Contains one "all-courts
// overview" link plus two compact inline rows (court displays, streaming
// overlays) each listing per-court links inline rather than one card per
// court. Each link opens in a new tab so the operator's viewer session stays
// open. Lives in viewer.jsx (not display.jsx) because it is a viewer-side
// surface that consumes the display routes rather than rendering them.
function DisplayModes({ tournament }) {
  const courts = (tournament && tournament.courts) || [];
  // No court list — render nothing rather than a confusing single "all"
  // card. The tournament setup flow guarantees ≥1 court in practice; this
  // guard exists for the very-early-bootstrap window before tournament
  // data has loaded fully.
  if (courts.length === 0) return null;
  return (
    <details className="viewer-display-modes">
      <summary className="section-title">Display modes</summary>
      <div className="vlist" data-testid="viewer-home-display-modes">
        <a
          className="vlist-item vlist-item--row"
          href="/display?court=all"
          target="_blank"
          rel="noopener noreferrer"
        >
          <span className="vlist-item__icon">🪟</span>
          <div className="vlist-item__rowbody">
            <div className="vlist-item__rowtitle">All courts overview</div>
            <div className="vlist-item__rowsub">Lobby grid showing every court at a glance · opens in a new tab</div>
          </div>
          <span className="vlist-item__rowchev">→</span>
        </a>
        {/* Per-court links consolidated into compact inline rows. */}
        {[
          { icon: "📺", title: "Court displays", suffix: "" },
          { icon: "🎥", title: "Streaming overlays", suffix: "&overlay=1" },
        ].map((row) => (
          <div key={row.title} className="vlist-item vlist-item--row vlist-item--static">
            <span className="vlist-item__icon">{row.icon}</span>
            <div className="vlist-item__rowbody">
              <div className="vlist-item__rowtitle">{row.title}</div>
              <div className="vlist-item__rowsub viewer-display-modes__row-links">
                {courts.map((cc, i) => (
                  <span key={cc}>
                    <a href={`/display?court=${encodeURIComponent(cc)}${row.suffix}`} target="_blank" rel="noopener noreferrer"
                      className="viewer-display-modes__link">Shiaijo {cc}</a>
                    {i < courts.length - 1 && <span aria-hidden="true" className="viewer-display-modes__sep">·</span>}
                  </span>
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>
    </details>
  );
}

// isSwissFinalStandings, swissStandingsHeading, WinnerBadge, SwissStandingsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.

function ViewerCompetition({ tournament, competition, pools, poolMatches, standings, bracket, onBack, authed, onEditCompetition, tweaks }) {
  const [tab, setTab] = useState("overview");
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
    // produced a jumbled "Recent results" list — sort by scheduledAt desc,
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
  // client-side placeholder fallback — just use what the server sends.
  const derivedBracket = useMemo(() => {
    if (bracket && bracket.rounds && bracket.rounds.length > 0) return bracket;
    return null;
  }, [bracket]);

  // draw-ready is NOT pre-start for the purposes of showing pool/bracket
  // structure — the draw has been generated and the payload already includes
  // pools + bracket data returned unconditionally by handlers_viewer.go.
  // Only setup (no draw yet) is treated as pre-start.
  const isPreStart = !c.status || c.status === "setup";
  const hasPools = !isPreStart && !!pools && pools.length > 0;
  const hasBracket = !isPreStart && !!derivedBracket && derivedBracket.rounds && derivedBracket.rounds.length > 0;
  // T192 (FR-050e): Swiss competitions surface a dedicated standings
  // tab in place of pools/bracket. The standings tab fetches its own
  // data via /swiss/standings (it's not part of the competition-detail
  // payload — see api_client.jsx).
  const isSwiss = c.format === "swiss";
  const isLeague = c.format === "league";
  const tabs = [
    { id: "overview", label: "Overview" },
    isSwiss ? { id: "swiss", label: "Standings" } : null,
    hasPools && !isSwiss ? { id: "pools", label: isLeague ? "League" : "Pools" } : null,
    hasBracket && !isSwiss ? { id: "bracket", label: "Bracket" } : null,
    c.status === "completed" ? { id: "results", label: "Awards" } : null,
  ].filter(Boolean);

  const currentMatch = useMemo(() => {
    if (runningMatches.length > 0) return runningMatches[0];
    return upcomingMatches[0] || null;
  }, [runningMatches, upcomingMatches]);

  const [bracketScrollTarget, setBracketScrollTarget] = useState(null);
  const bracketScrollRef = useRefV(null);
  const [selectedMatch, setSelectedMatch] = useState(null);
  const [bracketOverflowRight, setBracketOverflowRight] = useState(false);

  React.useEffect(() => {
    if (tab === "bracket" && currentMatch) {
      setBracketScrollTarget(currentMatch.id + "::" + Date.now());
    }
  }, [tab, currentMatch?.id]);

  const hasBracketEl = tab === "bracket" && !!derivedBracket;
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
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
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
            <button className="viewer__admin-pill" onClick={() => onEditCompetition(c.id)}>✎ Edit</button>
          )}
        </div>
        <div className="viewer__tabs">
          {tabs.map((tb) => (
            <button key={tb.id} className={`viewer__tab ${tab === tb.id ? "is-active" : ""}`} onClick={() => setTab(tb.id)}>
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
                    <button onClick={() => { setWatchlist(prev => prev.filter((e) => entryKey(e) !== k)); if (primaryKey === k) setPrimaryKey(""); }} aria-label={`Remove ${entry.dojo}`}>×</button>
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
                  <button onClick={() => { setWatchlist(prev => prev.filter((e) => entryKey(e) !== k)); if (primaryKey === k) setPrimaryKey(""); }} aria-label={`Remove ${name}`}>×</button>
                </span>
              );
            })}
            {compWatchlist.length > 1 && (
              <button className="viewer__filter-clear" onClick={() => { const ks = new Set(compWatchlist.map(entryKey)); setWatchlist(prev => prev.filter((e) => !ks.has(entryKey(e)))); if (ks.has(primaryKey)) setPrimaryKey(""); }}>Clear all</button>
            )}
          </div>
        )}
        <div className="viewer__body">
          {tab === "overview" && (
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
              onSwitchTab={setTab}
              hasActiveFilter={hasActiveFilter}
              filterLabel={filterLabel}
              highlightPlayers={highlightPlayers}
            />
          )}
          {tab === "bracket" && derivedBracket && (
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
          {tab === "pools" && hasPools && (
            <PoolsViewer pools={pools} standings={standings} poolMatches={poolMatches} tweaks={tweaks} competition={c} onMatchClick={setSelectedMatch} highlightPlayers={highlightPlayers} />
          )}
          {tab === "swiss" && isSwiss && (
            <SwissStandingsViewer competition={c} poolMatches={poolMatches} tweaks={tweaks} />
          )}
          {tab === "results" && c.status === "completed" && (
            // Pass the *real* server bracket (not derivedBracket) — the latter
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

// Inline match detail card — shown directly on the page (no modal needed).
// MatchDetailCard — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

function ViewerOverview({ c, myPlayer, myUpcoming, currentMatch, runningMatches, upcomingMatches, recentMatches, tweaks, tournament, compId, standings, pools, poolMatches, onSwitchTab, hasActiveFilter, filterLabel, highlightPlayers }) {
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

  // setup: no draw yet — plain "not started" message.
  if (!c.status || c.status === "setup") {
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">⏳</div>
        <h3>Not started yet</h3>
        <div style={{ fontSize: 13 }}>Starts at {c.startTime}. Check back when the competition begins.</div>
      </div>
    );
  }

  // draw-ready: the draw is published but no match has been called yet.
  // The comp is not running, so the Overview has no running/recent matches to
  // show — point spectators to the now-available tabs. Swiss comps render a
  // Standings tab instead of Pools/Bracket (same isSwiss signal as the tab
  // logic in ViewerCompetition), so the pointer text must match.
  if (c.status === "draw-ready") {
    const isSwiss = c.format === "swiss";
    return (
      <div className="empty" style={{ padding: 32 }}>
        <div className="icon">📋</div>
        <h3>Draw is ready</h3>
        <div style={{ fontSize: 13 }}>
          Starts at {c.startTime}. {isSwiss
            ? "Check the Standings tab to follow the rounds."
            : isLeague
            ? "Browse the League tab to see the draw."
            : "Browse the Pools and Bracket tabs to see the draw."}
        </div>
      </div>
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
                <tr key={s.player?.id || s.player?.name || i}>
                  {/* Rank-ordered summary: "#" is the authoritative standing rank
                      (s.rank), not the row index — DRY with the full standings and
                      the backend tiebreak/override logic. */}
                  <td className={`pool-standings__draw-pos${s.isOverridden ? " pool-standings__draw-pos--override" : ""}`}>{s.rank || i + 1}{s.isOverridden ? "*" : ""}</td>
                  <td>
                    <div className="pool__player-name">
                      {s.player?.number ? <span className="num-prefix">{s.player.number}</span> : null}
                      {s.player?.name || ""}
                      {/* No rank badge here: this summary is rank-sorted, so the
                          "#" column already IS the rank — a badge would just echo
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
            <button
              className="btn btn--link pool__view-all-btn"
              onClick={() => onSwitchTab("pools")}
              data-testid="league-overview-view-all"
            >
              View full standings →
            </button>
          )}
        </div>
      )}

      {/* Current match — shown inline, before Up Next */}
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
        <div className="empty" style={{ padding: 20 }}><h3>Nothing scheduled</h3></div>
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

// VSchedItem — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

// PoolMatchRow, LeagueMatrix, rankOrdinal, PoolNumberedMatchRow, PoolsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.


// PlayerMultiFilter, applyFilters, matchHighlightedBy
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

export { PlayerMultiFilter, applyFilters, matchHighlightedBy, competitionKindLabel, compMatches, tournamentMatches, currentMatchOf, buildPlayerMatchHighlight, buildWatchlistUpcoming, useWatchlist, entryKey, normalizeWatchlistEntry, normalizeWatchlist, migrateWatchlistOnLoad, resolveEntryPlayerIds, resolveWatchedPlayers, effectivePrimaryKey, findPrimaryEntry, buildPrimaryNextMatch, computeSecondaryAlert, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, isPlayerWatched, deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, buildRoster, MatchDetailCard, MatchViewerModal, AnnouncementCard, AnnouncementBanner, ViewerCompetition, ViewerOverview, ViewerHome, MyMatchAlertBanner, LeagueMatrix, PoolsViewer, PoolNumberedMatchRow, AwardsView, FightingSpiritSection, matchParticipantIds, TermV, VSchedItem, addPlayerToWatchlist, poolLabel, WATCHLIST_MAX };
// Re-export symbols moved to viewer_utils.jsx so the public export surface is unchanged (mp-pxxc step 1).
export { isHttpURL, linkBase, isNonPublicOrigin, TournamentInfo } from './viewer_utils.jsx';
// Re-export symbols moved to viewer_alerts.jsx so the public export surface is unchanged (mp-pxxc step 3).
export { NOTIF_SYNC_EVENT, notifEnable, notifDisable, isFollowedMatchOnDeck } from './viewer_alerts.jsx';
// Re-export symbols moved to viewer_notifications.jsx so the public export surface is unchanged (mp-pxxc step 8).
export { notificationSupported, AnnBellBtn } from './viewer_notifications.jsx';
// Re-export symbols moved to viewer_match.jsx so the public export surface is unchanged (mp-pxxc step 4).
// mymatchQueueLabel is imported above (used for window.mymatchQueueLabel); subBoutLabel has no local use.
export { subBoutLabel } from './viewer_match.jsx';
export { mymatchQueueLabel };
// isSwissFinalStandings, swissStandingsHeading, WinnerBadge (private), SwissStandingsViewer,
// PoolMatchRow (private), LeagueMatrix, PoolNumberedMatchRow, PoolsViewer
// — moved to viewer_standings.jsx (mp-pxxc step 5). Imported above.
// PlayerMultiFilter, applyFilters, matchHighlightedBy, buildPlayerMatchHighlight,
// buildWatchlistUpcoming, ScheduleViewer, ViewerSchedule, TWMatch
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above; window.* set there.

// window.PlayerMultiFilter, window.applyFilters, window.matchHighlightedBy,
// window.buildPlayerMatchHighlight, window.buildWatchlistUpcoming,
// window.ViewerSchedule, window.ScheduleViewer
// — set in viewer_schedule.jsx (mp-pxxc step 7).
if (typeof window !== 'undefined') {
    window.deriveAwards = deriveAwards;
    window.bracketHasDecidedFinal = bracketHasDecidedFinal;
    window.resolveCompetitionAwards = resolveCompetitionAwards;
}

// deriveAwards, bracketHasDecidedFinal, resolveCompetitionAwards, AwardsView,
// FightingSpiritSection — moved to viewer_awards.jsx (mp-pxxc step 6). Imported above.

// ViewerSchedule, ScheduleViewer, TWMatch
// — moved to viewer_schedule.jsx (mp-pxxc step 7). Imported above.

// MatchViewerModal — moved to viewer_match.jsx (mp-pxxc step 4). Imported above.

// notificationSupported, formatAnnouncementTimeLeft, AnnBellBtn,
// AnnouncementCard, AnnouncementBanner
// — moved to viewer_notifications.jsx (mp-pxxc step 8). Imported above.
// window.AnnouncementCard and window.AnnouncementBanner are set in viewer_notifications.jsx.

// buildAllWinnersPublic, AllWinnersView — moved to viewer_awards.jsx (mp-pxxc step 6). Imported above.

window.ViewerHome = ViewerHome;
window.ViewerCompetition = ViewerCompetition;
window.isFollowedPlayer = isFollowedPlayer;
window.isPlayerWatched = isPlayerWatched;
window.ViewerSchedule = ViewerSchedule;
window.ScheduleViewer = ScheduleViewer;
window.SwissStandingsViewer = SwissStandingsViewer;
window.competitionKindLabel = competitionKindLabel;
window.compMatches = compMatches;
window.tournamentMatches = tournamentMatches;
window.currentMatchOf = currentMatchOf;
// mp-s1gl: expose link-base helpers for admin_shell.jsx / admin_schedule.jsx
// (those files don't ES-import viewer.jsx; they pick globals off window).
window.linkBase = linkBase;
window.isNonPublicOrigin = isNonPublicOrigin;
// mp-koqh: public results page.
window.buildAllWinnersPublic = buildAllWinnersPublic;
window.AllWinnersView = AllWinnersView;
// Reused read-only on the shiaijo operator console (pool standings + results).
window.PoolsViewer = PoolsViewer;

// Helpers consumed by viewer_watchlist.jsx (WatchPicker/WatchHeroCard/
// WatchlistPanel) at render time. poolLabel is already exposed above.
window.matchParticipantIds = matchParticipantIds;
window.addPlayerToWatchlist = addPlayerToWatchlist;
window.effectivePrimaryKey = effectivePrimaryKey;
window.entryKey = entryKey;
window.resolveEntryPlayerIds = resolveEntryPlayerIds;
window.mymatchQueueLabel = mymatchQueueLabel;
window.TermV = TermV;
window.VSchedItem = VSchedItem;
window.WATCHLIST_MAX = WATCHLIST_MAX;

export { shouldShowRegister };

