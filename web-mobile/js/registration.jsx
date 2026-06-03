// registration.jsx — public self-registration page for self-run tournaments.
//
// Mounted by app.jsx when the URL is /register/:compId.
// The backing endpoints are:
//   GET  /api/register/competitions/:id — competition metadata (name, withZekkenName)
//   POST /api/register/competitions/:id — register a participant (tag="registered")
//
// Both endpoints return 404 in officiated-mode tournaments.

const { useState: useStateReg, useEffect: useEffectReg, useRef: useRefReg } = React;

function RegistrationForm({ compId, onBack }) {
  const [meta, setMeta] = useStateReg(null);       // { id, name, withZekkenName, status }
  const [loading, setLoading] = useStateReg(true);
  const [metaErr, setMetaErr] = useStateReg(null);

  const [name, setName] = useStateReg("");
  const [dojo, setDojo] = useStateReg("");
  const [danGrade, setDanGrade] = useStateReg("");
  const [displayName, setDisplayName] = useStateReg("");

  const [saving, setSaving] = useStateReg(false);
  const [success, setSuccess] = useStateReg(false);
  const [err, setErr] = useStateReg("");

  // Guard post-await setState against unmounted component.
  const mountedRef = useRefReg(true);
  useEffectReg(() => () => { mountedRef.current = false; }, []);

  // Fetch competition metadata on mount to know the comp name,
  // withZekkenName flag, and whether registration is still open.
  useEffectReg(() => {
    setMeta(null);
    setMetaErr(null);
    setLoading(true);
    setSuccess(false);
    setErr("");
    setName("");
    setDojo("");
    setDanGrade("");
    setDisplayName("");
    if (!compId) {
      setMetaErr("No competition specified.");
      setLoading(false);
      return;
    }
    const ac = new AbortController();
    fetch(`/api/register/competitions/${encodeURIComponent(compId)}`, { signal: ac.signal })
      .then(async (res) => {
        if (ac.signal.aborted) return;
        if (res.status === 404) {
          setMetaErr("Registration is not available for this competition.");
          setLoading(false);
          return;
        }
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          if (ac.signal.aborted) return;
          setMetaErr(body.error || "Failed to load competition details.");
          setLoading(false);
          return;
        }
        const data = await res.json();
        if (ac.signal.aborted) return;
        if (data.status && data.status !== "setup" && data.status !== "") {
          setMetaErr("Registration is closed for this competition.");
          setLoading(false);
          return;
        }
        setMeta(data);
        setLoading(false);
      })
      .catch((e) => {
        if (e.name === "AbortError") return;
        if (!mountedRef.current) return;
        setMetaErr("Could not reach server. Please try again.");
        setLoading(false);
      });
    return () => ac.abort();
  }, [compId]);

  const submit = async (e) => {
    e.preventDefault();
    const trimName = name.trim();
    const trimDojo = dojo.trim();
    if (!trimName) { setErr("Name is required."); return; }
    if (!trimDojo) { setErr("Dojo is required."); return; }
    setSaving(true);
    setErr("");
    try {
      const body = { name: trimName, dojo: trimDojo };
      if (danGrade.trim()) body.danGrade = danGrade.trim();
      if (meta && meta.withZekkenName && displayName.trim()) {
        body.displayName = displayName.trim();
      }

      const res = await fetch(`/api/register/competitions/${encodeURIComponent(compId)}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!mountedRef.current) return;
      const data = await res.json().catch(() => ({}));
      if (!mountedRef.current) return;
      if (res.ok) {
        setSuccess(true);
        setSaving(false);
        return;
      }
      if (res.status === 404) {
        setErr("Registration is not available for this competition.");
      } else if (res.status === 409) {
        // Two distinct 409 cases: closed or duplicate name.
        const msg = data.error || "";
        if (msg.toLowerCase().includes("closed")) {
          setErr("Registration is closed for this competition.");
        } else {
          // Duplicate name — use the friendly message from the server verbatim.
          setErr(msg || "A participant with this name is already registered.");
        }
      } else {
        setErr(data.error || "Registration failed. Please try again.");
      }
    } catch {
      if (mountedRef.current) setErr("Could not reach server. Please try again.");
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
        <div className="card card--pad-lg">
          <p style={{ color: "var(--ink-3)" }}>Loading…</p>
        </div>
      </div>
    );
  }

  if (metaErr) {
    return (
      <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
        <div className="card card--pad-lg">
          <h2 style={{ marginBottom: 8 }}>Registration unavailable</h2>
          <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>{metaErr}</p>
          {onBack && (
            <button className="btn btn--ghost" onClick={onBack}>← Back</button>
          )}
        </div>
      </div>
    );
  }

  const registerAnother = () => {
    setSuccess(false);
    setName("");
    setDojo("");
    setDanGrade("");
    setDisplayName("");
    setErr("");
  };

  if (success) {
    return (
      <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
        <div className="card card--pad-lg">
          <h2 style={{ marginBottom: 8 }}>You're registered!</h2>
          <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>
            You have been registered for{meta ? ` ${meta.name}` : " this competition"}.
            Results and match schedules will be available on the day of the tournament.
          </p>
          <div style={{ display: "flex", gap: 12 }}>
            {onBack && (
              <button className="btn btn--ghost" onClick={onBack}>← Back to home</button>
            )}
            <button className="btn btn--primary" onClick={registerAnother} style={{ flex: 1 }}>
              Register another participant
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
      <div className="card card--pad-lg">
        <h2 style={{ marginBottom: 4 }}>Register</h2>
        {meta && (
          <p style={{ color: "var(--ink-3)", marginBottom: 20 }}>{meta.name}</p>
        )}
        <form onSubmit={submit}>
          <div className="field">
            <label className="field__label">Full name <span style={{ color: "var(--red)" }}>*</span></label>
            <input
              autoFocus
              className="input"
              type="text"
              value={name}
              onChange={(e) => { setName(e.target.value); setErr(""); }}
              placeholder="e.g. Alice Tanaka"
              disabled={saving}
              required
            />
          </div>
          <div className="field">
            <label className="field__label">Dojo <span style={{ color: "var(--red)" }}>*</span></label>
            <input
              className="input"
              type="text"
              value={dojo}
              onChange={(e) => { setDojo(e.target.value); setErr(""); }}
              placeholder="e.g. Gyokusen"
              disabled={saving}
              required
            />
          </div>
          {meta && meta.withZekkenName && (
            <div className="field">
              <label className="field__label">Display name (zekken)</label>
              <input
                className="input"
                type="text"
                value={displayName}
                onChange={(e) => { setDisplayName(e.target.value); setErr(""); }}
                placeholder="e.g. TANAKA"
                disabled={saving}
              />
              <div className="field__hint">Name as it appears on your zekken (optional).</div>
            </div>
          )}
          <div className="field">
            <label className="field__label">Dan grade (optional)</label>
            <input
              className="input"
              type="text"
              value={danGrade}
              onChange={(e) => { setDanGrade(e.target.value); setErr(""); }}
              placeholder="e.g. 3 Dan"
              disabled={saving}
            />
          </div>
          {err && <div className="auth__error" style={{ marginBottom: 12 }}>{err}</div>}
          <div style={{ display: "flex", gap: 12, marginTop: 16 }}>
            {onBack && (
              <button type="button" className="btn btn--ghost" onClick={onBack} disabled={saving}>
                ← Back
              </button>
            )}
            <button type="submit" className="btn btn--primary btn--lg" disabled={saving} style={{ flex: 1 }}>
              {saving ? "Registering…" : "Register"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

if (typeof window !== "undefined") {
  window.RegistrationForm = RegistrationForm;
}

export { RegistrationForm };
