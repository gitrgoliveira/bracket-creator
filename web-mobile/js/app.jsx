// Main App — single tournament per app/url. Tournament has multiple Competitions
// (Men's Individual, Women's Individual, Teams, etc.). Auth gates admin mode.

import { applyPatch as patchCompetitionData } from './patch.jsx';
import { setCachedAuthConfig } from './admin_helpers.jsx';

const { useState: useS, useEffect: useE, useRef: useR, useCallback: useC } = React;

// preact-router wrapper from router.jsx (T005). Used for URL → state
// synchronisation. The render path itself still drives off
// `mode` / `viewerScreen` / `viewerCompId` / `adminView` state because
// those carry richer information than path components alone (e.g., the
// admin section sub-tab); the Router only hydrates and updates that
// state from the URL.
const AppRouter = window.AppRouter || null;

const THEME = {
  "accentColor": "#1d3557",
  "showDojo": true,
  "cardVariant": 1
};

// Pure helper: parse the current pathname into the App's view state.
// Extracted so it remains unit-testable; previously inlined as a
// closure in App() which prevented both reuse and isolated testing.
function parsePath(path) {
    if (path.startsWith("/admin")) {
      const parts = path.split("/").filter(Boolean);
      if (parts.length === 1) return { mode: "admin", admin: { kind: "dashboard" } };
      if (parts[1] === "schedule") return { mode: "admin", admin: { kind: "schedule" } };
      if (parts[1] === "score-editor") return { mode: "admin", admin: { kind: "scoreEditor" } };
      if (parts[1] === "import") return { mode: "admin", admin: { kind: "import" } };
      if (parts[1] === "edit-tournament") return { mode: "admin", admin: { kind: "editTournament" } };
      if (parts[1] === "create-competition") return { mode: "admin", admin: { kind: "createComp" } };
      if (parts[1] === "competition" && parts[2]) {
        return { mode: "admin", admin: { kind: "competition", id: parts[2], section: parts[3] || "overview" } };
      }
      return { mode: "admin", admin: { kind: "dashboard" } };
    }
    // T060: `/display` family — public, no-auth, read-only consumer
    // surfaces (TV / lobby / overlay). Surface the mode here so App()
    // can short-circuit the normal viewer/admin render path. Query
    // params (?court=, ?overlay=, ?position=) are read inside
    // DisplayRoute via useQuery() — they intentionally don't shape the
    // mode token because changing query params shouldn't remount the
    // display surfaces (the SSE-driven tournament state stays alive).
    if (path === "/display" || path.startsWith("/display?")) {
      return { mode: "display" };
    }
    if (path.startsWith("/competition/")) {
      const id = path.split("/")[2];
      return { mode: "viewer", viewerCompId: id };
    }
    if (path === "/schedule") {
      return { mode: "viewer", viewerScreen: "schedule" };
    }
    // U1: /glossary — the kendo-term reference page (rendered by
    // GlossaryPage from glossary.jsx). Public, no auth required;
    // linked from viewer home as "Help / Glossary".
    if (path === "/glossary") {
      return { mode: "viewer", viewerScreen: "glossary" };
    }
    // /reset — public password-reset surface backed by
    // POST /api/tournament/reset (handlers_reset.go). Available in
    // file mode; renders an "operator-disabled" message in locked
    // mode (the SPA learns the mode via GET /api/auth-config on App
    // mount and the backing API also 404s defensively).
    if (path === "/reset") {
      return { mode: "viewer", viewerScreen: "reset" };
    }
    if (path.startsWith("/register/")) {
      const compId = path.split("/")[2] || "";
      if (compId) return { mode: "viewer", viewerScreen: "register", viewerCompId: compId };
    }
    return { mode: "viewer", viewerScreen: "home" };
}

// Pure helper: render the App's view state back into a URL pathname.
function pathFromState(m, vs, vcid, av) {
    if (m === "admin") {
      if (av.kind === "dashboard") return "/admin";
      if (av.kind === "schedule") return "/admin/schedule";
      if (av.kind === "scoreEditor") return "/admin/score-editor";
      if (av.kind === "import") return "/admin/import";
      if (av.kind === "editTournament") return "/admin/edit-tournament";
      if (av.kind === "createComp") return "/admin/create-competition";
      if (av.kind === "competition") {
        let url = `/admin/competition/${av.id}`;
        if (av.section && av.section !== "overview") url += `/${av.section}`;
        return url;
      }
      return "/admin";
    }
    if (vs === "register" && vcid) return `/register/${vcid}`;
    if (vcid) return `/competition/${vcid}`;
    if (vs === "schedule") return "/schedule";
    if (vs === "glossary") return "/glossary";
    if (vs === "reset") return "/reset";
    return "/";
}

// ErrorBoundary — Preact Components support componentDidCatch via the
// preact/compat layer (window.React above is aliased to preactCompat).
// On caught render exception we render a recoverable banner with a
// reload button instead of letting the whole tree go blank. Per NFR-008.
class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }
  componentDidCatch(error) {
    console.error("App crashed:", error);
    this.setState({ error });
  }
  render() {
    if (this.state.error) {
      return React.createElement('div', { className: 'page', 'data-testid': 'error-boundary-banner', style: { padding: 24 } },
        React.createElement('div', { className: 'card card--pad-lg' },
          React.createElement('h2', null, 'Something went wrong'),
          React.createElement('p', { style: { color: 'var(--ink-3)', marginBottom: 16 } },
            'The app hit an unexpected error. Reload to try again.'),
          React.createElement('pre', {
            style: { background: 'var(--bg-2)', padding: 12, borderRadius: 6, overflow: 'auto', fontSize: 12, marginBottom: 16 }
          }, String(this.state.error?.message || this.state.error)),
          React.createElement('button', {
            className: 'btn btn--primary',
            onClick: () => window.location.reload()
          }, 'Reload')
        )
      );
    }
    return this.props.children;
  }
}

// mp-4fd: Generic notification helper. Guards: Notification API available +
// permission granted + localStorage opt-in on. The document.hidden check is
// intentionally NOT here — callers gate on it when needed (announcements) but
// the match-alert path fires even when the tab is focused (the banner + title
// provide the in-tab surface; the Notification is for backgrounded tabs).
// Exported for unit testing. NOT pure — side-effecting.
export function fireNotification(title, body, { tag } = {}) {
  if (typeof Notification === "undefined") return;
  if (Notification.permission !== "granted") return;
  let enabled = false;
  try {
    enabled = window.localStorage.getItem("viewer.notifications.enabled") === "true";
  } catch (_e) { /* storage unavailable */ }
  if (!enabled) return;
  try {
    new Notification(title, {
      tag: tag || "",
      body: body || "",
      icon: "/favicon.jpeg",
    });
  } catch (_e) { /* Notification constructor can throw in some envs */ }
}

// mp-cw1: Fire browser Notification for each newly-added announcement.
// Guards: Notification API available + permission granted + document hidden
// + localStorage toggle on. Uses tag:id to coalesce duplicate fires.
// NOT pure — reads Notification.permission, document.hidden and localStorage,
// and constructs Notification instances. Exported for unit testing (tests stub
// those globals). Callers must treat it as side-effecting.
export function fireBrowserNotifications(additions) {
  if (!document.hidden) return;
  for (const a of additions) {
    if (!a || !a.id) continue;
    fireNotification("Tournament Announcement", a.message || "", { tag: a.id });
  }
}

// mp-cw1: Given a ref holding the seen-ID Set (or null if not yet seeded) and
// a new full-list announcement snapshot, returns the additions that should
// fire a notification and updates the ref's Set in place.
//
// The FIRST call with an unseeded (null) ref is always treated as the SEED:
// every ID is recorded and NO additions are returned. The intended caller flow:
//   1. fetchAnnouncements() HTTP success → first call → seeds from HTTP response.
//   2. SSE snapshots that arrive before the HTTP seed → buffered externally in
//      pendingSseAnnouncements; replayed (second call) AFTER step 1 to detect
//      announcements added in the race window.
//   3. fetchAnnouncements() HTTP failure + buffered SSE → first call → seeds
//      from SSE (no notifications for pre-fetch announcements).
//   4. fetchAnnouncements() HTTP failure + nothing buffered → ref stays null;
//      first subsequent SSE call seeds (original fallback).
// Dismiss/clear snapshots (shrinking lists) never produce additions because
// their remaining IDs are all already seen.
export function diffAnnouncementSnapshot(seenRef, list) {
  const arr = Array.isArray(list) ? list : [];
  if (!seenRef.current) {
    seenRef.current = new Set();
    arr.forEach(a => { if (a && a.id) seenRef.current.add(a.id); });
    return []; // first snapshot is the seed — fire nothing
  }
  const seen = seenRef.current;
  const additions = arr.filter(a => a && a.id && !seen.has(a.id));
  // Mark ALL current IDs as seen (additions + existing) so a later snapshot
  // that still contains the same IDs won't re-fire.
  arr.forEach(a => { if (a && a.id) seen.add(a.id); });
  return additions;
}

function App() {
  const [tournament, setTournament] = useS(null);
  const [loading, setLoading] = useS(true);
  const [announcements, setAnnouncements] = useS([]);
  // Hydrate the route state from the URL synchronously, BEFORE the first
  // render. The post-mount useEffect that previously did this ran AFTER
  // the URL-sync effect, so a direct load of /reset (or any non-`/`
  // path) saw the URL get overwritten back to `/` on first paint
  // because pathFromState() read the default state.
  //
  // The IIFE runs on EVERY render (JavaScript evaluates all arguments
  // eagerly), but React only uses the return value on the FIRST render —
  // subsequent renders use the current state value and ignore the
  // initial-state argument entirely. So parsePath is called repeatedly
  // but its result is only meaningful once. If this ever becomes a
  // performance concern, convert to the lazy-initializer function form:
  // useState(() => parsePath(window.location.pathname))
  const initialRoute = (() => {
    try {
      return parsePath(window.location.pathname);
    } catch {
      return { mode: "viewer", viewerScreen: "home" };
    }
  })();
  // mode: viewer | admin | display.
  // "display" is the public /display family — read-only TV / lobby /
  // OBS overlay surfaces. We track it here (rather than as a sub-state
  // of viewer) so App() can short-circuit the viewer/admin auth/render
  // logic entirely; the display surfaces require no auth and don't
  // touch viewerCompId / viewerScreen.
  const [mode, setMode] = useS(initialRoute.mode || "viewer");
  // Wrap localStorage reads in try/catch so private-browsing / enterprise
  // policy environments that throw on getItem don't crash the app on mount.
  // The write path (persistence effect below) already has its own try/catch.
  const [authed, setAuthed] = useS(() => {
    try { return localStorage.getItem("bc_authed") === "true"; } catch { return false; }
  });
  // authedRef mirrors `authed` so the SSE handler — created once per
  // viewerCompId/mode change — observes the current auth state instead
  // of the closure-captured value from when it was attached. Without
  // the ref, a user who lands on `/admin`, signs in, and stays in
  // admin mode would keep the pre-login `authed=false` closure and
  // ignore later password_reset events. authedRef is updated in the
  // localStorage-persist effect below.
  const authedRef = useR(authed);
  const [password, setPassword] = useS(() => {
    try { return localStorage.getItem("bc_password") || ""; } catch { return ""; }
  });
  const [authPrompt, setAuthPrompt] = useS(false);
  const [viewerCompId, setViewerCompId] = useS(initialRoute.viewerCompId || null);
  const [viewerScreen, setViewerScreen] = useS(initialRoute.viewerScreen || "home"); // home | schedule | glossary | reset
  const [adminView, setAdminView] = useS(initialRoute.admin || { kind: "dashboard" });
  const [toast, setToast] = useS(null);
  // T063: SSE connection status, surfaced to display surfaces so the
  // TV / lobby / overlay can render a reconnect indicator during the
  // window between EventSource error and reconnect-onopen. The
  // EventSource itself lives inside subscribeToEvents; the second
  // callback hands status events up here.
  const [sseConnected, setSseConnected] = useS(true);
  // Per-tab client ID, generated once on mount. Used as the originator
  // identifier on password-reset POSTs so the SSE broadcast can be
  // ignored in the originating tab. Without this, the tab that just
  // submitted /reset receives its own password_reset event and the
  // handler immediately clears the localStorage credential the
  // ResetPasswordForm just wrote — kicking the operator who reset
  // straight back to the AuthModal. Survives only the tab lifetime;
  // each tab gets a fresh ID so two operators resetting from
  // different tabs both see the other's reset and log out as they
  // should.
  const clientIdRef = useR(null);
  if (!clientIdRef.current) {
    clientIdRef.current = (typeof crypto !== 'undefined' && crypto.randomUUID)
      ? crypto.randomUUID()
      : `c${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  }
  // mp-cw1: Unix-ms timestamp of when this viewer session mounted. Used to
  // distinguish pre-existing announcements (sentAt ≤ mount time) from
  // announcements created after the viewer opened the tab (sentAt > mount
  // time). Post-mount IDs are excluded from the HTTP seed so the buffered
  // SSE replay still fires notifications for them. Stored as a number so
  // the filter can use Date.parse() for correct cross-format comparison
  // (Go RFC3339Nano may omit fractional seconds; lexicographic string
  // comparison of "…Z" vs "….500Z" is incorrect — 'Z' > '.' in ASCII).
  const mountTimeRef = useR(Date.now());
  // Set of announcement IDs already seen by this session. Seeded by the
  // initial HTTP fetchAnnouncements response (pre-mount IDs only) so a
  // page-reload does NOT re-fire for already-active announcements, and an
  // announcement created in the mount→fetch race window still fires.
  const seenAnnouncementIds = useR(null); // lazily initialised to a Set below
  // Holds the latest SSE announcement snapshot received before the HTTP seed
  // has settled. Full snapshots are idempotent: only the latest matters.
  const pendingSseAnnouncements = useR(null);

  // Auth-mode discovery (file vs. locked). Fetched once on App mount
  // from GET /api/auth-config. Starts as null ("loading") so that
  // CreateTournament can gate its submit until the mode is known —
  // a locked-mode deployment would otherwise render as file-mode
  // and omit X-Tournament-Password on the bootstrap POST. The
  // useEffect below always resolves the null state (success or
  // fail-open) within one HTTP round-trip.
  const [authConfig, setAuthConfig] = useS(null);
  const authPromptRef = React.useRef(false);

  const showToast = (message, type = 'success') => {
    setToast({ message, type });
  };

  // Route state was hydrated synchronously by the useState initializers
  // above (see initialRoute). This effect now handles only the
  // side-effects of the initial route — surfacing the AuthModal when
  // the user deep-linked to /admin without being authenticated. State
  // sync to/from the URL is owned by initialRoute + the URL-sync
  // effect below.
  useE(() => {
    if (initialRoute.mode === "admin" && !authed) {
      setAuthPrompt(true);
    }
    // Mount-only side effect; intentional empty deps. The project's
    // ESLint config doesn't include react-hooks/exhaustive-deps, so
    // no disable directive is needed (and including one errored
    // because the rule isn't defined).
  }, []);

  // Sync state to URL whenever it changes. Uses the AppRouter.route()
  // helper (preact-router-backed) which mirrors history.pushState while
  // also notifying any mounted Routers — letting <Router>-driven
  // listeners react to programmatic navigation without a separate
  // popstate dispatch.
  useE(() => {
    // Display mode doesn't have an internal state machine — the URL is
    // already at /display?... and DisplayRoute reads the query params
    // on its own. Skip URL syncing here to avoid stripping the
    // query string (pathFromState only emits the path, not the query).
    if (mode === "display") return;
    const url = pathFromState(mode, viewerScreen, viewerCompId, adminView);
    if (window.location.pathname !== url) {
      if (AppRouter && AppRouter.route) {
        AppRouter.route(url);
      } else {
        history.pushState(null, "", url);
      }
    }
  }, [mode, viewerScreen, viewerCompId, adminView]);

  // The popstate handler is preserved as a fallback for back/forward
  // navigation. preact-router would also fire its own listeners on
  // history changes, but our App owns the routing-state-of-record so
  // we keep this explicit. (The previous implementation used the same
  // pattern; we did not remove it because the App's state machine is
  // richer than what's encodable in path components.)
  useE(() => {
    const handlePop = () => {
      const route = parsePath(window.location.pathname);
      if (route.mode === "admin") {
        setMode("admin");
        if (route.admin) setAdminView(route.admin);
      } else if (route.mode === "display") {
        setMode("display");
      } else {
        setMode("viewer");
        setViewerCompId(route.viewerCompId || null);
        setViewerScreen(route.viewerScreen || "home");
      }
    };
    window.addEventListener("popstate", handlePop);
    return () => window.removeEventListener("popstate", handlePop);
  }, []);

  useE(() => {
    // Guard localStorage writes with try/catch. In private-browsing
    // and storage-denied contexts (Safari ITP, Firefox strict mode,
    // certain enterprise policies) localStorage.setItem throws a
    // SecurityError or QuotaExceededError. Without the guard, a storage
    // failure here would propagate out of the effect and crash the React
    // tree. The credential is lost on page reload in those contexts, but
    // the session continues — the user just re-authenticates after
    // closing the tab (same experience as if cookies were blocked).
    try {
      localStorage.setItem("bc_authed", authed);
      localStorage.setItem("bc_password", password);
    } catch { /* storage unavailable — session only, no persistent credential */ }
    // Keep the ref in lockstep so the long-lived SSE handler below
    // can read the current value without being recreated on every
    // sign-in/sign-out.
    authedRef.current = authed;
  }, [authed, password]);

  useE(() => { document.documentElement.style.setProperty("--accent", THEME.accentColor); }, []);

  const load = async () => {
    try {
      const t = await window.API.fetchTournament();
      if (t && t.error) {
        setTournament(null);
      } else {
        const comps = await window.API.fetchCompetitions();
        t.competitions = comps;
        setTournament(t);
      }
    } catch (e) {
      console.error("Failed to load tournament", e);
    } finally {
      setLoading(false);
    }
  };

  useE(() => { load(); }, []);

  useE(() => {
    window.API.fetchAnnouncements()
      .then(list => {
        const active = filterActiveAnnouncements(list || []);
        setAnnouncements(active);
        // mp-cw1: Consume any SSE snapshot buffered before the HTTP seed arrived.
        const buffered = pendingSseAnnouncements.current;
        pendingSseAnnouncements.current = null;

        // Seed strategy depends on whether we have a buffered SSE to replay:
        //
        // • WITH buffered SSE: seed only pre-mount IDs (sentAt ≤ mountMs) so
        //   the replay below can fire notifications for post-mount IDs even
        //   when the HTTP GET happened to capture them. Date.parse() for numeric
        //   comparison — Go's RFC3339Nano may omit fractional seconds, making
        //   lexicographic comparison wrong ('Z' > '.' in ASCII).
        //   Missing/unparseable sentAt → treated as pre-existing (no spam risk).
        //
        // • WITHOUT buffered SSE: seed the full HTTP list. Excluding post-mount
        //   IDs with no SSE to replay would leave them unseeded, causing a later
        //   unrelated SSE snapshot to treat them as new and fire stale
        //   notifications for announcements the viewer already sees on screen.
        let seedList;
        if (buffered !== null) {
          seedList = (list || []).filter(a => {
            if (!a || !a.id) return false;
            if (!a.sentAt) return true;
            const ms = Date.parse(a.sentAt);
            return isNaN(ms) || ms <= mountTimeRef.current;
          });
        } else {
          seedList = list || [];
        }
        diffAnnouncementSnapshot(seenAnnouncementIds, seedList);

        // Replay the buffered SSE (if any) to fire for announcements added in
        // the mount→fetch race window.
        if (buffered !== null) {
          const additions = diffAnnouncementSnapshot(seenAnnouncementIds, buffered);
          if (additions.length > 0) {
            fireBrowserNotifications(additions);
          }
        }
      })
      .catch(err => {
        console.error("Failed to fetch initial announcements:", err);
        // Fall back: if an SSE snapshot was buffered while the HTTP request
        // was in flight, seed from it so the next SSE diff works correctly.
        // If nothing was buffered, leave the ref null so the first subsequent
        // SSE snapshot becomes the seed (original spam-prevention fallback).
        const buffered = pendingSseAnnouncements.current;
        pendingSseAnnouncements.current = null;
        if (buffered !== null && seenAnnouncementIds.current === null) {
          diffAnnouncementSnapshot(seenAnnouncementIds, buffered); // seed, no notifications
        }
      });
  }, []);

  // Fetch the auth-config once on mount. Always resolves the null
  // initial state — success sets the actual config; fail-open
  // (5xx/timeout/parse-error) falls back to file mode so the
  // historical UX is preserved for local deployments where /api/auth-config
  // may be unreachable for a brief moment at startup.
  useE(() => {
    const fileDefault = { mode: 'file', resetEnabled: true };
    window.API.fetchAuthConfig().then((cfg) => {
      setAuthConfig((cfg && typeof cfg === 'object') ? cfg : fileDefault);
    }).catch(() => {
      // Fail-open: unknown server errors default to file mode so the
      // recovery-via-/reset path stays accessible and sign-in works.
      setAuthConfig(fileDefault);
    });
  }, []);

  // Mirror authConfig into the admin_helpers cache so promptAdminPassword()
  // (spec 004) can read the elevated-password bits at destructive call sites
  // without prop-drilling authConfig through every admin component.
  useE(() => { setCachedAuthConfig(authConfig); }, [authConfig]);

  useE(() => {
    // Track every jittered timer so the cleanup can cancel them when
    // viewerCompId / mode changes. Without this, a timer queued for
    // viewerCompId="A" fires after the user switches to comp "B",
    // calls setSelectedCompData(data_for_A), and races the new
    // useEffect([viewerCompId]) fetch — whichever resolves last wins.
    const pendingTimers = [];
    const jitteredTimeout = (fn, delay) => {
        const id = setTimeout(fn, delay);
        pendingTimers.push(id);
        return id;
    };

    const unsub = window.API.subscribeToEvents((event) => {
        const jitter = Math.random() * 500;
        if (event.type === "tournament_updated") {
            if (!authPromptRef.current) jitteredTimeout(load, jitter);
        } else if (event.type === "password_reset") {
            // The admin password was rotated by someone hitting
            // POST /api/tournament/reset. Any logged-in admin's
            // localStorage credential is now stale — clear it so the
            // next request doesn't silently 401, and re-show
            // AuthModal so they notice immediately. The viewer flow
            // doesn't need to react: it never sends the header.
            //
            // Originator suppression: the tab that just submitted /reset
            // also receives this broadcast. If we react in that tab we'd
            // immediately clobber the new credential ResetPasswordForm
            // just wrote to localStorage. The form passes its clientId
            // as `originatorId`, the server echoes it on the event
            // payload, and we ignore matches here.
            if (event.data && event.data.originatorId && event.data.originatorId === clientIdRef.current) {
                return;
            }
            //
            // Read via authedRef instead of the closure-captured
            // `authed` so we see the CURRENT auth state. This effect's
            // deps are [viewerCompId, mode]; an `/admin` deep-link
            // where the user signs in without mode changing would
            // otherwise leave the handler holding `authed=false` and
            // miss the reset event entirely.
            if (authedRef.current) {
                setAuthed(false);
                setPassword("");
                try {
                    localStorage.removeItem("bc_authed");
                    localStorage.removeItem("bc_password");
                } catch {
                    // private-browsing modes can throw; the in-memory
                    // state above is the load-bearing fix anyway.
                }
                if (mode === "admin") {
                    setMode("viewer");
                    setAuthPrompt(true);
                    authPromptRef.current = true;
                }
            }
        } else if (event.type === "competitor_status_updated") {
            // T099: viewer doesn't need to mutate selectedCompData itself —
            // the eligibility change feeds back through the full
            // fetchCompetitionDetails refetch below. Route through
            // patchCompetitionData so the window-level CustomEvent fires
            // for any view that caches its own derived state.
            if (viewerCompId === event.data?.competitionId) {
                setSelectedCompData(prev => patchCompetitionData(prev, event));
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            jitteredTimeout(load, jitter);
        } else if (event.type === "competition_started" || event.type === "match_updated" || event.type === "competition_completed") {
            // Display mode (T060) reads the full tournament tree
            // (every competition's poolMatches + bracket) and needs a
            // refresh on every match update — without this, the TV
            // would stay frozen on the initial snapshot. We piggy-back
            // on the existing load() which re-fetches both /tournament
            // and /competitions, so the display sees the new match
            // state on the next render. The jittered refresh avoids
            // thundering the server when many displays are mounted on
            // the same venue LAN.
            if (mode === "display") {
                jitteredTimeout(load, jitter);
            } else if (viewerCompId === event.data.competitionId) {
                // Apply partial update immediately (match_updated only —
                // competition_completed has no per-match payload)
                if (event.type === "match_updated") {
                    setSelectedCompData(prev => patchCompetitionData(prev, event));
                }
                // Refresh current competition (jittered) — the backend has
                // already persisted the new status before broadcasting, so
                // this fetch deterministically picks up the transition.
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            // Also refresh tournament list for status updates
            jitteredTimeout(load, jitter);
        } else if (event.type === "schedule_updated") {
            // Court/time move: no competitionId in payload, so refresh the
            // currently selected competition (if any) and the tournament list.
            if (viewerCompId) {
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            jitteredTimeout(load, jitter);
        } else if (event.type === "draw_generated" || event.type === "draw_discarded") {
            // Draw generated/discarded: refresh the selected competition's
            // details (new pools/bracket data or cleared state) and the
            // tournament list so status badges update.
            if (viewerCompId === event.data?.competitionId) {
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            jitteredTimeout(load, jitter);
        } else if (event.type === "swiss_round_generated") {
            // T192 (US13 — FR-050d): a new Swiss round's matches have been
            // generated. The payload carries competitionId + swissCurrentRound
            // (see handlers_swiss.go) but the viewer needs the freshly-saved
            // poolMatches + the updated comp.swissCurrentRound on the
            // tournament list, so we refetch both. Mirrors the match_updated
            // path's jittered fetchCompetitionDetails + load pattern.
            if (mode === "display") {
                jitteredTimeout(load, jitter);
            } else if (viewerCompId === event.data?.competitionId) {
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            jitteredTimeout(load, jitter);
        } else if (event.type === "participants_updated") {
            // Check-in toggle: viewer-side badges (checked-in indicator) need
            // the updated player list. Refetch the selected competition when the
            // event targets it; also refresh the tournament list so participant
            // counts stay accurate.
            if (viewerCompId === event.data?.competitionId) {
                jitteredTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData).catch(err => console.error('SSE refresh failed:', err)), jitter);
            }
            jitteredTimeout(load, jitter);
        } else if (event.type === "announcement") {
            // Payload is now the full list snapshot.
            const list = Array.isArray(event.data) ? event.data : [];
            setAnnouncements(filterActiveAnnouncements(list));

            // mp-cw1: diff against seen IDs to fire browser notifications ONLY
            // for genuinely new announcements.
            // If the HTTP seed hasn't arrived yet, buffer this snapshot and let
            // fetchAnnouncements replay it — so an announcement added in the
            // mount→fetch race window still fires a notification (the HTTP seed
            // records only pre-existing IDs; the replay surfaces the new one).
            if (seenAnnouncementIds.current === null) {
              pendingSseAnnouncements.current = list; // keep latest; seed pending
            } else {
              const additions = diffAnnouncementSnapshot(seenAnnouncementIds, list);
              if (additions.length > 0) {
                fireBrowserNotifications(additions);
              }
            }
        }
    }, (status) => {
        // T063: track SSE connection status so /display surfaces can
        // render a reconnect indicator during disconnects.
        setSseConnected(status === 'open');
    });
    return () => { unsub(); pendingTimers.forEach(clearTimeout); };
  }, [viewerCompId, mode]);

  const [selectedCompData, setSelectedCompData] = useS(null);

  useE(() => {
    if (viewerCompId && viewerScreen !== "register") {
      let cancelled = false;
      setLoading(true);
      window.API.fetchCompetitionDetails(viewerCompId)
        .then(data => {
          if (cancelled) return;
          setSelectedCompData(data);
          setLoading(false);
        })
        .catch(err => {
          if (cancelled) return;
          console.error(err);
          setLoading(false);
        });
      return () => { cancelled = true; };
    } else {
      setSelectedCompData(null);
      setLoading(false);
    }
  }, [viewerCompId, viewerScreen]);

  const requestAdmin = () => {
    if (authed) {
      setMode("admin");
    } else {
      authPromptRef.current = true;
      setAuthPrompt(true);
    }
  };
  const onLogout = () => {
    setAuthed(false);
    setMode("viewer");
    setPassword("");
    localStorage.removeItem("bc_authed");
    localStorage.removeItem("bc_password");
  };

  // Stable dismiss callback — wrapping in useCallback prevents the
  // AnnouncementBanner countdown useEffect from re-running on every
  // parent render (the effect lists onDismiss as a dependency, so an
  // inline arrow function would reset the interval on every re-render).
  // Must be declared before any conditional returns to satisfy Rules of Hooks.
  // Viewer-side dismiss: hides announcement locally without calling server.
  const handleDismissAnnouncement = useC((id) => {
    try {
      sessionStorage.setItem(`bc_dismissed_announcement_${id}`, "true");
    } catch (_e) {
      // private-browsing modes can throw
    }
    setAnnouncements(prev => prev.filter(a => a.id !== id));
  }, []);

  if (loading && !selectedCompData) return <div className="loading">Loading...</div>;
  if (!tournament) return (
    <CreateTournament
      authConfig={authConfig}
      onCreated={(t, p) => {
        setTournament(t);
        setAuthed(true);
        setMode("admin");
        setPassword(p);
      }}
    />
  );

  // T060: /display family — public, read-only TV / lobby / overlay
  // surfaces. Short-circuit before viewer/admin so no auth prompt is
  // shown, no admin chrome leaks in, and the DisplayRoute owns its
  // own query-param routing (court=, overlay=, position=). The
  // tournament prop carries .competitions already (load() merges
  // them), and `connected` is the SSE-status boolean from T063.
  if (mode === "display") {
    const DisplayRoute = window.DisplayRoute;
    if (!DisplayRoute) {
      // Defensive: display.js bundle is part of the standard build, so
      // this should never fire in production. Render a minimal message
      // rather than a blank screen if it does.
      return <div className="loading" style={{ background: '#000', color: '#fff', padding: 40 }}>Display module not loaded.</div>;
    }
    return (
      <DisplayRoute
        tournament={tournament}
        competitions={tournament.competitions || []}
        connected={sseConnected}
      />
    );
  }

  if (mode === "admin" && authed) {
    return (
      <>
        <window.AdminApp
          tournament={tournament}
          onUpdate={setTournament}
          onLogout={onLogout}
          onViewerMode={() => setMode("viewer")}
          onPasswordChange={setPassword}
          tweaks={THEME}
          password={password}
          view={adminView}
          setView={setAdminView}
          showToast={showToast}
          authConfig={authConfig}
        />
        {toast && <window.Toast {...toast} onClose={() => setToast(null)} />}
      </>
    );
  }

  // viewer mode
  return (
    <>
      {announcements.length > 0 && window.AnnouncementBanner && (
        <window.AnnouncementBanner
          announcements={announcements}
          onDismiss={handleDismissAnnouncement}
        />
      )}
      {selectedCompData && viewerScreen !== "register" ? (
        <window.ViewerCompetition
          // key on the competition id so switching comps (notably the
          // mp-rrd pools<->playoffs cross-link, which calls
          // onSelectCompetition without unmounting) remounts the
          // component and resets its per-comp UI state (active tab,
          // open match modal, bracket scroll target). Otherwise a tab
          // that doesn't exist in the destination comp (e.g. "pools"
          // when navigating to a playoffs comp) would leave the body
          // rendering empty.
          key={selectedCompData.config.id}
          tournament={tournament}
          competition={selectedCompData.config}
          pools={selectedCompData.pools}
          poolMatches={selectedCompData.poolMatches}
          standings={selectedCompData.standings}
          bracket={selectedCompData.bracket}
          onBack={() => setViewerCompId(null)}
          onSelectCompetition={setViewerCompId}
          onAdminClick={requestAdmin}
          authed={authed}
          onEditCompetition={(id) => { setMode("admin"); setAdminView({ kind: "competition", id, section: "settings" }); }}
          tweaks={THEME}
        />
      ) : viewerScreen === "schedule" ? (
        <window.ViewerSchedule
          tournament={tournament}
          onBack={() => setViewerScreen("home")}
          tweaks={THEME}
        />
      ) : viewerScreen === "glossary" ? (
        // U1 /glossary: the kendo-term reference page. Lives in
        // glossary.jsx; we mount it through window.GlossaryPage so
        // the app.jsx render tree doesn't need a static import.
        window.GlossaryPage
          ? <window.GlossaryPage onBack={() => setViewerScreen("home")} />
          : <div className="loading">Loading glossary…</div>
      ) : viewerScreen === "reset" ? (
        // Password reset surface. Lives in reset.jsx; mounted through
        // window.ResetPasswordForm following the per-screen-file
        // convention. On success the user is auto-logged-in with the
        // new password (the form persists bc_password/bc_authed) and
        // we navigate to admin.
        window.ResetPasswordForm
          ? <window.ResetPasswordForm
              authConfig={authConfig}
              originatorId={clientIdRef.current}
              onBack={() => setViewerScreen("home")}
              onSuccess={(pw) => {
                setAuthed(true);
                setPassword(pw);
                setViewerScreen("home");
                setMode("admin");
              }}
            />
          : <div className="loading">Loading…</div>
      ) : viewerScreen === "register" ? (
        window.RegistrationForm
          ? <window.RegistrationForm
              compId={viewerCompId}
              onBack={() => {
                setViewerScreen("home");
                setViewerCompId(null);
              }}
            />
          : <div className="loading">Loading…</div>
      ) : (
        <window.ViewerHome
          tournament={tournament}
          onSelectCompetition={setViewerCompId}
          onOpenSchedule={() => setViewerScreen("schedule")}
          onAdminClick={requestAdmin}
          onRegister={(compId) => {
            setViewerCompId(compId);
            setViewerScreen("register");
          }}
        />
      )}
      {authPrompt && (
        <AuthModal
          // Treat null (authConfig still loading) as "reset not yet
          // confirmed enabled" — show the link only after the server
          // explicitly says resetEnabled === true. Defaulting to true
          // would briefly expose the "Forgot password?" link on a
          // locked deployment on a direct /admin deep-link, before
          // /api/auth-config resolves; clicking through to /reset
          // would then 404 on submit. fetchAuthConfig is fail-open to
          // {resetEnabled: true} on any transport error, so this only
          // adds a sub-second delay before the link appears on
          // genuinely file-mode deployments.
          resetEnabled={authConfig?.resetEnabled === true}
          onForgotPassword={() => {
            authPromptRef.current = false;
            setAuthPrompt(false);
            // Drop out of admin mode (we're heading to a public route)
            // and navigate to /reset via the same state-machine path
            // any other viewer route uses.
            if (mode === "admin") setMode("viewer");
            setViewerCompId(null);
            setViewerScreen("reset");
          }}
          onClose={() => {
            authPromptRef.current = false;
            setAuthPrompt(false);
            if (mode === "admin") setMode("viewer");
          }}
          onSuccess={(pw) => {
            setAuthed(true);
            authPromptRef.current = false;
            setAuthPrompt(false);
            setMode("admin");
            setPassword(pw);
          }}
        />
      )}
      {toast && <window.Toast {...toast} onClose={() => setToast(null)} />}
    </>
  );
}

function AuthModal({ onClose, onSuccess, onForgotPassword, resetEnabled }) {
  const [pw, setPw] = useS("");
  const [err, setErr] = useS("");
  const [checking, setChecking] = useS(false);
  window.useEscapeToClose(onClose);
  // AuthModal can be dismissed by backdrop click / Escape during the
  // in-flight fetch (the input/submit are disabled while checking but
  // the dismissal paths aren't). Without this guard the post-await
  // setErr / setChecking would fire on a torn-down modal. Same shape
  // as the admin-side mountedRef pattern.
  const mountedRef = useR(true);
  useE(() => () => { mountedRef.current = false; }, []);

  const submit = async (e) => {
    e.preventDefault();
    if (pw === "") { setErr("Enter a password."); return; }
    setChecking(true);
    setErr("");
    try {
      const res = await fetch('/api/tournament', {
        headers: { 'X-Tournament-Password': pw }
      });
      if (!mountedRef.current) return;
      if (res.ok) {
        onSuccess(pw);
      } else if (res.status === 401) {
        setErr("Invalid password. Please try again.");
      } else {
        const body = await res.json().catch(() => ({}));
        if (!mountedRef.current) return;
        setErr(body.error || "Authentication failed. Please try again.");
      }
    } catch {
      if (mountedRef.current) setErr("Could not reach server. Please try again.");
    } finally {
      if (mountedRef.current) setChecking(false);
    }
  };
  return (
    // zIndex 1000 matches MatchViewerModal's modal layer (the shared
    // .modal-backdrop default of 100 sits below viewer chrome at 200/500 and
    // below the mp-udb announcement overlay at 900); without this the sign-in
    // dialog could be obscured by chrome or by announcement cards.
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 1000 }}>
      <div className="modal auth" onClick={(e) => e.stopPropagation()}>
        <img src="/logo.jpeg" alt="Kendo Tournament Logo" className="auth__logo" decoding="async" />
        <div className="auth__title">Admin sign in</div>
        <div className="auth__sub">Enter the tournament password to manage brackets, schedules and live results.</div>
        <form onSubmit={submit}>
          <div className="field">
            <label className="field__label">Password</label>
            <input
              autoFocus
              className="input"
              type="password"
              id="admin-password"
              name="password"
              value={pw}
              onChange={(e) => { setPw(e.target.value); setErr(""); }}
              placeholder="••••••••"
              disabled={checking}
            />
            {err && <div className="auth__error">{err}</div>}
          </div>
          <button type="submit" className="btn btn--primary btn--lg btn--full" disabled={checking}>{checking ? "Checking…" : "Sign in"}</button>
        </form>
        {resetEnabled && onForgotPassword && (
          <div style={{ marginTop: 16, textAlign: 'center' }}>
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => {
                // Confirm intent: reset is global (everyone's
                // re-authenticated) — see resetPassword's tournament
                // broadcast. Cheaper to ask twice than to surprise an
                // operator who clicked the wrong link.
                if (window.confirm("Reset the tournament password? This will sign out all other admins.")) {
                  onForgotPassword();
                }
              }}
              disabled={checking}
            >
              Forgot password?
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function CreateTournament({ onCreated, authConfig }) {
  // In locked mode the server requires X-Tournament-Password on the
  // bootstrap POST and discards the body's `password` field. The form's
  // single password input is repurposed: in file mode it's the new
  // admin password to set; in locked mode it's the env-var password
  // the operator already supplied via TOURNAMENT_PASSWORD_HASH (now
  // the live admin credential). After a successful bootstrap we cache
  // that same value as the localStorage admin password — in locked
  // mode the cached value IS what subsequent admin requests use to
  // authenticate against the bcrypt hash. Pre-fix the form sent no
  // header, the server discarded the typed password, and the SPA
  // immediately tried to authenticate with a value the env-var hash
  // didn't match → instant 401.
  // authConfig starts as null ("loading") and resolves after the first
  // fetchAuthConfig round-trip. While null, treat as unlocked (file mode
  // rendering) — the submit button is disabled until null resolves so
  // the operator can't accidentally omit X-Tournament-Password.
  const locked = authConfig?.mode === "locked";
  const [name, setName] = useS("");
  // Initialize date in canonical DD-MM-YYYY format, not ISO YYYY-MM-DD.
  // toISOString() emits ISO; without this conversion the picker boundary
  // (dmyToIso below) reads it as malformed and renders empty, AND the
  // submit body would send ISO to POST /api/tournament — which now
  // rejects non-DMY dates with 400 "date must be DD-MM-YYYY".
  const today = new Date();
  const todayDmy = `${String(today.getDate()).padStart(2, '0')}-${String(today.getMonth() + 1).padStart(2, '0')}-${today.getFullYear()}`;
  const [date, setDate] = useS(todayDmy);
  const [venue, setVenue] = useS("");
  const [courts, setCourts] = useS(2);
  const [pass, setPass] = useS("");
  // Tournament mode (mp-7h7): "officiated" (default) or "self-run".
  // Chosen once at creation; read-only after that. Default officiated
  // means existing deployments are unaffected.
  const [mode, setMode] = useS("officiated");
  // Destructive-ops (admin) password — required when creating a
  // self-run tournament in file mode: the main auth gate is skipped in
  // self-run, so destructive actions must fall back to this credential.
  // Not shown in locked mode (the env-var hash is the credential).
  const [adminPass, setAdminPass] = useS("");
  const [saving, setSaving] = useS(false);
  // submit's catch sets setSaving(false) post-await; on success the
  // parent calls onCreated which unmounts CreateTournament. The catch
  // branch only fires on error, so the unmount race is narrow but real
  // (parent navigates away on a different signal during the in-flight
  // fetch). Gate via mountedRef for symmetry with the admin sweep.
  const mountedRef = useR(true);
  useE(() => () => { mountedRef.current = false; }, []);

  const isSelfRun = mode === "self-run";

  const submit = async (e) => {
    e.preventDefault();
    // Trim before the empty-check so whitespace-only ("   ") doesn't
    // pass the truthy gate. The backend POST /tournament now trims too
    // (handlers_tournament.go), but trimming here avoids the
    // "  My Tournament  " round-trip producing a canonical name that
    // diverges from what the user just typed.
    const trimmedName = name.trim();
    const trimmedVenue = venue.trim();
    if (!trimmedName || !pass) {
      alert("Name and Password are required.");
      return;
    }
    // Self-run in file mode requires a destructive-ops (admin) password
    // so that destructive routes (delete, invalidate, etc.) are not
    // left fully public. The backend enforces the same rule — this is
    // client-side feedback only.
    if (isSelfRun && !locked && !adminPass) {
      alert("Self-run tournaments require a Destructive-ops password to protect delete and import actions.");
      return;
    }
    // Match the admin-side handleSave guard from admin_setup.jsx.
    // Browser number inputs accept fractional values unless explicitly
    // guarded; Array.from({length:2.5}) silently truncates to 2. This
    // client-side guard provides immediate feedback before submit.
    // The POST /tournament handler now calls validateCourts (which
    // wraps helper.ValidateCourts + per-label single-character check),
    // so a hand-crafted request bypassing this guard is still rejected
    // server-side with a 400 — the two checks are defensive duplicates
    // for the same invariant rather than the client-side being the
    // sole defense.
    if (!Number.isInteger(courts) || courts < 1 || courts > window.MAX_COURTS) {
      alert(`Number of courts must be a whole number between 1 and ${window.MAX_COURTS}.`);
      return;
    }
    setSaving(true);
    try {
      const config = {
        name: trimmedName, date, venue: trimmedVenue,
        password: pass,
        courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i)),
        mode,
      };
      // Self-run in file mode: send the destructive-ops password as a
      // transient `adminPassword` field on the SAME POST. The server reads
      // it via a second body bind (Tournament.AdminPassword is json:"-" so
      // it can't be bound directly) and persists it atomically with the
      // tournament — so the self-run fail-open guard never sees a self-run
      // tournament without an admin credential. In locked mode the server
      // ignores this field (the env-var bcrypt hash is authoritative).
      if (isSelfRun && !locked && adminPass) {
        config.adminPassword = adminPass;
      }
      // In locked mode, the typed password IS the env-var admin
      // credential — pass it as authPassword so api_client sends
      // X-Tournament-Password. In file mode authPassword is undefined
      // and the header is omitted (the bootstrap branch in middleware
      // lets it through unauthenticated).
      const t = await window.API.createTournament(config, locked ? pass : undefined);
      if (!mountedRef.current) return;
      // Refresh the elevated-password auth config so promptAdminPassword()
      // reads the correct gate state before the admin view mounts (mp-7h7).
      // For a self-run tournament the admin password is set atomically in the
      // same POST, so the /api/auth-config response changes: elevatedRequired
      // flips from false to true. Without this refresh, the stale cached value
      // would cause promptAdminPassword() to return "" and destructive actions
      // would 401 without ever prompting the operator.
      try {
        const freshCfg = await window.API.fetchAuthConfig();
        if (mountedRef.current && freshCfg && typeof freshCfg === "object") {
          setCachedAuthConfig(freshCfg);
        }
      } catch (_) {
        // Non-fatal: the cache update is best-effort; the operator can still
        // retry destructive actions which will re-prompt after a 401.
      }
      if (!mountedRef.current) return;
      onCreated(t, pass);
    } catch (err) {
      alert(err.message);
      if (mountedRef.current) setSaving(false);
    }
  };

  // Same render-NaN-as-"" pattern as admin_setup.jsx so clearing the
  // courts input doesn't trigger React's "Received NaN for the value
  // attribute" warning. decideNumericUpdate lives in admin_helpers.jsx,
  // which loads before app.js per index.html.
  const decideNumericUpdate = window.decideNumericUpdate;

  return (
    <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
      <div className="card card--pad-lg">
        <h2 style={{ marginBottom: 8 }}>Welcome to Bracket Creator</h2>
        <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>Set up your new tournament to get started.</p>
        <form onSubmit={submit}>
          <div className="field">
            <label className="field__label">Tournament Name</label>
            <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. London Cup 2026" required />
          </div>
          <div className="row">
            <div className="field"><label className="field__label">Date</label>
              {/* Picker bounds mirror admin_setup.jsx + admin_competition.jsx. */}
              {/* validateAndNormalizeDate enforces MIN_YEAR–MAX_YEAR; without */}
              {/* these bounds the user could pick 1850 and only learn on submit. */}
              <input className="input" type="date" min={`${window.MIN_YEAR}-01-01`} max={`${window.MAX_YEAR}-12-31`} value={window.dmyToIso(date)} onChange={(e) => setDate(window.isoToDmy(e.target.value))} required />
            </div>
            <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => setVenue(e.target.value)} /></div>
          </div>
          <div className="field">
            <label className="field__label">Number of Shiaijo (courts)</label>
            <input
              className="input"
              type="number"
              min="1"
              max={window.MAX_COURTS}
              step="1"
              value={Number.isFinite(courts) ? courts : ""}
              onChange={(e) => setCourts(decideNumericUpdate(e.target.value, 1).value)}
              required
            />
            <div className="field__hint">{`Enter a number (1-${window.MAX_COURTS}). Courts will be automatically labeled A, B, C, etc.`}</div>
          </div>
          {/* Tournament mode selector (mp-7h7). Chosen once at creation;
              immutable after that. Default is officiated (existing behaviour). */}
          <div className="field">
            <label className="field__label">Tournament type</label>
            <div role="group" aria-label="Tournament type" style={{ display: "flex", gap: 8, marginTop: 4 }}>
              <button
                type="button"
                className={`btn${mode === "officiated" ? " btn--primary" : ""}`}
                aria-pressed={mode === "officiated"}
                onClick={() => setMode("officiated")}
                style={{ flex: 1 }}
              >
                Officiated
              </button>
              <button
                type="button"
                className={`btn${mode === "self-run" ? " btn--primary" : ""}`}
                aria-pressed={mode === "self-run"}
                onClick={() => setMode("self-run")}
                style={{ flex: 1 }}
              >
                Self-run
              </button>
            </div>
            <div className="field__hint" style={{ marginTop: 6 }}>
              {mode === "officiated"
                ? "An operator manages scoring and bracket progression. All admin actions require the tournament password. This is the standard setup."
                : "No dedicated operator. Participants self-report scores; all constructive actions (scoring, check-in) are public. Destructive actions (delete competition, discard draw, import roster) still require a separate admin password. Cannot be changed after creation."}
            </div>
          </div>
          <div className="field">
            <label className="field__label">{locked ? "Admin Password (from TOURNAMENT_PASSWORD_HASH)" : "Admin Password"}</label>
            <input
              className="input"
              type="password"
              value={pass}
              onChange={(e) => setPass(e.target.value)}
              placeholder={locked ? "Enter the env-var password" : "Choose a password"}
              required
            />
            <div className="field__hint">
              {locked && isSelfRun
                ? "This server is in locked mode. Enter the password whose bcrypt hash is in TOURNAMENT_PASSWORD_HASH — it authorises this bootstrap and organiser-setup mutations. Destructive actions (delete competition, discard draw, import) require the separate TOURNAMENT_ADMIN_PASSWORD_HASH credential."
                : locked
                  ? "This server is running in locked mode. Enter the password whose bcrypt hash is in TOURNAMENT_PASSWORD_HASH — it's used both to authorize this bootstrap and as your admin credential afterwards."
                  : isSelfRun
                    ? "Used to authorise tournament setup. In self-run mode, scoring and check-in are public so participants don't need this."
                    : "This password will be required to manage the tournament."}
            </div>
          </div>
          {/* In self-run + file mode, require a destructive-ops password at
              creation so delete/invalidate/import can't be called anonymously.
              Locked mode uses TOURNAMENT_ADMIN_PASSWORD_HASH (no UI input). */}
          {isSelfRun && !locked && (
            <div className="field">
              <label className="field__label">Destructive-ops password (required for self-run)</label>
              <input
                className="input"
                type="password"
                value={adminPass}
                onChange={(e) => setAdminPass(e.target.value)}
                placeholder="Password to protect delete / import actions"
                required
              />
              <div className="field__hint">
                A separate password required for destructive actions (delete competition, discard draw, roster add/edit, import). Since this tournament is self-run, scoring is public — this password is the only thing protecting irreversible actions.
              </div>
            </div>
          )}
          {/* Disable submit until authConfig is known (null = loading) so a
              locked-mode deployment doesn't submit without X-Tournament-Password.
              The null window lasts at most one HTTP round-trip on startup. */}
          <button type="submit" className="btn btn--primary btn--lg btn--full" disabled={saving || authConfig === null} style={{ marginTop: 16 }}>
            {saving ? "Creating…" : authConfig === null ? "Loading…" : "Create Tournament"}
          </button>
        </form>
      </div>
    </div>
  );
}

window.App = App;
window.ErrorBoundary = ErrorBoundary;
window.parsePath = parsePath;
window.pathFromState = pathFromState;
// mp-4fd: expose generic notification helper for viewer.jsx (separate bundle).
window.fireNotification = fireNotification;

// Mount the App inside an ErrorBoundary so any uncaught render exception
// renders a recoverable banner instead of a blank screen. Per NFR-008.
ReactDOM.createRoot(document.getElementById("root")).render(
  <ErrorBoundary>
    <App />
  </ErrorBoundary>
);

// Pure helper: returns true when a single announcement should be shown.
// `dismissedKey` is the sessionStorage value for this id (truthy = dismissed).
// `now` defaults to new Date() and can be overridden in tests for determinism.
function isAnnouncementActive(ann, dismissedKey, now) {
  if (!ann || dismissedKey) return false;
  return (now || new Date()) < new Date(ann.expiresAt);
}

function filterActiveAnnouncements(list, now) {
  return list.filter(ann => {
    let dismissedKey = null;
    try {
      dismissedKey = sessionStorage.getItem(`bc_dismissed_announcement_${ann.id}`);
    } catch (_e) {
      // private-browsing modes can throw
    }
    return isAnnouncementActive(ann, dismissedKey, now);
  });
}

export { parsePath, pathFromState, ErrorBoundary, isAnnouncementActive, filterActiveAnnouncements };
