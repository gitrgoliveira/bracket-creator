// Participants section of a competition: paste/import roster, edit seeds,
// manage reserved slots. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

const pluralize = window.pluralize;

// Returns true when line (at index idx in the source array) looks like a CSV
// header that should be skipped.  Checks the first line only.
function looksLikeHeader(line, idx) {
  if (idx !== 0) return false;
  // More robust header detection: contains multiple keywords commonly found in headers
  const keywords = ['name', 'zekken', 'dojo', 'club', 'team', 'grade', 'dan', 'rank'];
  const lower = line.toLowerCase();
  const matched = keywords.filter(k => lower.includes(k));
  // If it contains at least 2 keywords, it's likely a header.
  // Or if it matches a very specific pattern like "Name, Dojo".
  if (matched.length >= 2) return true;
  return false;
}

function parsePastedRows(text, transform) {
  const out = [];
  text.split(/\r?\n/).forEach((line, i) => {
    const trimmed = line.trim();
    if (!trimmed || looksLikeHeader(trimmed, i)) return;
    out.push(transform ? transform(trimmed) : trimmed);
  });
  return out;
}

function LinedTextarea({ value, onChange, onFocus, onBlur, rows, placeholder }) {
  const textareaRef = useRefA(null);
  const numsRef = useRefA(null);
  const lineCount = Math.max(1, (value || '').split('\n').length);
  const nums = Array.from({ length: lineCount }, (_, i) => i + 1).join('\n');

  const syncScroll = () => {
    if (numsRef.current && textareaRef.current) {
      numsRef.current.scrollTop = textareaRef.current.scrollTop;
    }
  };

  return (
    <div className="lined-textarea">
      <div ref={numsRef} className="lined-textarea__nums" aria-hidden="true">{nums}</div>
      <textarea
        ref={textareaRef}
        className="lined-textarea__area"
        value={value}
        onChange={onChange}
        onFocus={onFocus}
        onBlur={onBlur}
        onScroll={syncScroll}
        rows={rows}
        placeholder={placeholder}
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
      />
    </div>
  );
}

function levenshtein(a, b) {
  const m = a.length, n = b.length;
  if (m === 0) return n;
  if (n === 0) return m;
  let prev = Array.from({ length: n + 1 }, (_, j) => j);
  let curr = new Array(n + 1);
  for (let i = 1; i <= m; i++) {
    curr[0] = i;
    for (let j = 1; j <= n; j++)
      curr[j] = a[i - 1] === b[j - 1] ? prev[j - 1] : 1 + Math.min(prev[j], curr[j - 1], prev[j - 1]);
    [prev, curr] = [curr, prev];
  }
  return prev[n];
}

function AdminParticipants({ c, tournament, reservedSlots, onUpdate, password, showToast, onSection }) {
  const [showAllPreview, setShowAllPreview] = useStateA(false);
  const [seedImportResult, setSeedImportResult] = useStateA(null);
  const [importSummary, setImportSummary] = useStateA(null);
  const [text, setText] = useStateA(() => (c.players || []).map((p) => {
    if (c.withZekkenName && p.displayName) {
      const base = `${p.name ?? ""}, ${p.displayName ?? ""}, ${p.dojo ?? ""}`;
      return p.danGrade ? `${base}, ${p.danGrade}` : base;
    }
    const base = `${p.name ?? ""}, ${p.dojo ?? ""}`;
    return p.danGrade ? `${base}, ${p.danGrade}` : base;
  }).join("\n"));
  const [dragOver, setDragOver] = useStateA(false);
  const fileRef = useRefA(null);
  const seedFileRef = useRefA(null);
  const textFocusRef = useRefA(false);

  const generateText = (playersList) => (playersList || []).map((p) => {
    if (c.withZekkenName && p.displayName) {
      const base = `${p.name ?? ""}, ${p.displayName ?? ""}, ${p.dojo ?? ""}`;
      return p.danGrade ? `${base}, ${p.danGrade}` : base;
    }
    const base = `${p.name ?? ""}, ${p.dojo ?? ""}`;
    return p.danGrade ? `${base}, ${p.danGrade}` : base;
  }).join("\n");

  useEffectA(() => {
    if (!textFocusRef.current) {
      setText(generateText(c.players || []));
    }
  }, [c.players, c.withZekkenName]);

  const handleSeedFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const raw = e.target.result;
      const np = [...(c.players || [])];
      let updatedCount = 0;
      const unmatched = [];
      const allNames = np.map(p => ({ name: p.name, display: p.displayName }));

      raw.split(/\r?\n/).forEach((line, i) => {
        const trimmed = line.trim();
        if (!trimmed) return;
        // Skip header if it looks like one
        if (i === 0 && (/name/i.test(trimmed) || /zekken/i.test(trimmed)) && /seed|rank/i.test(trimmed)) return;

        const parts = trimmed.split(",").map(s => s.trim());
        if (parts.length >= 2) {
          const name = parts[0];
          const seedStr = parts[1];
          const seed = parseInt(seedStr);

          if (name && !isNaN(seed) && seed > 0) {
            const nameLower = name.toLowerCase();
            const pIdx = np.findIndex(p =>
              p.name.toLowerCase() === nameLower ||
              (p.displayName && p.displayName.toLowerCase() === nameLower) ||
              p.name.toLowerCase().includes(nameLower) ||
              nameLower.includes(p.name.toLowerCase())
            );

            if (pIdx >= 0) {
              np[pIdx] = { ...np[pIdx], seed };
              updatedCount++;
            } else {
              // Find closest name suggestion
              let best = null, bestDist = Infinity;
              allNames.forEach(({ name: n, display: d }) => {
                const d1 = levenshtein(nameLower, n.toLowerCase());
                const d2 = d ? levenshtein(nameLower, d.toLowerCase()) : Infinity;
                const dist = Math.min(d1, d2);
                if (dist < bestDist) { bestDist = dist; best = n; }
              });
              unmatched.push({ name, seed, suggestion: bestDist <= 5 ? best : null });
            }
          }
        }
      });

      if (updatedCount > 0) {
        onUpdate({ ...c, players: np });
        showToast(`Matched ${updatedCount} seeds`);
      }
      setSeedImportResult({ updatedCount, unmatched, totalRows: updatedCount + unmatched.length });
    };
    reader.readAsText(file);
  };

  const handleFile = (file) => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (e) => {
      const parsedLines = parsePastedRows(e.target.result);
      const newText = parsedLines.join("\n");
      setText(newText);

      const newCount = parsedLines.length;
      const existingCount = (c.players || []).length;
      setImportSummary({ newCount, existingCount });

      showToast(`CSV loaded: ${newCount} entries.`);
    };
    reader.readAsText(file);
  };

  const [tagFilter, setTagFilter] = useStateA(null);
  const lines = useMemoA(() => text.split("\n").filter((l) => l.trim()), [text]);
  const players = useMemoA(() => c.players || [], [c.players]);
  const allTags = useMemoA(() => [...new Set(players.map(p => p.tag).filter(Boolean))], [players]);
  const visiblePlayers = useMemoA(() => tagFilter ? players.filter(p => p.tag === tagFilter) : players, [players, tagFilter]);
  const { gaps, hasGaps } = useMemoA(() => {
    const sortedSeeds = players.filter(p => p.seed).map(p => p.seed).sort((a, b) => a - b);
    const gaps = [];
    if (sortedSeeds.length > 0) {
      const maxSeed = sortedSeeds[sortedSeeds.length - 1];
      for (let s = 1; s <= maxSeed; s++) {
        if (!sortedSeeds.includes(s)) gaps.push(s);
      }
    }
    return { gaps, hasGaps: gaps.length > 0 };
  }, [players]);

  const updateSeed = (idx, val) => {
    const np = [...(c.players || [])];
    const seed = parseInt(val);
    np[idx] = { ...np[idx], seed: isNaN(seed) || seed <= 0 ? null : seed };
    onUpdate({ ...c, players: np });
  };

  const dragIdxRef = useRefA(null);
  const [dragOverIdx, setDragOverIdx] = useStateA(null);
  const moveSeedRow = (fromIdx, toIdx) => {
    if (fromIdx === toIdx) return;
    const np = [...(c.players || [])];
    const [moved] = np.splice(fromIdx, 1);
    np.splice(toIdx, 0, moved);
    // Re-assign seeds by new position order (only for currently-seeded players)
    const seededCount = np.filter(p => p.seed).length;
    if (seededCount > 0) {
      let rank = 1;
      const renumbered = np.map(p => p.seed ? { ...p, seed: rank++ } : p);
      onUpdate({ ...c, players: renumbered });
    } else {
      onUpdate({ ...c, players: np });
    }
  };

  const shuffleUnseeded = () => {
    const np = [...(c.players || [])];
    const unseeded = np.filter(p => !p.seed);
    if (unseeded.length < 2) return;
    for (let i = unseeded.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [unseeded[i], unseeded[j]] = [unseeded[j], unseeded[i]];
    }
    let uIdx = 0;
    const shuffled = np.map(p => p.seed ? p : unseeded[uIdx++]);
    onUpdate({ ...c, players: shuffled });
    showToast("Unseeded list shuffled");
  };

  const [showSlotForm, setShowSlotForm] = useStateA(false);
  const [slotSrcComp, setSlotSrcComp] = useStateA("");
  const [slotRank, setSlotRank] = useStateA(1);
  const [slotLoading, setSlotLoading] = useStateA(false);

  const otherComps = (tournament?.competitions || []).filter(cc => cc.id !== c.id);

  const addSlot = async () => {
    if (!slotSrcComp || slotRank < 1) return;
    setSlotLoading(true);
    try {
      await window.API.addReservedSlot(c.id, slotSrcComp, slotRank, password);
      setShowSlotForm(false);
      setSlotSrcComp("");
      setSlotRank(1);
      showToast("Reserved slot added");
    } catch (e) {
      alert("Failed to add reserved slot: " + e.message);
    } finally {
      setSlotLoading(false);
    }
  };

  const removeSlot = async (slotID) => {
    try {
      await window.API.deleteReservedSlot(c.id, slotID, password);
      showToast("Reserved slot removed");
    } catch (e) {
      alert("Failed to remove reserved slot: " + e.message);
    }
  };

  const apply = () => {
    try {
      const withZekken = c.withZekkenName;
      // Key by lowercase name so a casing-only edit (e.g. "Alice" → "alice")
      // preserves the existing player's id and seed — matches the
      // case-insensitive duplicate-detection check below.
      const existingMap = new Map((c.players || []).map(p => [p.name.toLowerCase(), p]));
      const parsed = window.parseParticipantLines(lines, withZekken);

      // Duplicate detection (case-insensitive)
      const nameSeen = new Map();
      const dupes = [];
      parsed.forEach(({ name }) => {
        const key = name.toLowerCase();
        if (nameSeen.has(key)) { if (!dupes.includes(name)) dupes.push(name); }
        else nameSeen.set(key, true);
      });
      if (dupes.length > 0) {
        showToast(`Duplicate names detected: ${dupes.join(", ")}`, "error");
        return;
      }

      // ID generation: existing players keep their stable id; new players
      // get the next free `${c.id}-pN` slot. Tracking used ids prevents a
      // new player's index-based id from colliding with an existing
      // player's id when the visible row order differs from the original
      // assignment order (e.g. after drag-reordering seeds).
      let added = 0, updatedCount = 0;
      const usedIds = new Set();
      let nextSlot = 1;
      const np = parsed.map(({ name, displayName, dojo, danGrade, tag }) => {
        const existing = existingMap.get(name.toLowerCase());
        if (existing) {
          updatedCount++;
          usedIds.add(existing.id);
          return { id: existing.id, name, displayName, dojo, danGrade, tag, seed: existing.seed || null };
        }
        added++;
        while (usedIds.has(`${c.id}-p${nextSlot}`)) nextSlot++;
        const id = `${c.id}-p${nextSlot}`;
        usedIds.add(id);
        nextSlot++;
        return { id, name, displayName, dojo, danGrade, tag, seed: null };
      });
      onUpdate({ ...c, players: np });

      const label = c.kind === "team" ? "team" : "participant";
      let msg = `Saved ${pluralize(np.length, label)}`;
      if (added > 0 || updatedCount > 0) {
        msg += ` (${added} new, ${updatedCount} updated)`;
      }
      showToast(msg);
      setImportSummary(null);
    } catch (err) {
      console.error("AdminParticipants: Apply failed", err);
      showToast("Failed to apply participants: " + err.message, "error");
    }
  };

  const onDrop = (e) => {
    e.preventDefault();
    setDragOver(false);
    const file = e.dataTransfer.files[0];
    handleFile(file);
  };

  const pasteFromExcel = async () => {
    try {
      const clipboardText = await navigator.clipboard.readText();
      // Normalise tabs to commas, strip leading row numbers (e.g. "1, Name, Dojo")
      setText(parsePastedRows(clipboardText, (s) => s.replace(/\t/g, ", ").replace(/^\d+,\s*/, "")).join("\n"));
    } catch (err) {
      console.error("Paste failed", err);
      alert("Failed to read clipboard: " + err.message + "\n\nMake sure you have granted clipboard permissions.");
    }
  };

  const downloadTemplate = () => {
    const content = c.kind === "team"
      ? "Team Name, Dojo\nTora A, Tora Dojo London\n"
      : c.withZekkenName
        ? "Name, Zekken, Dojo, Dan\nAkira Tanaka, TANAKA, Mumeishi, 3\n"
        : "Name, Dojo, Dan\nAkira Tanaka, Mumeishi, 3\n";
    const blob = new Blob([content], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "participants_template.csv";
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  };

  const isStarted = c.status !== "setup";
  return (
    <>
      {isStarted && (
        <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
          <button className="btn btn--primary" onClick={() => onSection("scores")}>Go to Scoring →</button>
        </div>
      )}
      <div className="row row--equal" style={{ alignItems: "start" }}>
        <div className="card">
          <div className="card__head">
            <div>
              <div className="card__title">{c.kind === "team" ? "Team list" : "Participant list"}</div>
              <div className="card__sub">
                {lines.length} entries · One per line · <span style={{ color: "var(--ink-2)", fontWeight: 600 }}>Example: Alice Smith, Mumeishi, 3</span>
              </div>
              <div className="field__hint" style={{ marginTop: 2, fontSize: 11 }}>
                Format: "{c.kind === "team" ? "Team name, Dojo" : c.withZekkenName ? "Name, Zekken, Dojo[, Dan]" : "Name, Dojo[, Dan grade]"}"
                <br />* Dan = kendo grade (optional)
                <br /><button className="btn--link" style={{ padding: 0, fontSize: 11, fontWeight: 600 }} onClick={downloadTemplate}>Download CSV template</button>
              </div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <button className="btn btn--sm" type="button" onClick={pasteFromExcel} title="Reads clipboard and converts tab-separated values (e.g. from Excel) to CSV">Paste clipboard</button>
              <button className="btn btn--sm btn--primary" type="button" onClick={apply} disabled={hasGaps}>Apply changes</button>
            </div>
          </div>

          <div style={{ display: "flex", gap: 10, marginBottom: 12 }}>
            <div
              className={`dropzone ${dragOver ? "dropzone--active" : ""}`}
              onClick={() => fileRef.current?.click()}
              onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              style={{ flex: 1, height: 80, minHeight: 80 }}
            >
              <div className="dropzone__icon">📥</div>
              <div>
                <div className="dropzone__title">{dragOver ? "Drop CSV to import" : "Click or drop CSV to import participants"}</div>
                <div className="dropzone__sub">
                  {c.withZekkenName ? "Name, Zekken, Dojo[, Dan]" : "Name, Dojo[, Dan grade] (e.g. Alice Smith, Mumeishi, 3)"}
                </div>
              </div>
              <input ref={fileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleFile(e.target.files[0])} />
            </div>
          </div>

          {importSummary && (
            <div className="alert alert--success" style={{ marginBottom: 12, display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span>✔ Loaded <strong>{importSummary.newCount}</strong> entries. {importSummary.existingCount > 0 ? `This will replace ${importSummary.existingCount} existing ${c.kind === "team" ? "teams" : "players"} on Apply.` : ""}</span>
              <button className="btn btn--sm btn--ghost" onClick={() => setImportSummary(null)}>Dismiss</button>
            </div>
          )}

          <LinedTextarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onFocus={() => { textFocusRef.current = true; }}
            onBlur={() => { textFocusRef.current = false; }}
            rows={14}
            placeholder={c.kind === "team" ? "Tora A, Tora Dojo London" : c.withZekkenName ? "Akira Tanaka, TANAKA, Mumeishi" : "Akira Tanaka, Mumeishi"}
          />
          <div className="field__hint" style={{ marginTop: 6 }}>Click "Apply" to save the participant list. Existing seeds are preserved by name match (case-insensitive), so you can reorder rows freely.</div>
          {otherComps.length > 0 && (
            <div style={{ marginTop: 10 }}>
              {!showSlotForm ? (
                <button className="btn btn--sm" onClick={() => setShowSlotForm(true)}>+ Reserved slot</button>
              ) : (
                <div style={{ padding: "10px 0", display: "flex", flexDirection: "column", gap: 8 }}>
                  <div style={{ fontWeight: 600, fontSize: 13 }}>Add reserved slot</div>
                  <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
                    <div style={{ flex: 2 }}>
                      <div className="field__label">Source competition</div>
                      <select className="field__select" value={slotSrcComp} onChange={e => setSlotSrcComp(e.target.value)}>
                        <option value="">Select…</option>
                        {otherComps.map(cc => <option key={cc.id} value={cc.id}>{cc.name}</option>)}
                      </select>
                    </div>
                    <div style={{ flex: 1 }}>
                      <div className="field__label">Rank</div>
                      <input className="field__input" type="number" min={1} value={slotRank} onChange={e => setSlotRank(+e.target.value)} />
                    </div>
                    <div style={{ display: "flex", gap: 6 }}>
                      <button className="btn btn--sm btn--primary" onClick={addSlot} disabled={!slotSrcComp || slotRank < 1 || slotLoading}>Add</button>
                      <button className="btn btn--sm" onClick={() => setShowSlotForm(false)}>Cancel</button>
                    </div>
                  </div>
                  <div className="field__hint">The placeholder participant will be replaced with the real player when the source competition reaches playoffs.</div>
                </div>
              )}
            </div>
          )}
          {lines.length > 0 && (() => {
            const previewLimit = showAllPreview ? lines.length : 10;
            const preview = window.parseParticipantLines(lines.slice(0, previewLimit), c.withZekkenName);
            const cols = c.withZekkenName ? ["Name", "Zekken", "Dojo", "Dan"] : ["Name", "Dojo", "Dan"];
            return (
              <div style={{ marginTop: 8, overflowX: "auto" }}>
                <table className="parse-preview">
                  <thead><tr>{cols.map(h => <th key={h}>{h}</th>)}</tr></thead>
                  <tbody>{preview.map((p, i) => (
                    <tr key={i}>
                      <td className={!p.name ? "cell--missing" : ""}>{p.name || "—"}</td>
                      {c.withZekkenName && <td className={!p.displayName ? "cell--missing" : ""}>{p.displayName || "—"}</td>}
                      <td className={!p.dojo ? "cell--missing" : ""}>{p.dojo || "—"}</td>
                      <td>{p.danGrade || "—"}</td>
                    </tr>
                  ))}</tbody>
                </table>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 4 }}>
                  <div className="field__hint">Preview of {Math.min(lines.length, previewLimit)} of {lines.length} rows</div>
                  {lines.length > 10 && (
                    <button className="btn btn--ghost btn--sm" style={{ color: "var(--accent)", padding: "2px 6px" }} onClick={() => setShowAllPreview(!showAllPreview)}>
                      {showAllPreview ? "Show less" : "Show all"}
                    </button>
                  )}
                </div>
              </div>
            );
          })()}
        </div>
        <div className="card">
          <div className="card__head">
            <div>
              <div className="card__title">Seeding</div>
              <div className="card__sub">{players.filter((p) => p.seed).length} of {players.length} seeded</div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <button className="btn btn--sm" type="button" onClick={shuffleUnseeded} disabled={players.length === 0} title="Shuffle unseeded players">Shuffle unseeded</button>
              <button className="btn btn--sm" type="button" onClick={() => seedFileRef.current?.click()} disabled={players.length === 0} title={players.length === 0 ? "Add participants first" : undefined}>Import Seeds CSV</button>
              <input ref={seedFileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleSeedFile(e.target.files[0])} />
              <button className="btn btn--sm" type="button" onClick={() => onUpdate({ ...c, players: c.players.map((p) => ({ ...p, seed: null })) })}>Clear seeds</button>
            </div>
          </div>
          <div className="card__body" style={{ paddingTop: 0, paddingBottom: 8 }}>
            <div className="field__hint" style={{ marginBottom: 12 }}>
              Assign seed ranks (1, 2, 3…) to separate top players. Seeds 1 and 2 will be placed on opposite sides of the bracket.
              Drag rows to change order.
            </div>
          </div>
          {hasGaps && (
            <div className="alert alert--error" style={{ margin: "0 16px 16px" }}>
              ❌ Seed gap detected: rank {gaps.join(", ")} {gaps.length > 1 ? "are" : "is"} missing. Seeds must be sequential (1, 2, 3…).
            </div>
          )}
          {seedImportResult && (
            <div style={{ margin: "0 16px 12px" }}>
              <div className="alert alert--success" style={{ marginBottom: 6 }}>
                ✔ Matched {seedImportResult.updatedCount} of {seedImportResult.totalRows} seeded players.
              </div>
              {seedImportResult.unmatched.length > 0 && (
                <div className="alert alert--warn">
                  ⚠ {seedImportResult.unmatched.length} row{seedImportResult.unmatched.length !== 1 ? "s" : ""} not matched:
                  <ul style={{ margin: "4px 0 0 16px", padding: 0 }}>
                    {seedImportResult.unmatched.map(({ name, suggestion }) => (
                      <li key={name}>{name}{suggestion ? <span style={{ color: "var(--ink-3)" }}> — did you mean <em>{suggestion}</em>?</span> : ""}</li>
                    ))}
                  </ul>
                </div>
              )}
              <button className="btn btn--sm" style={{ marginTop: 4 }} onClick={() => setSeedImportResult(null)}>Dismiss</button>
            </div>
          )}
          {allTags.length > 0 && (
            <div style={{ padding: "0 16px 10px", display: "flex", gap: 6, flexWrap: "wrap" }}>
              <button className={`radio-pill ${!tagFilter ? "is-active" : ""}`} onClick={() => setTagFilter(null)}>All</button>
              {allTags.map(t => (
                <button key={t} className={`radio-pill ${tagFilter === t ? "is-active" : ""}`} onClick={() => setTagFilter(tagFilter === t ? null : t)}>{t}</button>
              ))}
            </div>
          )}
          {players.length === 0 ? (
            <div className="empty" style={{ padding: 24 }}>
              <div className="icon">🌱</div>
              <h3>No participants yet</h3>
              <div style={{ fontSize: 12 }}>Add names on the left, then "Apply".</div>
            </div>
          ) : (
            <div className="seed-list">
              {/* When a tag filter is active, reorder controls would operate on */}
              {/* full-list indices but rows are filtered — so they'd swap with hidden */}
              {/* neighbours. Disable reordering until the filter is cleared. */}
              {tagFilter && (
                <div className="field__hint" style={{ padding: "0 16px 8px" }}>
                  Reordering disabled while filtered by tag. Clear the filter to drag rows or use the arrows.
                </div>
              )}
              {visiblePlayers.map((p) => {
                const i = players.indexOf(p);
                const reorderDisabled = !!tagFilter;
                return (
                  <div
                    key={p.id}
                    className={`seed-row ${p.seed ? "has-seed" : ""} ${dragOverIdx === i ? "seed-row--drop-target" : ""}`}
                    draggable={!reorderDisabled}
                    onDragStart={() => { dragIdxRef.current = i; }}
                    onDragOver={(e) => { if (reorderDisabled) return; e.preventDefault(); setDragOverIdx(i); }}
                    onDragLeave={() => { if (dragOverIdx === i) setDragOverIdx(null); }}
                    onDrop={() => {
                      if (reorderDisabled) return;
                      moveSeedRow(dragIdxRef.current, i);
                      dragIdxRef.current = null;
                      setDragOverIdx(null);
                    }}
                    style={{ cursor: reorderDisabled ? "default" : "grab" }}
                  >
                    <span className="seed-row__handle" title={reorderDisabled ? "Clear the tag filter to reorder" : "Drag to reorder"}>⠿</span>
                    <span className="seed-row__rank">{p.seed ? `#${p.seed}` : ""}</span>
                    <div style={{ flex: 1 }}>
                      <div className="seed-row__name" title={p.name}>{p.name}{p.tag && <span className="tag-badge">{p.tag}</span>}</div>
                      <div className="seed-row__dojo">{p.dojo}</div>
                    </div>
                    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                      <button className="btn btn--sm btn--icon-sm" onClick={() => moveSeedRow(i, i - 1)} disabled={i === 0 || reorderDisabled} aria-label="Move up">↑</button>
                      <button className="btn btn--sm btn--icon-sm" onClick={() => moveSeedRow(i, i + 1)} disabled={i === players.length - 1 || reorderDisabled} aria-label="Move down">↓</button>
                    </div>
                     <window.StableInput
                        className="seed-row__input"
                        type="number"
                        placeholder="—"
                        value={p.seed || ""}
                        onChange={(val) => updateSeed(i, val)}
                        autoSelect={false}
                      />
                  </div>
                );
              }
              )}
            </div>
          )}
        </div>
      </div>
      {reservedSlots && reservedSlots.length > 0 && (
        <div className="card" style={{ marginTop: 12 }}>
          <div className="card__head">
            <div className="card__title">Reserved slots ({reservedSlots.length})</div>
          </div>
          <div className="card__body" style={{ padding: "0 0 8px" }}>
            {reservedSlots.map(slot => {
              const srcComp = (tournament?.competitions || []).find(cc => cc.id === slot.sourceCompID);
              const ready = srcComp && (srcComp.status === "playoffs" || srcComp.status === "completed");
              return (
                <div key={slot.id} style={{ display: "flex", alignItems: "center", padding: "8px 16px", gap: 8, borderBottom: "1px solid var(--border)" }}>
                  <div style={{ flex: 1 }}>
                    <div style={{ fontWeight: 500, fontSize: 13 }}>
                      {srcComp?.name || slot.sourceCompID} — rank {slot.sourceRank}
                    </div>
                    <span className={`tag-badge ${ready ? "" : "tag-badge--warn"}`}>
                      {ready ? "✓ Source ready" : "⚠ Source not yet in playoffs"}
                    </span>
                  </div>
                  <button className="btn btn--sm btn--danger" onClick={() => removeSlot(slot.id)} title="Remove reserved slot">✕</button>
                </div>
              );
            })}
            {reservedSlots.some(s => { const src = (tournament?.competitions || []).find(cc => cc.id === s.sourceCompID); return !src || (src.status !== "playoffs" && src.status !== "completed"); }) && (
              <div className="alert alert--warn" style={{ margin: "12px 16px 4px" }}>
                ⚠ Some reserved slots cannot be resolved yet. The competition cannot be started until all source competitions have reached playoffs.
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}

window.AdminParticipants = AdminParticipants;
