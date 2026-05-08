// Main App — single tournament per app/url. Tournament has multiple Competitions
// (Men's Individual, Women's Individual, Teams, etc.). Auth gates admin mode.

const { useState: useS, useEffect: useE } = React;

const THEME = {
  "accentColor": "#1d3557",
  "showDojo": true,
  "cardVariant": 1
};

function App() {
  const [tournament, setTournament] = useS(null);
  const [loading, setLoading] = useS(true);
  const [mode, setMode] = useS("viewer"); // viewer | admin
  const [authed, setAuthed] = useS(false);
  const [password, setPassword] = useS("");
  const [authPrompt, setAuthPrompt] = useS(false);
  const [viewerCompId, setViewerCompId] = useS(null);
  const [viewerScreen, setViewerScreen] = useS("home"); // home | schedule

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
        if (event.type === "tournament_updated") {
            load();
        } else if (event.type === "competition_started" || event.type === "match_updated") {
            if (viewerCompId === event.data.competitionId) {
                // Refresh current competition
                window.API.fetchCompetitionDetails(viewerCompId).then(setSelectedCompData);
            }
            // Also refresh tournament list for status updates
            load();
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

  const requestAdmin = () => { if (authed) setMode("admin"); else setAuthPrompt(true); };
  const onLogout = () => { setAuthed(false); setMode("viewer"); setPassword(""); };

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
      <window.AdminApp
        tournament={tournament}
        onUpdate={setTournament}
        onLogout={onLogout}
        onViewerMode={() => setMode("viewer")}
        tweaks={THEME}
        password={password}
      />
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
          onClose={() => setAuthPrompt(false)}
          onSuccess={(pw) => { setAuthed(true); setAuthPrompt(false); setMode("admin"); setPassword(pw); }}
        />
      )}
    </>
  );
}

function AuthModal({ onClose, onSuccess }) {
  const [pw, setPw] = useS("");
  const [err, setErr] = useS("");
  const [checking, setChecking] = useS(false);
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
    } catch (e) {
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
            <input className="input" type="password" id="admin-password" name="password" autoFocus value={pw} onChange={(e) => { setPw(e.target.value); setErr(""); }} placeholder="••••••••" disabled={checking} />
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
