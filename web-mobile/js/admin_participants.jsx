// Participants section of a competition: paste/import roster, edit seeds.
// See web-mobile/admin_split_plan.md.

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

const pluralize = window.pluralize;

// EscapeListener: registers the global Escape→onClose handler only while
// it's mounted. Used inside conditionally-rendered modals so the listener's
// lifetime tracks the modal's, avoiding the "always-active preventDefault"
// problem of calling useEscapeToClose at the parent level. Renders nothing.
function EscapeListener({ onClose }) {
  window.useEscapeToClose(onClose);
  return null;
}

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
  const np = parsed.map(({ name, displayName, dojo, danGrade, tag, checkedIn: parsedCheckedIn }) => {
    const existing = existingMap.get(name.toLowerCase());
    if (existing) {
      updatedCount++;
      // Preserve existing check-in state; CSV token takes precedence if explicitly set.
      const checkedIn = parsedCheckedIn || existing.checkedIn || false;
      return { id: existing.id, name, displayName, dojo, danGrade, tag, seed: existing.seed || null, checkedIn };
    }
    added++;
    while (usedIds.has(`${compID}-p${nextSlot}`)) nextSlot++;
    const id = `${compID}-p${nextSlot}`;
    usedIds.add(id);
    nextSlot++;
    return { id, name, displayName, dojo, danGrade, tag, seed: null, checkedIn: parsedCheckedIn || false };
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

function AdminParticipants({ c, tournament: _tournament, onUpdate, password, showToast, onSection }) {
  const [showOnlyUnchecked, setShowOnlyUnchecked] = useStateA(false);
  const [replaceTarget, setReplaceTarget] = useStateA(null);
  const [showAddForm, setShowAddForm] = useStateA(false);
  const [addName, setAddName] = useStateA("");
  const [addDojo, setAddDojo] = useStateA("");
  const [addDanGrade, setAddDanGrade] = useStateA("");
  const [addZekken, setAddZekken] = useStateA("");
  const [addLoading, setAddLoading] = useStateA(false);
  const [replaceLoading, setReplaceLoading] = useStateA(false);
  const [replaceName, setReplaceName] = useStateA("");
  const [replaceDojo, setReplaceDojo] = useStateA("");
  const [replaceDanGrade, setReplaceDanGrade] = useStateA("");
  const [replaceZekken, setReplaceZekken] = useStateA("");
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

  // If check-in tracking is turned off while "Show unchecked" is active, the
  // toggle button hides (it's gated on c.checkInEnabled) and the operator
  // would be stuck with a filtered list and no visible way to reset it.
  // Reset the filter so the full roster reappears.
  useEffectA(() => {
    if (!c.checkInEnabled && showOnlyUnchecked) {
      setShowOnlyUnchecked(false);
    }
  }, [c.checkInEnabled, showOnlyUnchecked]);

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
  const [searchQuery, setSearchQuery] = useStateA("");
  const trimmedSearch = useMemoA(() => searchQuery.trim(), [searchQuery]);
  const lines = useMemoA(() => text.split("\n").filter((l) => l.trim()), [text]);
  const players = useMemoA(() => c.players || [], [c.players]);

  // Provisional competitor numbers for the pre-draw check-in list (mp-1tk).
  // The draw assigns the final, pool-interleaved numbers (player.number);
  // before that there is none. We surface a stable registration-order number
  // (numberPrefix + position in c.players) so operators can call competitors
  // by number during check-in. Keyed off the unfiltered roster so the number
  // doesn't jump when the list is searched/sorted. Rendered as provisional
  // (muted, dotted) since the final numbers may differ after the draw.
  const provisionalNumberById = useMemoA(() => {
    // Null-prototype object: keys are user-controlled (p.id ?? p.name), so a
    // participant named "__proto__" or "constructor" against a plain `{}` map
    // could pollute the prototype chain or return inherited values on lookup.
    // `Object.create(null)` removes both risks and keeps the `map[key]` /
    // `map[key] = …` ergonomics. (Copilot mp-1tk follow-up.)
    const map = Object.create(null);
    if (c.numberPrefix) {
      (c.players || []).forEach((p, i) => {
        map[p.id ?? p.name] = `${c.numberPrefix}${i + 1}`;
      });
    }
    return map;
  }, [c.players, c.numberPrefix]);
  const allTags = useMemoA(() => [...new Set(players.map(p => p.tag).filter(Boolean))], [players]);
  const playerSearchTargets = useMemoA(() => {
    const map = new Map();
    players.forEach(p => { map.set(p.id ?? p.name, participantSearchTarget(p)); });
    return map;
  }, [players]);
  const visiblePlayers = useMemoA(() => {
    const q = trimmedSearch.toLowerCase();
    let out = players;
    if (tagFilter) out = out.filter(p => p.tag === tagFilter);
    if (showOnlyUnchecked) out = out.filter(p => !p.checkedIn);
    if (q) out = out.filter(p => playerSearchTargets.get(p.id ?? p.name)?.includes(q));
    return out;
  }, [players, tagFilter, showOnlyUnchecked, trimmedSearch, playerSearchTargets]);
  const dojoFirstRowSet = useMemoA(() => {
    const seen = new Set();
    const first = new Set();
    visiblePlayers.forEach((p) => {
      if (!seen.has(p.dojo)) { seen.add(p.dojo); first.add(p.id ?? p.name); }
    });
    return first;
  }, [visiblePlayers]);
  const dojoUncheckedCount = useMemoA(() => {
    const counts = new Map();
    players.forEach(p => {
      if (!p.checkedIn) counts.set(p.dojo, (counts.get(p.dojo) || 0) + 1);
    });
    return counts;
  }, [players]);
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
      // State refresh is handled by the SSE participants_updated event,
      // which admin.jsx's REFRESHABLE_EVENTS subscriber picks up and
      // uses to call fetchCompetitionDetails. No onUpdate() call needed.
    } catch (err) {
      console.error("AdminParticipants: toggleCheckIn failed", err);
      showToast(err.message, "error");
    }
  };

  const bulkCheckInDojo = async (dojo) => {
    const targets = (c.players || []).filter(p => p.dojo === dojo && !p.checkedIn);
    if (targets.length === 0) return;

    if (!confirm(`Mark all ${targets.length} participants from ${dojo} as checked-in?`)) return;

    const results = await Promise.allSettled(targets.map(p => window.API.toggleCheckIn(c.id, p.id ?? p.name, true, password)));
    const failed = results.filter(r => r.status === "rejected").length;
    const succeeded = results.length - failed;
    if (failed > 0) {
      console.error("AdminParticipants: bulkCheckInDojo partial failure", results.filter(r => r.status === "rejected").map(r => r.reason));
      showToast(`${succeeded}/${results.length} checked in from ${dojo} (${failed} failed)`, "error");
    } else {
      showToast(`Checked in ${succeeded} participants from ${dojo}`);
    }
  };

  const bulkCheckInAll = async () => {
    const targets = (c.players || []).filter(p => !p.checkedIn);
    if (targets.length === 0) { showToast("All participants already checked in"); return; }
    const results = await Promise.allSettled(targets.map(p => window.API.toggleCheckIn(c.id, p.id ?? p.name, true, password)));
    const failed = results.filter(r => r.status === "rejected").length;
    const succeeded = results.length - failed;
    if (failed > 0) {
      showToast(`${succeeded}/${results.length} checked in (${failed} failed)`, "error");
    } else {
      showToast(`All ${succeeded} participants checked in`);
    }
  };

  const handleAddParticipant = async () => {
    const name = addName.trim(), dojo = addDojo.trim(), danGrade = addDanGrade.trim();
    const zekken = addZekken.trim();
    if (!name || !dojo) { showToast("Name and dojo are required", "error"); return; }
    const admin = window.promptAdminPassword();
    if (admin === null) return;
    setAddLoading(true);
    try {
      // displayName is only forwarded for zekken-enabled competitions; for
      // non-zekken comps the backend derives DisplayName = SanitizeName(Name)
      // and we MUST send "" or omit the key so the operator can't accidentally
      // poison the slot with a stale value (see TestReplaceDoesNotInherit…).
      const payload = { name, dojo, danGrade: danGrade || undefined };
      if (c.withZekkenName && zekken) payload.displayName = zekken;
      await window.API.addParticipant(c.id, payload, password, admin);
      if (!mountedRef.current) return;
      setAddName(""); setAddDojo(""); setAddDanGrade(""); setAddZekken(""); setShowAddForm(false);
      showToast(`${name} added`);
    } catch (err) {
      if (!mountedRef.current) return;
      showToast(err.message, "error");
    } finally {
      if (mountedRef.current) setAddLoading(false);
    }
  };

  const handleReplaceParticipant = async () => {
    if (!replaceTarget) return;
    const name = replaceName.trim(), dojo = replaceDojo.trim(), danGrade = replaceDanGrade.trim();
    const zekken = replaceZekken.trim();
    if (!name || !dojo) { showToast("Name and dojo are required", "error"); return; }
    // Capture the old name before the await so the success toast is accurate
    // even if replaceTarget has changed by the time the response arrives.
    const oldName = replaceTarget.name;
    const targetId = replaceTarget.id;
    const targetTag = replaceTarget.tag || "";
    const admin = window.promptAdminPassword();
    if (admin === null) return;
    setReplaceLoading(true);
    try {
      // Build metadata from danGrade so the edited grade actually persists —
      // the backend prefers Metadata over danGrade when Metadata is non-empty,
      // so forwarding the old replaceTarget.metadata blindly would discard any
      // grade change. Slots 1+ are preserved via the shared buildPlayerMetadata
      // helper (mirrored from updateCompetition).
      //
      // displayName handling — zekken comps: forward the operator's value (or
      // "" so the backend re-derives via SanitizeName(name)). Non-zekken comps:
      // ALWAYS send "" — otherwise stale "A. SMITH" from the replaced slot
      // would carry over and corrupt the 3-column CSV row.
      const metadata = window.buildPlayerMetadata(danGrade, replaceTarget.metadata);
      const payload = { name, dojo, displayName: c.withZekkenName ? zekken : "", tag: targetTag };
      if (metadata !== undefined) payload.metadata = metadata;
      const updated = await window.API.replaceParticipant(c.id, targetId, payload, password, admin);
      if (!mountedRef.current) return;
      setReplaceTarget(null);
      showToast(oldName === updated.name ? `Saved changes for ${updated.name}` : `Renamed ${oldName} → ${updated.name}`);
      if (updated.warnings && updated.warnings.length > 0) {
        updated.warnings.forEach(w => showToast(`Warning: ${w}`, "error"));
      }
      if (updated.cascadeError) {
        showToast(`Draw update failed: ${updated.cascadeError}`, "error");
      }
    } catch (err) {
      if (!mountedRef.current) return;
      showToast(err.message, "error");
    } finally {
      if (mountedRef.current) setReplaceLoading(false);
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

  const isSetup = !c.status || c.status === "setup";
  const isDrawReady = c.status === "draw-ready";
  const isStarted = !isSetup && !isDrawReady;

  return (
    <>
      {isDrawReady && (
        <div className="notice notice--info" style={{ marginBottom: 12 }}>
          Draw pending — edits will update the draw in place.
        </div>
      )}
      {isStarted && (
        <div style={{ marginBottom: 16, display: "flex", justifyContent: "flex-end" }}>
          <button className="btn btn--primary" onClick={() => onSection("scores")}>Go to Scoring →</button>
        </div>
      )}
      <div className="row" style={{ alignItems: "start" }}>
        <div className="card">
          <div className="card__head">
            <div>
              <div className="card__title">Check-in & Seeding</div>
              <div className="card__sub">
                {c.checkInEnabled && `${players.filter(p => p.checkedIn).length} / ${players.length} checked in · `}{players.filter((p) => p.seed).length} seeded
              </div>
            </div>
            <div style={{ display: "flex", gap: 6 }}>
              {c.checkInEnabled && (
                <button className={`btn btn--sm ${showOnlyUnchecked ? "btn--primary" : ""}`} type="button" onClick={() => setShowOnlyUnchecked(!showOnlyUnchecked)}>
                  {showOnlyUnchecked ? "Show all" : "Show unchecked"}
                </button>
              )}
              {c.checkInEnabled && (
                <button className="btn btn--sm" type="button" onClick={bulkCheckInAll} disabled={players.length === 0} title="Mark all as checked in">Check in all</button>
              )}
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
            {players.length > 0 && (
              <div style={{ position: "relative", marginBottom: 8 }}>
                <input
                  className="input"
                  type="search"
                  aria-label="Search participants"
                  value={searchQuery}
                  onChange={e => setSearchQuery(e.target.value)}
                  onKeyDown={e => e.key === "Escape" && setSearchQuery("")}
                  placeholder="Search name, dojo…"
                  style={{ width: "100%", paddingRight: trimmedSearch ? 28 : undefined }}
                />
                {trimmedSearch && (
                  <button
                    type="button"
                    onClick={() => setSearchQuery("")}
                    style={{ position: "absolute", right: 6, top: "50%", transform: "translateY(-50%)", background: "none", border: "none", cursor: "pointer", color: "var(--ink-3)", fontSize: 14, padding: 2 }}
                    aria-label="Clear search"
                  >×</button>
                )}
              </div>
            )}
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
          {isSetup && (
            <div style={{ padding: "0 16px 12px" }}>
              {!showAddForm ? (
                <button className="btn btn--sm" type="button" onClick={() => setShowAddForm(true)}>+ Add participant</button>
              ) : (
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
                  <div>
                    <div className="field__label" style={{ fontSize: 11 }}>Name *</div>
                    <input className="input" style={{ width: 160 }} value={addName} onChange={e => setAddName(e.target.value)} placeholder="Full name" />
                  </div>
                  {c.withZekkenName && (
                    <div>
                      <div className="field__label" style={{ fontSize: 11 }}>Zekken</div>
                      <input className="input" style={{ width: 120 }} value={addZekken} onChange={e => setAddZekken(e.target.value)} placeholder="Auto if blank" />
                    </div>
                  )}
                  <div>
                    <div className="field__label" style={{ fontSize: 11 }}>Dojo *</div>
                    <input className="input" style={{ width: 140 }} value={addDojo} onChange={e => setAddDojo(e.target.value)} placeholder="Dojo" />
                  </div>
                  <div>
                    <div className="field__label" style={{ fontSize: 11 }}>Dan grade</div>
                    <input className="input" style={{ width: 100 }} value={addDanGrade} onChange={e => setAddDanGrade(e.target.value)} placeholder="Optional" />
                  </div>
                  <button className="btn btn--sm btn--primary" disabled={addLoading || !addName.trim() || !addDojo.trim()} onClick={handleAddParticipant}>
                    {addLoading ? "Adding…" : "Add"}
                  </button>
                  <button className="btn btn--sm" onClick={() => { setShowAddForm(false); setAddName(""); setAddDojo(""); setAddDanGrade(""); setAddZekken(""); }}>Cancel</button>
                </div>
              )}
            </div>
          )}
          {replaceTarget && (
            <div role="dialog" aria-modal="true" aria-labelledby="edit-modal-title" style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.4)", zIndex: 200, display: "flex", alignItems: "center", justifyContent: "center" }} onClick={e => { if (e.target === e.currentTarget) setReplaceTarget(null); }}>
              <EscapeListener onClose={() => setReplaceTarget(null)} />
              <div className="card" style={{ minWidth: 320, maxWidth: 420, margin: 16 }}>
                <div className="card__head"><div id="edit-modal-title" className="card__title">Edit {replaceTarget.name}</div></div>
                <div className="card__body" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                  <div>
                    <div className="field__label">Name *</div>
                    <input className="input" value={replaceName} onChange={e => setReplaceName(e.target.value)} placeholder="Replacement name" />
                  </div>
                  {c.withZekkenName && (
                    <div>
                      <div className="field__label">Zekken</div>
                      <input className="input" value={replaceZekken} onChange={e => setReplaceZekken(e.target.value)} placeholder="Auto-derived if blank" />
                    </div>
                  )}
                  <div>
                    <div className="field__label">Dojo *</div>
                    <input className="input" value={replaceDojo} onChange={e => setReplaceDojo(e.target.value)} placeholder="Dojo" />
                  </div>
                  <div>
                    <div className="field__label">Dan grade</div>
                    <input className="input" value={replaceDanGrade} onChange={e => setReplaceDanGrade(e.target.value)} placeholder="Optional" />
                  </div>
                  <div className="field__hint">ID, seed, and check-in state are preserved. Seed rankings are updated to match the new name automatically.</div>
                  <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
                    <button className="btn" onClick={() => setReplaceTarget(null)}>Cancel</button>
                    <button className="btn btn--primary" disabled={replaceLoading || !replaceName.trim() || !replaceDojo.trim()} onClick={handleReplaceParticipant}>
                      {replaceLoading ? "Saving…" : "Save"}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}
          {players.length === 0 ? (
            <div className="empty" style={{ padding: 24 }}>
              <div className="icon">🌱</div>
              <h3>No participants yet</h3>
              <div style={{ fontSize: 12 }}>Add names on the right, then "Apply".</div>
            </div>
          ) : (
            <div className="seed-list">
              {/* When a tag filter is active, reorder controls would operate on */}
              {/* full-list indices but rows are filtered — so they'd swap with hidden */}
              {/* neighbours. Disable reordering until the filter is cleared. */}
              {(tagFilter || showOnlyUnchecked || trimmedSearch) && (
                <div className="field__hint" style={{ padding: "0 16px 8px" }}>
                  {visiblePlayers.length < players.length && `Showing ${visiblePlayers.length} of ${players.length}. `}
                  Reordering disabled while a filter is active. Clear all filters to drag rows or use the arrows.
                </div>
              )}
              {visiblePlayers.length === 0 && (trimmedSearch || tagFilter || showOnlyUnchecked) && (
                <div className="empty" style={{ padding: "16px 24px" }}>
                  {(tagFilter || showOnlyUnchecked)
                    ? "No participants match current filters."
                    : `No match for "${trimmedSearch}".`}
                </div>
              )}
              {visiblePlayers.map((p) => {
                const i = players.indexOf(p);
                const reorderDisabled = !!tagFilter || showOnlyUnchecked || !!trimmedSearch;
                return (
                  <div
                    key={p.id ?? p.name}
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
                    style={{
                      cursor: reorderDisabled ? "default" : "grab",
                      gridTemplateColumns: c.checkInEnabled ? undefined : "20px 36px 1fr 32px 64px",
                    }}
                  >
                    {c.checkInEnabled && (
                      <div style={{ display: "flex", alignItems: "center", gap: 8, marginRight: 4 }}>
                        <input
                          type="checkbox"
                          checked={p.checkedIn}
                          onChange={(e) => toggleCheckIn(p.id ?? p.name, e.target.checked)}
                          style={{ width: 18, height: 18, cursor: "pointer" }}
                          aria-label={p.checkedIn ? `Undo check-in for ${p.name}` : `Mark ${p.name} as checked-in`}
                        />
                      </div>
                    )}
                    <span className="seed-row__handle" title={reorderDisabled ? "Clear all filters to reorder" : "Drag to reorder"}>⠿</span>
                    <span className="seed-row__rank">{p.seed ? `#${p.seed}` : ""}</span>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: "flex", alignItems: "center", minWidth: 0 }}>
                        <div className="seed-row__name" title={p.name} style={{ minWidth: 0 }}>
                          {p.number ? (
                            <span className="num-prefix">{p.number}</span>
                          ) : provisionalNumberById[p.id ?? p.name] ? (
                            <span className="num-prefix num-prefix--provisional" title="Provisional number — the final competitor number is assigned when the draw runs">{provisionalNumberById[p.id ?? p.name]}</span>
                          ) : null}
                          {p.name}
                        </div>
                        {p.tag && <span className="tag-badge" style={{ flexShrink: 0 }}>{p.tag}</span>}
                      </div>
                      <div className="seed-row__dojo">
                        {p.dojo}
                        {c.checkInEnabled && dojoFirstRowSet.has(p.id ?? p.name) && (dojoUncheckedCount.get(p.dojo) || 0) > 0 && (
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
                      {(isSetup || isDrawReady) && (
                        <button className="btn btn--sm btn--icon-sm" style={{ fontSize: 11 }} title={`Edit ${p.name}`} onClick={() => { setReplaceTarget(p); setReplaceName(p.name); setReplaceDojo(p.dojo); setReplaceDanGrade(p.danGrade || ""); setReplaceZekken(c.withZekkenName ? (p.displayName || "") : ""); }} aria-label={`Edit ${p.name}`}>✎</button>
                      )}
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
      </div>
    </>
  );
}

function participantSearchTarget(p) {
  return [p.name, p.displayName, p.dojo, p.danGrade].filter(Boolean).join(" ").toLowerCase();
}

window.AdminParticipants = AdminParticipants;

// ES export for the vitest suite — pure helpers only. Components remain
// behind the window.* global pattern to match the rest of admin_*.jsx.
export { mintParticipantIds, findSeedMatchIndex, participantSearchTarget };
