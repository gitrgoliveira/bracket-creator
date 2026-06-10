// Tournament-edit, competition-create, and bulk-import pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const validateAndNormalizeDate = window.validateAndNormalizeDate;
const decideNumericUpdate = window.decideNumericUpdate;
const dmyToIso = window.dmyToIso;
const isoToDmy = window.isoToDmy;
const deriveTournamentDays = window.deriveTournamentDays;
const MAX_TEAM_SIZE = window.MAX_TEAM_SIZE;
const MAX_TOURNAMENT_DURATION_DAYS = window.MAX_TOURNAMENT_DURATION_DAYS;
const MIN_YEAR = window.MIN_YEAR;
const MAX_YEAR = window.MAX_YEAR;
// Canonical courts cap (admin_helpers.jsx) — mirrors helper.MaxCourts
// on the Go side. Anchored to the A–Z labelling used on Shiaijo headers.
const MAX_COURTS = window.MAX_COURTS;
const pluralize = window.pluralize;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
// mp-s1gl: link-base helpers from viewer.jsx (exposed on window at load time).
const isNonPublicOrigin = window.isNonPublicOrigin || (() => false);

// Returns the final competition name, trimming the raw user input before
// the empty-check so a whitespace-only string ("   ") falls through to the
// kind/gender-based default instead of being treated as a valid name.
//
// The bug shape without this trim: `name || default` where name=" " is
// truthy → creates a comp with name=" " → backend trims `comp.Name` on
// save → canonical stored name is empty. The uniqueness check on the JS
// side would also compare untrimmed values against the canonical
// (already-trimmed) names on `tournament.competitions`, so a user typing
// "  Men's Cup  " when "Men's Cup" exists would miss the dedupe.
//
// Defaults match the labels users see in the dashboard's "team event" and
// "individual event" pickers.
function deriveCompetitionName(rawName, kind, gender) {
  const trimmed = (rawName || "").trim();
  if (trimmed) return trimmed;
  if (kind === "team") return gender === "F" ? "Women's Teams" : "Men's Teams";
  if (gender === "F") return "Women's Individual";
  if (gender === "M") return "Men's Individual";
  return "Individual";
}

// Pure submit-time validation for AdminCreateCompetition's pool-format
// fields. Returns { ok, error } so the caller (create) can route the
// error string through setError without duplicating the per-field
// thresholds.
//
// Why this exists: with the decideNumericUpdate switch, the inputs now
// store NaN when cleared (so the display stays empty instead of
// collapsing to "0"). Without a submit-time guard, NaN would land at
// buildEmptyCompetition's `poolSize || 3` fallback (NaN is falsy →
// defaults to 3) — silently using a different value than the user
// thought they entered. Negative/zero/non-integer values are even
// worse: `2.5 || 3` evaluates to `2.5` (truthy) and slips through.
//
// playoffs-only and league-only competitions don't use pool-size
// settings (knockout has no pools; league runs a single round-robin
// without user-configured size), so the guard short-circuits for those
// formats. "mixed" (pools + knockout) requires valid pool size +
// winners-per-pool.
//
// Exported for vitest at __tests__/admin_setup.test.jsx.
function validatePoolSettings(format, poolSize, winners) {
  if (format !== "mixed") return { ok: true, error: null };
  if (!Number.isInteger(poolSize) || poolSize < 3) {
    return { ok: false, error: "Players per pool must be a whole number ≥ 3." };
  }
  if (!Number.isInteger(winners) || winners < 1) {
    return { ok: false, error: "Winners per pool must be a whole number ≥ 1." };
  }
  return { ok: true, error: null };
}

// T190 (US13 — FR-050a): submit-time validation for the swissRounds
// field. Only meaningful when format === "swiss"; short-circuits with
// { ok: true } for other formats. Same shape as validatePoolSettings —
// NaN/fractional/zero/negative all blocked. Default UI value is 4
// (common Swiss tournament size for ~16 players).
//
// Exported for vitest at __tests__/admin_setup.test.jsx.
function validateSwissSettings(format, swissRounds) {
  if (format !== "swiss") return { ok: true, error: null };
  if (!Number.isInteger(swissRounds) || swissRounds < 1) {
    return { ok: false, error: "Number of Swiss rounds must be a whole number ≥ 1." };
  }
  return { ok: true, error: null };
}

function AdminEditTournament({ tournament, onCancel, onSave, onLogout, onViewerMode, authConfig, password, showToast }) {
  // In locked mode the on-disk Password is irrelevant — auth comes
  // from TOURNAMENT_PASSWORD_HASH and the backend rejects PUTs that
  // try to set a non-empty password. Surfacing the field anyway would
  // let an operator type a new password, click Save, and (depending
  // on the backend version) either see a 400 or silently believe
  // rotation succeeded. Hide it.
  const locked = authConfig === null || authConfig.mode === "locked";
  const [name, setName] = useStateA(tournament.name);
  const [venue, setVenue] = useStateA(tournament.venue);
  const [date, setDate] = useStateA(tournament.date);
  // DurationDays: default 1 for tournaments that predate this field
  // (tournament.durationDays is undefined / 0 for older records).
  const [durationDays, setDurationDays] = useStateA(tournament.durationDays || 1);
  const [courts, setCourts] = useStateA(tournament.courts.length);
  // Tournament mode (mp-7h7): read-only after creation — shown for
  // information only and NEVER included in the PUT payload.
  const tournamentMode = tournament.mode || "officiated";
  // mp-zoh Phase 5: ceremony block duration strings ("30m", "1h", "1h30m").
  // Empty string means "no block configured".
  const [openingBlock, setOpeningBlock] = useStateA(tournament.openingBlock || "");
  const [lunchBlock, setLunchBlock] = useStateA(tournament.lunchBlock || "");
  const [closingBlock, setClosingBlock] = useStateA(tournament.closingBlock || "");
  // mp-ef3: public tournament info fields.
  // mp-s1gl: externally-shareable base URL for QR codes / share links.
  const [publicURL, setPublicURL] = useStateA(tournament.publicURL || "");
  const [venueAddress, setVenueAddress] = useStateA(tournament.venueAddress || "");
  const [venueMapURL, setVenueMapURL] = useStateA(tournament.venueMapURL || "");
  const [openingTime, setOpeningTime] = useStateA(tournament.openingTime || "");
  const [closingTime, setClosingTime] = useStateA(tournament.closingTime || "");
  const [rulesURL, setRulesURL] = useStateA(tournament.rulesURL || "");
  const [awardsNote, setAwardsNote] = useStateA(tournament.awardsNote || "");
  const [infoNotes, setInfoNotes] = useStateA(tournament.infoNotes || "");
  const [contacts, setContacts] = useStateA((tournament.contacts || []).map((ct, i) => ({ ...ct, _key: i })));
  const nextKeyRef = useRefA((tournament.contacts || []).length);
  const [pass, setPass] = useStateA(""); // Leave empty to keep existing, unless changed
  const [error, setError] = useStateA("");
  // mp-sspn: which field the current validation error belongs to, so the
  // message renders inline beside its cause instead of only at the page top.
  // null = no field-scoped error (general errors still use the top banner).
  const [errorField, setErrorField] = useStateA(null);
  // mp-scf: theme colors (updated live by BrandingManager via onThemeChange).
  const [theme, setTheme] = useStateA(tournament.theme || null);

  // mp-sspn: in-flight save state — disables the primary button and swaps its
  // label to "Saving…" so the long form's primary action gives feedback
  // (previously it fired-and-navigated with no on-button cue). mountedRef
  // gates the post-await setSaving so a successful save (which unmounts this
  // view via navigation) doesn't setState on a torn-down component.
  const [saving, setSaving] = useStateA(false);
  // The `saving` STATE drives the button label/disabled; the savingRef LATCH
  // is the actual re-entry guard. State updates are async, so two rapid
  // Ctrl+S / clicks could both pass an `if (saving)` check before the
  // re-render flips it and fire duplicate PUTs. The ref is mutated and read
  // synchronously within the same tick, so the second call sees it.
  const savingRef = useRefA(false);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // mp-sspn: refs for the four validated fields so a failed save can focus +
  // scroll the offending input into view — on a 4-card form the top banner
  // alone lands off-screen when Save is clicked from the bottom.
  const nameRef = useRefA(null);
  const dateRef = useRefA(null);
  const daysRef = useRefA(null);
  const courtsRef = useRefA(null);
  const fieldRefs = { name: nameRef, date: dateRef, durationDays: daysRef, courts: courtsRef };

  // mp-sspn: progressive disclosure for the long optional public-info block.
  // Open by default only when the tournament already has public data, so
  // existing values are never hidden behind a collapsed section.
  const hasPublicInfo = !!(publicURL || venueAddress || venueMapURL || openingTime ||
    closingTime || rulesURL || awardsNote || infoNotes || (contacts && contacts.length));
  const [publicOpen, setPublicOpen] = useStateA(hasPublicInfo);

  // mp-sspn: dirty tracking for the unsaved-changes cue + cancel guard.
  // Snapshot the editable form fields (theme is excluded — BrandingManager's
  // mount-time onThemeChange sync would otherwise register a false change).
  const initialSnapRef = useRefA(null);
  const [dirty, setDirty] = useStateA(false);
  useEffectA(() => {
    const snap = JSON.stringify({
      name, venue, date, durationDays, courts, openingBlock, lunchBlock, closingBlock,
      publicURL, venueAddress, venueMapURL, openingTime, closingTime, rulesURL,
      awardsNote, infoNotes, pass,
      contacts: contacts.map(c => ({ label: c.label || "", value: c.value || "" })),
    });
    if (initialSnapRef.current === null) { initialSnapRef.current = snap; return; }
    setDirty(snap !== initialSnapRef.current);
  }, [name, venue, date, durationDays, courts, openingBlock, lunchBlock, closingBlock,
    publicURL, venueAddress, venueMapURL, openingTime, closingTime, rulesURL,
    awardsNote, infoNotes, pass, contacts]);

  // Elevated (destructive-ops) password — spec 004 / mp-e21. File mode only;
  // in locked mode it's the TOURNAMENT_ADMIN_PASSWORD_HASH env var (read-only
  // here). `elevatedConfigured` decides whether the current-password field is
  // shown (rotation) vs. a first-time set (TOFU).
  const elevatedConfigured = authConfig && authConfig.elevatedConfigured === true;
  const [adminNew, setAdminNew] = useStateA("");
  const [adminCurrent, setAdminCurrent] = useStateA("");
  const [adminSaving, setAdminSaving] = useStateA(false);

  const handleSetAdminPassword = async () => {
    if (!adminNew) { if (showToast) showToast("Enter a new admin password", "error"); return; }
    setAdminSaving(true);
    try {
      await window.API.setAdminPassword(adminNew, adminCurrent, password);
      setAdminNew(""); setAdminCurrent("");
      // Refresh auth-config so elevatedConfigured/elevatedRequired reflect the
      // new state (the gate is now active) for subsequent destructive actions.
      try {
        const cfg = await window.API.fetchAuthConfig();
        if (cfg && typeof cfg === "object") window.setCachedAuthConfig(cfg);
      } catch (_e) { /* non-fatal */ }
      if (showToast) showToast("Admin password updated", "success");
    } catch (e) {
      if (showToast) showToast(e.message, "error");
    } finally {
      setAdminSaving(false);
    }
  };

  // mp-sspn: set a field-scoped error, then focus + scroll its input so the
  // message is visible at its cause. Returns false so callers can `return
  // failField(...)` in one line. The public-info fields aren't validated
  // here, so failField only targets the four basics that are.
  const failField = (field, message) => {
    setError(message);
    setErrorField(field);
    const ref = fieldRefs[field];
    if (ref && ref.current) {
      try { ref.current.focus({ preventScroll: true }); } catch (_e) { ref.current.focus(); }
      // Respect reduced-motion: smooth scroll is motion the user may have opted out of.
      const reduce = typeof window.matchMedia === "function" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      ref.current.scrollIntoView({ block: "center", behavior: reduce ? "auto" : "smooth" });
    }
    return false;
  };

  const handleSave = async () => {
    if (savingRef.current) return; // synchronous re-entry guard (see savingRef)
    // Trim early and send the trimmed value. The empty-name check below
    // already used `name.trim()`, but the onSave payload was passing the
    // raw `name` — so " Tournament " on the wire would round-trip to the
    // backend's trim and produce a canonical "Tournament" that diverges
    // from what the user sees in the input until next refresh.
    const trimmedName = name.trim();
    if (!trimmedName) return failField("name", "Tournament name is required.");
    const { norm, error: dateError } = validateAndNormalizeDate(date);
    if (dateError) return failField("date", dateError);
    if (!Number.isInteger(durationDays) || durationDays < 1 || durationDays > MAX_TOURNAMENT_DURATION_DAYS) {
      return failField("durationDays", `Number of days must be a whole number between 1 and ${MAX_TOURNAMENT_DURATION_DAYS}.`);
    }
    if (!Number.isInteger(courts) || courts < 1 || courts > MAX_COURTS) {
      return failField("courts", `Number of courts must be a whole number between 1 and ${MAX_COURTS}.`);
    }
    setError("");
    setErrorField(null);
    savingRef.current = true; // latch set synchronously, before the first await
    setSaving(true);
    try {
      await onSave({
        name: trimmedName,
        venue: venue.trim(),
        date: norm,
        durationDays,
        password: pass || undefined,
        courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i)),
        openingBlock: openingBlock.trim() || undefined,
        lunchBlock: lunchBlock.trim() || undefined,
        closingBlock: closingBlock.trim() || undefined,
        publicURL: publicURL.trim() || undefined,
        venueAddress: venueAddress.trim() || undefined,
        venueMapURL: venueMapURL.trim() || undefined,
        openingTime: openingTime.trim() || undefined,
        closingTime: closingTime.trim() || undefined,
        rulesURL: rulesURL.trim() || undefined,
        awardsNote: awardsNote.trim() || undefined,
        infoNotes: infoNotes.trim() || undefined,
        contacts: contacts.filter(c => (c.value || "").trim()).map(c => ({ label: (c.label || "").trim(), value: (c.value || "").trim() })),
        theme: theme || undefined,
      });
    } finally {
      // Release the latch so a failed save can be retried. On success onSave
      // navigates away (this view unmounts); the mountedRef guard skips the
      // setState, but the ref reset is harmless either way.
      savingRef.current = false;
      if (mountedRef.current) setSaving(false);
    }
  };

  // mp-sspn: guard navigation away from unsaved edits.
  const handleCancel = () => {
    if (dirty && !window.confirm("Discard unsaved changes?")) return;
    onCancel();
  };

  // mp-sspn: keyboard accelerators — Cmd/Ctrl+S saves, Esc cancels. Latest
  // handlers are read through refs so the once-bound listener never calls a
  // stale closure (handleSave/handleCancel close over current form state).
  const handleSaveRef = useRefA(handleSave);
  handleSaveRef.current = handleSave;
  const handleCancelRef = useRefA(handleCancel);
  handleCancelRef.current = handleCancel;
  useEffectA(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "s" || e.key === "S")) {
        e.preventDefault();
        handleSaveRef.current();
      } else if (e.key === "Escape") {
        handleCancelRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page" style={{ maxWidth: 720 }}>
        <Breadcrumbs items={[
          { label: tournament.name, onClick: handleCancel },
          { label: "Edit details" }
        ]} />
        <div className="page-head"><h1 className="page-head__title">Edit tournament details</h1></div>
        {/* mp-sspn: top banner is only for general (non-field) errors; field- */}
        {/* scoped validation renders inline beside its input instead. */}
        {error && !errorField && <div className="auth__error" role="alert" style={{ marginBottom: 16 }}>{error}</div>}
        <div className="edit-stack">
        <div className="card card--pad-lg">
          <div className="card__head"><div className="card__title">Tournament details</div></div>
          <div className="field"><label className="field__label">Name</label><input ref={nameRef} className="input" value={name} onChange={(e) => { setName(e.target.value); setError(""); }} />{errorField === "name" && error && <div className="field__error" role="alert">{error}</div>}</div>
          <div className="row">
            <div className="field">
              <label className="field__label">Start date (Day 1)</label>
              {/* Picker bounds mirror AdminSettings's date input in */}
              {/* admin_competition.jsx and the MIN_YEAR/MAX_YEAR range */}
              {/* that validateAndNormalizeDate enforces at handleSave — */}
              {/* keeps the picker from offering years the validator */}
              {/* will then reject on submit. */}
              <input ref={dateRef} className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(date)} onChange={(e) => { setDate(isoToDmy(e.target.value)); setError(""); }} />
              <div className="field__hint">Pick the first day of the tournament.</div>
              {errorField === "date" && error && <div className="field__error" role="alert">{error}</div>}
            </div>
            <div className="field">
              <label className="field__label">Number of days</label>
              {/* decideNumericUpdate stores NaN for cleared input so render */}
              {/* side can use Number.isFinite check (same pattern as courts). */}
              <input
                ref={daysRef}
                className="input"
                type="number"
                min="1"
                max={MAX_TOURNAMENT_DURATION_DAYS}
                step="1"
                value={Number.isFinite(durationDays) ? durationDays : ""}
                onChange={(e) => { setDurationDays(decideNumericUpdate(e.target.value, 1).value); setError(""); }}
                style={{ maxWidth: 100 }}
              />
              <div className="field__hint">{`Duration in days (1–${MAX_TOURNAMENT_DURATION_DAYS}). Multi-day tournaments constrain competitions to their day.`}</div>
              {errorField === "durationDays" && error && <div className="field__error" role="alert">{error}</div>}
            </div>
          </div>
          <div className="row">
            <div className="field"><label className="field__label">Venue</label><input className="input" value={venue} onChange={(e) => { setVenue(e.target.value); setError(""); }} /></div>
            <div className="field">
              <label className="field__label">Number of Shiaijo (courts)</label>
              {/* decideNumericUpdate stores NaN for an empty input; render */}
              {/* NaN as "" so React doesn't warn ("Received NaN for the value */}
              {/* attribute") and the cleared input stays visually empty. */}
              {/* handleSave's Number.isInteger(courts) && courts >= 1 && */}
              {/* courts <= MAX_COURTS guard catches NaN, so the explicit Save */}
              {/* click can't push an invalid value to onSave. MAX_COURTS */}
              {/* mirrors helper.MaxCourts (admin_helpers.jsx). */}
              <input
                ref={courtsRef}
                className="input"
                type="number"
                min="1"
                max={MAX_COURTS}
                step="1"
                value={Number.isFinite(courts) ? courts : ""}
                onChange={(e) => { setCourts(decideNumericUpdate(e.target.value, 1).value); setError(""); }}
              />
              <div className="field__hint">{`Enter a number (1-${MAX_COURTS}). Courts will be automatically labeled A, B, C, etc.`}</div>
              {errorField === "courts" && error && <div className="field__error" role="alert">{error}</div>}
            </div>
          </div>
          {/* Tournament type (mp-7h7): read-only after creation. Displayed
              for information — it affects the auth boundary (officiated:
              main password gates everything; self-run: only destructive
              actions require a password). Never submitted on PUT. */}
          <div className="field" style={{ marginBottom: 0 }}>
            <label className="field__label">Tournament type</label>
            <div className="field__hint" style={{ marginTop: 4 }}>
              <strong>{tournamentMode === "self-run" ? "Self-run" : "Officiated"}</strong>
              {tournamentMode === "self-run"
                ? " — Scoring, check-in and other constructive actions are public. Only destructive actions (delete, import, roster changes) require the destructive-ops password."
                : " — All admin actions require the tournament password."}
              {" "}This setting was fixed at creation and cannot be changed.
            </div>
          </div>
        </div>
        {/* mp-zoh Phase 5: ceremony block duration inputs. These feed the */}
        {/* schedule estimator (backend Tournament.OpeningBlock etc.). */}
        {/* Duration strings like "30m", "1h", "1h30m". Leave blank to omit. */}
        <div className="card card--pad-lg">
          <div className="card__head">
            <div>
              <div className="card__title">Schedule blocks</div>
              <div className="card__sub">Durations feed the schedule estimator. Leave blank to omit a block.</div>
            </div>
          </div>
          <div className="row-3" style={{ marginBottom: 0 }}>
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field__label">Opening ceremony <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(duration)</span></label>
              <input className="input" value={openingBlock} onChange={(e) => setOpeningBlock(e.target.value)} placeholder="e.g. 30m" />
              <div className="field__hint">Duration of the opening ceremony block (e.g. "30m", "1h").</div>
            </div>
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field__label">Lunch break <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(duration)</span></label>
              <input className="input" value={lunchBlock} onChange={(e) => setLunchBlock(e.target.value)} placeholder="e.g. 1h" />
              <div className="field__hint">Duration of the lunch break block (e.g. "1h", "45m").</div>
            </div>
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field__label">Closing ceremony <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(duration)</span></label>
              <input className="input" value={closingBlock} onChange={(e) => setClosingBlock(e.target.value)} placeholder="e.g. 30m" />
              <div className="field__hint">Duration of the closing ceremony block.</div>
            </div>
          </div>
        </div>
        {/* mp-ef3: Tournament Information (Public) */}
        {/* mp-sspn: collapsible — all fields here are optional, so the section */}
        {/* is a disclosure that opens by default only when data already exists. */}
        <div className="card card--pad-lg">
          <button
            type="button"
            className="disclosure"
            aria-expanded={publicOpen}
            onClick={() => setPublicOpen((o) => !o)}
          >
            <span className="disclosure__text">
              <span className="card__title">Public information</span>
              <span className="card__sub">Shown to attendees on the public tournament page.{!publicOpen && hasPublicInfo ? " Configured." : ""}</span>
            </span>
            <span className="disclosure__toggle" aria-hidden="true">{publicOpen ? "−" : "+"}</span>
          </button>
          {publicOpen && (
          <div style={{ marginTop: 16 }}>
            {/* mp-s1gl: Public URL — single source of truth for externally-shareable links */}
            <div className="field">
              <label className="field__label">Public URL</label>
              <input
                className="input"
                value={publicURL}
                onChange={(e) => setPublicURL(e.target.value)}
                placeholder="https://my-tournament.example.com"
              />
              <div className="field__hint">The address participants reach this tournament at — used for QR codes and share links. Leave blank to use the current browser address.</div>
              {publicURL.trim() === "" && isNonPublicOrigin(window.location.origin) && (
                <div className="field__hint" style={{ color: "var(--red)", marginTop: 4 }}>
                  {(() => {
                    const o = window.location.origin;
                    const label = (!o || o === "null") ? "an unknown local address" : o;
                    return `Links will use this device's address (${label}), which may not be reachable by remote attendees. Set a Public URL to fix this.`;
                  })()}
                </div>
              )}
            </div>
            <div className="field">
              <label className="field__label">Venue address</label>
              <input className="input" value={venueAddress} onChange={(e) => setVenueAddress(e.target.value)} placeholder="123 Sport Centre Dr, City" />
            </div>
            <div className="field">
              <label className="field__label">Map link</label>
              <input className="input" value={venueMapURL} onChange={(e) => setVenueMapURL(e.target.value)} placeholder="https://maps.google.com/..." />
              <div className="field__hint">Link to venue on Google Maps or similar.</div>
            </div>
            <div className="row">
              <div className="field">
                <label className="field__label">Opening time</label>
                <input className="input" type="time" value={openingTime} onChange={(e) => setOpeningTime(e.target.value)} />
                <div className="field__hint">Doors open / spectator arrival time.</div>
              </div>
              <div className="field">
                <label className="field__label">Closing time</label>
                <input className="input" type="time" value={closingTime} onChange={(e) => setClosingTime(e.target.value)} />
                <div className="field__hint">Expected end time for the day.</div>
              </div>
            </div>
            <div className="field">
              <label className="field__label">Rules link</label>
              <input className="input" value={rulesURL} onChange={(e) => setRulesURL(e.target.value)} placeholder="https://..." />
              <div className="field__hint">Link to tournament rules document or PDF.</div>
            </div>
            <div className="field">
              <label className="field__label">Awards</label>
              <textarea className="input" rows={2} value={awardsNote} onChange={(e) => setAwardsNote(e.target.value)} placeholder="Gold, Silver, Bronze per competition" />
            </div>
            <div className="field">
              <label className="field__label">Notes</label>
              <textarea className="input" rows={3} value={infoNotes} onChange={(e) => setInfoNotes(e.target.value)} placeholder="General information for attendees" />
            </div>
            <div className="field">
              <label className="field__label">Contacts</label>
              <div className="field__hint" style={{ marginBottom: 8 }}>Add contact methods for attendees (max 10).</div>
              {contacts.map((ct, i) => (
                <div key={ct._key} style={{ display: "flex", gap: 8, marginBottom: 6 }}>
                  <input className="input" style={{ flex: "0 0 120px" }} value={ct.label} onChange={(e) => { const next = [...contacts]; next[i] = { ...next[i], label: e.target.value }; setContacts(next); }} placeholder="Label" />
                  <input className="input" style={{ flex: 1 }} value={ct.value} onChange={(e) => { const next = [...contacts]; next[i] = { ...next[i], value: e.target.value }; setContacts(next); }} placeholder="Value (email, phone, URL, etc.)" />
                  <button className="btn" style={{ padding: "4px 10px" }} onClick={() => setContacts(contacts.filter((_, j) => j !== i))}>✕</button>
                </div>
              ))}
              {contacts.length < 10 && (
                <button className="btn" style={{ fontSize: 12, marginTop: 4 }} onClick={() => setContacts([...contacts, { label: "", value: "", _key: nextKeyRef.current++ }])}>+ Add contact</button>
              )}
            </div>
          </div>
          )}
        </div>
        <div className="card card--pad-lg">
          <div className="card__head"><div className="card__title">Access &amp; security</div></div>
          {locked ? (
            <div className="field">
              <label className="field__label">Admin password</label>
              <div className="field__hint" style={{ marginTop: 4 }}>
                This server is in locked mode. The admin password comes from <code>TOURNAMENT_PASSWORD_HASH</code> and can only be rotated by restarting the server with a new hash.
              </div>
            </div>
          ) : (
            <div className="field">
              <label className="field__label">Admin password</label>
              <input className="input" type="password" value={pass} onChange={(e) => { setPass(e.target.value); setError(""); }} placeholder="••••••••" autoComplete="new-password" />
              <div className="field__hint">Enter a new password to change it. Leave blank to keep the current one.</div>
            </div>
          )}
          {/* Elevated (destructive-ops) password — spec 004 / mp-e21. */}
          {locked ? (
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field__label">Destructive-ops password</label>
              <div className="field__hint" style={{ marginTop: 4 }}>
                This server is in locked mode. The destructive-ops password comes from <code>TOURNAMENT_ADMIN_PASSWORD_HASH</code> and can only be changed by restarting the server with a new hash. If unset, destructive actions (delete competition, discard draw, roster changes, import) return 503.
              </div>
            </div>
          ) : (
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field__label">Destructive-ops password {elevatedConfigured ? "(set)" : "(not set)"}</label>
              {elevatedConfigured && (
                <input className="input" type="password" value={adminCurrent} onChange={(e) => setAdminCurrent(e.target.value)} placeholder="Current destructive-ops password" autoComplete="off" style={{ marginBottom: 8 }} />
              )}
              <div style={{ display: "flex", gap: 8 }}>
                <input className="input" type="password" value={adminNew} onChange={(e) => setAdminNew(e.target.value)} placeholder={elevatedConfigured ? "New destructive-ops password" : "Set destructive-ops password"} autoComplete="new-password" />
                <button className="btn" disabled={adminSaving} onClick={handleSetAdminPassword}>
                  {adminSaving ? "Saving…" : (elevatedConfigured ? "Update" : "Set")}
                </button>
              </div>
              <div className="field__hint">
                A separate password required for destructive actions (delete competition, discard draw, roster add/edit, import). Leave unset to gate those behind the main password only. {elevatedConfigured && "Changing it requires the current destructive-ops password."}
              </div>
            </div>
          )}
        </div>
        {/* mp-scf: branding (colors + logo). Appears before sponsors so the
            brand-identity section leads the page. onThemeChange propagates
            color changes back to this component's theme state so they are
            included in the tournament PUT payload when Save is clicked.
            Sits above the action bar because the colors/title it edits ARE
            committed by "Save changes"; the logo self-saves on upload. */}
        {window.BrandingManager && (
          <window.BrandingManager
            tournament={tournament}
            password={password}
            showToast={showToast}
            onThemeChange={setTheme}
          />
        )}
        {/* mp-c38: sponsor management lives on the same edit page so admins
            don't need to navigate elsewhere. SponsorsManager owns its own
            sponsors list (seeded from the tournament prop, updated locally
            from the API response on upload/delete) so unsaved edits in the
            tournament form above survive a sponsor change — no page reload. */}
        {window.SponsorsManager && (
          <window.SponsorsManager
            tournament={tournament}
            password={password}
            showToast={showToast}
          />
        )}
        {/* Action bar is the page's final commit, sitting below every card it
            (partly) persists. Branding colors + main password ride along on
            this Save; logo, sponsors and the destructive-ops password
            self-save via their own buttons. */}
        <div className="edit-actions">
          {/* mp-sspn: make the split persistence model explicit so the single */}
          {/* Save button's scope isn't a guess. */}
          <div className="edit-actions__note">
            Saves the tournament details, schedule blocks, public information, branding colors, and admin password.
            The logo, sponsors, and destructive-ops password save on their own buttons.
          </div>
          <div className="edit-actions__buttons">
            {dirty && <span className="edit-actions__dirty" aria-live="polite">Unsaved changes</span>}
            <button className="btn" onClick={handleCancel} disabled={saving}>Cancel</button>
            <button className="btn btn--primary" onClick={handleSave} disabled={saving}>{saving ? "Saving…" : "Save changes"}</button>
          </div>
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
  // FR-050 / T044: per-phase round-robin shape selector. Only meaningful
  // when the format runs pool play ("mixed", "league"); default
  // "full" (every-vs-every) matches the historical behaviour. "partial"
  // (neighbour-only) is the new option for league-sized fields where a
  // full round-robin would not fit in the day's schedule.
  const [poolFormat, setPoolFormat] = useStateA("full");
  const [useSample, setUseSample] = useStateA(false);
  const [sampleSize, setSampleSize] = useStateA("medium");
  const [poolMode, setPoolMode] = useStateA("max");
  const [poolSize, setPoolSize] = useStateA(3);
  const [winners, setWinners] = useStateA(2);
  // T190 (FR-050a): Swiss round count. Default 4 is the canonical
  // Swiss tournament size for ~16 players (log2 of typical field) —
  // matches the example in spec.md US13. Only used when format=swiss.
  const [swissRounds, setSwissRounds] = useStateA(4);
  const [startTime, setStartTime] = useStateA("09:00");
  const [date, setDate] = useStateA(tournament.date);
  const [teamSize, setTeamSize] = useStateA(5);
  const [numberPrefix, setNumberPrefix] = useStateA("");
  const [withZekken, setWithZekken] = useStateA(false);
  const [naginata, setNaginata] = useStateA(false);
  const [checkInEnabled, setCheckInEnabled] = useStateA(false);
  const [selectedCourts, setSelectedCourts] = useStateA(tournament.courts.slice(0, Math.min(2, tournament.courts.length)));
  const [error, setError] = useStateA("");

  const toggleCourt = (cc) => setSelectedCourts((sc) => sc.includes(cc) ? sc.filter((c) => c !== cc) : [...sc, cc].sort());

  const create = () => {
    // deriveCompetitionName trims the raw input first so whitespace-only
    // never bypasses the default-fallback (truthy strings of spaces would
    // create a backend-trimmed empty name). See the helper at the top of
    // this file for the full rationale + tests.
    const finalName = deriveCompetitionName(name, kind, gender);

    const exists = (tournament.competitions || []).some(cc => cc.name.toLowerCase() === finalName.toLowerCase());
    if (exists) {
      setError(`A competition named "${finalName}" already exists. Please use a unique name.`);
      return;
    }

    const { norm: normDate, error: dateError } = validateAndNormalizeDate(date);
    if (dateError) {
      setError(dateError);
      return;
    }

    // Pool-format guards. Pure helper above; this just routes the error
    // string through setError. See validatePoolSettings comment for the
    // failure modes (NaN, fractional, negative — all sneak past the
    // `value || 3` fallback in buildEmptyCompetition).
    const poolResult = validatePoolSettings(format, poolSize, winners);
    if (!poolResult.ok) {
      setError(poolResult.error);
      return;
    }

    // T190 (FR-050a): Swiss-format guard. Same shape/concerns as
    // validatePoolSettings — NaN/fractional/zero/negative all blocked
    // before they can land on the backend.
    const swissResult = validateSwissSettings(format, swissRounds);
    if (!swissResult.ok) {
      setError(swissResult.error);
      return;
    }

    // Team-size guard. StableInput's NaN-on-clear fix means teamSize can
    // now legitimately be NaN — buildEmptyCompetition would silently
    // fall back to 5 via `teamSize || 5`, so the user's cleared input
    // produces a different stored value than they see. Reject early
    // when kind=team. (Individual competitions don't expose this field;
    // teamSize=0 is the canonical value there.)
    if (kind === "team") {
      if (!Number.isInteger(teamSize)) {
        setError('Team size must be a whole number.');
        return;
      }
      if (teamSize < 2 || teamSize > MAX_TEAM_SIZE) {
        setError(`A team needs at least 2 members (max ${MAX_TEAM_SIZE}).`);
        return;
      }
    }

    // Two distinct names can normalize to the same slug (e.g. "Men's" vs
    // "Mens" both → "mens"). The name-uniqueness check above is case-
    // insensitive on the *name*, so it won't catch slug collisions —
    // guard explicitly against existing ids and append a numeric suffix
    // (or fall back to a timestamp) so create never 409s server-side.
    const baseSlug = finalName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '').substring(0, 50);
    const existingIds = new Set((tournament.competitions || []).map(cc => cc.id));
    let id = baseSlug || ("c-" + Date.now().toString(36));
    if (existingIds.has(id)) {
      if (baseSlug) {
        let n = 2;
        while (existingIds.has(`${baseSlug}-${n}`)) n++;
        id = `${baseSlug}-${n}`;
      } else {
        // Timestamp ID already taken (extremely unlikely — same-ms create).
        // Re-mint with crypto entropy rather than producing a leading-dash
        // slug. crypto.randomUUID is universal in browsers we target; fall
        // back to Math.random for ancient environments.
        const entropy = (typeof crypto !== "undefined" && crypto.randomUUID)
          ? crypto.randomUUID().slice(0, 8)
          : Math.floor(Math.random() * 1e9).toString(36);
        id = `c-${Date.now().toString(36)}-${entropy}`;
      }
    }
    const c = window.buildCompetition({
      id,
      name: finalName,
      kind, gender,
      format,
      sampleRoster: useSample ? sampleSize : null,
      seedCount: 0, status: "setup",
      startTime,
      date: normDate,
      teamSize: kind === "team" ? teamSize : 0,
      courts: selectedCourts.length ? selectedCourts : [tournament.courts[0]],
      poolMode, poolSize, winnersPerPool: winners,
      numberPrefix: numberPrefix.trim().substring(0, 3),
      withZekkenName: kind === "individual" ? withZekken : false,
      checkInEnabled: checkInEnabled,
    });
    // FR-050 / T044: persist poolFormat alongside the rest of the
    // create payload. buildCompetition (data.jsx) doesn't know about
    // this field yet; setting it after construction keeps the helper's
    // signature unchanged and lets the backend's JSON binding pick up
    // the camelCase key directly. Only emit for formats that run pool
    // play — knockout-only competitions have no pool phase.
    if (format === "mixed" || format === "league") {
      c.poolFormat = poolFormat;
    }
    // T190 (FR-050a): persist swissRounds when format=swiss. Same
    // post-construction pattern as poolFormat above — buildCompetition
    // doesn't know about this field; setting it on the result object
    // lets the backend's JSON binding pick up the camelCase key. The
    // backend uses `omitempty` so the field is invisible on non-Swiss
    // competitions even if it were set.
    if (format === "swiss") {
      c.swissRounds = swissRounds;
    }
    c.naginata = naginata;
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
              <label className="field__label">Day</label>
              {/* When the tournament has a start date and durationDays, offer */}
              {/* a select over the derived day list so the competition date */}
              {/* is always within the tournament's range. For a single-day */}
              {/* tournament (or when the tournament has no date yet) a single- */}
              {/* option select is shown so the label matches the day. */}
              {(() => {
                const days = deriveTournamentDays(tournament.date, tournament.durationDays || 1);
                if (days.length > 0) {
                  return (
                    <select
                      className="input"
                      value={date}
                      onChange={(e) => setDate(e.target.value)}
                    >
                      {days.map((d, i) => (
                        <option key={d} value={d}>Day {i + 1} — {d}</option>
                      ))}
                    </select>
                  );
                }
                // Fallback: tournament has no date yet — free date picker.
                return (
                  <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(date)} onChange={(e) => setDate(isoToDmy(e.target.value))} />
                );
              })()}
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
              <button className={`radio-pill ${format === "mixed" ? "is-active" : ""}`} type="button" onClick={() => setFormat("mixed")}>Pools + Knockout</button>
              <button className={`radio-pill ${format === "league" ? "is-active" : ""}`} type="button" onClick={() => setFormat("league")}>League</button>
              <button className={`radio-pill ${format === "swiss" ? "is-active" : ""}`} type="button" onClick={() => setFormat("swiss")}>Swiss</button>
            </div>
            <div className="field__hint">
              {format === "playoffs" && "Direct single-elimination knockout."}
              {format === "mixed" && "Round-robin pools first, then top finishers advance to a knockout bracket."}
              {format === "league" && "Single round-robin across all participants; final standings determine the winner (no knockout)."}
              {format === "swiss" && "Swiss-system: fixed number of rounds, pairing players with equal win counts; cumulative standings decide the winner."}
            </div>
          </div>

          {format === "league" && (
            <div className="field">
              <label className="field__label">Round-robin shape</label>
              <div className="radio-group">
                <button className={`radio-pill ${poolFormat === "full" ? "is-active" : ""}`} type="button" onClick={() => setPoolFormat("full")}>Full round-robin</button>
                <button className={`radio-pill ${poolFormat === "partial" ? "is-active" : ""}`} type="button" onClick={() => setPoolFormat("partial")}>Partial / neighbour-only</button>
              </div>
              <div className="field__hint">{poolFormat === "full" ? "Every participant plays every other participant in their pool." : "Each participant plays a neighbourhood subset — useful when a full round-robin would not fit in the day's schedule."}</div>
            </div>
          )}

          {/* T190 (FR-050a): Swiss rounds input. Only rendered for */}
          {/* format=swiss — keeps the create form uncluttered for other */}
          {/* formats. Same NaN-as-"" + decideNumericUpdate pattern as */}
          {/* poolSize / winners above; validateSwissSettings at submit */}
          {/* time rejects NaN / fractional / <1 before the field reaches */}
          {/* the backend. */}
          {format === "swiss" && (
            <div className="field">
              <label className="field__label">Number of rounds</label>
              <input
                className="input"
                type="number"
                min="1"
                step="1"
                value={Number.isFinite(swissRounds) ? swissRounds : ""}
                onChange={(e) => setSwissRounds(decideNumericUpdate(e.target.value, 1).value)}
                style={{ maxWidth: 120 }}
              />
              <div className="field__hint">Typical: 4 rounds for 16 players, 5 for 32, 6 for 64 (≈ log₂ of field size).</div>
            </div>
          )}

          <div className="field">
            <label className="checkbox field__label" style={{ display: "inline-flex" }}>
              <input type="checkbox" checked={useSample} onChange={(e) => setUseSample(e.target.checked)} />
              Pre-fill with sample roster
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

          {format === "mixed" && (
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
                {/* Same NaN-as-"" + decideNumericUpdate pattern as the courts */}
                {/* field above and admin_competition.jsx AdminSettings. */}
                {/* poolSize min=3 matches the backend's round-robin lower */}
                {/* bound; winners min=1 matches the backend's playoff entry */}
                {/* requirement. Submit-time guard at create() rejects */}
                {/* NaN/<min before passing to buildCompetition. */}
                <div className="field"><label className="field__label">Players per pool</label><input
                  className="input"
                  type="number"
                  min="3"
                  step="1"
                  value={Number.isFinite(poolSize) ? poolSize : ""}
                  onChange={(e) => setPoolSize(decideNumericUpdate(e.target.value, 3).value)}
                /></div>
                <div className="field"><label className="field__label">Winners per pool</label><input
                  className="input"
                  type="number"
                  min="1"
                  step="1"
                  value={Number.isFinite(winners) ? winners : ""}
                  onChange={(e) => setWinners(decideNumericUpdate(e.target.value, 1).value)}
                /></div>
              </div>
            </>
          )}

          {kind === "team" && (
            <div className="field">
              <label className="field__label">Team size</label>
              {/* Non-debounced input — uses onChange directly, not StableInput. */}
              {/* StableInput debounces 200ms; if the user clears the field and */}
              {/* immediately clicks "Create", the parent teamSize would still */}
              {/* hold the previous good value and the guard at create() would */}
              {/* let the stale value through. Direct onChange + decideNumericUpdate */}
              {/* keeps parent state synchronous with what the user sees. */}
              <input
                className="input"
                type="number"
                min="1"
                max={MAX_TEAM_SIZE}
                value={Number.isFinite(teamSize) ? teamSize : ""}
                onChange={(e) => setTeamSize(decideNumericUpdate(e.target.value, 1).value)}
              />
              <div className="field__hint">Standard kendo team is 5 (Senpou, Jihou, Chuken, Fukushou, Taishou).</div>
            </div>
          )}

          {kind === "individual" && (
            <div className="field">
              <label className="checkbox"><input type="checkbox" checked={withZekken} onChange={(e) => setWithZekken(e.target.checked)} /> Use Zekken display name</label>
              <div className="field__hint" style={{ marginTop: 4 }}>When enabled, participant CSV uses three columns: Name, Zekken, Dojo.</div>
            </div>
          )}

          <div className="field">
            <label className="checkbox"><input type="checkbox" checked={naginata} onChange={(e) => setNaginata(e.target.checked)} /> Naginata competition</label>
            <div className="field__hint" style={{ marginTop: 4 }}>Adds the Sune (S) ippon button to the score editor. Use for Naginata divisions.</div>
          </div>

          <div className="field">
            <label className="checkbox"><input type="checkbox" checked={checkInEnabled} onChange={(e) => setCheckInEnabled(e.target.checked)} /> Check-in tracking</label>
            <div className="field__hint" style={{ marginTop: 4 }}>Show check-in column and counter for this competition.</div>
          </div>

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
  // doImport's setResults/setError/setLoading and onImported timer fire
  // post-await. The page can unmount via onBack() while the upload is in
  // flight — gate via mountedRef in addition to the existing timer
  // cleanup below.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);
  // Tracks the success-confirmation timer so we can cancel it if the page
  // unmounts before onImported fires (avoids stray navigation after teardown).
  const importedTimerRef = useRefA(null);
  useEffectA(() => () => {
    if (importedTimerRef.current) {
      clearTimeout(importedTimerRef.current);
      importedTimerRef.current = null;
    }
  }, []);
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
    const admin = window.promptAdminPassword();
    if (admin === null) return;
    setLoading(true);
    setError(null);
    try {
      const fd = new FormData();
      files.forEach(f => fd.append("files", f, f.webkitRelativePath || f.name));
      // Use the centralized API wrapper (api.jsx) so auth + error handling
      // stay consistent with the rest of the admin UI.
      const body = await window.API.importCompetitions(fd, password, admin);
      // mountedRef gates the post-await setStates so a navigate-back
      // during the upload doesn't fire setResults / setTimeout on a
      // torn-down component. importedTimerRef has its own unmount
      // cleanup, but only if it was set — so don't even schedule it
      // post-unmount.
      if (!mountedRef.current) return;
      setResults(body.results || []);
      const hasErrors = (body.results || []).some(r => r.error);
      if (!hasErrors) {
        importedTimerRef.current = setTimeout(() => {
          importedTimerRef.current = null;
          // onImported is async (admin.jsx wires it to fetchCompetitions
          // + navigate). Wrap in Promise.resolve so a refresh rejection
          // doesn't surface as an unhandled promise rejection and leave
          // the UI stuck on the import page. Surface as a non-fatal
          // toast — the server-side import already completed; the user
          // can reload to recover.
          Promise.resolve()
            .then(() => onImported())
            .catch((e) => {
              console.warn("post-import refresh failed:", e);
              if (mountedRef.current) {
                setError("Import succeeded; refresh failed — please reload the page. " + (e?.message || ""));
              }
            });
        }, 1500);
      }
    } catch (e) {
      if (mountedRef.current) setError(e.message);
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  };

  const manifestFile = files.find(f => f.name === "manifest.yaml" || f.name === "manifest.yml" || f.name === "manifest.json");
  const csvFiles = files.filter(f => f.name.endsWith(".csv"));

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page">
        <Breadcrumbs items={[{ label: tournament?.name || "Tournament", onClick: onBack }, { label: "Import competitions" }]} />
        <div className="page-head"><h1 className="page-head__title">Import competitions</h1></div>

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
                      <td>{comp.format || "—"}</td>
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

// ES export for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
// The announcement-broadcast helpers (isSendAnnouncementDisabled /
// sendAnnouncementLabel) moved to admin_announcement.jsx alongside the
// AnnouncementComposer component they drive (mp-djc).
export { deriveCompetitionName, validatePoolSettings, validateSwissSettings };
