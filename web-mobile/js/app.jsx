// Main App — single tournament per app/url. Tournament has multiple Competitions
// (Men's Individual, Women's Individual, Teams, etc.). Auth gates admin mode.

import { applyPatch as patchCompetitionData } from './patch.jsx';

const { useState: useS, useEffect: useE, useRef: useR } = React;

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
    if (vcid) return `/competition/${vcid}`;
    if (vs === "schedule") return "/schedule";
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
      return React.createElement('div', { className: 'page', style: { padding: 24 } },
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

function App() {
  const [tournament, setTournament] = useS(null);
  const [loading, setLoading] = useS(true);
  // mode: viewer | admin | display.
  // "display" is the public /display family — read-only TV / lobby /
  // OBS overlay surfaces. We track it here (rather than as a sub-state
  // of viewer) so App() can short-circuit the viewer/admin auth/render
  // logic entirely; the display surfaces require no auth and don't
  // touch viewerCompId / viewerScreen.
  const [mode, setMode] = useS("viewer");
  const [authed, setAuthed] = useS(() => localStorage.getItem("bc_authed") === "true");
  const [password, setPassword] = useS(() => localStorage.getItem("bc_password") || "");
  const [authPrompt, setAuthPrompt] = useS(false);
  const [viewerCompId, setViewerCompId] = useS(null);
  const [viewerScreen, setViewerScreen] = useS("home"); // home | schedule
  const [adminView, setAdminView] = useS({ kind: "dashboard" });
  const [toast, setToast] = useS(null);
  // T063: SSE connection status, surfaced to display surfaces so the
  // TV / lobby / overlay can render a reconnect indicator during the
  // window between EventSource error and reconnect-onopen. The
  // EventSource itself lives inside subscribeToEvents; the second
  // callback hands status events up here.
  const [sseConnected, setSseConnected] = useS(true);
  const authPromptRef = React.useRef(false);

  const showToast = (message, type = 'success') => {
    setToast({ message, type });
  };

  // Hydrate state from the current URL on first render. Without this,
  // a deep-link page-load (e.g. /admin/schedule typed directly) would
  // boot into the default viewer-home view until the user navigated.
  useE(() => {
    const route = parsePath(window.location.pathname);
    if (route.mode === "admin") {
      setMode("admin");
      if (route.admin) setAdminView(route.admin);
      if (!authed) setAuthPrompt(true);
    } else if (route.mode === "display") {
      setMode("display");
    } else {
      setMode("viewer");
      if (route.viewerCompId) setViewerCompId(route.viewerCompId);
      if (route.viewerScreen) setViewerScreen(route.viewerScreen);
    }
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
    localStorage.setItem("bc_authed", authed);
    localStorage.setItem("bc_password", password);
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
    if (viewerCompId) {
      setLoading(true);
      window.API.fetchCompetitionDetails(viewerCompId)
        .then(data => {
          setSelectedCompData(data);
          setLoading(false);
        })
        .catch(err => {
          console.error(err);
          setLoading(false);
        });
    } else {
      setSelectedCompData(null);
    }
  }, [viewerCompId]);

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

  if (loading && !selectedCompData) return <div className="loading">Loading...</div>;
  if (!tournament) return (
    <CreateTournament onCreated={(t, p) => {
      setTournament(t);
      setAuthed(true);
      setMode("admin");
      setPassword(p);
    }} />
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
        />
        {toast && <window.Toast {...toast} onClose={() => setToast(null)} />}
      </>
    );
  }

  // viewer mode
  return (
    <>
      {selectedCompData ? (
        <window.ViewerCompetition
          tournament={tournament}
          competition={selectedCompData.config}
          pools={selectedCompData.pools}
          poolMatches={selectedCompData.poolMatches}
          standings={selectedCompData.standings}
          bracket={selectedCompData.bracket}
          onBack={() => setViewerCompId(null)}
          onAdminClick={requestAdmin}
          tweaks={THEME}
        />
      ) : viewerScreen === "schedule" ? (
        <window.ViewerSchedule
          tournament={tournament}
          onBack={() => setViewerScreen("home")}
          tweaks={THEME}
        />
      ) : (
        <window.ViewerHome
          tournament={tournament}
          onSelectCompetition={setViewerCompId}
          onOpenSchedule={() => setViewerScreen("schedule")}
          onAdminClick={requestAdmin}
        />
      )}
      {authPrompt && (
        <AuthModal
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

function AuthModal({ onClose, onSuccess }) {
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
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal auth" onClick={(e) => e.stopPropagation()}>
        <div className="auth__logo">BC</div>
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
      </div>
    </div>
  );
}

function CreateTournament({ onCreated }) {
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
  const [saving, setSaving] = useS(false);
  // submit's catch sets setSaving(false) post-await; on success the
  // parent calls onCreated which unmounts CreateTournament. The catch
  // branch only fires on error, so the unmount race is narrow but real
  // (parent navigates away on a different signal during the in-flight
  // fetch). Gate via mountedRef for symmetry with the admin sweep.
  const mountedRef = useR(true);
  useE(() => () => { mountedRef.current = false; }, []);

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
        courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i))
      };
      const t = await window.API.createTournament(config);
      if (!mountedRef.current) return;
      // Wait for backend to broadcast or just pass it up
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
          <div className="field">
            <label className="field__label">Admin Password</label>
            <input className="input" type="password" value={pass} onChange={(e) => setPass(e.target.value)} placeholder="Choose a password" required />
            <div className="field__hint">This password will be required to manage the tournament.</div>
          </div>
          <button type="submit" className="btn btn--primary btn--lg btn--full" disabled={saving} style={{ marginTop: 16 }}>
            {saving ? "Creating…" : "Create Tournament"}
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

// Mount the App inside an ErrorBoundary so any uncaught render exception
// renders a recoverable banner instead of a blank screen. Per NFR-008.
ReactDOM.createRoot(document.getElementById("root")).render(
  <ErrorBoundary>
    <App />
  </ErrorBoundary>
);

export { parsePath, pathFromState, ErrorBoundary };
