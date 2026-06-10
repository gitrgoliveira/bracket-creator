// Sponsors admin section — embedded in the Edit Tournament page (mp-c38).
// Shows existing sponsor logos with delete controls and an upload form.
//
// Endpoints (via window.API.uploadSponsor / window.API.deleteSponsor):
//   POST   /api/sponsors        multipart {name, link?, file}
//   DELETE /api/sponsors/:index
// Live list is seeded from tournament.sponsors on first render and then
// maintained in local state — updates come from API responses, not the
// parent prop. This keeps unsaved tournament-form edits above intact
// when a sponsor is added or deleted (no page reload needed).

const { useState: useStateSp, useRef: useRefSp } = React;

function SponsorsManager({ tournament, password, showToast, maxSponsors }) {
  const cap = maxSponsors || 6;

  // Manage the sponsor list in local state so upload/delete don't
  // force window.location.reload() — that would wipe any unsaved edits
  // in the tournament form above. We seed from the tournament prop on
  // first render and update locally from the API response shape:
  //   POST /api/sponsors → returns the created Sponsor
  //   DELETE /api/sponsors/:index → returns {removed}
  const [sponsors, setSponsors] = useStateSp(
    (tournament && tournament.sponsors) || [],
  );
  const [name, setName] = useStateSp("");
  const [link, setLink] = useStateSp("");
  const [busy, setBusy] = useStateSp(false);
  const fileRef = useRefSp(null);

  const reachedCap = sponsors.length >= cap;

  const handleUpload = async (e) => {
    e.preventDefault();
    const file = fileRef.current && fileRef.current.files && fileRef.current.files[0];
    if (!name.trim()) {
      if (showToast) showToast("Sponsor name is required", "error");
      return;
    }
    if (!file) {
      if (showToast) showToast("Choose a PNG or JPEG file", "error");
      return;
    }
    setBusy(true);
    try {
      const created = await window.API.uploadSponsor({ file, name: name.trim(), link: link.trim(), password });
      setSponsors((prev) => [...prev, created]);
      setName("");
      setLink("");
      if (fileRef.current) fileRef.current.value = "";
      if (showToast) showToast("Sponsor added", "success");
    } catch (err) {
      if (showToast) showToast(err && err.message ? err.message : "Upload failed", "error");
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (idx, label) => {
    if (!(await window.confirmDialog({ message: `Remove sponsor "${label}"?`, confirmLabel: "Remove sponsor", danger: true }))) return;
    setBusy(true);
    try {
      await window.API.deleteSponsor(idx, password);
      setSponsors((prev) => prev.filter((_, i) => i !== idx));
      if (showToast) showToast("Sponsor removed", "success");
    } catch (err) {
      if (showToast) showToast(err && err.message ? err.message : "Delete failed", "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card card--pad-lg">
      <div className="card__title">Sponsors</div>
      <div className="field__hint" style={{ marginBottom: 12 }}>
        Logos shown on the viewer home and on the lobby/TV display surfaces. Up to {cap} sponsors;
        PNG or JPEG up to 1 MB each.
      </div>

      {sponsors.length === 0 ? (
        <div className="field__hint" style={{ marginBottom: 16, opacity: 0.7 }}>
          No sponsors yet.
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 16 }}>
          {sponsors.map((s, i) => (
            <div key={s.file} style={{
              display: "flex", alignItems: "center", gap: 12,
              padding: "8px 12px", border: "1px solid var(--line)", borderRadius: 8,
            }}>
              <img
                src={"/api/sponsors/" + s.file}
                alt={s.name}
                style={{ height: 32, width: "auto", maxWidth: 64, objectFit: "contain" }}
              />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontWeight: 600 }}>{s.name}</div>
                {s.link && (
                  <div style={{ fontSize: 12, color: "var(--ink-3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {s.link}
                  </div>
                )}
              </div>
              <button
                className="btn btn--danger"
                disabled={busy}
                onClick={() => handleDelete(i, s.name)}
              >
                Remove
              </button>
            </div>
          ))}
        </div>
      )}

      {reachedCap ? (
        <div className="field__hint" style={{ opacity: 0.7 }}>
          Sponsor cap reached. Remove one to add another.
        </div>
      ) : (
        <form onSubmit={handleUpload} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div className="field">
            <label className="field__label">Sponsor name</label>
            <input
              className="input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Acme Corp"
              maxLength={80}
            />
          </div>
          <div className="field">
            <label className="field__label">Link (optional)</label>
            <input
              className="input"
              value={link}
              onChange={(e) => setLink(e.target.value)}
              placeholder="https://acme.example"
              maxLength={500}
            />
            <div className="field__hint">Opens in a new tab on the viewer surface.</div>
          </div>
          <div className="field">
            <label className="field__label">Logo (PNG or JPEG, ≤1 MB)</label>
            <input
              ref={fileRef}
              type="file"
              accept="image/png,image/jpeg"
            />
            <div className="field__hint">Landscape (wide) image recommended — logos display as a small strip.</div>
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <button type="submit" className="btn btn--primary" disabled={busy}>
              {busy ? "Uploading…" : "Add sponsor"}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

window.SponsorsManager = SponsorsManager;
export { SponsorsManager };
