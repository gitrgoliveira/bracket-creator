// reset.jsx — the /reset SPA page. Lets an operator who has forgotten
// the admin password set a new one without authenticating against the
// old one (since they don't know it). The backing endpoint is
// POST /api/tournament/reset (handlers_reset.go); it is unauthenticated
// by design and the operator hardens production deployments by passing
// --lock-password (mobile-app CLI) which 404s the reset endpoint AND
// switches auth to a bcrypt env-var hash.
//
// Mounted by app.jsx's parsePath/pathFromState when the URL is /reset.
// authConfig.resetEnabled (fetched from /api/auth-config on App() mount)
// gates which UI is shown — form or "operator-disabled" message.

const { useState: useStateR, useEffect: useEffectR, useRef: useRefR } = React;

function ResetPasswordForm({ authConfig, onBack, onSuccess, originatorId }) {
  const [pw, setPw] = useStateR("");
  const [confirm, setConfirm] = useStateR("");
  const [err, setErr] = useStateR("");
  const [saving, setSaving] = useStateR(false);
  // submit's catch sets setSaving(false) post-await. On success the
  // parent navigates away so the component unmounts; gate the post-await
  // setters with mountedRef for symmetry with AuthModal and
  // CreateTournament (the mountedRef pattern is the convention in
  // app.jsx for any post-await setState).
  const mountedRef = useRefR(true);
  useEffectR(() => () => { mountedRef.current = false; }, []);

  // authConfig is still loading (null at App mount, undefined if a
  // direct caller omitted the prop). Don't render the active reset
  // form yet — a direct /reset deep-link on a locked-mode deployment
  // would otherwise expose the form for the sub-second window before
  // /api/auth-config resolves and the user could submit a password
  // that 404s. fetchAuthConfig always resolves (fail-open to file
  // mode on transport errors), so this loading branch is short-lived
  // in production. `== null` matches both null and undefined.
  if (authConfig == null) {
    return (
      <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
        <div className="card card--pad-lg">
          <p style={{ color: "var(--ink-3)" }}>Loading…</p>
        </div>
      </div>
    );
  }

  // Locked mode: the operator has explicitly disabled reset and the
  // server will 404 the POST. Show a clear message rather than letting
  // the user type, submit, and be confused by a 404.
  if (authConfig.resetEnabled === false) {
    return (
      <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
        <div className="card card--pad-lg">
          <h2 style={{ marginBottom: 8 }}>Password reset disabled</h2>
          <p style={{ color: "var(--ink-3)", marginBottom: 16 }}>
            This deployment has password reset disabled by the operator. The
            admin password's bcrypt hash is supplied via an environment
            variable and can only be rotated by restarting the server with a
            new hash.
          </p>
          <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>
            Contact your tournament administrator for the credential.
          </p>
          {onBack && (
            <button type="button" className="btn btn--ghost" onClick={onBack}>← Back</button>
          )}
        </div>
      </div>
    );
  }

  const submit = async (e) => {
    e.preventDefault();
    if (pw === "") { setErr("Enter a new password."); return; }
    if (pw !== confirm) { setErr("Passwords do not match."); return; }
    setSaving(true);
    setErr("");
    try {
      // Pass the per-tab originatorId so the server can echo it on
      // the SSE password_reset broadcast — without that, the
      // originator tab receives its own event and clears the
      // localStorage credential we're about to write below.
      await window.API.resetPassword(pw, originatorId);
      if (!mountedRef.current) return;
      // Success path: persist the new password into localStorage so
      // the user is logged straight into admin without a second sign-in
      // step. Mirror what AuthModal does on success (app.jsx).
      try {
        localStorage.setItem("bc_password", pw);
        localStorage.setItem("bc_authed", "true");
      } catch {
        // localStorage can throw in private-browsing modes; the
        // password rotation still succeeded, so swallow the error
        // and let onSuccess proceed — the user will just have to
        // sign in again manually.
      }
      onSuccess(pw);
    } catch (e2) {
      if (!mountedRef.current) return;
      // 404 specifically means the server's authConfig says reset is
      // disabled — surface a precise message rather than the generic
      // server one. This races with a stale-cached authConfig (operator
      // flipped --lock-password while the SPA was open).
      if (e2.status === 404) {
        setErr("Password reset has been disabled by the operator.");
      } else {
        setErr(e2.message || "Failed to reset password. Please try again.");
      }
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  };

  return (
    <div className="page" style={{ maxWidth: 600, marginTop: 40 }}>
      <div className="card card--pad-lg">
        <h2 style={{ marginBottom: 8 }}>Reset tournament password</h2>
        <p style={{ color: "var(--ink-3)", marginBottom: 24 }}>
          Set a new admin password. The new password will replace the current one
          for everyone — any logged-in admins will need to sign in again.
        </p>
        <form onSubmit={submit}>
          <div className="field">
            <label className="field__label">New password</label>
            <input
              autoFocus
              className="input"
              type="password"
              value={pw}
              onChange={(e) => { setPw(e.target.value); setErr(""); }}
              placeholder="Choose a new password"
              disabled={saving}
              required
            />
          </div>
          <div className="field">
            <label className="field__label">Confirm new password</label>
            <input
              className="input"
              type="password"
              value={confirm}
              onChange={(e) => { setConfirm(e.target.value); setErr(""); }}
              placeholder="Repeat the new password"
              disabled={saving}
              required
            />
            {err && <div className="auth__error">{err}</div>}
          </div>
          <div style={{ display: 'flex', gap: 12, marginTop: 16 }}>
            {onBack && (
              <button type="button" className="btn btn--ghost" onClick={onBack} disabled={saving}>← Back</button>
            )}
            <button type="submit" className="btn btn--primary btn--lg" disabled={saving} style={{ flex: 1 }}>
              {saving ? "Saving…" : "Reset password"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

if (typeof window !== 'undefined') {
  window.ResetPasswordForm = ResetPasswordForm;
}

export { ResetPasswordForm };
