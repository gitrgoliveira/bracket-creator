// Tournament-edit, competition-create, and bulk-import pages. See
// web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const validateAndNormalizeDate = window.validateAndNormalizeDate;
const decideNumericUpdate = window.decideNumericUpdate;
const dmyToIso = window.dmyToIso;
const isoToDmy = window.isoToDmy;
const MAX_TEAM_SIZE = window.MAX_TEAM_SIZE;
const MIN_YEAR = window.MIN_YEAR;
const MAX_YEAR = window.MAX_YEAR;
// Canonical courts cap (admin_helpers.jsx) — mirrors helper.MaxCourts
// on the Go side. Anchored to the A–Z labelling used on Shiaijo headers.
const MAX_COURTS = window.MAX_COURTS;
const pluralize = window.pluralize;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;

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
// formats. "pools" (pool-only) and "mixed" (pools + knockout) both
// require valid pool size + winners-per-pool.
//
// FR-050 / T044: the format taxonomy split "pools" into two distinct
// values — "pools" (legacy meaning in the old wire format kept for
// back-compat) and "mixed" (new: pools followed by knockout in the new
// taxonomy). Pool-size validation applies to both since both run pool
// play; the difference is whether a knockout stage follows.
//
// Exported for vitest at __tests__/admin_setup.test.jsx.
function validatePoolSettings(format, poolSize, winners) {
  if (format !== "pools" && format !== "mixed") return { ok: true, error: null };
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
  const [courts, setCourts] = useStateA(tournament.courts.length);
  const [checkInStart, setCheckInStart] = useStateA(tournament.checkInWindowStart || "");
  const [checkInEnd, setCheckInEnd] = useStateA(tournament.checkInWindowEnd || "");
  const [pass, setPass] = useStateA(""); // Leave empty to keep existing, unless changed
  const [error, setError] = useStateA("");

  const [announcementMessage, setAnnouncementMessage] = useStateA("");
  const [announcementDuration, setAnnouncementDuration] = useStateA(5);
  const [announcementInFlight, setAnnouncementInFlight] = useStateA(false);

  const handleSendAnnouncement = async () => {
    const trimmed = announcementMessage.trim();
    if (!trimmed) return;
    setAnnouncementInFlight(true);
    try {
      await window.API.sendAnnouncement(trimmed, announcementDuration, password);
      setAnnouncementMessage("");
      if (showToast) {
        showToast(`Announcement broadcast for ${announcementDuration} minutes!`, "success");
      }
    } catch (e) {
      if (showToast) {
        showToast(e.message, "error");
      }
    } finally {
      setAnnouncementInFlight(false);
    }
  };

  const handleSave = () => {
    // Trim early and send the trimmed value. The empty-name check below
    // already used `name.trim()`, but the onSave payload was passing the
    // raw `name` — so " Tournament " on the wire would round-trip to the
    // backend's trim and produce a canonical "Tournament" that diverges
    // from what the user sees in the input until next refresh.
    const trimmedName = name.trim();
    if (!trimmedName) { setError("Tournament name is required."); return; }
    const { norm, error: dateError } = validateAndNormalizeDate(date);
    if (dateError) { setError(dateError); return; }
    if (!Number.isInteger(courts) || courts < 1 || courts > MAX_COURTS) { setError(`Number of courts must be a whole number between 1 and ${MAX_COURTS}.`); return; }
    if ((checkInStart && !checkInEnd) || (!checkInStart && checkInEnd)) { setError("Both check-in start and end must be set together, or both must be empty."); return; }
    if (checkInStart && checkInEnd && checkInStart >= checkInEnd) { setError("Check-in start must be before check-in end."); return; }

    onSave({
      name: trimmedName,
      venue: venue.trim(),
      date: norm,
      password: pass || undefined,
      courts: Array.from({ length: courts }, (_, i) => String.fromCharCode(65 + i)),
      checkInWindowStart: checkInStart || undefined,
      checkInWindowEnd: checkInEnd || undefined
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
              {/* Picker bounds mirror AdminSettings's date input in */}
              {/* admin_competition.jsx and the MIN_YEAR/MAX_YEAR range */}
              {/* that validateAndNormalizeDate enforces at handleSave — */}
              {/* keeps the picker from offering years the validator */}
              {/* will then reject on submit. */}
              <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(date)} onChange={(e) => { setDate(isoToDmy(e.target.value)); setError(""); }} />
              <div className="field__hint">Pick the tournament day.</div>
            </div>
          </div>
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
              className="input"
              type="number"
              min="1"
              max={MAX_COURTS}
              step="1"
              value={Number.isFinite(courts) ? courts : ""}
              onChange={(e) => { setCourts(decideNumericUpdate(e.target.value, 1).value); setError(""); }}
            />
            <div className="field__hint">{`Enter a number (1-${MAX_COURTS}). Courts will be automatically labeled A, B, C, etc.`}</div>
          </div>
          <div className="row" style={{ marginTop: 16 }}>
            <div className="field">
              <label className="field__label">Check-in start (HH:MM)</label>
              <input className="input" type="time" value={checkInStart} onChange={(e) => setCheckInStart(e.target.value)} />
            </div>
            <div className="field">
              <label className="field__label">Check-in end (HH:MM)</label>
              <input className="input" type="time" value={checkInEnd} onChange={(e) => setCheckInEnd(e.target.value)} />
            </div>
          </div>
          {locked ? (
            <div className="field">
              <label className="field__label">Admin Password</label>
              <div className="field__hint" style={{ marginTop: 4 }}>
                This server is in locked mode. The admin password comes from <code>TOURNAMENT_PASSWORD_HASH</code> and can only be rotated by restarting the server with a new hash.
              </div>
            </div>
          ) : (
            <div className="field">
              <label className="field__label">Admin Password</label>
              <input className="input" type="password" value={pass} onChange={(e) => { setPass(e.target.value); setError(""); }} placeholder="••••••••" autoComplete="new-password" />
              <div className="field__hint">Enter a new password to change it. Leave blank to keep the current one.</div>
            </div>
          )}
          <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 16 }}>
            <button className="btn" onClick={onCancel}>Cancel</button>
            <button className="btn btn--primary" onClick={handleSave}>Save changes</button>
          </div>
        </div>

        <div className="page-head" style={{ marginTop: 32 }}><h1 className="page-head__title">Broadcast announcement</h1></div>
        <div className="card card--pad-lg">
          <div className="field">
            <label className="field__label">Message</label>
            <textarea
              className="input"
              style={{ width: "100%", height: 80, boxSizing: "border-box", padding: "8px 12px", resize: "vertical" }}
              maxLength={200}
              placeholder="e.g. Lunch break for 30 minutes, Delay on court B..."
              value={announcementMessage}
              onChange={(e) => setAnnouncementMessage(e.target.value)}
            />
            <div className="field__hint" style={{ display: "flex", justifyContent: "space-between", marginTop: 4 }}>
              <span>Maximum 200 characters. Pushes a temporary announcement to all active viewer screens.</span>
              <span>{announcementMessage.length}/200</span>
            </div>
          </div>
          <div className="field">
            <label className="field__label">Duration</label>
            <div style={{ display: "flex", gap: 16, marginTop: 8 }}>
              {[5, 10, 15, 30].map((m) => (
                <label key={m} style={{ display: "flex", alignItems: "center", gap: 6, cursor: "pointer", fontWeight: 500 }}>
                  <input
                    type="radio"
                    name="duration"
                    value={m}
                    checked={announcementDuration === m}
                    onChange={() => setAnnouncementDuration(m)}
                  />
                  <span>{m} min</span>
                </label>
              ))}
            </div>
          </div>
          <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 16 }}>
            <button
              className="btn btn--primary"
              disabled={!announcementMessage.trim() || announcementInFlight}
              onClick={handleSendAnnouncement}
            >
              {announcementInFlight ? "Sending..." : "Send announcement"}
            </button>
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
  // when the format runs pool play ("pools", "mixed", "league"); default
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
      if (!Number.isInteger(teamSize) || teamSize < 1 || teamSize > MAX_TEAM_SIZE) {
        setError(`Team size must be a whole number between 1 and ${MAX_TEAM_SIZE}.`);
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
    });
    // FR-050 / T044: persist poolFormat alongside the rest of the
    // create payload. buildCompetition (data.jsx) doesn't know about
    // this field yet; setting it after construction keeps the helper's
    // signature unchanged and lets the backend's JSON binding pick up
    // the camelCase key directly. Only emit for formats that run pool
    // play — knockout-only competitions have no pool phase.
    if (format === "pools" || format === "mixed" || format === "league") {
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
              {/* Picker bounds match validateAndNormalizeDate at create() — */}
              {/* see the equivalent comment on AdminEditTournament's date */}
              {/* field above and AdminSettings's date input in */}
              {/* admin_competition.jsx. */}
              <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(date)} onChange={(e) => setDate(isoToDmy(e.target.value))} />
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
            {/* FR-050 / T044 / T190: five-option format taxonomy. */}
            {/* - "playoffs" — knockout only (no pools). */}
            {/* - "mixed"    — pools followed by knockout (the new */}
            {/*               canonical value for what the old UI labelled */}
            {/*               "Pools + Knockout"; old "pools" rows remain */}
            {/*               readable but new competitions use "mixed"). */}
            {/* - "pools"    — pool play only, no knockout phase. */}
            {/* - "league"   — single round-robin, standings final, no */}
            {/*               playoff phase. IsPlayoffEnabled returns */}
            {/*               false on the backend, so the create-playoff */}
            {/*               affordance is hidden for these (T045). */}
            {/* - "swiss"    — Swiss-system pairing across N rounds */}
            {/*               (FR-050a); no pools or bracket. The */}
            {/*               swissRounds field below configures the */}
            {/*               round count. */}
            <div className="radio-group">
              <button className={`radio-pill ${format === "playoffs" ? "is-active" : ""}`} type="button" onClick={() => setFormat("playoffs")}>Knockout only</button>
              <button className={`radio-pill ${format === "mixed" ? "is-active" : ""}`} type="button" onClick={() => setFormat("mixed")}>Pools + Knockout</button>
              <button className={`radio-pill ${format === "pools" ? "is-active" : ""}`} type="button" onClick={() => setFormat("pools")}>Pools only</button>
              <button className={`radio-pill ${format === "league" ? "is-active" : ""}`} type="button" onClick={() => setFormat("league")}>League</button>
              <button className={`radio-pill ${format === "swiss" ? "is-active" : ""}`} type="button" onClick={() => setFormat("swiss")}>Swiss</button>
            </div>
            <div className="field__hint">
              {format === "playoffs" && "Direct single-elimination knockout."}
              {format === "mixed" && "Round-robin pools first, then top finishers advance to a knockout bracket."}
              {format === "pools" && "Round-robin pool play; standings within each pool decide placements (no knockout)."}
              {format === "league" && "Single round-robin across all participants; final standings determine the winner (no knockout)."}
              {format === "swiss" && "Swiss-system: fixed number of rounds, pairing players with equal win counts; cumulative standings decide the winner."}
            </div>
          </div>

          {(format === "pools" || format === "league") && (
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

          {(format === "pools" || format === "mixed") && (
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
    setLoading(true);
    setError(null);
    try {
      const fd = new FormData();
      files.forEach(f => fd.append("files", f, f.webkitRelativePath || f.name));
      // Use the centralized API wrapper (api.jsx) so auth + error handling
      // stay consistent with the rest of the admin UI.
      const body = await window.API.importCompetitions(fd, password);
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

// ES export for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
export { deriveCompetitionName, validatePoolSettings, validateSwissSettings };
