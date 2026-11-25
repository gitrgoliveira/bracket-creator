// registration.jsx: public self-registration page for self-run tournaments.
//
// Mounted by app.jsx when the URL is /register/:compId.
// The backing endpoints are:
//   GET  /api/register/competitions/:id: competition metadata (name, withZekkenName,
//        status, engi, engiSolo)
//   POST /api/register/competitions/:id: register a participant (tag="registered")
//
// Both endpoints return 404 in officiated-mode tournaments.

import { useUnsavedInput } from './app_version.jsx';
import { isNoConnection, REGISTRATION_NOT_CONFIRMED } from './write_result.jsx';
import { engiShape, engiEntryProblem, engiNameFieldLabel } from './engi_entry.jsx';
import { TEAMS_ENTERED_BY_ORGANIZER } from './competition_shape.jsx';

const { useState, useEffect, useRef } = React;

// registrationNameProblem asks the one engi entry rule (engi_entry.jsx) for a
// competition of the given engiShape and words its answer for a participant:
// null when the name fits, or the competition is not engi. A participant
// cannot reach Settings, so a solo refusal is asked as one whose setting is
// locked. A pair competition's sentence ("Enter both names of the pair as
// ...") already reads for a participant. The blank name is the form's own
// "Name is required." and never reaches here.
export function registrationNameProblem(shape, name) {
  return engiEntryProblem(name, shape, true);
}

// registrationRefusal words a 404 from the registration route by its body's
// code, never by its English text: a team competition is entered by the
// organizer (the sentence lives in competition_shape.jsx), anything else gets
// the general refusal.
function registrationRefusal(body) {
  if (body && body.code === "team_competition") return `${TEAMS_ENTERED_BY_ORGANIZER} Ask them to add your team.`;
  return "Registration is not available for this competition.";
}

function RegistrationForm({ compId, onBack }) {
  const [meta, setMeta] = useState(null);       // { id, name, withZekkenName, status, engi, engiSolo }
  const [loading, setLoading] = useState(true);
  const [metaErr, setMetaErr] = useState(null);

  const [name, setName] = useState("");
  const [dojo, setDojo] = useState("");
  const [danGrade, setDanGrade] = useState("");
  const [displayName, setDisplayName] = useState("");
  // A registration being typed holds a new build's automatic reload (app_version.jsx).
  useUnsavedInput([name, dojo, danGrade, displayName].some((s) => s.trim() !== ""));

  const [saving, setSaving] = useState(false);
  const [success, setSuccess] = useState(false);
  const [err, setErr] = useState("");

  // Guard post-await setState against unmounted component.
  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);

  // Fetch competition metadata on mount to know the comp name,
  // withZekkenName flag, and whether registration is still open.
  useEffect(() => {
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
          const body = await res.json().catch(() => ({}));
          if (ac.signal.aborted) return;
          setMetaErr(registrationRefusal(body));
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
    // The server keeps no engi shape rule of its own, so the form asks it
    // before sending.
    const shapeProblem = registrationNameProblem(engiShape(meta), trimName);
    if (shapeProblem) { setErr(shapeProblem); return; }
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
        setErr(registrationRefusal(data));
      } else if (res.status === 409) {
        // Two distinct 409 cases: closed or duplicate name.
        const msg = data.error || "";
        if (msg.toLowerCase().includes("closed")) {
          setErr("Registration is closed for this competition.");
        } else {
          // Duplicate name: use the friendly message from the server verbatim.
          setErr(msg || "A participant with this name and dojo is already registered.");
        }
      } else {
        setErr(data.error || "Registration failed. Please try again.");
      }
    } catch (err) {
      // A fetch that got no answer says the participant's sentence
      // (write_result.jsx); a retry that landed twice is a 409 duplicate.
      if (mountedRef.current) setErr(isNoConnection(err) ? REGISTRATION_NOT_CONFIRMED : "Registration failed. Please try again.");
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
            <button type="button" className="btn btn--ghost" onClick={onBack}>Back</button>
          )}
        </div>
      </div>
    );
  }

  const registerAnother = () => {
    setSuccess(false);
    setSaving(false);
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
          <h2 style={{ marginBottom: 8 }}>You&apos;re registered!</h2>
          <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>
            You have been registered for{meta ? ` ${meta.name}` : " this competition"}.
            Results and match schedules will be available on the day of the tournament.
          </p>
          <div style={{ display: "flex", gap: 12 }}>
            {onBack && (
              <button type="button" className="btn btn--ghost" onClick={onBack}>Back to home</button>
            )}
            <button type="button" className="btn btn--primary" onClick={registerAnother} style={{ flex: 1 }}>
              Register another participant
            </button>
          </div>
        </div>
      </div>
    );
  }

  // A pair engi competition takes both names in the one field, so its label
  // says the format; every other competition (solo engi included) asks for
  // one full name.
  const pairForm = engiShape(meta) === "pair";

  return (
    <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
      <div className="card card--pad-lg">
        <h2 style={{ marginBottom: 4 }}>Register</h2>
        {meta && (
          <p style={{ color: "var(--ink-3)", marginBottom: 20 }}>{meta.name}</p>
        )}
        <form onSubmit={submit}>
          <div className="field">
            <label className="field__label" htmlFor="register-name">{pairForm ? engiNameFieldLabel("pair") : "Full name"} <span style={{ color: "var(--red)" }}>*</span></label>
            <input
              id="register-name"
              autoFocus
              className="input"
              type="text"
              value={name}
              onChange={(e) => { setName(e.target.value); setErr(""); }}
              placeholder={pairForm ? "e.g. Alice Tanaka - Ren Fujita" : "e.g. Alice Tanaka"}
              disabled={saving}
              required
            />
          </div>
          <div className="field">
            <label className="field__label" htmlFor="register-dojo">Dojo <span style={{ color: "var(--red)" }}>*</span></label>
            <input
              id="register-dojo"
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
              <label className="field__label" htmlFor="register-zekken">Display name (zekken)</label>
              <input
                id="register-zekken"
                className="input"
                type="text"
                value={displayName}
                onChange={(e) => { setDisplayName(e.target.value); setErr(""); }}
                placeholder={pairForm ? "e.g. TANAKA - FUJITA" : "e.g. TANAKA"}
                disabled={saving}
              />
              <div className="field__hint">Name as it appears on your zekken (optional).</div>
            </div>
          )}
          <div className="field">
            <label className="field__label" htmlFor="register-dan-grade">Dan grade (optional)</label>
            <input
              id="register-dan-grade"
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
                Back
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
