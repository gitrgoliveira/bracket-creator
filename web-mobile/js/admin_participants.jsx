// Participants section of a competition: paste/import roster, edit seeds,
// manage reserved slots. See web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

const pluralize = window.pluralize;
const decideNumericUpdate = window.decideNumericUpdate;

// Returns true when line (at index idx in the source array) looks like a CSV
// header that should be skipped.  Checks the first line only.
function looksLikeHeader(line, idx) {
  if (idx !== 0) return false;
  // Header heuristic: the first line is treated as a header if it contains 2+
  // of the common roster-column keywords below. Two-keyword threshold avoids
  // false-positives on a single-column "name" header line that may overlap
  // a real participant whose name contains one of these words.
  const keywords = ['name', 'zekken', 'dojo', 'club', 'team', 'grade', 'dan', 'rank'];
  const lower = line.toLowerCase();
  const matched = keywords.filter(k => lower.includes(k));
  return matched.length >= 2;
}

// Find the participant whose canonical `name` or `displayName` exactly
// matches `candidate` (case-insensitive). Returns the index in `players`,
// or -1 if no exact match exists.
//
// Older versions also accepted *substring* matches in either direction
// ("Bob".includes("bob") OR "bob".includes("bob smith")), which silently
// produced wrong matches when one participant's name was a prefix of
// another (e.g. roster has both "Bob" and "Bob Smith" → CSV row "Bob, 3"
// matched "Bob Smith" by array order, leaving "Bob" unseeded). The
// substring path is intentionally removed: when an exact match isn't
// found, the caller surfaces the row via the unmatched-with-suggestion
// UI so the admin can confirm the right participant explicitly.
function findSeedMatchIndex(players, candidate) {
  const lower = candidate.toLowerCase();
  return players.findIndex(p =>
    p.name.toLowerCase() === lower ||
    (p.displayName && p.displayName.toLowerCase() === lower)
  );
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

// Build the participants list to save by reconciling existing players
// against a newly-parsed roster. Returns { np, added, updatedCount }.
//
// - Existing players (matched by lowercase name) keep their stable id
//   and seed.
// - New players get the next free `${compID}-pN` slot, skipping any id
//   already in use by an existing player who is still in the parsed
//   list (two-pass: pre-populate usedIds before minting, so visible row
//   order can't cause collisions).
// - IDs of *removed* players (in c.players but not in parsed) are
//   intentionally not reserved, so their slots can be reused — keeps
//   the `${compID}-pN` numbering compact.
//
// Exported for tests in __tests__/admin_participants.test.jsx.
function mintParticipantIds(compID, existingPlayers, parsed) {
  const existingMap = new Map((existingPlayers || []).map(p => [p.name.toLowerCase(), p]));
  const parsedKeys = new Set(parsed.map(p => p.name.toLowerCase()));
  const usedIds = new Set();
  (existingPlayers || []).forEach(p => {
    if (parsedKeys.has(p.name.toLowerCase())) usedIds.add(p.id);
  });
  let nextSlot = 1;
  let added = 0, updatedCount = 0;
  const np = parsed.map(({ name, displayName, dojo, danGrade, tag }) => {
    const existing = existingMap.get(name.toLowerCase());
    if (existing) {
      updatedCount++;
      return { id: existing.id, name, displayName, dojo, danGrade, tag, seed: existing.seed || null };
    }
    added++;
    while (usedIds.has(`${compID}-p${nextSlot}`)) nextSlot++;
    const id = `${compID}-p${nextSlot}`;
    usedIds.add(id);
    nextSlot++;
    return { id, name, displayName, dojo, danGrade, tag, seed: null };
  });
  return { np, added, updatedCount };
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

function CheckInBanner({ tournament, players }) {
  const [nowStr, setNowStr] = useStateA(() => {
    const d = new Date();
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  });

  useEffectA(() => {
    const timer = setInterval(() => {
      const d = new Date();
      setNowStr(`${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`);
    }, 60000);
    return () => clearInterval(timer);
  }, []);

  if (!tournament?.checkInWindowStart || !tournament?.checkInWindowEnd) return null;
  const start = tournament.checkInWindowStart;
  const end = tournament.checkInWindowEnd;

  const diffStart = window.diffMinutes(start, nowStr);
  const diffEnd = window.diffMinutes(end, nowStr);

  if (diffStart > 0) {
    return (
      <div className="alert alert--info" style={{ marginBottom: 12 }}>
        🕒 Check-in window starts at {start} (in {diffStart}m)
      </div>
    );
  }
  if (diffEnd > 0) {
    return (
      <div className="alert alert--success" style={{ marginBottom: 12 }}>
        ✅ Check-in window is OPEN: {start}–{end} (closes in {diffEnd}m)
      </div>
    );
  }

  const uncheckeds = (players || []).filter(p => !p.checkedIn).length;
  return (
    <div className="alert alert--warn" style={{ marginBottom: 12 }}>
      ⌛ Check-in CLOSED — {uncheckeds > 0 ? `${uncheckeds} participants did not check in` : "all participants checked in"}
    </div>
  );
}

function AdminParticipants({ c, tournament, reservedSlots, onUpdate, password, showToast, onSection }) {
  const [showOnlyUnchecked, setShowOnlyUnchecked] = useStateA(false);
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
  // apply() is async (awaits onUpdate which now re-throws on PUT failure).
  // If the user navigates away (or AdminCompetition unmounts AdminParticipants)
  // while the PUT is in flight, the post-await setImportSummary(null) would
  // setState on a torn-down component. Track mount state via ref and
  // gate post-await setState behind it. Pre-fix this was silent under
  // React 18 but still a real teardown-race signal.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
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
    // reader.onload is an async closure so we can await onUpdate before
    // showing the "Matched N seeds" success toast. Pre-fix: the PUT was
    // fire-and-forget (Promise.resolve(...).catch(()=>{})) and the
    // success toast fired immediately — on PUT failure the user saw
    // "Matched N seeds" followed (~1s later) by an error toast, which
    // misleads about whether the persisted state was actually updated.
    reader.onload = async (e) => {
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
            const pIdx = findSeedMatchIndex(np, name);

            if (pIdx >= 0) {
              np[pIdx] = { ...np[pIdx], seed };
              updatedCount++;
            } else {
              // Find closest name suggestion via Levenshtein distance —
              // surfaced in the unmatched-rows UI so the admin can pick
              // the right participant explicitly.
              const nameLower = name.toLowerCase();
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

      // Always render the summary panel with the parsed counts (matched
      // + unmatched rows) so the admin sees what the file produced even
      // when the save fails. The PUT only fires when there's something
      // to save; the success toast only fires on successful save.
      //
      // Pre-fix-of-fix: the catch branch set updatedCount=0 in the
      // summary, which hid the parsed-match count from the admin after
      // a PUT failure (they saw "0 matched, K unmatched" even though
      // parse found N matches). Fix: surface the parsed N regardless
      // of save outcome; the success toast (and updateCompetition's
      // error toast) are what communicate save status.
      let saveError = null;
      if (updatedCount > 0) {
        try {
          await onUpdate({ ...c, players: np });
        } catch (err) {
          // updateCompetition already surfaced an error toast. Log for
          // dev-console diagnosis but don't emit a second toast or a
          // misleading "Matched N seeds" success message.
          console.error("AdminParticipants: seed import PUT failed", err);
          saveError = err;
        }
        if (!mountedRef.current) return;
        if (!saveError) {
          showToast(`Matched ${updatedCount} seeds`);
        }
      }
      if (mountedRef.current) {
        setSeedImportResult({ updatedCount, unmatched, totalRows: updatedCount + unmatched.length });
      }
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
  const visiblePlayers = useMemoA(() => {
    let out = players;
    if (tagFilter) out = out.filter(p => p.tag === tagFilter);
    if (showOnlyUnchecked) out = out.filter(p => !p.checkedIn);
    return out;
  }, [players, tagFilter, showOnlyUnchecked]);
  const dojoFirstRowSet = useMemoA(() => {
    const seen = new Set();
    const first = new Set();
    visiblePlayers.forEach((p, i) => {
      if (!seen.has(p.dojo)) { seen.add(p.dojo); first.add(i); }
    });
    return first;
  }, [visiblePlayers]);
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

  // Extracted from inline onClick so we can await + log instead of
  // silencing the rejection. Same pattern as updateSeed below: success
  // toast omitted because clearing all seeds is visible in the UI
  // (rows re-render without rank), but errors must surface (via
  // updateCompetition's catch in admin.jsx).
  const clearAllSeeds = async () => {
    try {
      await onUpdate({ ...c, players: c.players.map((p) => ({ ...p, seed: null })) });
    } catch (err) {
      console.error("AdminParticipants: clearAllSeeds PUT failed", err);
    }
  };

  // Async so we can await onUpdate(): updateCompetition re-throws on
  // PUT failure (admin.jsx) and surfaces the error via showToast.
  // The pre-async fire-and-forget form silenced the rejection here
  // with `.catch(() => {})` — asymmetric with the other now-awaited
  // mutation flows in this component (apply(), handleSeedFile,
  // shuffleUnseeded). No success toast needed: rank changes are
  // implicit-feedback (the input commits visually), so we just need
  // to NOT swallow errors silently.
  const updateSeed = async (idx, val) => {
    const np = [...(c.players || [])];
    const seed = parseInt(val);
    np[idx] = { ...np[idx], seed: isNaN(seed) || seed <= 0 ? null : seed };
    try {
      await onUpdate({ ...c, players: np });
    } catch (err) {
      console.error("AdminParticipants: updateSeed PUT failed", err);
      // updateCompetition already toasted the error.
    }
  };

  const dragIdxRef = useRefA(null);
  const [dragOverIdx, setDragOverIdx] = useStateA(null);
  // Same async + await + log pattern as updateSeed. The drop completes
  // visually before the PUT; await is for error surfacing, not for
  // gating the UI.
  const moveSeedRow = async (fromIdx, toIdx) => {
    if (fromIdx === toIdx) return;
    const np = [...(c.players || [])];
    const [moved] = np.splice(fromIdx, 1);
    np.splice(toIdx, 0, moved);
    // Re-assign seeds by new position order (only for currently-seeded players)
    const seededCount = np.filter(p => p.seed).length;
    const payloadPlayers = seededCount > 0
      ? (() => { let rank = 1; return np.map(p => p.seed ? { ...p, seed: rank++ } : p); })()
      : np;
    try {
      await onUpdate({ ...c, players: payloadPlayers });
    } catch (err) {
      console.error("AdminParticipants: moveSeedRow PUT failed", err);
    }
  };

  // Async so the "Unseeded list shuffled" success toast is gated on the
  // PUT actually succeeding. Pre-fix: fire-and-forget + immediate toast
  // → on PUT failure the admin saw "shuffled" while the persisted order
  // was unchanged (only the error toast hinted at the discrepancy).
  const shuffleUnseeded = async () => {
    const np = [...(c.players || [])];
    const unseeded = np.filter(p => !p.seed);
    if (unseeded.length < 2) return;
    for (let i = unseeded.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [unseeded[i], unseeded[j]] = [unseeded[j], unseeded[i]];
    }
    let uIdx = 0;
    const shuffled = np.map(p => p.seed ? p : unseeded[uIdx++]);
    try {
      await onUpdate({ ...c, players: shuffled });
    } catch (err) {
      console.error("AdminParticipants: shuffleUnseeded PUT failed", err);
      // updateCompetition already toasted the error; don't double-toast
      // or emit a misleading success message.
      return;
    }
    if (mountedRef.current) showToast("Unseeded list shuffled");
  };

  const toggleCheckIn = async (pid, checkedIn) => {
    try {
      await window.API.toggleCheckIn(c.id, pid, checkedIn, password);
      // No onUpdate() call needed here because the SSE broadcast will trigger
      // a reload of the competition data in the parent component.
    } catch (err) {
      console.error("AdminParticipants: toggleCheckIn failed", err);
      showToast(err.message, "error");
    }
  };

  const bulkCheckInDojo = async (dojo) => {
    const targets = (c.players || []).filter(p => p.dojo === dojo && !p.checkedIn);
    if (targets.length === 0) return;

    if (!confirm(`Mark all ${targets.length} participants from ${dojo} as checked-in?`)) return;

    try {
      // Parallel execution for better UX and performance
      await Promise.all(targets.map(p => window.API.toggleCheckIn(c.id, p.id, true, password)));
      showToast(`Checked in ${targets.length} participants from ${dojo}`);
    } catch (err) {
      console.error("AdminParticipants: bulkCheckInDojo failed", err);
      showToast(err.message, "error");
    }
  };

  const [showSlotForm, setShowSlotForm] = useStateA(false);
  const [slotSrcComp, setSlotSrcComp] = useStateA("");
  const [slotRank, setSlotRank] = useStateA(1);
  const [slotLoading, setSlotLoading] = useStateA(false);

  const otherComps = (tournament?.competitions || []).filter(cc => cc.id !== c.id);

  // Number.isInteger + >= 1 catches NaN (cleared input) AND fractional
  // values that the browser number-input lets through despite step="1".
  // The bare `slotRank < 1` guard the function used to have was vacuous
  // for NaN (NaN < 1 is false), so Add stayed enabled and addReservedSlot
  // was called with NaN — the backend rejected with an unhelpful 400.
  const slotRankValid = Number.isInteger(slotRank) && slotRank >= 1;

  const addSlot = async () => {
    if (!slotSrcComp || !slotRankValid) return;
    setSlotLoading(true);
    try {
      await window.API.addReservedSlot(c.id, slotSrcComp, slotRank, password);
      // mountedRef declared above gates post-await own setStates so a
      // navigate-away during the PUT can't setState on a torn-down
      // component. showToast is safe (lifted to AdminApp). alert is a
      // browser API, safe regardless.
      if (!mountedRef.current) return;
      setShowSlotForm(false);
      setSlotSrcComp("");
      setSlotRank(1);
      showToast("Reserved slot added");
    } catch (e) {
      alert("Failed to add reserved slot: " + e.message);
    } finally {
      // Skip when unmounted — setSlotLoading on a dead component is a
      // teardown signal even if React 18 silently no-ops.
      if (mountedRef.current) setSlotLoading(false);
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

  // Async so we can await onUpdate(): updateCompetition re-throws on
  // PUT failure now, so awaiting lets us gate the "Saved N participants"
  // toast on actual success. Pre-fix the rejection was swallowed and
  // the success + error toasts fired back-to-back.
  //
  // Split into two try blocks so local errors (parse / mint) get a
  // user-visible toast, while PUT failures only log here — the latter
  // are already toasted by updateCompetition's catch, so a second
  // toast would double up.
  const apply = async () => {
    let np, added, updatedCount;
    try {
      const withZekken = c.withZekkenName;
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

      ({ np, added, updatedCount } = mintParticipantIds(c.id, c.players, parsed));
    } catch (err) {
      // Local errors: parseParticipantLines throws on malformed input
      // (bad column counts, etc.), mintParticipantIds can throw on
      // unexpected shape. Surface so the user knows what went wrong.
      console.error("AdminParticipants: parse failed", err);
      showToast("Failed to parse participants: " + err.message, "error");
      return;
    }

    try {
      await onUpdate({ ...c, players: np });

      // Bail if we unmounted during the in-flight PUT — see mountedRef
      // declaration above. showToast is safe (lifted to AdminApp, still
      // mounted on logout-free navigation), but setImportSummary targets
      // this component's local state.
      if (!mountedRef.current) return;

      const label = c.kind === "team" ? "team" : "participant";
      let msg = `Saved ${pluralize(np.length, label)}`;
      if (added > 0 || updatedCount > 0) {
        msg += ` (${added} new, ${updatedCount} updated)`;
      }
      showToast(msg);
      setImportSummary(null);
    } catch (err) {
      // PUT failure path. updateCompetition already showed an error
      // toast for the user; log here so the dev console has the stack
      // for diagnosis but don't emit a second toast.
      console.error("AdminParticipants: PUT failed", err);
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
      // Same teardown-race guard as apply() / addSlot — gate the
      // post-await setText so a navigate-away during clipboard read
      // doesn't fire setState on a dead component.
      if (!mountedRef.current) return;
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
      <CheckInBanner tournament={tournament} players={players} />
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
                      {/* Render NaN as "" so clearing the input stays empty */}
                      {/* instead of triggering React's "Received NaN for the */}
                      {/* value attribute" warning. decideNumericUpdate keeps */}
                      {/* state at NaN when input is empty/non-integer, so the */}
                      {/* slotRankValid guard above keeps Add disabled until the */}
                      {/* user re-enters a positive integer. */}
                      <input
                        className="field__input"
                        type="number"
                        min={1}
                        step="1"
                        value={Number.isFinite(slotRank) ? slotRank : ""}
                        onChange={e => setSlotRank(decideNumericUpdate(e.target.value, 1).value)}
                      />
                    </div>
                    <div style={{ display: "flex", gap: 6 }}>
                      <button className="btn btn--sm btn--primary" onClick={addSlot} disabled={!slotSrcComp || !slotRankValid || slotLoading}>Add</button>
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
              <div className="card__title">Check-in & Seeding</div>
              <div className="card__sub">
                {players.filter(p => p.checkedIn).length} / {players.length} checked in · {players.filter((p) => p.seed).length} seeded
              </div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              <button className={`btn btn--sm ${showOnlyUnchecked ? "btn--primary" : ""}`} type="button" onClick={() => setShowOnlyUnchecked(!showOnlyUnchecked)}>
                {showOnlyUnchecked ? "Show all" : "Show unchecked"}
              </button>
              <button className="btn btn--sm" type="button" onClick={shuffleUnseeded} disabled={players.length === 0} title="Shuffle unseeded players">Shuffle unseeded</button>
              <button className="btn btn--sm" type="button" onClick={() => seedFileRef.current?.click()} disabled={players.length === 0} title={players.length === 0 ? "Add participants first" : undefined}>Import Seeds CSV</button>
              <input ref={seedFileRef} type="file" accept=".csv,.txt,text/csv,text/plain" style={{ display: "none" }} onChange={(e) => handleSeedFile(e.target.files[0])} />
              <button className="btn btn--sm" type="button" onClick={clearAllSeeds}>Clear seeds</button>
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
              {(tagFilter || showOnlyUnchecked) && (
                <div className="field__hint" style={{ padding: "0 16px 8px" }}>
                  Reordering disabled while a filter is active. Clear all filters to drag rows or use the arrows.
                </div>
              )}
              {visiblePlayers.map((p) => {
                const i = players.indexOf(p);
                const reorderDisabled = !!tagFilter || showOnlyUnchecked;
                return (
                  <div
                    key={p.id}
                    className={`seed-row ${p.seed ? "has-seed" : ""} ${p.checkedIn ? "is-checked-in" : ""} ${dragOverIdx === i ? "seed-row--drop-target" : ""}`}
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
                    <div style={{ display: "flex", alignItems: "center", gap: 8, marginRight: 4 }}>
                      <input
                        type="checkbox"
                        checked={p.checkedIn}
                        onChange={(e) => toggleCheckIn(p.id, e.target.checked)}
                        style={{ width: 18, height: 18, cursor: "pointer" }}
                        title={p.checkedIn ? "Undo check-in" : "Mark as checked-in"}
                      />
                    </div>
                    <span className="seed-row__handle" title={reorderDisabled ? "Clear the tag filter to reorder" : "Drag to reorder"}>⠿</span>
                    <span className="seed-row__rank">{p.seed ? `#${p.seed}` : ""}</span>
                    <div style={{ flex: 1 }}>
                      <div className="seed-row__name" title={p.name}>{p.name}{p.tag && <span className="tag-badge">{p.tag}</span>}</div>
                      <div className="seed-row__dojo">
                        {p.dojo}
                        {dojoFirstRowSet.has(i) && players.filter(px => px.dojo === p.dojo && !px.checkedIn).length > 0 && (
                          <button
                            className="btn--link"
                            style={{ marginLeft: 8, fontSize: 10, padding: 0 }}
                            onClick={() => bulkCheckInDojo(p.dojo)}
                          >
                            Mark all from {p.dojo}
                          </button>
                        )}
                      </div>
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

// ES export for the vitest suite — pure helpers only. Components remain
// behind the window.* global pattern to match the rest of admin_*.jsx.
export { mintParticipantIds, findSeedMatchIndex };
