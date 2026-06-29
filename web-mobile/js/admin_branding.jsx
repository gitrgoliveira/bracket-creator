// Branding admin section: embedded in the Edit Tournament page (mp-scf).
// Lets operators configure accent colors and upload a tournament logo.
//
// Endpoints (via window.API.uploadBrandingLogo / window.API.deleteBrandingLogo):
//   POST   /api/branding/logo   multipart {file}
//   DELETE /api/branding/logo
// Theme colors are saved via the parent's tournament PUT call, not here.

const { useState: useStateBr, useRef: useRefBr, useEffect: useEffectBr } = React;

// Canonical defaults for the branding theme (mp-scf). Single source of truth so
// admin_setup.jsx's dirty-tracking snapshot stays in lockstep with the values
// BrandingManager emits: without this, changing a default here would silently
// false-dirty the form on load until admin_setup was patched in tandem.
const BRANDING_DEFAULTS = Object.freeze({
  primaryColor: "#1d3557",
  accentSoftColor: "#e7eaf3",
  windowTitle: "",
});

// Normalize a raw theme to BrandingManager's emitted shape, or null when no
// colour/title field is set (empty {}: e.g. logo-only: counts as absent so
// the PUT payload stays clean). Exported and reused by admin_setup.jsx for
// dirty tracking, ensuring the pre-mount raw theme and the post-mount synced
// theme compare equal in the dirty snapshot.
function normalizeTheme(t) {
  const has = t && (t.primaryColor || t.accentSoftColor || t.windowTitle);
  if (!has) return null;
  return {
    primaryColor: t.primaryColor || BRANDING_DEFAULTS.primaryColor,
    accentSoftColor: t.accentSoftColor || BRANDING_DEFAULTS.accentSoftColor,
    windowTitle: t.windowTitle || BRANDING_DEFAULTS.windowTitle,
  };
}

function BrandingManager({ tournament, password, showToast, onThemeChange }) {
  const theme = (tournament && tournament.theme) || {};

  const [primaryColor, setPrimaryColor] = useStateBr(theme.primaryColor || "#1d3557");
  const [accentSoftColor, setAccentSoftColor] = useStateBr(theme.accentSoftColor || "#e7eaf3");
  const [windowTitle, setWindowTitle] = useStateBr(theme.windowTitle || "");
  const [hasLogo, setHasLogo] = useStateBr(false);
  const [logoKey, setLogoKey] = useStateBr(0); // bump to force img reload
  const [busy, setBusy] = useStateBr(false);
  const fileRef = useRefBr(null);

  // Probe whether a logo exists on mount.
  useEffectBr(() => {
    fetch('/api/branding/logo', { method: 'HEAD' })
      .then(r => setHasLogo(r.ok))
      .catch(() => setHasLogo(false));
  }, [logoKey]);

  // Sync incoming theme changes (e.g. after a tournament SSE reload).
  // Always update pickers/inputs, but only propagate to the parent when
  // tournament.theme is explicitly set: passing null when absent keeps
  // the parent PUT payload clean (theme block omitted unless configured).
  useEffectBr(() => {
    const norm = normalizeTheme(tournament && tournament.theme);
    // Update the pickers with effective values even when no theme is set, so
    // they show defaults rather than empty; only propagate to the parent when
    // a theme is actually configured (norm !== null).
    setPrimaryColor((norm && norm.primaryColor) || BRANDING_DEFAULTS.primaryColor);
    setAccentSoftColor((norm && norm.accentSoftColor) || BRANDING_DEFAULTS.accentSoftColor);
    setWindowTitle((norm && norm.windowTitle) || BRANDING_DEFAULTS.windowTitle);
    if (onThemeChange) onThemeChange(norm);
  }, [tournament]);

  const handleColorChange = (field, value) => {
    const next = {
      primaryColor: field === "primaryColor" ? value : primaryColor,
      accentSoftColor: field === "accentSoftColor" ? value : accentSoftColor,
      windowTitle,
    };
    if (field === "primaryColor") setPrimaryColor(value);
    else setAccentSoftColor(value);
    // Propagate to parent so the PUT /tournament body includes all branding.
    if (onThemeChange) onThemeChange(next);
    // Live-preview the color change in CSS without waiting for save.
    if (typeof window.applyTheme === "function") {
      window.applyTheme(next);
    } else {
      const root = document.documentElement;
      if (field === "primaryColor") root.style.setProperty("--accent", value);
      else root.style.setProperty("--accent-soft", value);
    }
  };

  const handleWindowTitleChange = (value) => {
    setWindowTitle(value);
    if (onThemeChange) onThemeChange({ primaryColor, accentSoftColor, windowTitle: value });
    // Route through the shared helper so the default string stays in one place.
    if (typeof window.applyTheme === "function") {
      window.applyTheme({ primaryColor, accentSoftColor, windowTitle: value });
    } else {
      document.title = value || "Bracket Creator Mobile";
    }
  };

  const handleLogoUpload = async (e) => {
    e.preventDefault();
    const file = fileRef.current && fileRef.current.files && fileRef.current.files[0];
    if (!file) {
      if (showToast) showToast("Choose a PNG or JPEG file", "error");
      return;
    }
    setBusy(true);
    try {
      await window.API.uploadBrandingLogo({ file, password });
      setHasLogo(true);
      setLogoKey(k => k + 1);
      if (fileRef.current) fileRef.current.value = "";
      if (showToast) showToast("Logo updated", "success");
    } catch (err) {
      if (showToast) showToast(err && err.message ? err.message : "Upload failed", "error");
    } finally {
      setBusy(false);
    }
  };

  const handleLogoDelete = async () => {
    if (!(await window.confirmDialog({ message: "Remove the tournament logo?", confirmLabel: "Remove logo", danger: true }))) return;
    setBusy(true);
    try {
      await window.API.deleteBrandingLogo(password);
      setHasLogo(false);
      setLogoKey(k => k + 1);
      if (showToast) showToast("Logo removed", "success");
    } catch (err) {
      if (showToast) showToast(err && err.message ? err.message : "Delete failed", "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card card--pad-lg">
      <div className="card__head">
        <div>
          <div className="card__title">Branding</div>
          <div className="card__sub">Customize the browser tab title, accent colors, and the tournament logo shown on the viewer, lobby, and admin screens.</div>
        </div>
      </div>
      <div>
        <div className="field">
          <label className="field__label">Browser tab / window title</label>
          <input type="text" value={windowTitle} maxLength={100} placeholder="Bracket Creator Mobile"
            onChange={(e) => handleWindowTitleChange(e.target.value)}
            style={{ width: "100%", boxSizing: "border-box" }} />
          <div className="field__hint">Shown in the browser tab and title bar on all pages. Defaults to "Bracket Creator Mobile" when blank.</div>
        </div>
        <div className="branding__colors">
          <div className="field">
            <label className="field__label">Primary accent color</label>
            <input type="color" value={primaryColor}
              onChange={(e) => handleColorChange("primaryColor", e.target.value)}
              style={{ width: "100%", height: 36, cursor: "pointer", border: "1px solid var(--line)", borderRadius: 6 }} />
            <div className="field__hint">Used for buttons, headers, and highlights. Default: #1d3557.</div>
          </div>
          <div className="field">
            <label className="field__label">Soft accent (background tint)</label>
            <input type="color" value={accentSoftColor}
              onChange={(e) => handleColorChange("accentSoftColor", e.target.value)}
              style={{ width: "100%", height: 36, cursor: "pointer", border: "1px solid var(--line)", borderRadius: 6 }} />
            <div className="field__hint">Used for row highlights and badges. Default: #e7eaf3.</div>
          </div>
        </div>
        <div className="branding__logo">
          <div className="field__label" style={{ marginBottom: 8 }}>Tournament logo</div>
          <div className="field__hint" style={{ marginBottom: 12 }}>PNG or JPEG, up to 1 MB. Replaces the default kendo logo on all screens. Falls back to the default logo if removed.</div>
          {hasLogo && (
            <div className="branding__logo-preview">
              <img key={logoKey} src={`/api/branding/logo?v=${logoKey}`} alt="Tournament logo" style={{ height: 48, width: "auto", maxWidth: 120, objectFit: "contain" }} />
              <div style={{ flex: 1 }}>Current logo</div>
              <button type="button" className="btn btn--danger" disabled={busy} onClick={handleLogoDelete}>Remove</button>
            </div>
          )}
          <form onSubmit={handleLogoUpload} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div className="field">
              <label className="field__label">{hasLogo ? "Replace logo" : "Upload logo"} (PNG or JPEG, ≤1 MB)</label>
              <input ref={fileRef} type="file" accept="image/png,image/jpeg" />
              <div className="field__hint">Square image recommended : non-square images will be cropped to fit the top bar icon.</div>
            </div>
            <div className="branding__logo-actions">
              <button type="submit" className="btn btn--primary" disabled={busy}>{busy ? "Uploading…" : hasLogo ? "Replace logo" : "Upload logo"}</button>
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}

window.BrandingManager = BrandingManager;
export { BrandingManager, normalizeTheme, BRANDING_DEFAULTS };
