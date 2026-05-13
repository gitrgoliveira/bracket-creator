// Tournament-edit, competition-create, and bulk-import pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useRef: useRefA } = React;

const normalizeDate = window.normalizeDate;
const pluralize = window.pluralize;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;

function AdminEditTournament({ tournament, onCancel, onSave, onLogout, onViewerMode }) {
  const [name, setName] = useStateA(tournament.name);
  const [venue, setVenue] = useStateA(tournament.venue);
  const [date, setDate] = useStateA(tournament.date);
  const [courts, setCourts] = useStateA(tournament.courts.length);
  const [pass, setPass] = useStateA(""); // Leave empty to keep existing, unless changed
  const [error, setError] = useStateA("");

  const handleSave = () => {
    if (!name.trim()) { setError("Tournament name is required."); return; }
    const norm = normalizeDate(date);
    if (!/^\d{4}-\d{2}-\d{2}$/.test(norm)) { setError("Invalid date. Please pick a valid day."); return; }
    const year = parseInt(norm.substring(0, 4));
    if (year < 1900 || year > 2100) { setError("Year must be between 1900 and 2100."); return; }
    if (courts < 1 || courts > 26) { setError("Number of courts must be between 1 and 26."); return; }

    onSave({
      name,
      venue,
      date: norm,
      password: pass || undefined,
      courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i))
    });
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 720 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onCancel },
          { label: "Edit details" }
        ]} />
        <div className="page-head"><h1 className="page-head__title">Edit tournament details</h1></div>
        {error && <div className="auth__error" style={{ marginBottom: 16 }}>{error}</div>}
        <div className="card card--pad-lg">
          <div className="row">
            <div className="field"><label className="field__label">Name</label><input className="input" value={name} onChange={(e) => { setName(e.target.value); setError(""); }} /></div>
            <div className="field">
              <label className="field__label">Date</label>
              <input className="input" type="date" value={date} onChange={(e) => { setDate(e.target.value); setError(""); }} />
              <div className="field__hint">Pick the tournament day.</div>
            </div>
          </div>
          <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => { setVenue(e.target.value); setError(""); }} /></div>
          <div className="field">
            <label className="field__label">Number of Shiaijo (courts)</label>
            <input className="input" type="number" min="1" max="26" value={courts} onChange={(e) => { setCourts(+e.target.value); setError(""); }} />
            <div className="field__hint">Enter a number (1-26). Courts will be automatically labeled A, B, C, etc.</div>
          </div>
          <div className="field">
            <label className="field__label">Admin Password</label>
            <input className="input" type="password" value={pass} onChange={(e) => { setPass(e.target.value); setError(""); }} placeholder="••••••••" autoComplete="new-password" />
            <div className="field__hint">Enter a new password to change it. Leave blank to keep the current one.</div>
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={handleSave}>Save changes</button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AdminCreateCompetition({ tournament, onCancel, onCreate, onLogout, onViewerMode }) {
  const [name, setName] = useStateA("");
  const [kind, setKind] = useStateA("individual");
  const [gender, setGender] = useStateA("M"); // for individual: M/F/X
  const [format, setFormat] = useStateA("playoffs");
  const [useSample, setUseSample] = useStateA(false);
  const [sampleSize, setSampleSize] = useStateA("medium");
  const [poolMode, setPoolMode] = useStateA("max");
  const [poolSize, setPoolSize] = useStateA(3);
  const [winners, setWinners] = useStateA(2);
  const [startTime, setStartTime] = useStateA("09:00");
  const [date, setDate] = useStateA(tournament.date);
  const [teamSize, setTeamSize] = useStateA(5);
  const [numberPrefix, setNumberPrefix] = useStateA("");
  const [withZekken, setWithZekken] = useStateA(false);
  const [selectedCourts, setSelectedCourts] = useStateA(tournament.courts.slice(0, Math.min(2, tournament.courts.length)));
  const [error, setError] = useStateA("");

  const toggleCourt = (cc) => setSelectedCourts((sc) => sc.includes(cc) ? sc.filter((c) => c !== cc) : [...sc, cc].sort());

  const create = () => {
    const finalName = name || (kind === "team"
      ? (gender === "F" ? "Women's Teams" : "Men's Teams")
      : (gender === "F" ? "Women's Individual" : gender === "M" ? "Men's Individual" : "Individual"));

    const exists = (tournament.competitions || []).some(cc => cc.name.toLowerCase() === finalName.toLowerCase());
    if (exists) {
      setError(`A competition named "${finalName}" already exists. Please use a unique name.`);
      return;
    }

    const slug = finalName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '').substring(0, 50);
    const id = slug || "c-" + Date.now().toString(36);
    const c = window.buildCompetition({
      id,
      name: finalName,
      kind, gender,
      format,
      sampleRoster: useSample ? sampleSize : null,
      seedCount: 0, status: "setup",
      startTime,
      date,
      teamSize: kind === "team" ? teamSize : 0,
      courts: selectedCourts.length ? selectedCourts : [tournament.courts[0]],
      poolMode, poolSize, winnersPerPool: winners,
      numberPrefix: numberPrefix.trim().substring(0, 3),
      withZekkenName: kind === "individual" ? withZekken : false,
    });
    onCreate(c);
  };

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 760 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: onCancel },
          { label: "Add competition" }
        ]} />
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Add competition</h1>
            <div className="page-head__sub">A competition is one event within the tournament — e.g. Men's Individual, Women's Teams.</div>
          </div>
        </div>

        {error && <div className="alert alert--error" style={{ marginBottom: 16 }}>{error}</div>}

        <div className="card card--pad-lg">
          <div className="field">
            <label className="field__label">Competition type</label>
            <div className="radio-group">
              <button className={`radio-pill ${kind === "individual" ? "is-active" : ""}`} type="button" onClick={() => setKind("individual")}>Individual</button>
              <button className={`radio-pill ${kind === "team" ? "is-active" : ""}`} type="button" onClick={() => setKind("team")}>Team</button>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Category (optional)</label>
            <div className="radio-group">
              <button className={`radio-pill ${gender === "M" ? "is-active" : ""}`} type="button" onClick={() => setGender("M")}>Men</button>
              <button className={`radio-pill ${gender === "F" ? "is-active" : ""}`} type="button" onClick={() => setGender("F")}>Women</button>
              <button className={`radio-pill ${gender === "X" ? "is-active" : ""}`} type="button" onClick={() => setGender("X")}>Mixed / Other</button>
            </div>
            <div className="field__hint">Used for the display label and in name suggestions. You can change later.</div>
          </div>

          <div className="row">
            <div className="field">
              <label className="field__label">Display name</label>
              <input className="input" placeholder="e.g. Men's Individual" value={name} onChange={(e) => { setName(e.target.value); setError(""); }} />
            </div>
            <div className="field">
              <label className="field__label">Start time</label>
              <input className="input" type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
            </div>
          </div>

          <div className="row">
            <div className="field">
              <label className="field__label">Date</label>
              <input className="input" type="date" value={date} onChange={(e) => setDate(e.target.value)} />
              <div className="field__hint">For multi-day tournaments, specify which day this competition takes place.</div>
            </div>
            <div className="field">
              <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
              <input className="input" placeholder="e.g. A" maxLength="3" value={numberPrefix} onChange={(e) => setNumberPrefix(e.target.value)} style={{ maxWidth: 80 }} />
              <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
            </div>
          </div>

          <div className="field">
            <label className="field__label">Format</label>
            <div className="radio-group">
              <button className={`radio-pill ${format === "playoffs" ? "is-active" : ""}`} type="button" onClick={() => setFormat("playoffs")}>Knockout only</button>
              <button className={`radio-pill ${format === "pools" ? "is-active" : ""}`} type="button" onClick={() => setFormat("pools")}>Pools + Knockout</button>
            </div>
            <div className="field__hint">"Pools + Knockout" runs round-robin pools first, then top finishers advance to a knockout bracket.</div>
          </div>

          <div className="field">
            <label className="field__label">
              <label className="checkbox" style={{ display: "inline-flex" }}>
                <input type="checkbox" checked={useSample} onChange={(e) => setUseSample(e.target.checked)} />
                Pre-fill with sample roster
              </label>
            </label>
            {useSample && (
              <div className="radio-group" style={{ marginTop: 8 }}>
                <button className={`radio-pill ${sampleSize === "small" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("small")}>Small (8)</button>
                <button className={`radio-pill ${sampleSize === "medium" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("medium")}>Medium (16)</button>
                <button className={`radio-pill ${sampleSize === "large" ? "is-active" : ""}`} type="button" onClick={() => setSampleSize("large")}>Large (32)</button>
              </div>
            )}
            <div className="field__hint">Leave off to add real participants in the next step.</div>
          </div>

          <div className="field">
            <label className="field__label">Assigned shiaijo (courts)</label>
            <div className="radio-group">
              {tournament.courts.map((cc) => (
                <button key={cc} className={`radio-pill ${selectedCourts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)}>Shiaijo (court) {cc}</button>
              ))}
            </div>
            <div className="field__hint">Concurrency for this competition equals the number of shiaijo (courts) assigned. Different competitions can share shiaijo (courts); the schedule prevents conflicts.</div>
          </div>

          {format === "pools" && (
            <>
              <div className="field">
                <label className="field__label">Pool size is a</label>
                <div className="radio-group">
                  <button className={`radio-pill ${poolMode === "max" ? "is-active" : ""}`} type="button" onClick={() => setPoolMode("max")}>maximum</button>
                  <button className={`radio-pill ${poolMode === "min" ? "is-active" : ""}`} type="button" onClick={() => setPoolMode("min")}>minimum</button>
                </div>
                <div className="field__hint">
                  {poolMode === "max"
                    ? "No pool will have more than the size below (more pools, smaller pools)."
                    : "Each pool will have at least the size below (fewer pools, larger pools)."}
                </div>
              </div>
              <div className="row">
                <div className="field"><label className="field__label">Players per pool</label><input className="input" type="number" min="3" value={poolSize} onChange={(e) => setPoolSize(+e.target.value)} /></div>
                <div className="field"><label className="field__label">Winners per pool</label><input className="input" type="number" min="1" value={winners} onChange={(e) => setWinners(+e.target.value)} /></div>
              </div>
            </>
          )}

          {kind === "team" && (
            <div className="field">
              <label className="field__label">Team size</label>
              <window.StableInput className="input" type="number" min="1" max="9" value={teamSize} onChange={(val) => setTeamSize(val)} />
              <div className="field__hint">Standard kendo team is 5 (Senpou, Jihou, Chuken, Fukushou, Taishou).</div>
            </div>
          )}

          {kind === "individual" && (
            <div className="field">
              <label className="checkbox"><input type="checkbox" checked={withZekken} onChange={(e) => setWithZekken(e.target.checked)} /> Use Zekken display name</label>
              <div className="field__hint" style={{ marginTop: 4 }}>When enabled, participant CSV uses three columns: Name, Zekken, Dojo.</div>
            </div>
          )}

          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={create}>Create & continue →</button>
          </div>
        </div>
      </div>
    </div>
  );
}


function AdminImportPage({ tournament, onBack, onImported, onLogout, onViewerMode, password }) {
  const [files, setFiles] = useStateA([]);
  const [preview, setPreview] = useStateA(null);
  const [loading, setLoading] = useStateA(false);
  const [results, setResults] = useStateA(null);
  const [error, setError] = useStateA(null);
  const folderRef = useRefA(null);
  const filesRef = useRefA(null);

  const collectFiles = (fileList) => {
    const arr = Array.from(fileList);
    setFiles(arr);
    setPreview(null);
    setResults(null);
    setError(null);

    // Try to parse manifest client-side for preview (JSON only — YAML needs server).
    const manifestFile = arr.find(f => f.name === "manifest.yaml" || f.name === "manifest.yml" || f.name === "manifest.json");
    if (manifestFile && manifestFile.name.endsWith(".json")) {
      const reader = new FileReader();
      reader.onload = (e) => {
        try {
          const m = JSON.parse(e.target.result);
          setPreview(m.competitions || []);
        } catch { setPreview(null); }
      };
      reader.readAsText(manifestFile);
    } else {
      setPreview(null);
    }
  };

  const doImport = async () => {
    if (!files.length) return;
    if (!confirm("Are you sure you want to import these competitions? This will add new competitions to the tournament.")) return;
    setLoading(true);
    setError(null);
    try {
      const fd = new FormData();
      files.forEach(f => fd.append("files", f, f.webkitRelativePath || f.name));
      const res = await fetch("/api/tournament/import", {
        method: "POST",
        headers: { "X-Tournament-Password": password },
        body: fd,
      });
      const body = await res.json();
      if (!res.ok) {
        setError(body.error || "Import failed");
      } else {
        setResults(body.results || []);
        const hasErrors = (body.results || []).some(r => r.error);
        if (!hasErrors) {
          setTimeout(onImported, 1500);
        }
      }
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const manifestFile = files.find(f => f.name === "manifest.yaml" || f.name === "manifest.yml" || f.name === "manifest.json");
  const csvFiles = files.filter(f => f.name.endsWith(".csv"));

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page">
        <Breadcrumbs items={[{ label: tournament?.name || "Tournament", onClick: onBack }, { label: "Import competitions" }]} />
        <h2 style={{ margin: "0 0 16px" }}>Import competitions</h2>

        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card__title">Select files</div>
          <div className="card__body">
            <p style={{ fontSize: 13, color: "var(--ink-2)", marginTop: 0 }}>
              Select a folder containing <strong>manifest.yaml</strong> and participant CSV files, or select the files individually.
              The manifest must list competitions with their CSV file names.
            </p>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap", marginBottom: 12 }}>
              <button className="btn btn--primary" onClick={() => folderRef.current?.click()}>Select folder</button>
              <button className="btn" onClick={() => filesRef.current?.click()}>Select files individually</button>
            </div>
            <input ref={folderRef} type="file" style={{ display: "none" }} webkitdirectory="true" multiple onChange={e => collectFiles(e.target.files)} />
            <input ref={filesRef} type="file" style={{ display: "none" }} multiple accept=".yaml,.yml,.json,.csv,.txt" onChange={e => collectFiles(e.target.files)} />

            {files.length > 0 && (
              <div>
                <div style={{ fontSize: 13, color: "var(--ink-2)", marginBottom: 6 }}>
                  {files.length} file{files.length !== 1 ? "s" : ""} selected
                  {manifestFile ? <span className="tag-badge">✓ manifest found: {manifestFile.name}</span> : <span className="tag-badge tag-badge--warn">⚠ no manifest.yaml found</span>}
                  {csvFiles.length > 0 && <span style={{ marginLeft: 6, fontSize: 12 }}>· {csvFiles.length} CSV file{csvFiles.length !== 1 ? "s" : ""}</span>}
                </div>
              </div>
            )}
          </div>
        </div>

        {preview && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__title">Preview ({preview.length} competitions)</div>
            <div className="card__body">
              <table className="parse-preview" style={{ width: "100%" }}>
                <thead><tr><th>ID</th><th>Name</th><th>Format</th><th>Participants file</th><th>Seeds file</th></tr></thead>
                <tbody>
                  {preview.map(comp => (
                    <tr key={comp.id || comp.name}>
                      <td>{comp.id || "—"}</td>
                      <td>{comp.name || "—"}</td>
                      <td>{comp.format || "pools"}</td>
                      <td className={!comp.participants ? "cell--missing" : ""}>{comp.participants || "—"}</td>
                      <td>{comp.seeds || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {error && <div className="alert alert--error" style={{ marginBottom: 16 }}>{error}</div>}

        {results && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__title">Import results</div>
            <div className="card__body">
              {results.map(r => (
                <div key={r.id} style={{ padding: "6px 0", borderBottom: "1px solid var(--border)", display: "flex", gap: 8, alignItems: "center" }}>
                  <div style={{ flex: 1 }}>
                    <strong>{r.name || r.id}</strong>
                    {!r.error && <span style={{ fontSize: 12, color: "var(--ink-3)", marginLeft: 8 }}>{pluralize(r.participantCount, "participant")} {r.seedCount > 0 ? `, ${pluralize(r.seedCount, "seed")}` : ""}</span>}
                  </div>
                  {r.error
                    ? <span className="tag-badge tag-badge--warn">✕ {r.error}</span>
                    : <span className="tag-badge">✓ imported</span>}
                </div>
              ))}
              {!results.some(r => r.error) && (
                <div className="alert alert--success" style={{ marginTop: 12 }}>All competitions imported successfully. Returning to dashboard…</div>
              )}
            </div>
          </div>
        )}

        <div style={{ display: "flex", gap: 10 }}>
          <button className="btn btn--primary" onClick={doImport} disabled={!manifestFile || loading}>
            {loading ? "Importing…" : "Import"}
          </button>
          <button className="btn" onClick={onBack}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

window.AdminEditTournament = AdminEditTournament;
window.AdminCreateCompetition = AdminCreateCompetition;
window.AdminImportPage = AdminImportPage;
