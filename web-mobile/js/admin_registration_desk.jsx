// Registration Desk — cross-competition check-in surface for operators (mp-25bk).
//
// Reached from the Admin dashboard "Registration desk" card. Gathers every
// competition's roster into one place so the desk never has to hop between
// per-competition Participants tabs. The arrival loop is the whole design:
//
//     fuzzy-find  →  check in  →  hand over the player tag
//
// "Player tag" is the thing physically handed to the competitor: the assigned
// competitor number for individuals (e.g. "K1"; "Not assigned yet" before the
// draw), or the team name for team competitions. It is NOT the zekken (the
// back name-tag, used only for identity/search) nor the provenance tag pill
// (manual/registered/transfer).
//
// Backend facts this relies on (see internal/mobileapp/handlers_participants.go):
//   - check-in (PUT/DELETE/bulk) is NOT status-gated and NOT gated on
//     checkInEnabled — anyone can be checked in for any competition at any time
//     (latecomers included).
//   - walk-up add (POST single) needs the elevated password and only works
//     while a competition is in "setup" status.
//
// SSE: when this view is mounted, AdminApp renders ONLY this page — neither the
// dashboard's nor the competition-view's SSE subscription is active. So this
// page owns its own freshness: optimistic local writes for snappy feedback, a
// participants_updated subscription that reconciles other desks' changes, and
// onUpdate() pushes back to the parent so navigating "Back" shows fresh data.

const { useState: useStateRD, useEffect: useEffectRD, useRef: useRefRD, useMemo: useMemoRD, useCallback: useCallbackRD } = React;

const AdminTopbarRD = window.AdminTopbar;
const BreadcrumbsRD = window.Breadcrumbs;
const StatusBadgeRD = window.StatusBadge;
const pluralizeRD = window.pluralize;

// ---------------------------------------------------------------------------
// Pure helpers (no React) — kept module-scoped so they're unit-testable.
// ---------------------------------------------------------------------------

// Shared normalization for fuzzy matching and person identity. Falls back to a
// minimal lowercase/trim if the shared helper isn't loaded yet.
function rdNorm(s) {
  if (window.normalizeParticipantName) return window.normalizeParticipantName(s || "");
  return (s || "").toString().normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase().trim().replace(/\s+/g, " ");
}

// A competitor's cross-competition identity: same name + same dojo is the same
// person, regardless of which competitions they entered. Matches the backend's
// check-in-preservation key (normalizedName + normalizedDojo).
function rdPersonKey(p) {
  return `${rdNorm(p.name)}|${rdNorm(p.dojo)}`;
}

// Stable per-competition participant id used by the check-in endpoints, mirroring
// the rest of the admin code: prefer the UUID, fall back to the name.
function rdPid(p) {
  return p.id ?? p.name;
}

// Subsequence score for one token against a normalized haystack. Returns null
// when the token isn't even a subsequence; otherwise a score where lower is a
// better match (contiguous substrings beat scattered subsequences, and earlier
// positions beat later ones). This is what makes the search genuinely fuzzy —
// "tnk" finds "Tanaka", "sato a" finds "Aiko Sato".
function rdTokenScore(needle, hay) {
  if (!needle) return 0;
  const at = hay.indexOf(needle);
  if (at >= 0) return at; // contiguous: rank by position, prefix is best
  let h = 0, start = -1, gaps = 0;
  for (let i = 0; i < needle.length; i++) {
    const found = hay.indexOf(needle[i], h);
    if (found < 0) return null;
    if (start < 0) start = found;
    if (i > 0 && found > h) gaps += found - h;
    h = found + 1;
  }
  // Reject scattered "matches": if the characters are spread far apart (e.g.
  // "yama" landing on r-y-o n-a-k-a-m-…-a) it isn't a real hit. A tight typo
  // ("tnk" → tanaka, gaps 2 < len 3) keeps small gaps; a coincidental spread
  // (gaps ≥ needle length, e.g. "yama"→"ryonakama" with gaps 4 == len 4) doesn't.
  if (gaps >= needle.length) return null;
  return 200 + start + gaps; // any subsequence ranks below any contiguous hit
}

// Whole-query score: every whitespace-separated token must match (AND), so
// multi-word queries work in any order. Returns null on no-match, else a score.
function rdQueryScore(queryNorm, hay) {
  if (!queryNorm) return 0;
  let total = 0;
  for (const tok of queryNorm.split(" ")) {
    if (!tok) continue;
    const s = rdTokenScore(tok, hay);
    if (s == null) return null;
    total += s;
  }
  return total;
}

// The player tag to hand over for a given (competition, participant).
//   team  → the team name (always present)
//   indiv → the assigned competitor number, or null before the draw runs
function rdPlayerTag(comp, p) {
  if (comp && comp.kind === "team") return { kind: "team", value: p.name };
  if (p.number) return { kind: "number", value: p.number };
  return { kind: "pending", value: null };
}

// Build the cross-competition people index from the competitions array.
// Map<personKey, { key, name, dojo, displayName, entries: [{comp, player}] }>
// entries preserve competition order.
function rdBuildPeopleIndex(comps) {
  const index = new Map();
  (comps || []).forEach((comp) => {
    (comp.players || []).forEach((player) => {
      const key = rdPersonKey(player);
      let rec = index.get(key);
      if (!rec) {
        rec = { key, name: player.name, dojo: player.dojo, displayName: player.displayName || "", entries: [] };
        index.set(key, rec);
      }
      rec.entries.push({ comp, player });
    });
  });
  return index;
}

// Normalized search haystack for a person across one or more entries.
function rdHaystack(name, dojo, entries) {
  const parts = [name, dojo];
  entries.forEach(({ comp, player }) => {
    if (player.displayName) parts.push(player.displayName);
    if (player.number) parts.push(player.number);
    if (comp && comp.kind === "team") parts.push(player.name);
  });
  return rdNorm(parts.join(" "));
}

// The genuine zekken to show for identity: the display name from a competition
// that actually uses zekken (withZekkenName). Non-zekken competitions still
// carry a server-derived DisplayName (e.g. "A. SATO" from "Aiko Sato"), which
// is NOT a real zekken and must not be surfaced as one. Returns "" when there
// is no real zekken or it just echoes the name.
function rdZekken(entries) {
  for (const { comp, player } of entries) {
    if (comp && comp.withZekkenName && player.displayName && player.displayName !== player.name) {
      return player.displayName;
    }
  }
  return "";
}

// A person is "present" (checked in) when every competition they entered has
// recorded their check-in. Partial = some-but-not-all.
function rdPresence(entries) {
  const total = entries.length;
  const checked = entries.filter((e) => e.player.checkedIn).length;
  if (checked === 0) return "none";
  if (checked === total) return "all";
  return "partial";
}

// ---------------------------------------------------------------------------
// Small inline icons (the project's Icon set doesn't ship search/check glyphs).
// ---------------------------------------------------------------------------
function RdSearchIcon() {
  return (
    <svg className="rd-search__icon" width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="11" cy="11" r="7" stroke="currentColor" strokeWidth="2" />
      <line x1="16.5" y1="16.5" x2="21" y2="21" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}
function RdCheckIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 12.5l4.5 4.5L19 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function RdPencilIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 20h9" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// ---------------------------------------------------------------------------
// Player-tag pill — the hand-over element. Loud for individuals' numbers and
// team names; muted "Not assigned yet" before the draw.
// ---------------------------------------------------------------------------
function RdPlayerTag({ comp, player, size }) {
  const tag = rdPlayerTag(comp, player);
  const cls = `rd-tag rd-tag--${tag.kind}${size === "lg" ? " rd-tag--lg" : ""}`;
  if (tag.kind === "pending") {
    return <span className={cls} title="The competitor number is assigned when the draw runs">Not assigned yet</span>;
  }
  const label = tag.kind === "team" ? "Team" : "Tag";
  return (
    <span className={cls}>
      <span className="rd-tag__label">{label}</span>
      <span className="rd-tag__value">{tag.value}</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Rail — competition selector. Pinned "All competitions" entry on top, then one
// row per competition with a check-in progress meter.
// ---------------------------------------------------------------------------
function RdRail({ comps, allStats, selected, onSelect }) {
  const item = (key, label, sub, present, total, status) => {
    const pct = total > 0 ? Math.round((present / total) * 100) : 0;
    const done = total > 0 && present === total;
    return (
      <button
        type="button"
        key={key}
        className={`rd-rail__item${selected === key ? " is-active" : ""}`}
        aria-current={selected === key ? "true" : undefined}
        onClick={() => onSelect(key)}
      >
        <div className="rd-rail__top">
          <span className="rd-rail__name">{label}</span>
          {status && <StatusBadgeRD status={status} />}
        </div>
        <div className="rd-rail__sub">{sub}</div>
        <div className="rd-rail__bar" role="presentation">
          <div className={`rd-rail__bar-fill${done ? " is-done" : ""}`} style={{ width: `${pct}%` }} />
        </div>
        <div className="rd-rail__count">
          <strong>{present}</strong> / {total} checked in
        </div>
      </button>
    );
  };

  return (
    <nav className="rd-rail" aria-label="Competitions">
      {item("all", "All competitions", `${pluralizeRD(comps.length, "competition")} · everyone`, allStats.present, allStats.total, null)}
      <div className="rd-rail__divider" role="presentation" />
      {comps.map((c) => {
        const players = c.players || [];
        const checked = players.filter((p) => p.checkedIn).length;
        const kindLabel = c.kind === "team" ? "teams" : "competitors";
        return item(c.id, c.name, `${players.length} ${kindLabel}`, checked, players.length, c.status);
      })}
    </nav>
  );
}

// ---------------------------------------------------------------------------
// Other-competition chips — shown on a row to reveal the person's footprint.
// Each chip carries its own check status; clicking checks them into that
// competition too (the cross-competition convenience).
// ---------------------------------------------------------------------------
function RdOtherChips({ entries, onToggle, busy, label }) {
  if (!entries.length) return null;
  return (
    <div className="rd-chips">
      <span className="rd-chips__label">{label || "Also in"}</span>
      {entries.map(({ comp, player }) => {
        const checked = player.checkedIn;
        // The player tag (number) to hand over for THIS competition. Individuals
        // get a per-competition number ("M1"); teams' tag is the team name,
        // which is already the row's name, so we don't repeat it on the chip.
        const tag = rdPlayerTag(comp, player);
        const num = tag.kind === "number" ? tag.value : null;
        return (
          <button
            type="button"
            key={comp.id}
            className={`rd-chip${checked ? " is-checked" : ""}`}
            disabled={busy}
            title={checked ? `Checked in — ${comp.name}` : `Check in for ${comp.name}`}
            onClick={() => !checked && onToggle(comp.id, rdPid(player), true)}
          >
            <span className="rd-chip__mark" aria-hidden="true">{checked ? <RdCheckIcon /> : null}</span>
            <span className="rd-chip__name">{comp.name}</span>
            {num && <span className="rd-chip__num">{num}</span>}
          </button>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Roster row. In competition mode the primary toggle is THIS competition's
// check-in and the player tag is shown prominently. In "all" mode the toggle is
// person-level presence (checks into every competition at once).
// ---------------------------------------------------------------------------
function RdRow({ mode, comp, player, zekken, entries, others, checked, presence, busy, onPrimary, onToggle, onEdit, isLast }) {
  const danGrade = player.danGrade || (player.metadata && player.metadata[0]) || "";

  const stateClass = mode === "all"
    ? (presence === "all" ? "is-checked" : presence === "partial" ? "is-partial" : "")
    : (checked ? "is-checked" : "");

  const checkLabel = mode === "all"
    ? (presence === "all" ? `Undo check-in for ${player.name}` : `Check in ${player.name} for all their competitions`)
    : (checked ? `Undo check-in for ${player.name}` : `Check in ${player.name}`);

  // In "all" mode a person checked into SOME-but-not-all of their competitions
  // is a partial/indeterminate checkbox: it shows a checkmark visually, so it
  // must report aria-checked="mixed" (not "false") or a screen reader would
  // announce it as unchecked. comp mode is a plain boolean.
  const ariaChecked = mode === "all"
    ? (presence === "all" ? "true" : presence === "partial" ? "mixed" : "false")
    : checked;

  return (
    <div className={`rd-row ${stateClass}${isLast ? " rd-row--last" : ""}`}>
      <button
        type="button"
        className="rd-check"
        role="checkbox"
        aria-checked={ariaChecked}
        aria-label={checkLabel}
        disabled={busy}
        onClick={onPrimary}
      >
        <span className="rd-check__box" aria-hidden="true">
          {(mode === "all" ? presence !== "none" : checked) && <RdCheckIcon />}
        </span>
      </button>

      <div className="rd-row__id">
        <div className="rd-row__name-line">
          <span className="rd-row__name">{player.name}</span>
          {zekken && (
            <span className="rd-row__zekken" title="Zekken / display name">{zekken}</span>
          )}
        </div>
        <div className="rd-row__meta">
          {player.dojo && <span className="rd-row__dojo">{player.dojo}</span>}
          {danGrade && <span className="rd-row__dan">{danGrade}</span>}
          {player.seed ? <span className="rd-row__seed">Seed {player.seed}</span> : null}
        </div>
        {mode === "all" && <RdOtherChips entries={entries} onToggle={onToggle} busy={busy} label="In" />}
        {mode === "comp" && others.length > 0 && <RdOtherChips entries={others} onToggle={onToggle} busy={busy} label="Also in" />}
      </div>

      {mode === "comp" && (
        <div className="rd-row__right">
          <div className="rd-row__handover">
            <RdPlayerTag comp={comp} player={player} />
          </div>
          {onEdit && (
            <button
              type="button"
              className="rd-row__edit"
              aria-label={`Edit ${player.name}`}
              title="Fix name, dojo or zekken"
              disabled={busy}
              onClick={onEdit}
            >
              <RdPencilIcon />
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Walk-up add — inline panel. Setup-only (the add endpoint rejects started
// competitions). For team competitions the "name" is the team name.
// ---------------------------------------------------------------------------
function RdWalkUp({ comp, seedName, password, showToast, onAdded, onCancel }) {
  const isTeam = comp.kind === "team";
  const [name, setName] = useStateRD(seedName || "");
  const [dojo, setDojo] = useStateRD("");
  const [zekken, setZekken] = useStateRD("");
  const [dan, setDan] = useStateRD("");
  const [busy, setBusy] = useStateRD(false);
  const nameRef = useRefRD(null);

  useEffectRD(() => { nameRef.current && nameRef.current.focus(); }, []);

  const submit = async (e) => {
    e.preventDefault();
    const n = name.trim(), d = dojo.trim();
    if (!n || !d) { showToast(isTeam ? "Team name and dojo are required" : "Name and dojo are required", "error"); return; }
    const admin = await window.promptAdminPassword();
    if (admin === null) return;
    setBusy(true);
    try {
      const payload = { name: n, dojo: d };
      if (!isTeam && dan.trim()) payload.danGrade = dan.trim();
      if (!isTeam && comp.withZekkenName && zekken.trim()) payload.displayName = zekken.trim();
      const added = await window.API.addParticipant(comp.id, payload, password, admin);
      // Check them in straight away — they're standing at the desk.
      try {
        await window.API.toggleCheckIn(comp.id, rdPid(added), true, password);
        added.checkedIn = true;
      } catch (err) {
        showToast(`${n} added but check-in failed: ${err.message}`, "error");
      }
      onAdded(comp, added);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="rd-walkup" onSubmit={submit}>
      <div className="rd-walkup__head">
        <span className="rd-walkup__title">Add walk-up to {comp.name}</span>
        <button type="button" className="btn btn--ghost btn--sm" onClick={onCancel}>Cancel</button>
      </div>
      <div className="rd-walkup__grid">
        <label className="field">
          <span className="field__label">{isTeam ? "Team name" : "Name"}</span>
          <input ref={nameRef} className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder={isTeam ? "Tora A" : "Akira Tanaka"} />
        </label>
        <label className="field">
          <span className="field__label">Dojo</span>
          <input className="input" value={dojo} onChange={(e) => setDojo(e.target.value)} placeholder={isTeam ? "Tora Dojo London" : "Gyokusen"} />
        </label>
        {!isTeam && comp.withZekkenName && (
          <label className="field">
            <span className="field__label">Zekken</span>
            <input className="input" value={zekken} onChange={(e) => setZekken(e.target.value)} placeholder="TANAKA" />
          </label>
        )}
        {!isTeam && (
          <label className="field">
            <span className="field__label">Dan grade <span className="field__opt">(optional)</span></span>
            <input className="input" value={dan} onChange={(e) => setDan(e.target.value)} placeholder="3 dan" />
          </label>
        )}
      </div>
      <div className="rd-walkup__actions">
        <button type="submit" className="btn btn--primary" disabled={busy}>{busy ? "Adding…" : "Add & check in"}</button>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------------------
// Inline edit — fix a wrong name / dojo / zekken on an existing entry without
// leaving the desk. Operates on one (competition, participant); editing here
// changes only that competition's record (the per-competition data model). Uses
// the same elevated-password gate and replace endpoint as the Participants tab.
// ---------------------------------------------------------------------------
function RdEditModal({ comp, player, password, showToast, onSaved, onClose }) {
  const isTeam = comp.kind === "team";
  const Modal = window.Modal;
  const [name, setName] = useStateRD(player.name || "");
  const [dojo, setDojo] = useStateRD(player.dojo || "");
  const [zekken, setZekken] = useStateRD(player.displayName && player.displayName !== player.name ? player.displayName : "");
  const [dan, setDan] = useStateRD(player.danGrade || (player.metadata && player.metadata[0]) || "");
  const [busy, setBusy] = useStateRD(false);

  const save = async () => {
    const n = name.trim(), d = dojo.trim();
    if (!n || !d) { showToast(isTeam ? "Team name and dojo are required" : "Name and dojo are required", "error"); return; }
    const admin = await window.promptAdminPassword();
    if (admin === null) return;
    setBusy(true);
    try {
      const metadata = window.buildPlayerMetadata(isTeam ? "" : dan.trim(), player.metadata);
      const payload = { name: n, dojo: d, displayName: !isTeam && comp.withZekkenName ? zekken.trim() : "", tag: player.tag || "" };
      if (metadata !== undefined) payload.metadata = metadata;
      const updated = await window.API.replaceParticipant(comp.id, rdPid(player), payload, password, admin);
      onSaved(comp, updated);
    } catch (err) {
      showToast(err.message, "error");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title={`Edit ${isTeam ? "team" : "competitor"}`}
      onClose={onClose}
      dismissable={!busy}
      footer={<>
        <button type="button" className="btn btn--ghost" onClick={onClose} disabled={busy}>Cancel</button>
        <button type="button" className="btn btn--primary" onClick={save} disabled={busy}>{busy ? "Saving…" : "Save changes"}</button>
      </>}
    >
      <div className="rd-walkup__grid">
        <label className="field">
          <span className="field__label">{isTeam ? "Team name" : "Name"}</span>
          <input className="input" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </label>
        <label className="field">
          <span className="field__label">Dojo</span>
          <input className="input" value={dojo} onChange={(e) => setDojo(e.target.value)} />
        </label>
        {!isTeam && comp.withZekkenName && (
          <label className="field">
            <span className="field__label">Zekken</span>
            <input className="input" value={zekken} onChange={(e) => setZekken(e.target.value)} placeholder="TANAKA" />
          </label>
        )}
        {!isTeam && (
          <label className="field">
            <span className="field__label">Dan grade <span className="field__opt">(optional)</span></span>
            <input className="input" value={dan} onChange={(e) => setDan(e.target.value)} placeholder="3 dan" />
          </label>
        )}
      </div>
      <p className="rd-edit__note">Edits apply to this competition's record only.</p>
    </Modal>
  );
}

// ---------------------------------------------------------------------------
// Page.
// ---------------------------------------------------------------------------
function AdminRegistrationDeskPage({ tournament, onBack, password, showToast, onUpdate, onLogout, onViewerMode }) {
  // `comps` is the desk's own authoritative copy, seeded once from the parent
  // and thereafter mutated only via setLocal (optimistic) and refresh() (server
  // truth). We deliberately do NOT re-seed from the tournament prop: app.jsx
  // runs its own SSE-driven setTournament independently of this desk, so a
  // prop-sync effect would clobber an in-flight optimistic check-in with
  // possibly-stale parent data. The desk's own SSE subscription (below) keeps
  // it fresh; tRef tracks the latest tournament only so refresh()'s onUpdate
  // merge starts from the newest snapshot.
  const [comps, setComps] = useStateRD(() => tournament.competitions || []);
  const tRef = useRefRD(tournament);
  useEffectRD(() => { tRef.current = tournament; }, [tournament]);
  // Number of check-in writes in flight. While > 0 the SSE-driven refresh
  // defers, so a concurrent reload (this desk's echo or another desk's event)
  // can't momentarily revert an optimistic check-in mid-write.
  const inFlightRef = useRefRD(0);

  const [selected, setSelected] = useStateRD("all");
  const [query, setQuery] = useStateRD("");
  const [dojoFilter, setDojoFilter] = useStateRD("all");
  const [uncheckedOnly, setUncheckedOnly] = useStateRD(false);
  const [groupByDojo, setGroupByDojo] = useStateRD(false);
  const [busy, setBusy] = useStateRD(false);
  const [walkUpOpen, setWalkUpOpen] = useStateRD(false);
  const [editTarget, setEditTarget] = useStateRD(null); // { comp, player }
  const [handoff, setHandoff] = useStateRD(null); // { name, tags: [{compName, kind, value}] }
  const searchRef = useRefRD(null);
  const mountedRef = useRefRD(true);
  useEffectRD(() => () => { mountedRef.current = false; }, []);

  // Autofocus search on mount and whenever the selected competition changes —
  // the desk's first move is always to type a name.
  useEffectRD(() => { searchRef.current && searchRef.current.focus(); }, [selected]);

  // Reset competition-scoped UI when switching rail entries.
  useEffectRD(() => { setWalkUpOpen(false); setDojoFilter("all"); setHandoff(null); setEditTarget(null); }, [selected]);

  // Refresh from the server and reconcile both local + parent state.
  const refresh = useCallbackRD(async () => {
    try {
      const fresh = await window.API.fetchCompetitions();
      if (!mountedRef.current) return;
      setComps(fresh);
      onUpdate({ ...tRef.current, competitions: fresh });
    } catch (e) {
      console.warn("Registration desk refresh failed", e);
    }
  }, [onUpdate]);

  // SSE: reconcile other desks' check-ins. Debounced so a burst is one refetch.
  useEffectRD(() => {
    if (!window.API || typeof window.API.subscribeToEvents !== "function") return;
    let timer = null;
    const fire = () => {
      // Don't reconcile while our own writes are mid-flight — wait for them to
      // settle so the refetch can't transiently revert an optimistic check-in.
      if (inFlightRef.current > 0) { timer = setTimeout(fire, 300); return; }
      timer = null;
      refresh();
    };
    const unsub = window.API.subscribeToEvents((event) => {
      if (event && (event.type === "participants_updated" || event.type === "competition_started" || event.type === "competition_completed" || event.type === "competition_deleted")) {
        if (timer) return;
        timer = setTimeout(fire, 600);
      }
    });
    return () => { if (timer) clearTimeout(timer); unsub(); };
  }, [refresh]);

  const peopleIndex = useMemoRD(() => rdBuildPeopleIndex(comps), [comps]);
  // Tournament-wide presence, computed once and shared by the rail's "All
  // competitions" item and the "all" headline (previously each re-walked the
  // index independently).
  const allStats = useMemoRD(() => {
    let present = 0;
    peopleIndex.forEach((rec) => { if (rdPresence(rec.entries) === "all") present += 1; });
    return { present, total: peopleIndex.size };
  }, [peopleIndex]);
  const selectedComp = selected === "all" ? null : comps.find((c) => c.id === selected);

  // Optimistic local mutation of one check-in flag.
  const setLocal = (compId, pid, val) => {
    setComps((cs) => cs.map((c) => c.id !== compId ? c : {
      ...c,
      players: (c.players || []).map((p) => rdPid(p) === pid ? { ...p, checkedIn: val } : p),
    }));
  };

  const announceHandoff = (name, pairs) => {
    setHandoff({ name, tags: pairs });
  };

  // Toggle one (competition, participant). Optimistic; SSE refresh reconciles.
  const toggleOne = async (compId, pid, val, opts = {}) => {
    setLocal(compId, pid, val);
    inFlightRef.current += 1;
    try {
      await window.API.toggleCheckIn(compId, pid, val, password);
      if (val && opts.handoff) announceHandoff(opts.handoff.name, opts.handoff.tags);
    } catch (e) {
      if (!mountedRef.current) return;
      showToast(e.message, "error");
      setLocal(compId, pid, !val); // revert
    } finally {
      inFlightRef.current -= 1;
    }
  };

  // Core "set this person's check-in across every competition they entered" —
  // optimistic local writes + parallel API calls, returning the failure count.
  // No busy/refresh side effects so callers can coordinate one bulk operation
  // around many people without each clearing the shared busy flag early.
  const checkPersonEntries = async (rec, makeChecked) => {
    const targets = rec.entries.filter(({ player }) => player.checkedIn !== makeChecked);
    if (!targets.length) return { failed: 0, total: 0 };
    targets.forEach(({ comp, player }) => setLocal(comp.id, rdPid(player), makeChecked));
    inFlightRef.current += 1;
    try {
      const results = await Promise.allSettled(
        targets.map(({ comp, player }) => window.API.toggleCheckIn(comp.id, rdPid(player), makeChecked, password))
      );
      return { failed: results.filter((r) => r.status === "rejected").length, total: targets.length };
    } finally {
      inFlightRef.current -= 1;
    }
  };

  // Person-level toggle in "all" mode: check into every competition (or undo all).
  const togglePerson = async (rec) => {
    const makeChecked = rdPresence(rec.entries) !== "all"; // none/partial → check all; all → undo all
    setBusy(true);
    try {
      const { failed, total } = await checkPersonEntries(rec, makeChecked);
      if (total === 0) return;
      if (failed) showToast(`${failed} of ${total} check-ins failed — reloading`, "error");
      else if (makeChecked) {
        announceHandoff(rec.name, rec.entries.map(({ comp, player }) => ({ compName: comp.name, ...rdPlayerTag(comp, player) })));
      }
      await refresh();
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  // Bulk-check a whole dojo of people into all their competitions ("all" mode).
  // One coordinated operation: busy is set once, every person's writes run
  // concurrently, and a single refresh reconciles — instead of N independent
  // togglePerson calls each racing the shared busy flag.
  const bulkCheckPeople = async (recs) => {
    const pending = recs.filter((rec) => rdPresence(rec.entries) !== "all");
    if (!pending.length) return;
    setBusy(true);
    try {
      const results = await Promise.allSettled(pending.map((rec) => checkPersonEntries(rec, true)));
      const failed = results.reduce((n, r) => n + (r.status === "fulfilled" ? r.value.failed : 1), 0);
      if (failed) showToast(`${failed} check-in(s) failed — reloading`, "error");
      await refresh();
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  // Bulk check-in a dojo within the selected competition (uses the bulk endpoint).
  const bulkDojoComp = async (compId, pids) => {
    if (!pids.length) return;
    setBusy(true);
    pids.forEach((pid) => setLocal(compId, pid, true));
    inFlightRef.current += 1;
    try {
      const res = await window.API.bulkCheckIn(compId, pids, password);
      // A 200 can still report unmatched ids (e.g. legacy name-keyed pids the
      // server couldn't resolve) — surface that rather than silently dropping it.
      if (res && res.notFound && res.notFound.length) {
        showToast(`${res.notFound.length} not matched — reloading`, "error");
      }
      await refresh();
    } catch (e) {
      showToast(e.message, "error");
      await refresh();
    } finally {
      inFlightRef.current -= 1;
      if (mountedRef.current) setBusy(false);
    }
  };

  // ---- Build the filtered, sorted rows for the current view ----------------
  const queryNorm = rdNorm(query);

  const rows = useMemoRD(() => {
    const matchesFilters = (player) => {
      if (uncheckedOnly && player.checkedIn) return false;
      if (dojoFilter !== "all" && player.dojo !== dojoFilter) return false;
      return true;
    };

    if (selected === "all") {
      const out = [];
      peopleIndex.forEach((rec) => {
        // person-level filters: match if ANY entry passes tag/dojo filters
        const anyEntry = rec.entries.some(({ player }) => matchesFilters(player));
        if (!anyEntry) return;
        const presence = rdPresence(rec.entries);
        if (uncheckedOnly && presence === "all") return;
        const hay = rdHaystack(rec.name, rec.dojo, rec.entries);
        const score = rdQueryScore(queryNorm, hay);
        if (score == null) return;
        out.push({ rec, presence, score });
      });
      out.sort((a, b) => {
        if (queryNorm) return a.score - b.score;
        const ap = a.presence === "all" ? 1 : 0, bp = b.presence === "all" ? 1 : 0;
        if (ap !== bp) return ap - bp; // unchecked first
        return a.rec.name.localeCompare(b.rec.name);
      });
      return out;
    }

    const comp = comps.find((c) => c.id === selected);
    if (!comp) return [];
    const out = [];
    (comp.players || []).forEach((player) => {
      if (!matchesFilters(player)) return;
      const hay = rdHaystack(player.name, player.dojo, [{ comp, player }]);
      const score = rdQueryScore(queryNorm, hay);
      if (score == null) return;
      const rec = peopleIndex.get(rdPersonKey(player));
      const others = rec ? rec.entries.filter((e) => e.comp.id !== comp.id) : [];
      out.push({ comp, player, others, score });
    });
    out.sort((a, b) => {
      if (queryNorm) return a.score - b.score;
      const ac = a.player.checkedIn ? 1 : 0, bc = b.player.checkedIn ? 1 : 0;
      if (ac !== bc) return ac - bc;
      return a.player.name.localeCompare(b.player.name);
    });
    return out;
  }, [selected, comps, peopleIndex, queryNorm, uncheckedOnly, dojoFilter]);

  // Dojo list for the filter (scoped to the selected competition / all).
  const dojoOptions = useMemoRD(() => {
    const set = new Set();
    const scope = selected === "all" ? comps : comps.filter((c) => c.id === selected);
    scope.forEach((c) => (c.players || []).forEach((p) => p.dojo && set.add(p.dojo)));
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [comps, selected]);

  // Headline metric for the current view.
  const headline = useMemoRD(() => {
    if (selected === "all") {
      return { present: allStats.present, total: allStats.total, noun: "people" };
    }
    const comp = comps.find((c) => c.id === selected);
    const players = comp ? comp.players || [] : [];
    return { present: players.filter((p) => p.checkedIn).length, total: players.length, noun: comp && comp.kind === "team" ? "teams" : "competitors" };
  }, [selected, comps, allStats]);

  // Enter on the search box checks in the single top hit — the arrival loop.
  const onSearchKeyDown = (e) => {
    if (e.key !== "Enter") return;
    e.preventDefault();
    if (!rows.length) return;
    const top = rows[0];
    if (selected === "all") {
      if (top.presence !== "all") togglePerson(top.rec);
    } else if (!top.player.checkedIn) {
      toggleOne(top.comp.id, rdPid(top.player), true, {
        handoff: { name: top.player.name, tags: [{ compName: top.comp.name, ...rdPlayerTag(top.comp, top.player) }] },
      });
    }
    setQuery("");
    searchRef.current && searchRef.current.focus();
  };

  const onAdded = async (comp, added) => {
    setWalkUpOpen(false);
    announceHandoff(added.name, [{ compName: comp.name, ...rdPlayerTag(comp, added) }]);
    await refresh();
  };

  const canWalkUp = selectedComp && (selectedComp.status === "setup" || !selectedComp.status);
  const allChecked = headline.total > 0 && headline.present === headline.total;
  const noComps = comps.length === 0;

  return (
    <div className="app">
      <AdminTopbarRD onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1280 }}>
        <BreadcrumbsRD items={[{ label: "Dashboard", onClick: onBack }, { label: "Registration desk" }]} />
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Registration desk</h1>
            <div className="page-head__sub">Check competitors in across every competition and hand them their player tag.</div>
          </div>
        </div>

        {noComps ? (
          <div className="empty" style={{ padding: "48px 24px" }}>
            <div className="icon" aria-hidden="true">🥋</div>
            <h3>No competitions yet</h3>
            <div style={{ fontSize: 13, color: "var(--ink-2)", maxWidth: 440, margin: "0 auto", lineHeight: 1.5 }}>
              Add a competition and its participants first, then the registration desk will gather every roster here for check-in.
            </div>
          </div>
        ) : (
          <div className="workspace rd-workspace">
            <RdRail comps={comps} allStats={allStats} selected={selected} onSelect={setSelected} />

            <div className="rd-main">
              <div className="rd-header">
                <div className="rd-search">
                  <RdSearchIcon />
                  <input
                    ref={searchRef}
                    className="rd-search__input"
                    type="text"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={onSearchKeyDown}
                    placeholder={selectedComp && selectedComp.kind === "team"
                      ? "Search team name or dojo… (Enter checks in the top match)"
                      : "Search name, dojo or zekken… (Enter checks in the top match)"}
                    aria-label="Search competitors"
                    autoComplete="off"
                    spellCheck="false"
                  />
                  {query && (
                    <button type="button" className="rd-search__clear" onClick={() => { setQuery(""); searchRef.current && searchRef.current.focus(); }} aria-label="Clear search">×</button>
                  )}
                </div>
                <div className="rd-headline">
                  <span className="rd-headline__metric"><strong>{headline.present}</strong> of {headline.total}</span>
                  <span className="rd-headline__noun">{headline.noun} checked in</span>
                </div>
                {selectedComp && (
                  <button
                    type="button"
                    className="btn btn--primary rd-walkup-btn"
                    disabled={!canWalkUp}
                    title={canWalkUp ? undefined : "Walk-ups can only be added before the draw is generated"}
                    onClick={() => setWalkUpOpen((v) => !v)}
                  >
                    + Add walk-up
                  </button>
                )}
              </div>

              <div className="rd-toolbar">
                {dojoOptions.length > 0 && (
                  <label className="rd-filter">
                    <span>Dojo</span>
                    <select className="select" value={dojoFilter} onChange={(e) => setDojoFilter(e.target.value)}>
                      <option value="all">All dojos</option>
                      {dojoOptions.map((d) => <option key={d} value={d}>{d}</option>)}
                    </select>
                  </label>
                )}
                <button type="button" className={`rd-toggle${uncheckedOnly ? " is-on" : ""}`} aria-pressed={uncheckedOnly} onClick={() => setUncheckedOnly((v) => !v)}>
                  Unchecked only
                </button>
                <button type="button" className={`rd-toggle${groupByDojo ? " is-on" : ""}`} aria-pressed={groupByDojo} onClick={() => setGroupByDojo((v) => !v)}>
                  Group by dojo
                </button>
                {busy && <span className="rd-toolbar__busy"><span className="spinner" aria-hidden="true" /> Saving…</span>}
              </div>

              {handoff && (
                <div className="rd-handoff" role="status" aria-live="polite">
                  <button type="button" className="rd-handoff__close" aria-label="Dismiss" onClick={() => setHandoff(null)}>×</button>
                  <div className="rd-handoff__lead"><RdCheckIcon /> Checked in — hand over to <strong>{handoff.name}</strong></div>
                  <div className="rd-handoff__tags">
                    {handoff.tags.map((t, i) => (
                      <div key={i} className="rd-handoff__tag">
                        {handoff.tags.length > 1 && <span className="rd-handoff__comp">{t.compName}</span>}
                        {t.kind === "pending"
                          ? <span className="rd-tag rd-tag--pending rd-tag--lg">Not assigned yet</span>
                          : <span className={`rd-tag rd-tag--${t.kind} rd-tag--lg`}><span className="rd-tag__label">{t.kind === "team" ? "Team" : "Tag"}</span><span className="rd-tag__value">{t.value}</span></span>}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {walkUpOpen && selectedComp && (
                <RdWalkUp
                  comp={selectedComp}
                  seedName={query.trim()}
                  password={password}
                  showToast={showToast}
                  onAdded={onAdded}
                  onCancel={() => setWalkUpOpen(false)}
                />
              )}

              <RdRoster
                mode={selected === "all" ? "all" : "comp"}
                rows={rows}
                groupByDojo={groupByDojo}
                query={query}
                busy={busy}
                allChecked={allChecked}
                canWalkUp={canWalkUp}
                onOpenWalkUp={() => setWalkUpOpen(true)}
                onPrimary={(row) => {
                  if (selected === "all") {
                    togglePerson(row.rec);
                  } else {
                    const willCheck = !row.player.checkedIn;
                    toggleOne(row.comp.id, rdPid(row.player), willCheck, willCheck ? {
                      handoff: { name: row.player.name, tags: [{ compName: row.comp.name, ...rdPlayerTag(row.comp, row.player) }] },
                    } : {});
                  }
                }}
                onToggleChip={(compId, pid, val) => toggleOne(compId, pid, val)}
                onEdit={(row) => setEditTarget({ comp: row.comp, player: row.player })}
                onBulkDojo={(dojo, theseRows) => {
                  if (selected === "all") {
                    // One coordinated op: check every listed person from this dojo
                    // into all their competitions (single busy + single refresh).
                    bulkCheckPeople(theseRows.map((r) => r.rec));
                  } else {
                    const pids = theseRows.filter((r) => !r.player.checkedIn).map((r) => rdPid(r.player));
                    bulkDojoComp(selected, pids);
                  }
                }}
              />
            </div>
          </div>
        )}
        {window.VersionFooter && <window.VersionFooter />}
      </div>
      {editTarget && (
        <RdEditModal
          comp={editTarget.comp}
          player={editTarget.player}
          password={password}
          showToast={showToast}
          onSaved={async (comp, updated) => {
            setEditTarget(null);
            showToast(`${updated && updated.name ? updated.name : "Competitor"} updated`);
            await refresh();
          }}
          onClose={() => setEditTarget(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Roster list — flat or grouped-by-dojo, with empty / no-match / all-done states.
// ---------------------------------------------------------------------------
function RdRoster({ mode, rows, groupByDojo, query, busy, allChecked, canWalkUp, onOpenWalkUp, onPrimary, onToggleChip, onEdit, onBulkDojo }) {
  const rowOf = (row, isLast) => {
    if (mode === "all") {
      const rep = row.rec.entries[0].player;
      return (
        <RdRow
          key={row.rec.key}
          mode="all"
          comp={row.rec.entries[0].comp}
          player={{ ...rep, name: row.rec.name, dojo: row.rec.dojo }}
          zekken={rdZekken(row.rec.entries)}
          entries={row.rec.entries}
          others={[]}
          presence={row.presence}
          busy={busy}
          isLast={isLast}
          onPrimary={() => onPrimary(row)}
          onToggle={onToggleChip}
        />
      );
    }
    return (
      <RdRow
        key={`${row.comp.id}:${rdPid(row.player)}`}
        mode="comp"
        comp={row.comp}
        player={row.player}
        zekken={rdZekken([{ comp: row.comp, player: row.player }])}
        entries={[]}
        others={row.others}
        checked={row.player.checkedIn}
        busy={busy}
        isLast={isLast}
        onPrimary={() => onPrimary(row)}
        onToggle={onToggleChip}
        onEdit={onEdit ? () => onEdit(row) : undefined}
      />
    );
  };

  if (!rows.length) {
    if (query.trim()) {
      return (
        <div className="empty rd-empty">
          <h3>No one matches “{query.trim()}”</h3>
          <div className="rd-empty__sub">Check the spelling, or clear filters. If they're not registered yet:</div>
          {canWalkUp && <button type="button" className="btn btn--primary" onClick={onOpenWalkUp}>+ Add “{query.trim()}” as walk-up</button>}
        </div>
      );
    }
    if (allChecked) {
      return (
        <div className="empty rd-empty rd-empty--done">
          <div className="icon" aria-hidden="true">✓</div>
          <h3>Everyone's here</h3>
          <div className="rd-empty__sub">All checked in. Search above to undo or re-check anyone.</div>
        </div>
      );
    }
    return (
      <div className="empty rd-empty">
        <h3>No one to show</h3>
        <div className="rd-empty__sub">{canWalkUp ? "Add a walk-up, or adjust the filters above." : "Adjust the filters above."}</div>
      </div>
    );
  }

  if (groupByDojo) {
    const groups = new Map();
    rows.forEach((row) => {
      const dojo = (mode === "all" ? row.rec.dojo : row.player.dojo) || "—";
      if (!groups.has(dojo)) groups.set(dojo, []);
      groups.get(dojo).push(row);
    });
    const ordered = Array.from(groups.entries()).sort((a, b) => a[0].localeCompare(b[0]));
    return (
      <div className="rd-roster">
        {ordered.map(([dojo, theseRows]) => {
          const pending = theseRows.filter((r) => mode === "all" ? r.presence !== "all" : !r.player.checkedIn).length;
          return (
            <div key={dojo} className="rd-group">
              <div className="rd-group__head">
                <span className="rd-group__name">{dojo}</span>
                <span className="rd-group__count">{theseRows.length}</span>
                {pending > 0 && (
                  <button type="button" className="btn btn--sm rd-group__bulk" disabled={busy} onClick={() => onBulkDojo(dojo, theseRows)}>
                    Check in all {pending}
                  </button>
                )}
              </div>
              {theseRows.map((row, i) => rowOf(row, i === theseRows.length - 1))}
            </div>
          );
        })}
      </div>
    );
  }

  return <div className="rd-roster">{rows.map((row, i) => rowOf(row, i === rows.length - 1))}</div>;
}

window.AdminRegistrationDeskPage = AdminRegistrationDeskPage;

export {
  rdNorm, rdPersonKey, rdPid, rdTokenScore, rdQueryScore, rdPlayerTag,
  rdBuildPeopleIndex, rdHaystack, rdPresence, rdZekken,
};
