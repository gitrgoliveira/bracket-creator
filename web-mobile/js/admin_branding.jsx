// Branding admin section — embedded in the Edit Tournament page (mp-scf).
// Lets operators configure accent colors and upload a tournament logo.
//
// Endpoints (via window.API.uploadBrandingLogo / window.API.deleteBrandingLogo):
//   POST   /api/branding/logo   multipart {file}
//   DELETE /api/branding/logo
// Theme colors are saved via the parent's tournament PUT call, not here.

const { useState: useStateBr, useRef: useRefBr, useEffect: useEffectBr } = React;

function BrandingManager({ tournament, password, showToast, onThemeChange }) {
  const theme = (tournament && tournament.theme) || {};

  const [primaryColor, setPrimaryColor] = useStateBr(theme.primaryColor || "#1d3557");
  const [accentSoftColor, setAccentSoftColor] = useStateBr(theme.accentSoftColor || "#e7eaf3");
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
  useEffectBr(() => {
    const t = (tournament && tournament.theme) || {};
    if (t.primaryColor) setPrimaryColor(t.primaryColor);
    if (t.accentSoftColor) setAccentSoftColor(t.accentSoftColor);
  }, [tournament]);

  const handleColorChange = (field, value) => {
    if (field === "primaryColor") setPrimaryColor(value);
    else setAccentSoftColor(value);
    // Propagate to parent so the PUT /tournament body includes updated colors.
    if (onThemeChange) onThemeChange({ primaryColor: field === "primaryColor" ? value : primaryColor, accentSoftColor: field === "accentSoftColor" ? value : accentSoftColor });
    // Live-preview the color change in CSS without waiting for save.
    if (typeof window.applyTheme === "function") {
      window.applyTheme({
        primaryColor: field === "primaryColor" ? value : primaryColor,
        accentSoftColor: field === "accentSoftColor" ? value : accentSoftColor,
      });
    } else {
      const root = document.documentElement;
      if (field === "primaryColor") root.style.setProperty("--accent", value);
      else root.style.setProperty("--accent-soft", value);
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
    if (!window.confirm("Remove the tournament logo?")) return;
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
    <div className="card card--pad-lg" style={{ marginTop: 16 }}>
      <div className="card__title">Branding</div>
      <div className="field__hint" style={{ marginBottom: 16 }}>
        Customize accent colors and the tournament logo shown on the viewer,
        lobby, and admin screens. Changes refresh on next page load;
        colors preview instantly below.
      </div>

      <div style={{ display: "flex", gap: 16, flexWrap: "wrap", marginBottom: 16 }}>
        <div className="field" style={{ flex: 1, minWidth: 160 }}>
          <label className="field__label">Primary accent color</label>
          <input
            type="color"
            value={primaryColor}
            onChange={(e) => handleColorChange("primaryColor", e.target.value)}
            style={{ width: "100%", height: 36, cursor: "pointer", border: "1px solid var(--line)", borderRadius: 6 }}
          />
          <div className="field__hint">
            Used for buttons, headers, and highlights. Default: #1d3557.
          </div>
        </div>
        <div className="field" style={{ flex: 1, minWidth: 160 }}>
          <label className="field__label">Soft accent (background tint)</label>
          <input
            type="color"
            value={accentSoftColor}
            onChange={(e) => handleColorChange("accentSoftColor", e.target.value)}
            style={{ width: "100%", height: 36, cursor: "pointer", border: "1px solid var(--line)", borderRadius: 6 }}
          />
          <div className="field__hint">
            Used for row highlights and badges. Default: #e7eaf3.
          </div>
        </div>
      </div>

      <div className="field__hint" style={{ marginBottom: 16, fontSize: 12, color: "var(--ink-3)" }}>
        Save the tournament form below to persist color changes.
      </div>

      <div style={{ borderTop: "1px solid var(--line)", paddingTop: 16, marginTop: 4 }}>
        <div style={{ fontWeight: 600, marginBottom: 8 }}>Tournament logo</div>
        <div className="field__hint" style={{ marginBottom: 12 }}>
          PNG or JPEG, up to 1 MB. Replaces the default kendo logo on all screens.
          Falls back to the default logo if removed.
        </div>

        {hasLogo && (
          <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 12, padding: "8px 12px", border: "1px solid var(--line)", borderRadius: 8 }}>
            <img
              key={logoKey}
              src={`/api/branding/logo?v=${logoKey}`}
              alt="Tournament logo"
              style={{ height: 48, width: "auto", maxWidth: 120, objectFit: "contain" }}
            />
            <div style={{ flex: 1 }}>Current logo</div>
            <button
              className="btn btn--danger"
              disabled={busy}
              onClick={handleLogoDelete}
            >
              Remove
            </button>
          </div>
        )}

        <form onSubmit={handleLogoUpload} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div className="field">
            <label className="field__label">{hasLogo ? "Replace logo" : "Upload logo"} (PNG or JPEG, ≤1 MB)</label>
            <input ref={fileRef} type="file" accept="image/png,image/jpeg" />
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <button type="submit" className="btn btn--primary" disabled={busy}>
              {busy ? "Uploading…" : hasLogo ? "Replace logo" : "Upload logo"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

window.BrandingManager = BrandingManager;
export { BrandingManager };
