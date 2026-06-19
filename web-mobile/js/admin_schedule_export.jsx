// AdminExport card extracted from admin_schedule.jsx (mp-d7tl).

export function AdminExport({ c, t, password }) {
  const url = `${(window.linkBase || (() => window.location.origin))(t)}/viewer.html?id=${t.id}#comp-${c.id}`;

  const downloadXlsx = async () => {
    try {
      // /api/* is behind AuthMiddleware; without the password header
      // this returns 401 and the download silently fails.
      const resp = await fetch(`/api/competitions/${c.id}/export`, {
        headers: password ? { "X-Tournament-Password": password } : {},
      });
      if (!resp.ok) throw new Error(await resp.text());
      const blob = await resp.blob();
      const dlUrl = window.URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = dlUrl;
      a.download = `bracket-${c.id}.xlsx`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(dlUrl);
    } catch (err) {
      alert("Export failed: " + err.message);
    }
  };

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(url);
      alert("URL copied to clipboard!");
    } catch (err) {
      alert("Failed to copy URL: " + (err?.message || err));
    }
  };

  return (
    <div className="row">
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Export {c.name}</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Generate the official Excel workbook used during the day.</div>
        <button type="button" className="btn btn--primary btn--full" onClick={downloadXlsx}>Download .xlsx</button>
        <div className="field__hint" style={{ marginTop: 10 }}>Includes pool draws, pool matches, and elimination brackets with linked formulas.</div>
      </div>
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Public viewer link</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Players & spectators see this competition's bracket, schedule and results.</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input className="input" value={url} readOnly style={{ flex: 1 }} />
          <button type="button" className="btn" onClick={copyUrl}>Copy</button>
        </div>
      </div>
    </div>
  );
}
