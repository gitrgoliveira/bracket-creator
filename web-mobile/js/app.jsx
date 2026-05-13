// Main App — single tournament per app/url. Tournament has multiple Competitions
// (Men's Individual, Women's Individual, Teams, etc.). Auth gates admin mode.

const { useState: useS, useEffect: useE } = React;

const patchCompetitionData = (prev, event) => {
  if (!prev || !event.data) return prev;
  const { result, results } = event.data;
  const resultsToApply = results || (result ? [result] : []);
  if (resultsToApply.length === 0) return prev;

  const resultMap = new Map(resultsToApply.map(r => [r.id, r]));
  const next = { ...prev };
  let changed = false;

  if (next.poolMatches) {
    next.poolMatches = next.poolMatches.map(m => {
      const update = resultMap.get(m.id);
      if (update) { changed = true; return { ...m, ...update }; }
      return m;
    });
  }

  if (next.bracket && next.bracket.rounds) {
    let bChanged = false;
    const rounds = next.bracket.rounds.map(round =>
      round.map(m => {
        const update = resultMap.get(m.id);
        if (update) {
          bChanged = true; changed = true;
          const patch = { ...update };
          if (patch.ipponsA) patch.scoreA = patch.ipponsA.join("");
          if (patch.ipponsB) patch.scoreB = patch.ipponsB.join("");
          return { ...m, ...patch };
        }
        return m;
      })
    );
    if (bChanged) next.bracket = { ...next.bracket, rounds };
  }

  return changed ? next : prev;
};

const THEME = {
  "accentColor": "#1d3557",
  "showDojo": true,
  "cardVariant": 1
};

function App() {
  const [tournament, setTournament] = useS(null);
  const [loading, setLoading] = useS(true);
  const [mode, setMode] = useS("viewer"); // viewer | admin
  const [authed, setAuthed] = useS(() => localStorage.getItem("bc_authed") === "true");
  const [password, setPassword] = useS(() => localStorage.getItem("bc_password") || "");
  const [authPrompt, setAuthPrompt] = useS(false);
  const [viewerCompId, setViewerCompId] = useS(null);
  const [viewerScreen, setViewerScreen] = useS("home"); // home | schedule
  const [adminView, setAdminView] = useS({ kind: "dashboard" });
  const [toast, setToast] = useS(null);
  const authPromptRef = React.useRef(false);

  const showToast = (message, type = 'success') => {
    setToast({ message, type });
  };

  // --- Routing Logic ---
  const getRouteFromUrl = () => {
    const path = window.location.pathname;
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
    if (path.startsWith("/competition/")) {
      const id = path.split("/")[2];
      return { mode: "viewer", viewerCompId: id };
    }
    if (path === "/schedule") {
      return { mode: "viewer", viewerScreen: "schedule" };
    }
    return { mode: "viewer", viewerScreen: "home" };
  };

  const getUrlFromRoute = (m, vs, vcid, av) => {
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
  };

  // Initial load from URL
  useE(() => {
    const route = getRouteFromUrl();
    if (route.mode === "admin") {
      setMode("admin");
      if (route.admin) setAdminView(route.admin);
      if (!authed) setAuthPrompt(true);
    } else {
      setMode("viewer");
      if (route.viewerCompId) setViewerCompId(route.viewerCompId);
      if (route.viewerScreen) setViewerScreen(route.viewerScreen);
    }
  }, []);

  // Sync state to URL
  useE(() => {
    const url = getUrlFromRoute(mode, viewerScreen, viewerCompId, adminView);
    if (window.location.pathname !== url) {
      history.pushState(null, "", url);
    }
  }, [mode, viewerScreen, viewerCompId, adminView]);

  // Handle popstate (back/forward)
  useE(() => {
    const handlePop = () => {
      const route = getRouteFromUrl();
      if (route.mode === "admin") {
        setMode("admin");
        if (route.admin) setAdminView(route.admin);
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
    const unsub = window.API.subscribeToEvents((event) => {
        const jitter = Math.random() * 500;
        if (event.type === "tournament_updated") {
            if (!authPromptRef.current) setTimeout(load, jitter);
        } else if (event.type === "competition_started" || event.type === "match_updated") {
            if (viewerCompId === event.data.competitionId) {
                // Apply partial update immediately
                if (event.type === "match_updated") {
                    setSelectedCompData(prev => patchCompetitionData(prev, event));
                }
                // Refresh current competition (jittered)
                setTimeout(() => window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData), jitter);
            }
            // Also refresh tournament list for status updates
            setTimeout(load, jitter);
        }
    });
    return unsub;
  }, [viewerCompId]);

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

  const submit = async (e) => {
    e.preventDefault();
    if (pw === "") { setErr("Enter a password."); return; }
    setChecking(true);
    setErr("");
    try {
      const res = await fetch('/api/tournament', {
        headers: { 'X-Tournament-Password': pw }
      });
      if (res.ok) {
        onSuccess(pw);
      } else if (res.status === 401) {
        setErr("Invalid password. Please try again.");
      } else {
        const body = await res.json().catch(() => ({}));
        setErr(body.error || "Authentication failed. Please try again.");
      }
    } catch {
      setErr("Could not reach server. Please try again.");
    } finally {
      setChecking(false);
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
  const [date, setDate] = useS(new Date().toISOString().split("T")[0]);
  const [venue, setVenue] = useS("");
  const [courts, setCourts] = useS(2);
  const [pass, setPass] = useS("");
  const [saving, setSaving] = useS(false);

  const submit = async (e) => {
    e.preventDefault();
    if (!name || !pass) {
      alert("Name and Password are required.");
      return;
    }
    setSaving(true);
    try {
      const config = {
        name, date, venue,
        password: pass,
        courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i))
      };
      const t = await window.API.createTournament(config);
      // Wait for backend to broadcast or just pass it up
      onCreated(t, pass);
    } catch (err) {
      alert(err.message);
      setSaving(false);
    }
  };

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
            <div className="field"><label className="field__label">Date</label><input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} required /></div>
            <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => setVenue(e.target.value)} /></div>
          </div>
          <div className="field">
            <label className="field__label">Number of Shiaijo (courts)</label>
            <input className="input" type="number" min="1" max="26" value={courts} onChange={(e) => setCourts(+e.target.value)} required />
            <div className="field__hint">Enter a number (1-26). Courts will be automatically labeled A, B, C, etc.</div>
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
ReactDOM.createRoot(document.getElementById("root")).render(<App />);
