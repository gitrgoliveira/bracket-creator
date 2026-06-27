// admin_competition_settings.jsx — AdminSettings section (+ the formatCompMinutes
// schedule-time helper it consumes) split out of admin_competition.jsx (mp-hpe3).
// formatCompMinutes is ES-exported and re-exported by the admin_competition.jsx
// entry for the vitest suite.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

// Default on-clock minutes per match when a duration field is left blank.
// Mirrors defaultPerMatchClockMinutes in internal/engine/scheduler_slots.go
// (a nominal estimate anchor, not a regulation match time). Surfaced in the
// duration inputs so the operator knows what "blank" resolves to.
const DEFAULT_MATCH_MINUTES = 3;

const dmyToIso = window.dmyToIso;
const isoToDmy = window.isoToDmy;
const validateAndNormalizeDate = window.validateAndNormalizeDate;
const decideNumericUpdate = window.decideNumericUpdate;
const deriveTournamentDays = window.deriveTournamentDays;
const MIN_YEAR = window.MIN_YEAR;
const MAX_YEAR = window.MAX_YEAR;
const MAX_TEAM_SIZE = window.MAX_TEAM_SIZE;

// Format a total-minutes integer as "Xh Ym". Exported for unit tests.
export function formatCompMinutes(m) {
  if (!Number.isFinite(m) || m <= 0) return null;
  const h = Math.floor(m / 60);
  const min = m % 60;
  if (h === 0) return `${min}m`;
  return `${h}h ${String(min).padStart(2, "0")}m`;
}

function AdminSettings({ c, tournament, onUpdate, onBack, password, showToast, onStatusChange }) {
  const [lastSaved, setLastSaved] = useStateA(null);
  const [saveErr, setSaveErr] = useStateA(null);
  const [deleting, setDeleting] = useStateA(false);
  const [invalidating, setInvalidating] = useStateA(false);
  const [local, setLocal] = useStateA({ ...c });
  // Manual-save model (mp-3xn6): edits only persist when the operator clicks
  // "Save changes", matching the Tournament Edit-details page. isDirty drives
  // the unsaved indicator + the Save button's enabled state; saving disables
  // the button and shows "Saving…" during the in-flight PUT.
  const [isDirty, setIsDirty] = useStateA(false);
  const [saving, setSaving] = useStateA(false);

  // Schedule estimate (mp-zoh Phase 4): fetch per-competition estimate and
  // display it inline near the duration inputs. Re-fetches whenever the
  // saved competition changes (c.id, format, durations, courts, team size)
  // so the estimate reflects the latest server-persisted config. Uses an
  // AbortController so in-flight requests from a previous render are
  // cancelled before the next fetch starts (same pattern as admin_schedule.jsx).
  const [compEstimate, setCompEstimate] = useStateA(null);
  const [compEstimateLoading, setCompEstimateLoading] = useStateA(false);
  const [compEstimateErr, setCompEstimateErr] = useStateA(null);
  useEffectA(() => {
    if (!c.id) return;
    const controller = new AbortController();
    setCompEstimateLoading(true);
    setCompEstimateErr(null);
    window.API.estimateCompetitionSchedule(c.id, password, controller.signal).then(res => {
      setCompEstimate(res);
      setCompEstimateLoading(false);
    }).catch(e => {
      if (!controller.signal.aborted) {
        setCompEstimateErr(e.message || "Failed to estimate");
        setCompEstimateLoading(false);
      }
    });
    return () => controller.abort();
  // Re-fetch when the server-confirmed competition config changes. We depend
  // on `c` fields (not `local`) so we re-fetch after a successful save lands
  // in `c`, not on every unsaved edit. This also fires on mount and on any
  // SSE-driven competition_updated / schedule_updated refresh.
  //
  // Tournament ceremony/timing fields are included so the estimate refreshes
  // when the operator changes openingBlock, lunchBlock, closingBlock,
  // clockToElapsedMultiplier, or slowestCourtBufferPct on the tournament
  // settings screen and then returns here — otherwise the display would be
  // stale until a competition field changed (Finding 5 fix).
  }, [c.id, c.format, c.kind, c.poolMatchDuration, c.playoffMatchDuration, c.courts, c.teamSize, c.poolSize, c.poolSizeMode, c.poolWinners, c.roundRobin, c.poolFormat, c.swissRounds, c.checkInEnabled, password,
    tournament?.openingBlock, tournament?.lunchBlock, tournament?.closingBlock,
    tournament?.clockToElapsedMultiplier, tournament?.slowestCourtBufferPct]);
  // AdminSettings unmounts when the user navigates to a different section
  // via onSection() (AdminCompetition rerenders with a different child).
  // saveNow's .then/.catch and the delete handler's finally fire on
  // own state — gate via mountedRef. Same teardown-race shape as
  // admin_participants.jsx apply().
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Refs so saveNow reads fresh state at click time (NOT closure-captured
  // when the handler was defined). Same shape as admin.jsx's tRef/onUpdateRef
  // pattern from round-11.
  const cRef = useRefA(c);
  useEffectA(() => { cRef.current = c; }, [c]);
  const localRef = useRefA(local);
  useEffectA(() => { localRef.current = local; }, [local]);

  // Track which settings fields the user has actively edited since the
  // last successful save. Used by:
  //  (a) the sync effect below — preserve user's pending edits on these
  //      fields while still absorbing SSE updates to OTHER fields, and
  //  (b) saveNow's payload builder — overlay user-edited values onto a
  //      FRESH snapshot of `c` (cRef.current), so the PUT body reflects
  //      concurrent server-side changes to fields the user isn't editing
  //      rather than stale values captured when the edit was made.
  //
  // Without this set, a concurrent admin's settings change that lands in `c`
  // between the user's edit and their Save click would be dropped by the sync
  // effect AND overwritten by saveNow — net effect: saving one field silently
  // reverts simultaneous edits to other fields. Caught by Copilot round-15.
  const editedFieldsRef = useRefA(new Set());

  // Sync server-driven changes into local state (SSE → AdminApp → c prop).
  // For each field on `c`, propagate to `local` UNLESS the user has an
  // unsaved edit pending on that field (tracked in editedFieldsRef).
  // This absorbs concurrent admin changes without clobbering the user's
  // in-progress typing.
  //
  // Deps cover UI-rendered fields and any field round-tripped through
  // saveNow's PUT allowlist (`finalNext`). Status is listed so a
  // concurrent start/complete propagates into local for the delete-
  // confirm prompt's `local.status && local.status !== "setup"` check.
  useEffectA(() => {
    setLocal(prev => {
      const next = { ...prev };
      Object.keys(c).forEach(k => {
        if (!editedFieldsRef.current.has(k)) {
          next[k] = c[k];
        }
      });
      return next;
    });
  }, [c.id, c.name, c.date, c.startTime, c.poolSize, c.poolWinners, c.poolSizeMode, c.courts, c.roundRobin, c.withZekkenName, c.teamSize, c.numberPrefix, c.format, c.kind, c.mirror, c.status, c.poolFormat, c.poolMatchDuration, c.playoffMatchDuration, c.swissRounds, c.swissCurrentRound, c.naginata, c.checkInEnabled, c.leagueTiebreakTopN, c.leagueTwoThirdPlaces, c.teamMatchType]);

  const saveNow = () => {
    // Build `effective` from the LATEST server-known state (cRef.current)
    // overlaid with the user's currently-edited fields. Reading from refs at
    // click time (not values captured when the handler was defined) means any
    // SSE updates that landed since the user's last edit are absorbed, instead
    // of the PUT silently reverting concurrent admin changes to unrelated
    // fields. Caught by Copilot round-15.
    const latestC = cRef.current;
    const localSnap = localRef.current;
    const effective = { ...latestC };
    editedFieldsRef.current.forEach(k => {
      effective[k] = localSnap[k];
    });

    // Use the shared validator (admin_helpers.jsx). Returns the
    // canonical DD-MM-YYYY form on success, or an error message on
    // failure (bad shape, semantic-invalid day, year out of range).
    //
    // Skip validation for empty date — the backend's validateDateDMY
    // accepts "" as "Date TBA" and competitions created via the import
    // path can land here with an empty Date. Without this skip, the
    // user would be unable to save ANY unrelated setting on a date-less
    // competition (round-robin toggle, pool size, etc.) — saveNow would
    // reject with "Invalid date" even though the user hasn't touched the date.
    let dateNorm = "";
    if (effective.date && effective.date.trim() !== "") {
      const { norm, error: dateError } = validateAndNormalizeDate(effective.date);
      if (dateError) {
        setSaveErr(dateError);
        return;
      }
      dateNorm = norm;
    }

    // Trim before comparing AND before sending. The backend trims
    // `comp.Name` on save, so without normalizing here the JS-side
    // uniqueness check would compare "  Men's Cup  " against the
    // canonical "Men's Cup" and miss — landing two competitions with the
    // same effective name. Send the trimmed value so the value the user
    // sees in the input matches what the server stores.
    const trimmedName = (effective.name || "").trim();
    // Cross-file guard symmetry with the tournament edit/create paths
    // (admin_setup.jsx AdminEditTournament:80, app.jsx CreateTournament:410)
    // and with handlers_competition.go PUT (which now returns 400 on
    // empty-after-trim Name). Without this client-side guard, clicking Save
    // with an empty Name fires a wasted PUT roundtrip and only surfaces the
    // error via the inline .catch handler. Keep the failure inline + immediate
    // like the date validation above.
    if (!trimmedName) {
      setSaveErr("Competition name is required.");
      return;
    }
    if (trimmedName.toLowerCase() !== latestC.name.toLowerCase()) {
      const exists = (tournament.competitions || []).some(cc => cc.id !== latestC.id && cc.name.toLowerCase() === trimmedName.toLowerCase());
      if (exists) {
        setSaveErr(`Competition name "${trimmedName}" is already in use.`);
        return;
      }
    }

    // Trim numberPrefix — the input does substring(0, 3) per keystroke
    // but doesn't trim, so typing "  A" stores "  A" in local state and
    // (without this) lands "  A" on the server. The CREATE flow
    // (AdminCreateCompetition.create's deriveCompetitionName + trim
    // chain in admin_setup.jsx) already trims at create time; this
    // mirrors that for the SETTINGS edit flow so participant numbers
    // generated from the prefix can't end up like "  A1" / "  A2".
    // Cross-file guard symmetry: same shape as the comp.Name trim above.
    const trimmedPrefix = (effective.numberPrefix || "").trim();
    // Build the PUT payload from settings fields ONLY — do NOT spread the
    // full `c` snapshot or the full `next` snapshot. Pre-fix this was
    // `{ ...c, ...next, ... }`, which carried `local.status` and
    // `local.players` (and any other field the JSX/effects don't touch)
    // into the PUT body. If the sync-to-local effect deps list was
    // incomplete for any such field, SSE-pushed changes to that field
    // would not propagate into `local`, and the next save of ANY unrelated
    // setting would PUT the stale value back to the server — effectively
    // reverting the server-side change. Whitelisting the payload makes
    // AdminSettings genuinely settings-only and decouples save correctness
    // from the deps-list completeness of the sync effect.
    //
    // Fields server-managed via dedicated endpoints (status, players,
    // hasParticipantIDs) are deliberately excluded. If a new settings
    // field is added to the JSX or the OpenAPI settings list, also add
    // it here.
    //
    // `mirror` is in the allowlist even though AdminSettings doesn't
    // expose it as an editable control. data.jsx:200 (buildEmptyCompetition)
    // defaults new competitions to `mirror: true`; the backend transform
    // unconditionally applies `current.Mirror = comp.Mirror`, so an
    // omitted field would JSON-encode to false and clobber the disk
    // value on every settings save. Round-tripping `effective.mirror`
    // (sourced from latestC unless the user edited it) preserves the value.
    //
    // safeInt for the numeric fields: decideNumericUpdate stores NaN in
    // local state when the user clears a number input (so the render
    // layer can show "" instead of "0"). If the user clears poolSize and then
    // clicks Save, the cleared poolSize is still NaN in the edited overlay.
    // JSON.stringify({n: NaN}) produces '{"n":null}' — Go binds
    // JSON null to int as 0 — backend transform writes 0 to disk,
    // clobbering the prior good value. Falling back to `latestC.<field>`
    // when the effective value isn't a usable positive integer preserves
    // the disk value until the user types a valid replacement.
    const safeInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 1 ? v : fallback;
    // safeNonNegInt is the >=0 sibling for the per-phase duration
    // fields. T047: 0 means "no override — fall through to the legacy
    // matchDuration default per backend ApplyCompetitionDefaults", so
    // we DO want 0 to round-trip. Same NaN/fractional/negative guards
    // as safeInt; the only difference is the lower bound. Same
    // disk-clobber concern as safeInt: cleared input → NaN → JSON
    // null → Go zero. We fall back to latestC.<field> in that case so
    // an unrelated-field save doesn't silently zero out a duration the
    // user previously typed.
    const safeNonNegInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 0 ? v : fallback;
    const finalNext = {
      id: latestC.id,
      name: trimmedName,
      date: dateNorm,
      startTime: effective.startTime,
      poolSize: safeInt(effective.poolSize, latestC.poolSize),
      poolWinners: safeInt(effective.poolWinners, latestC.poolWinners),
      poolSizeMode: effective.poolSizeMode,
      courts: effective.courts,
      roundRobin: effective.roundRobin,
      withZekkenName: effective.withZekkenName,
      teamSize: safeInt(effective.teamSize, latestC.teamSize),
      numberPrefix: trimmedPrefix,
      format: effective.format,
      kind: effective.kind,
      mirror: effective.mirror,
      // FR-050 / T044: round-robin shape selector. Only meaningful when
      // the format runs pool play; the backend's validateCompetitionFormat
      // accepts the empty value, so a non-pool format can safely PUT "".
      poolFormat: effective.poolFormat || "",
      // FR-052..FR-054 / T047: per-phase duration overrides. Zero means
      // "use legacy default" — fall through to safeNonNegInt with
      // latestC's value to avoid NaN-clobbering a previously-set value.
      poolMatchDuration: safeNonNegInt(effective.poolMatchDuration, latestC.poolMatchDuration || 0),
      playoffMatchDuration: safeNonNegInt(effective.playoffMatchDuration, latestC.playoffMatchDuration || 0),
      // T190 (FR-050a): swissRounds is editable pre-start; safeInt
      // preserves the previously-saved value when the input is
      // cleared (so the cleared display doesn't clobber the disk
      // value before the user types a valid replacement).
      swissRounds: safeInt(effective.swissRounds, latestC.swissRounds || 0),
      naginata: !!effective.naginata,
      checkInEnabled: !!effective.checkInEnabled,
      // Phase 3b (mp-8rc9): league tie-breaker config. Only meaningful for
      // team-league competitions; safe to include for all formats because
      // the backend's PUT allowlist ignores unknown fields.
      leagueTiebreakTopN: safeInt(effective.leagueTiebreakTopN, latestC.leagueTiebreakTopN || 0),
      leagueTwoThirdPlaces: !!effective.leagueTwoThirdPlaces,
      // teamMatchType has no editable control in this form, but the settings
      // merge is a full replace — omitting it would clobber a kachinuki
      // competition's value to "" (fixed) on any save. Round-trip it like
      // `mirror` above to preserve the stored value.
      teamMatchType: effective.teamMatchType || latestC.teamMatchType || "",
    };
    // Capture the snapshot of edited fields we're about to persist. On
    // success we clear ONLY those fields from the edited set — preserving
    // any fields the user touched DURING the in-flight save (those need
    // to round-trip through a subsequent save).
    const persistingFields = new Set(editedFieldsRef.current);
    setSaving(true);
    Promise.resolve(onUpdate(finalNext)).then(() => {
      if (!mountedRef.current) return;
      // Drop the fields we just persisted from the edited set so the
      // sync effect can absorb the server-confirmed values on the next
      // SSE round-trip. Fields edited DURING the in-flight save stay in
      // the set and roll into the next save.
      persistingFields.forEach(k => editedFieldsRef.current.delete(k));
      const now = new Date();
      setLastSaved(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
      setSaveErr(null);
      setSaving(false);
      // Only clear the dirty flag if no fields were edited DURING the
      // in-flight save. Those linger in editedFieldsRef and still need a save.
      const stillDirty = editedFieldsRef.current.size > 0;
      setIsDirty(stillDirty);
      // Return to the dashboard on a clean save (nothing left pending) so the
      // operator lands back on the competition list after finishing edits.
      // A toast carries the confirmation since the on-page indicator unmounts.
      if (!stillDirty && onBack) {
        showToast("Competition settings saved");
        onBack();
      }
    }).catch((e) => {
      if (!mountedRef.current) return;
      setSaving(false);
      // Keep edited fields in the set and stay dirty — the user can retry.
      // updateCompetition already surfaced the error via showToast; mirror it
      // inline next to the Save button so the cause is visible without a
      // duplicate toast.
      setSaveErr(e?.message || "Save failed");
    });
  };

  // Manual-save model: edit handlers stage the change into `local` and mark
  // the field edited, but DO NOT persist — the operator commits all pending
  // edits explicitly via "Save changes". editedFieldsRef is still marked so
  // the sync effect preserves the user's in-progress edit if an SSE-driven
  // `c` update lands before they click Save (same concurrent-edit guard as
  // before, just over a longer window).
  const update = (k, v) => {
    editedFieldsRef.current.add(k);
    setLocal({ ...local, [k]: v });
    setIsDirty(true);
    // Clear a stale inline error once the user edits again.
    if (saveErr) setSaveErr(null);
  };

  // `updateNow` previously saved immediately (used by toggles / radio pills /
  // court chips). Under manual save it is identical to `update` — kept as a
  // separate name only to avoid churning the ~20 JSX call sites.
  const updateNow = (k, v) => {
    editedFieldsRef.current.add(k);
    setLocal({ ...local, [k]: v });
    setIsDirty(true);
    if (saveErr) setSaveErr(null);
  };

  // Number-input variant of `update`. Stores NaN in local state for empty
  // input so the render side can keep the display empty (see
  // decideNumericUpdate's contract). Marks the field as edited so the
  // sync effect preserves the user's in-progress clear / typed value
  // even if SSE pushes a c-update before they click Save.
  //
  // safeInt in saveNow's finalNext allowlist bridges the gap: an invalid
  // value (NaN / 1.5 / -1) falls back to latestC.<field>, so the PUT is
  // a no-op for that field but cross-field saves (e.g. Name typed
  // concurrently) still land. The cleared display resolves to the saved
  // value on the next SSE / PUT-response merge after the user types a
  // valid replacement or moves on.
  const updateNumber = (k, raw, min = 1) => {
    const { value } = decideNumericUpdate(raw, min);
    editedFieldsRef.current.add(k);
    setLocal({ ...local, [k]: value });
    setIsDirty(true);
    if (saveErr) setSaveErr(null);
  };

  const toggleCourt = (cc) => {
    const nextCourts = local.courts.includes(cc) ? local.courts.filter((x) => x !== cc) : [...local.courts, cc].sort();
    if (nextCourts.length) updateNow("courts", nextCourts);
  };

  // draw-ready lock: output-affecting fields — those that reach the Excel
  // generator (pools, courts, format, kind, team size, mirror, numberPrefix,
  // withZekkenName) — are disabled while a draw exists. Fields that do NOT
  // affect the generated workbook (name, date, startTime, checkInEnabled,
  // naginata) remain editable. Discard the draw from the competition header to
  // unlock everything.
  const isDrawReady = local.status === "draw-ready";

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{
            fontSize: 12.5,
            padding: "4px 8px",
            borderRadius: 4,
            background: saveErr ? "var(--red-soft)" : isDirty ? "var(--warn-soft)" : lastSaved ? "var(--accent-soft)" : "transparent",
            color: saveErr ? "var(--red)" : isDirty ? "var(--warn-ink)" : "var(--accent)",
            fontWeight: 600,
            transition: "all 300ms"
          }}>
            {saveErr ? `⚠ ${saveErr}` : saving ? "Saving…" : isDirty ? "● Unsaved changes" : lastSaved ? `✓ Saved at ${lastSaved}` : ""}
          </div>
          <button type="button" className="btn btn--primary" onClick={saveNow} disabled={!isDirty || saving}>
            {saving ? "Saving…" : "Save changes"}
          </button>
        </div>
      </div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={local.name} onChange={(e) => update("name", e.target.value)} /></div>
        <div className="field">
          <label className="field__label">Day</label>
          {/* When the tournament has a start date + durationDays, constrain */}
          {/* the competition date to the tournament's day list via a select. */}
          {/* Falls back to a free date picker when the tournament has no date. */}
          {(() => {
            const days = deriveTournamentDays(tournament.date, tournament.durationDays || 1);
            if (days.length > 0) {
              // A controlled <select> whose value matches no <option> would
              // silently display the first option while React state keeps the
              // real (stale or empty) value — the operator would then "save"
              // a value the UI never showed. Two cases need an explicit
              // matching option so the displayed value always tracks state:
              //   - empty date (legacy/imported competition with no day set):
              //     render a disabled "— Select a day —" placeholder so the
              //     select shows "unset" and forces a deliberate pick rather
              //     than persisting "" while appearing to show Day 1.
              //   - out-of-range date (e.g. the tournament duration was
              //     shortened after this competition was created): surface the
              //     stray value as a flagged option so the mismatch is visible.
              // Picking any real day clears either state on save.
              const isEmpty = !local.date;
              const outOfRange = local.date && !days.includes(local.date);
              return (
                <select
                  className="input"
                  value={local.date}
                  onChange={(e) => update("date", e.target.value)}
                >
                  {isEmpty && (
                    <option value="" disabled>— Select a day —</option>
                  )}
                  {outOfRange && (
                    <option key={local.date} value={local.date}>{local.date} (outside tournament days)</option>
                  )}
                  {days.map((d, i) => (
                    <option key={d} value={d}>Day {i + 1} — {d}</option>
                  ))}
                </select>
              );
            }
            return (
              <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(local.date)} onChange={(e) => update("date", isoToDmy(e.target.value))} />
            );
          })()}
          <div className="field__hint">Pick the competition day.</div>
        </div>
        <div className="field"><label className="field__label">Start time</label><input className="input" type="time" value={local.startTime} onChange={(e) => update("startTime", e.target.value)} /></div>
      </div>
      {local.kind === "team" && (
        <div className="field">
          <label className="field__label">Team size</label>
          {/* Cap is MAX_TEAM_SIZE (admin_helpers.jsx). TEAM_POSITIONS in */}
          {/* admin_scoring_modal.jsx is built from the same constant, so */}
          {/* this input can't allow a value the scoring UI doesn't render. */}
          {/* Render NaN as "" so clearing the input stays empty instead of */}
          {/* collapsing to "0"; saveNow's safeInt guard means a */}
          {/* cleared/invalid value never lands on the backend as 0. */}
          {/* draw-ready lock: teamSize is output-affecting. */}
          <input
            className="input"
            type="number"
            min="1"
            max={MAX_TEAM_SIZE}
            value={Number.isFinite(local.teamSize) ? local.teamSize : ""}
            onChange={(e) => updateNumber("teamSize", e.target.value, 1)}
            disabled={isDrawReady}
          />
        </div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo (courts)</label>
        {/* draw-ready lock: courts is output-affecting — discard the draw to reassign. */}
        {isDrawReady && (
          <div className="field__hint" style={{ marginBottom: 6, color: "var(--ink-2)", fontWeight: 500 }}>
            Discard the draw to change pools, courts, or format.
          </div>
        )}
        <div className="radio-group">
          {tournament.courts.map((cc) => (
            <button key={cc} className={`radio-pill ${local.courts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)} disabled={isDrawReady}>Shiaijo (court) {cc}</button>
          ))}
        </div>
        {(local.format === "league" || local.poolFormat === "partial") ? (() => {
          const playerCount = (c.players || []).length;
          const ct = (n) => n === 1 ? "1 court" : `${n} courts`;
          const pt = (n) => n === 1 ? "1 player" : `${n} players`;
          if (playerCount < 2) return <div className="field__hint">Suggested: up to {ct(Math.max(1, Math.floor(playerCount / 2) - 1))} for {pt(playerCount)}</div>;
          const numCourts = local.courts.length;
          const hardCap = Math.max(1, Math.floor(playerCount / 2));
          const suggestedCourts = Math.max(1, hardCap - 1);
          if (numCourts > hardCap) return <div className="field__hint" style={{ color: "var(--red)" }}>Too many courts — {hardCap} max for {pt(playerCount)} (suggested: {suggestedCourts})</div>;
          if (numCourts === hardCap && hardCap > suggestedCourts) return <div className="field__hint" style={{ color: "#78350f" }}>No rest between fights at {numCourts} courts — consider {ct(suggestedCourts)} for {pt(playerCount)}</div>;
          return <div className="field__hint">Suggested: up to {ct(suggestedCourts)} for {pt(playerCount)}</div>;
        })() : (
          <div className="field__hint">Concurrency = number of shiaijo assigned. Schedule prevents double-booking with other competitions.</div>
        )}
      </div>
      {local.format === "mixed" && (
        <>
          <div className="field">
            <label className="field__label">Pool size is a</label>
            {/* draw-ready lock: poolSizeMode, poolSize, poolWinners are output-affecting. */}
            <div className="radio-group">
              <button className={`radio-pill ${local.poolSizeMode === "max" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "max")} disabled={isDrawReady}>maximum</button>
              <button className={`radio-pill ${local.poolSizeMode === "min" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "min")} disabled={isDrawReady}>minimum</button>
            </div>
          </div>
          <div className="row">
            {/* Same NaN-as-"" + gated-save pattern as Team size above. */}
            {/* min=3 for poolSize matches the backend's pool-size lower */}
            {/* bound (3 players minimum to run a round-robin). */}
            <div className="field"><label className="field__label">Players per pool</label><input
              className="input"
              type="number"
              min="3"
              value={Number.isFinite(local.poolSize) ? local.poolSize : ""}
              onChange={(e) => updateNumber("poolSize", e.target.value, 3)}
              disabled={isDrawReady}
            /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input
              className="input"
              type="number"
              min="1"
              value={Number.isFinite(local.poolWinners) ? local.poolWinners : ""}
              onChange={(e) => updateNumber("poolWinners", e.target.value, 1)}
              disabled={isDrawReady}
            /></div>
          </div>
        </>
      )}
      {/* FR-052..FR-054 / T047: per-phase match-duration inputs. */}
      {/* Render rules: */}
      {(local.format === "mixed" || local.format === "league" || local.format === "playoffs" || local.format === "swiss") && (
        <div className="row">
          {(local.format === "mixed" || local.format === "league" || local.format === "swiss") && (
            <div className="field">
              <label className="field__label">{local.format === "swiss" ? "Round match duration (min)" : "Pool match duration (min)"}</label>
              <input
                className="input"
                type="number"
                min="0"
                step="1"
                value={Number.isFinite(local.poolMatchDuration) && local.poolMatchDuration > 0 ? local.poolMatchDuration : ""}
                onChange={(e) => updateNumber("poolMatchDuration", e.target.value, 0)}
                placeholder={`default: ${DEFAULT_MATCH_MINUTES}`}
              />
              <div className="field__hint">{local.format === "swiss" ? `Estimated minutes per Swiss-round match. Leave blank for the default (${DEFAULT_MATCH_MINUTES} min).` : `Estimated minutes per pool match. Leave blank for the default (${DEFAULT_MATCH_MINUTES} min).`}</div>
            </div>
          )}
          {(local.format === "playoffs" || local.format === "mixed") && (
            <div className="field">
              <label className="field__label">Playoff match duration (min)</label>
              <input
                className="input"
                type="number"
                min="0"
                step="1"
                value={Number.isFinite(local.playoffMatchDuration) && local.playoffMatchDuration > 0 ? local.playoffMatchDuration : ""}
                onChange={(e) => updateNumber("playoffMatchDuration", e.target.value, 0)}
                placeholder={`default: ${DEFAULT_MATCH_MINUTES}`}
              />
              <div className="field__hint">{`Estimated minutes per playoff/knockout match. Leave blank for the default (${DEFAULT_MATCH_MINUTES} min).`}</div>
            </div>
          )}
        </div>
      )}

      {/* T190 (FR-050a): swissRounds settings editor. Only rendered */}
      {/* when format=swiss. The backend allows editing pre-start; */}
      {/* changing rounds after start is allowed too (the next */}
      {/* "Generate next round" call will respect the new cap). */}
      {local.format === "swiss" && (
        <div className="field">
          <label className="field__label">Number of Swiss rounds</label>
          <input
            className="input"
            type="number"
            min="1"
            step="1"
            value={Number.isFinite(local.swissRounds) ? local.swissRounds : ""}
            onChange={(e) => updateNumber("swissRounds", e.target.value, 1)}
            style={{ maxWidth: 120 }}
          />
          <div className="field__hint">Typical: 4 rounds for 16 players, 5 for 32, 6 for 64 (≈ log₂ of field size).</div>
        </div>
      )}
      {/* mp-zoh Phase 4: inline schedule estimate. Shown below duration inputs */}
      {/* so the operator can immediately see the impact of duration changes */}
      {/* after the save lands. Re-fetches from the server on every */}
      {/* c-prop update (SSE schedule_updated / competition_updated). */}
      {(compEstimate || compEstimateLoading || compEstimateErr) && (
        <div style={{ padding: "10px 12px", borderRadius: 6, background: "var(--accent-soft, #f0f9ff)", border: "1px solid var(--accent, #3b82f6)", marginTop: 4 }}>
          <div style={{ fontSize: 12.5, fontWeight: 600, color: "var(--ink-2, #374151)", marginBottom: 4 }}>
            Schedule estimate
            {compEstimateLoading && <span className="spinner" style={{ marginLeft: 6, verticalAlign: "middle" }} />}
          </div>
          {compEstimateErr && (
            <div style={{ fontSize: 12, color: "var(--red, #ef4444)" }}>{compEstimateErr}</div>
          )}
          {compEstimate && !compEstimateErr && (() => {
            const total = formatCompMinutes(compEstimate.totalDurationMinutes);
            const perCourt = (compEstimate.perCourtMinutes || []).map(m => formatCompMinutes(m) || "0m");
            const ceremony = formatCompMinutes(compEstimate.ceremonyMinutes);
            if (!total) {
              return <div style={{ fontSize: 12, color: "var(--ink-3, #6b7280)" }}>No estimate yet — add participants and configure duration to see a projection.</div>;
            }
            return (
              <div style={{ fontSize: 12.5, color: "var(--ink-1, #111827)" }}>
                <div><strong>Total:</strong> {total}</div>
                {perCourt.length > 1 && (
                  <div style={{ marginTop: 2 }}>
                    <strong>Per court:</strong>{" "}
                    {perCourt.map((t, i) => `Court ${c.courts?.[i] || String.fromCharCode(65 + i)}: ${t}`).join(" · ")}
                  </div>
                )}
                {perCourt.length === 1 && (
                  <div style={{ marginTop: 2 }}>
                    <strong>Per court:</strong> {perCourt[0]}
                  </div>
                )}
                {ceremony && (
                  <div style={{ marginTop: 2 }}>
                    <strong>Ceremony blocks:</strong> {ceremony}
                  </div>
                )}
              </div>
            );
          })()}
        </div>
      )}
      <div className="field">
        <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={local.numberPrefix || ""} onChange={(e) => update("numberPrefix", e.target.value.substring(0, 3))} disabled={isDrawReady} style={{ maxWidth: 80 }} />
        <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {/* draw-ready lock: roundRobin is output-affecting. */}
        <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => updateNow("roundRobin", e.target.checked)} disabled={isDrawReady} /> Round-robin in pools</label>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => updateNow("withZekkenName", e.target.checked)} disabled={isDrawReady || local.kind === "team"} /> Use Zekken display name</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : "When enabled, participant CSV uses three columns: Name, Zekken, Dojo."}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.naginata} onChange={(e) => updateNow("naginata", e.target.checked)} /> Naginata competition</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Adds the Sune (S) ippon button to the score editor. Use for Naginata divisions.</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.checkInEnabled} onChange={(e) => updateNow("checkInEnabled", e.target.checked)} /> Check-in tracking</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Show check-in column and counter. Disable for competitions that don't need attendance tracking.</div>
        </div>
      </div>
      {/* Phase 3b (mp-8rc9): league tie-breaker settings — only for team leagues. */}
      {local.format === "league" && (local.teamSize > 0 || local.kind === "team") && (
        <div style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 8, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
          <div className="field">
            <label className="field__label">Break ties for top</label>
            <div className="radio-group">
              <button
                className={`radio-pill ${(local.leagueTiebreakTopN || 0) === 0 || local.leagueTiebreakTopN === 3 ? "is-active" : ""}`}
                type="button"
                onClick={() => updateNow("leagueTiebreakTopN", 3)}
              >Top 3</button>
              <button
                className={`radio-pill ${local.leagueTiebreakTopN === 4 ? "is-active" : ""}`}
                type="button"
                onClick={() => updateNow("leagueTiebreakTopN", 4)}
              >Top 4</button>
            </div>
            <div className="field__hint">Tied teams within this finishing band require an operator-run tie-breaker before standings are finalised.</div>
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <label className="checkbox">
              <input type="checkbox" checked={!!local.leagueTwoThirdPlaces} onChange={(e) => updateNow("leagueTwoThirdPlaces", e.target.checked)} />
              {" "}Award two joint 3rd places
            </label>
            <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>When enabled, teams tied entirely at 3rd place share bronze — no 3rd-vs-4th tie-breaker is needed. Standard kendo convention.</div>
          </div>
        </div>
      )}
      {/* Repeat Save at the foot of the long settings form so the operator
          doesn't have to scroll back to the header after editing. Same handler
          and disabled rules as the header button. */}
      <div style={{ marginTop: 20, display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 10 }}>
        {saveErr && <span style={{ fontSize: 12.5, color: "var(--red)", fontWeight: 600 }}>⚠ {saveErr}</span>}
        {!saveErr && isDirty && !saving && <span style={{ fontSize: 12.5, color: "var(--warn)", fontWeight: 600 }}>● Unsaved changes</span>}
        <button type="button" className="btn btn--primary" onClick={saveNow} disabled={!isDirty || saving}>
          {saving ? "Saving…" : "Save changes"}
        </button>
      </div>
      <div style={{ marginTop: 24, padding: 16, borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 12 }}>
        {(local.status === "pools" || local.status === "playoffs") && (
          <div>
            <button type="button" className="btn btn--danger btn--ghost" disabled={invalidating || deleting} onClick={async () => {
              if (await window.confirmDialog({ message: `Mark "${local.name}" as invalid? It will be excluded from results and can be deleted afterwards.`, confirmLabel: "Mark invalid", danger: true })) {
                const admin = await window.promptAdminPassword();
                if (admin === null) return;
                setInvalidating(true);
                try {
                  const updated = await window.API.invalidateCompetition(local.id, password, admin);
                  if (mountedRef.current) {
                    // Use the server response (if any) so that server-side
                    // field updates are reflected immediately. Fall back to
                    // forcing only `status: "invalid"` if the response isn't
                    // a competition object. Don't call onUpdate — that would
                    // trigger a full PUT with unsanitised local state.
                    const newStatus = (updated && typeof updated === "object" ? updated.status : null) ?? "invalid";
                    setLocal(prev => (updated && typeof updated === "object"
                      ? { ...prev, ...updated, players: updated.players ?? prev.players }
                      : { ...prev, status: "invalid" }));
                    if (onStatusChange) onStatusChange(newStatus);
                    showToast("Competition marked invalid.", "success");
                  }
                } catch (e) {
                  if (mountedRef.current) showToast(e.message, "error");
                } finally {
                  if (mountedRef.current) setInvalidating(false);
                }
              }
            }}>
              {invalidating && <span className="spinner" />}
              {invalidating ? "Marking invalid…" : "Mark competition invalid"}
            </button>
            <div className="field__hint" style={{ marginTop: 4 }}>Required before deleting an in-progress competition.</div>
          </div>
        )}
        <button type="button" className="btn btn--danger btn--ghost" disabled={deleting || invalidating} onClick={async () => {
          const started = local.status && local.status !== "setup" && local.status !== "draw-ready";
          const msg = started
            ? `"${local.name}" has already started. Deleting it will remove ALL matches and results. This cannot be undone. Continue?`
            : `Are you sure you want to delete "${local.name}"? This action cannot be undone.`;
          if (await window.confirmDialog({ message: msg, confirmLabel: "Delete competition", danger: true })) {
            const admin = await window.promptAdminPassword();
            if (admin === null) return;
            setDeleting(true);
            try {
              const ok = await window.API.deleteCompetition(local.id, password, admin);
              // onBack() unmounts AdminSettings via the parent's view
              // switch; setDeleting(false) in finally would then fire on
              // a torn-down component. Gate via mountedRef.
              if (ok) onBack();
              else if (mountedRef.current) showToast("Failed to delete competition.", "error");
            } catch (e) {
              console.error("Delete competition failed:", e);
              if (mountedRef.current) showToast(e.message, "error");
            } finally {
              if (mountedRef.current) setDeleting(false);
            }
          }
        }}>
          {deleting && <span className="spinner" />}
          {deleting ? "Deleting…" : "Delete competition"}
        </button>
        <div className="field__hint" style={{ marginTop: 4 }}>Deleting a started competition will remove all matches and results.</div>
      </div>
    </div>
  );
}


window.AdminSettings = AdminSettings;
window.formatCompMinutes = formatCompMinutes;
