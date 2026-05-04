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
  if (!tournament) return <div className="error">No tournament found. Start the app with a folder containing tournament.md</div>;

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
  const submit = (e) => {
    e.preventDefault();
    if (pw === "") { setErr("Enter a password."); return; }
    onSuccess(pw);
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
            <input className="input" type="password" id="admin-password" name="password" autoFocus value={pw} onChange={(e) => { setPw(e.target.value); setErr(""); }} placeholder="••••••••" />
            {err && <div className="auth__error">{err}</div>}
          </div>
          <button type="submit" className="btn btn--primary btn--lg btn--full">Sign in</button>
        </form>
        <div className="auth__hint">Demo: any password ≥ 3 chars works</div>
      </div>
    </div>
  );
}

window.App = App;
ReactDOM.createRoot(document.getElementById("root")).render(<App />);
