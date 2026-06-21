// ViewerHome — top-level home component + routing helpers.
// Extracted from viewer.jsx (mp-pxxc step 10).

import { competitionKindLabel, compMatches, tournamentMatches, TournamentInfo, compareDmy } from './viewer_utils.jsx';
import { matchParticipantIds, matchParticipantNames, addPlayerToWatchlist, resolveEntryPlayerIds, resolveWatchedPlayers, findPrimaryEntry, buildPrimaryNextMatch, buildRoster, useWatchlist } from './viewer_watchlist_core.jsx';
import { runOnce, notifEnable, notifDisable, useChimeMuted, isFollowedMatchOnDeck, useFollowedMatchAlert, useSecondaryWatchAlert, MyMatchAlertBanner } from './viewer_alerts.jsx';
import { notificationSupported } from './viewer_notifications.jsx';
import { VSchedItem, MatchViewerModal } from './viewer_match.jsx';
import { buildWatchlistUpcoming, usePrimaryWatch, WATCHED_UPCOMING_LIST_MAX } from './viewer_schedule.jsx';

const { useState, useMemo, useRef: useRefV, useEffect } = React;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const formatLabel = window.formatLabel;
const formatViewerHeaderEyebrow = window.formatViewerHeaderEyebrow;
const EmptyState = window.EmptyState;

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
// compareDmy (DD-MM-YYYY date comparator) is imported from viewer_utils.jsx.

const pluralize = window.pluralize;

// shouldShowRegister returns true when a "Register for this competition" button
// should be shown on a competition card. Extracted for unit testability (mp-e5j).
export function shouldShowRegister(tournament, competition, hasHandler) {
  return !!(hasHandler &&
    tournament && tournament.mode === "self-run" &&
    competition && competition.kind !== "team" &&
    (!competition.status || competition.status === "setup"));
}

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

export function ViewerHome({ tournament, onSelectCompetition, onAdminClick, onOpenSchedule, onRegister, onOpenResults, sseConnected = true }) {
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
          <button type="button" className={`viewer__admin-pill${comps.length === 0 ? " viewer__admin-pill--prominent" : ""}`} onClick={onAdminClick}>
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
            <button type="button" className="viewer-nav-card" onClick={onOpenSchedule}>
              <span className="viewer-nav-card__icon">🗓</span>
              <div className="viewer-nav-card__text">
                <div className="viewer-nav-card__title">Full schedule</div>
                <div className="viewer-nav-card__sub">{pluralize(bothSidesMatches.length, "match", "matches")} · {pluralize((tournament.courts || []).length, "court", "courts")}</div>
              </div>
              <span className="viewer-nav-card__chev">→</span>
            </button>

            {/* mp-koqh: Results summary — only shown when at least one comp has completed. */}
            {onOpenResults && completedCount > 0 && (
              <button type="button" className="viewer-nav-card" onClick={onOpenResults} data-testid="open-results-btn">
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
                <EmptyState
                  icon="⚙️"
                  title="No competitions yet"
                  message={<div className="hint--md">Head to Admin to set up the first competition.</div>}
                  cta={<button type="button" className="btn btn--primary empty__cta" onClick={onAdminClick}>Open admin</button>}
                  ctaNote="Requires the admin password."
                />
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
                      <button type="button" className="vlist-item vlist-item--comp" onClick={() => onSelectCompetition(c.id)}>
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
                          <button type="button"
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

// DisplayModes — viewer-home section linking to the public /display routes.
// Collapsed by default inside a <details> element. Contains one "all-courts
// overview" link plus two compact inline rows (court displays, streaming
// overlays) each listing per-court links inline rather than one card per
// court. Each link opens in a new tab so the operator's viewer session stays
// open. Lives in viewer_home.jsx (not display.jsx) because it is a viewer-side
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
