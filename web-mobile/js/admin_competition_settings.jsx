// admin_competition_settings.jsx: AdminSettings section (+ the formatCompMinutes
// schedule-time helper it consumes) split out of admin_competition.jsx (mp-hpe3).
// formatCompMinutes is ES-exported and re-exported by the admin_competition.jsx
// entry for the vitest suite.

import { teamMatchTypeHint } from './pool_ids.jsx';
import { DurationInput } from './duration.jsx';
import { EstimateHeadline } from './admin_schedule_utils.jsx';
// bc-qual LP-5a: the "Knockout qualifiers" preview arithmetic AND the
// pool-size/winners form-coupling rules are shared with admin_setup.jsx
// (the competition CREATE form renders the same "Pool size is a" /
// "Knockout qualifiers" radios); both screens import the same functions
// from qualifier_preview.jsx rather than each carrying its own copy.
import {
  EXTRA_QUALIFIERS_STANDARD, EXTRA_QUALIFIERS_LARGER_POOLS, EXTRA_QUALIFIERS_FILL_BRACKET,
  computeQualifierPreview, formatQualifierPreviewLine, effectiveDrawPlayers,
  extraQualifiersRadioVisible, resetExtraQualifiersOnPoolModeChange,
  winnersForExtraQualifiersChange, winnersInputDisabled,
  extraQualifiersLabel, extraQualifiersHint,
} from './qualifier_preview.jsx';
import { seededRanks } from './admin_helpers.jsx';

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

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
  // Court-clash warnings surfaced after a save (mp-4a52). Non-blocking: the
  // save already committed; when present we stay on the page to show them
  // instead of returning to the dashboard.
  const [clashWarnings, setClashWarnings] = useStateA(null);

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
  // settings screen and then returns here: otherwise the display would be
  // stale until a competition field changed (Finding 5 fix).
  }, [c.id, c.format, c.kind, c.poolMatchDurationSeconds, c.playoffMatchDurationSeconds, c.courts, c.teamSize, c.poolSize, c.poolSizeMode, c.poolWinners, c.roundRobin, c.poolFormat, c.swissRounds, c.checkInEnabled, password,
    tournament?.openingBlock, tournament?.lunchBlock, tournament?.closingBlock,
    tournament?.clockToElapsedMultiplier, tournament?.slowestCourtBufferPct]);
  // AdminSettings unmounts when the user navigates to a different section
  // via onSection() (AdminCompetition rerenders with a different child).
  // saveNow's .then/.catch and the delete handler's finally fire on
  // own state: gate via mountedRef. Same teardown-race shape as
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
  //  (a) the sync effect below: preserve user's pending edits on these
  //      fields while still absorbing SSE updates to OTHER fields, and
  //  (b) saveNow's payload builder: overlay user-edited values onto a
  //      FRESH snapshot of `c` (cRef.current), so the PUT body reflects
  //      concurrent server-side changes to fields the user isn't editing
  //      rather than stale values captured when the edit was made.
  //
  // Without this set, a concurrent admin's settings change that lands in `c`
  // between the user's edit and their Save click would be dropped by the sync
  // effect AND overwritten by saveNow: net effect: saving one field silently
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
  }, [c.id, c.name, c.date, c.startTime, c.poolSize, c.poolWinners, c.poolSizeMode, c.courts, c.roundRobin, c.withZekkenName, c.teamSize, c.numberPrefix, c.format, c.kind, c.mirror, c.status, c.poolFormat, c.poolMatchDurationSeconds, c.playoffMatchDurationSeconds, c.swissRounds, c.swissCurrentRound, c.naginata, c.engi, c.checkInEnabled, c.leagueTiebreakTopN, c.leagueTwoThirdPlaces, c.teamMatchType]);

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
    // Skip validation for empty date: the backend's validateDateDMY
    // accepts "" as "Date TBA" and competitions created via the import
    // path can land here with an empty Date. Without this skip, the
    // user would be unable to save ANY unrelated setting on a date-less
    // competition (round-robin toggle, pool size, etc.): saveNow would
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
    // canonical "Men's Cup" and miss: landing two competitions with the
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

    // Trim numberPrefix: the input does substring(0, 3) per keystroke
    // but doesn't trim, so typing "  A" stores "  A" in local state and
    // (without this) lands "  A" on the server. The CREATE flow
    // (AdminCreateCompetition.create's deriveCompetitionName + trim
    // chain in admin_setup.jsx) already trims at create time; this
    // mirrors that for the SETTINGS edit flow so participant numbers
    // generated from the prefix can't end up like "  A1" / "  A2".
    // Cross-file guard symmetry: same shape as the comp.Name trim above.
    const trimmedPrefix = (effective.numberPrefix || "").trim();
    // Build the PUT payload from settings fields ONLY: do NOT spread the
    // full `c` snapshot or the full `next` snapshot. Pre-fix this was
    // `{ ...c, ...next, ... }`, which carried `local.status` and
    // `local.players` (and any other field the JSX/effects don't touch)
    // into the PUT body. If the sync-to-local effect deps list was
    // incomplete for any such field, SSE-pushed changes to that field
    // would not propagate into `local`, and the next save of ANY unrelated
    // setting would PUT the stale value back to the server: effectively
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
    // JSON.stringify({n: NaN}) produces '{"n":null}'. Go binds
    // JSON null to int as 0: backend transform writes 0 to disk,
    // clobbering the prior good value. Falling back to `latestC.<field>`
    // when the effective value isn't a usable positive integer preserves
    // the disk value until the user types a valid replacement.
    const safeInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 1 ? v : fallback;
    // safeNonNegInt is the >=0 sibling for the per-phase duration fields.
    // T047: 0 means "unset, use the scheduler default", so we DO want 0 to
    // round-trip: that is how clearing a duration resets it. Same
    // NaN/fractional/negative guards as safeInt; the only difference is the
    // lower bound. The NaN fallback to latestC.<field> still protects a field
    // the operator never touched from being zeroed by an unrelated save.
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
      // FR-052..FR-054 / T047: per-phase durations, in SECONDS. The retired
      // whole-minute fields are no longer sent; seconds are the only duration
      // representation the API carries. safeNonNegInt still guards the
      // disk-clobber case for a field the operator never touched (NaN falls
      // back to the last-saved value); an explicit clear stages 0, which is
      // finite and therefore round-trips as a genuine reset to the default.
      poolMatchDurationSeconds: safeNonNegInt(effective.poolMatchDurationSeconds, latestC.poolMatchDurationSeconds || 0),
      playoffMatchDurationSeconds: safeNonNegInt(effective.playoffMatchDurationSeconds, latestC.playoffMatchDurationSeconds || 0),
      // T190 (FR-050a): swissRounds is editable pre-start; safeInt
      // preserves the previously-saved value when the input is
      // cleared (so the cleared display doesn't clobber the disk
      // value before the user types a valid replacement).
      swissRounds: safeInt(effective.swissRounds, latestC.swissRounds || 0),
      naginata: !!effective.naginata,
      // Engi (flag-scoring kata pairs). Round-tripped for the same reason as
      // `mirror` and `teamMatchType`: the backend transform unconditionally
      // applies `current.Engi = comp.Engi`, so omitting the field JSON-encodes
      // to false and either silently converts an engi competition to kendo
      // ippon scoring (status=setup, where the change guard doesn't fire) or
      // rejects EVERY settings save with "engi can only be changed before the
      // competition starts" (draw-ready and later, where it does) — even
      // though the operator never touched the control, which is disabled at
      // those statuses.
      engi: !!effective.engi,
      checkInEnabled: !!effective.checkInEnabled,
      // Phase 3b (mp-8rc9): league tie-breaker config. Only meaningful for
      // team-league competitions; safe to include for all formats because
      // the backend's PUT allowlist ignores unknown fields.
      leagueTiebreakTopN: safeInt(effective.leagueTiebreakTopN, latestC.leagueTiebreakTopN || 0),
      leagueTwoThirdPlaces: !!effective.leagueTwoThirdPlaces,
      // teamMatchType is edited via the Team match format pills above; the
      // merge is a full replace: omitting it would clobber a kachinuki
      // competition's value to "" (fixed) on any save. Round-trip it like
      // `mirror` above to preserve the stored value.
      teamMatchType: effective.teamMatchType || latestC.teamMatchType || "",
      // bc-qual LP-5a: knockout qualifiers, edited by the "Knockout
      // qualifiers" pills below. Included for the same reason as
      // mirror/teamMatchType/naginata above: the backend transform
      // unconditionally applies `current.ExtraQualifiers =
      // comp.ExtraQualifiers`, so omitting the field would clobber a stored
      // non-standard value to "" on every settings save.
      //
      // NO `|| latestC.extraQualifiers` fallback: "" is this field's STANDARD
      // value, not "unset". A falsy-coalescing chain reads the operator's
      // "Standard" pick as absent and re-sends the stored non-standard value,
      // which makes Standard unreachable from this screen -- and worse on the
      // sibling path, where switching "Pool size is a" to maximum stages
      // poolSizeMode="max" AND extraQualifiers="" together: the chain would
      // ship max sizing with the old non-standard value, which
      // state.ValidateExtraQualifiers rejects, 400ing every settings save with
      // no control able to clear it. `effective` is latestC overlaid with the
      // edited fields, so it already carries the stored value for an untouched
      // field; the `|| ""` only normalizes a legacy record's undefined.
      extraQualifiers: effective.extraQualifiers || "",
    };
    // Snapshot the VALUE of each edited field we're about to persist (not just
    // the field name). On success we clear a field only if its current staged
    // value still equals what we sent: so a field RE-EDITED during the
    // in-flight save (its value now differs) stays in the edited set and rolls
    // into the next save. A name-only Set couldn't distinguish the original
    // edit from a concurrent re-edit and would drop the user's latest change.
    // localRef.current is written synchronously by the edit handlers, so it
    // reflects any re-edit that landed while the PUT was in flight.
    const persistingValues = {};
    editedFieldsRef.current.forEach(k => { persistingValues[k] = localRef.current[k]; });
    setSaving(true);
    Promise.resolve(onUpdate(finalNext)).then(() => {
      if (!mountedRef.current) return;
      // Drop only the persisted fields whose staged value is unchanged, so the
      // sync effect can absorb the server-confirmed values on the next SSE
      // round-trip. Fields re-edited DURING the in-flight save (value differs)
      // stay in the set and roll into the next save.
      Object.keys(persistingValues).forEach(k => {
        if (localRef.current[k] === persistingValues[k]) editedFieldsRef.current.delete(k);
      });
      const now = new Date();
      setLastSaved(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
      setSaveErr(null);
      setSaving(false);
      // Only clear the dirty flag if no fields were edited DURING the
      // in-flight save. Those linger in editedFieldsRef and still need a save.
      const stillDirty = editedFieldsRef.current.size > 0;
      setIsDirty(stillDirty);
      // On a clean save (nothing left pending), check whether the change put
      // this competition on a court (shiaijo) at the same time as another. The
      // save has already committed: this is a non-blocking warning. If clashes
      // exist we surface them and STAY on the page; otherwise we return to the
      // dashboard. A clash-check failure must not strand the operator, so it
      // falls through to the normal saved-and-return path.
      if (!stillDirty) {
        window.API.getScheduleClashes(c.id, password)
          .then((clashes) => {
            if (!mountedRef.current) return;
            if (Array.isArray(clashes) && clashes.length > 0) {
              setClashWarnings(clashes);
              showToast(`Saved. ${clashes.length} court clash${clashes.length > 1 ? "es" : ""} detected, review below`, "error");
            } else {
              setClashWarnings(null);
              showToast("Competition settings saved");
              // Don't navigate away if the operator started a NEW edit during
              // the clash round-trip: that would silently discard it. Stay on
              // the page so the pending change can be saved.
              if (onBack && editedFieldsRef.current.size === 0) onBack();
            }
          })
          .catch(() => {
            if (!mountedRef.current) return;
            showToast("Competition settings saved");
            if (onBack && editedFieldsRef.current.size === 0) onBack();
          });
      }
    }).catch((e) => {
      if (!mountedRef.current) return;
      setSaving(false);
      // Keep edited fields in the set and stay dirty: the user can retry.
      // updateCompetition already surfaced the error via showToast; mirror it
      // inline next to the Save button so the cause is visible without a
      // duplicate toast.
      setSaveErr(e?.message || "Save failed");
    });
  };

  // Manual-save model: edit handlers stage the change into `local` and mark
  // the field edited, but DO NOT persist: the operator commits all pending
  // edits explicitly via "Save changes". editedFieldsRef is still marked so
  // the sync effect preserves the user's in-progress edit if an SSE-driven
  // `c` update lands before they click Save (same concurrent-edit guard as
  // before, just over a longer window).
  const update = (k, v) => {
    editedFieldsRef.current.add(k);
    // Functional updater so back-to-back edits (e.g. fast successive
    // toggles / paste-driven multi-field updates) don't clobber each other
    // by spreading a stale closure-captured `local`. We also write the
    // result to localRef synchronously: saveNow reads localRef.current when
    // the operator clicks Save, and the useEffect that syncs localRef from
    // `local` is async: a rapid edit-then-click could otherwise land before
    // the effect runs and the latest edit would be missing from the PUT.
    setLocal((prev) => {
      const next = { ...prev, [k]: v };
      localRef.current = next;
      return next;
    });
    setIsDirty(true);
    // Clear a stale inline error once the user edits again.
    if (saveErr) setSaveErr(null);
    // Clear the post-save clash banner: its "saved" wording and clash list go
    // stale the moment new unsaved edits are staged (the edit may even resolve
    // the clash). Re-checked on the next save.
    if (clashWarnings) setClashWarnings(null);
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
    setLocal((prev) => {
      const next = { ...prev, [k]: value };
      localRef.current = next;
      return next;
    });
    setIsDirty(true);
    if (saveErr) setSaveErr(null);
    if (clashWarnings) setClashWarnings(null);
  };

  // Duration handler for the m:ss DurationInput. Stages the canonical seconds
  // value, which is the only duration representation the API carries.
  //
  // A blank clear stages 0, not NaN. 0 is the wire value for "unset, use the
  // scheduler default", so clearing the field genuinely resets it. Staging NaN
  // instead would hit the safeNonNegInt fallback in saveNow and silently re-save
  // the previous value, while the field displayed "Using the default, 3:00" --
  // a save that confirms an outcome it did not perform. The disk-clobber guard
  // that fallback exists for still protects every field the operator never
  // touched: this handler only runs on an actual edit.
  const updateDurationSeconds = (secKey) => (secOrNaN) => {
    update(secKey, Number.isFinite(secOrNaN) ? secOrNaN : 0);
  };

  // Track which duration fields currently hold an out-of-band / unparseable
  // value so Save can be blocked. DurationInput never emits an invalid value,
  // so without this gate a bad entry would just be silently dropped on save,
  // which is the same "looked like it worked" failure the band is meant to end.
  const [durationErrors, setDurationErrors] = useStateA({});
  const setDurationError = (key) => (err) =>
    setDurationErrors((prev) => {
      if ((prev[key] || null) === err) return prev;
      const next = { ...prev };
      if (err) next[key] = err; else delete next[key];
      return next;
    });
  const hasDurationError = Object.keys(durationErrors).length > 0;

  // Render one per-phase m:ss duration field. Pool and playoff differ only in
  // label/hint/field-key, so share the markup to keep the two hint strings in
  // step. The label owns the field via htmlFor/id, so a screen reader announces
  // "Pool match duration" rather than a bare "minutes".
  const durationField = (label, secKey, hint) => (
    <div className="field">
      <label className="field__label" htmlFor={`duration-${secKey}`}>{label}</label>
      <DurationInput
        id={`duration-${secKey}`}
        describedBy={`duration-${secKey}-hint`}
        seconds={local[secKey]}
        onChange={updateDurationSeconds(secKey)}
        onValidity={setDurationError(secKey)}
      />
      <div className="field__hint" id={`duration-${secKey}-hint`}>{hint}</div>
    </div>
  );

  const toggleCourt = (cc) => {
    // Compute from localRef.current (kept authoritative by update) rather than
    // the render-closure `local.courts`: rapid toggles fired before React
    // commits a re-render would otherwise both read the same stale snapshot and
    // drop a toggle.
    const cur = localRef.current.courts || [];
    const nextCourts = cur.includes(cc) ? cur.filter((x) => x !== cc) : [...cur, cc].sort();
    // The last pill CAN be turned off. This used to drop the update instead,
    // which enforced "at least one shiaijo" by making the click do nothing --
    // no pill change, no message, nothing to read. The rule is real, so it is
    // now stated where the operator is looking: courtsErr says it and Save is
    // blocked until they pick one, the same way the create form answers the
    // same action. A silent no-op teaches nothing and reads as a broken button.
    update("courts", nextCourts);
  };

  // draw-ready lock: output-affecting fields: those that reach the Excel
  // generator (pools, courts, format, kind, team size, mirror, numberPrefix,
  // withZekkenName): are disabled while a draw exists. Fields that do NOT
  // affect the generated workbook (name, date, startTime, checkInEnabled,
  // naginata) remain editable. Discard the draw from the competition header to
  // unlock everything.
  const isDrawReady = local.status === "draw-ready";
  // Engi and Naginata are locked once the competition has started (pools, playoffs,
  // completed, or any future status beyond draw-ready): flipping engi mid-tournament
  // changes the scoring paradigm; flipping naginata affects the bronze match.
  const isStarted = !!(local.status && local.status !== "setup" && local.status !== "draw-ready");

  // Shiaijo-count rule (shiaijoCountErrorFor, mirrored from
  // engine.ValidateCompetitionShiaijoCount): a competition whose draw builds a
  // knockout bracket runs on 1, 2, 4, 8 or 16 shiaijo. League and Swiss are out
  // of scope, which is why the format is passed in rather than gated here:
  // their shiaijo run in parallel with no bracket blocks to merge, and the
  // league hint right under these pills recommends floor(players/2)-1 courts,
  // which is rarely a power of two.
  //
  // Four distinct states, deliberately kept apart:
  //
  //   courtsHint     the STANDING teaching hint: which counts this operator
  //                  may pick and why, shown whether or not the current
  //                  selection is valid. Venue-aware, so a 3-shiaijo
  //                  tournament reads "can use 1 or 2 (this tournament has
  //                  3)" instead of learning the rule from a refusal.
  //   courtsErr      the CURRENT selection is invalid, drives the red hint
  //                  under the court pills.
  //   savedCourtsErr the allocation ON DISK is invalid, drives the persistent
  //                  warning banner. A competition saved before this rule
  //                  existed (or one that inherited a 3-shiaijo venue court
  //                  list) lands here and keeps running; it just cannot
  //                  generate a draw until the operator fixes it.
  //   courtsChanged  the operator is actually reassigning shiaijo.
  //
  // Save is blocked only for `courtsErr && courtsChanged`. The server's own
  // gate is broader -- it revalidates the shiaijo count when the courts change
  // OR the format does -- but this screen has no format control at all
  // (local.format is read everywhere and staged nowhere), so the format half is
  // unreachable from here and a courtsChanged-only block covers every edit this
  // form can actually submit. If a format editor is ever added here, this must
  // gain `|| local.format !== c.format` with it, or switching a league on 3
  // shiaijo to mixed offers a live Save and takes a 400.
  //
  // Neither state means "the stored value is invalid" -- savedCourtsErr covers
  // that and deliberately does NOT block, because it would lock the operator
  // out of every unrelated edit on this screen (name, date, durations,
  // check-in), which is the one outcome this rule must not cause.
  // The two read an empty list DIFFERENTLY, and must: on screen it is an
  // operator mid-edit, on disk it is inheritance. Each helper owns its own half
  // of that (shiaijoPickerError takes `authored`, which courtsChanged supplies;
  // resolvedShiaijoCountError resolves before judging).
  const savedCourts = c.courts || [];
  const courtsChanged = (local.courts || []).join(",") !== savedCourts.join(",");
  const courtsErr = window.shiaijoPickerError(local.format, local.courts, courtsChanged, (tournament.courts || []).length);
  const savedCourtsErr = window.resolvedShiaijoCountError(c.format, savedCourts, tournament.courts);
  const blockingCourtsErr = !!courtsErr && courtsChanged;
  // The mechanism sentence is dropped from the standing hint while the red
  // error is on screen: the error states it one line above, and printing it
  // twice buries the part that changes (which counts to pick).
  const courtsHint = window.shiaijoCountHintFor(local.format, (tournament.courts || []).length, !courtsErr);

  // Shiaijo the competition holds that the tournament no longer has (the
  // operator shrank the venue's court count under a competition already
  // assigned the removed court). Drives the flagged pill + hint below;
  // courtPillOptions is what keeps the rendered selection equal to
  // local.courts, so the screen can never show one allocation and save
  // another. NOT a Save blocker, for the same reason savedCourtsErr isn't:
  // the stored value is already in this state and every unrelated edit on
  // this screen must stay possible. The server refuses the tournament change
  // that would create this, and refuses to draw while it stands.
  const courtOptions = window.courtPillOptions(tournament.courts, local.courts);
  const orphanedCourtsErr = window.orphanedShiaijoError(tournament.courts, local.courts);

  // The single disabled-condition for BOTH Save buttons (header and footer).
  // Derived once on purpose: the footer button used to repeat the expression
  // and had drifted, omitting hasDurationError AND blockingCourtsErr, so with
  // an invalid shiaijo count the header button greyed out while the footer
  // one stayed live, fired the PUT and took a 400. Anything that should block
  // saving belongs here, never at a call site.
  const saveDisabled = !isDirty || saving || hasDurationError || blockingCourtsErr;

  // The blocking message and its precedence, shared by the header chip and the
  // footer for the same reason saveDisabled is: the footer used to restate the
  // chain as three spans with cascading negations, which is the shape that
  // silently keeps rendering "● Unsaved changes" when a fourth blocker is added
  // and one negation is missed.
  //
  // ONLY the blocking half is shared. The header additionally reports the
  // transient "Saving…" and "✓ Saved at" states; the footer deliberately shows
  // neither, so those stay where they are rather than being forced into a
  // common value that would change what the footer renders.
  const saveBlockMessage = saveErr ? `⚠ ${saveErr}`
    : hasDurationError ? "⚠ Fix match duration"
      // "allocation", not "count": courtsErr also covers a selection with
      // nothing in it, which is not a counting problem.
      : blockingCourtsErr ? "⚠ Fix shiaijo allocation" : "";
  const saveBlocked = !!saveBlockMessage;

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{
            fontSize: 12.5,
            padding: "4px 8px",
            borderRadius: 4,
            background: saveBlocked ? "var(--red-soft)" : isDirty ? "var(--warn-soft)" : lastSaved ? "var(--accent-soft)" : "transparent",
            color: saveBlocked ? "var(--red)" : isDirty ? "var(--warn-ink)" : "var(--accent)",
            fontWeight: 600,
            transition: "all 300ms"
          }}>
            {saving && !saveErr ? "Saving…" : saveBlocked ? saveBlockMessage : isDirty ? "● Unsaved changes" : lastSaved ? `✓ Saved at ${lastSaved}` : ""}
          </div>
          <button type="button" className="btn btn--primary" onClick={saveNow} disabled={saveDisabled}>
            {saving ? "Saving…" : "Save changes"}
          </button>
        </div>
      </div>
      {/* Persistent warning for an allocation ALREADY on disk that the draw
          cannot halve down. Not a blocker: the competition keeps running and
          the rest of this screen stays editable (the server validates courts
          on write only), which is what lets a record written before this rule
          existed keep loading and rendering. It stays visible until the
          allocation is changed, because the draw cannot be generated while it
          stands. Suppressed once the operator has staged a fix, where the
          pills speak for themselves. */}
      {savedCourtsErr && !courtsChanged && (
        <div className="alert alert--warn" style={{ marginBottom: 12 }} data-testid="shiaijo-count-banner">
          {/* The headline states only that the draw is blocked. Which count, why
              it cannot be split and what to pick instead all live in
              savedCourtsErr, so the verdict is not phrased a second time here --
              and an INHERITED allocation, whose own list is empty, is not
              announced as "assigned 0 shiaijo". */}
          <div style={{ fontWeight: 600, marginBottom: 6 }}>
            ⚠ This competition's shiaijo allocation blocks the draw.
          </div>
          <div>
            {savedCourtsErr} Change the assignment below and save; the draw cannot be generated until you do.
          </div>
        </div>
      )}
      {clashWarnings && clashWarnings.length > 0 && (
        <div className="alert alert--warn" style={{ marginBottom: 12 }} data-testid="clash-banner">
          <div style={{ fontWeight: 600, marginBottom: 6 }}>
            ⚠ Court clash. This competition overlaps {clashWarnings.length === 1 ? "another" : `${clashWarnings.length} other`} on a shared shiaijo:
          </div>
          <ul style={{ margin: "0 0 8px 16px", padding: 0 }}>
            {clashWarnings.map((w) => (
              <li key={`${w.otherCompName}|${w.overlapStart}|${w.overlapEnd}`}>
                <strong>{w.otherCompName}</strong>. Shiaijo {(w.sharedCourts || []).join(", ")} · {w.overlapStart}–{w.overlapEnd}
              </li>
            ))}
          </ul>
          <div style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 8 }}>
            The change was saved. Two competitions can't run on the same court at the same time. Adjust the start time or assigned courts here or on the other competition.
          </div>
          <button type="button" className="btn btn--sm" onClick={() => { setClashWarnings(null); if (onBack) onBack(); }}>Dismiss & return to dashboard</button>
        </div>
      )}
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
              // real (stale or empty) value: the operator would then "save"
              // a value the UI never showed. Two cases need an explicit
              // matching option so the displayed value always tracks state:
              //   - empty date (legacy/imported competition with no day set):
              //     render a disabled placeholder "(select a day)" so the
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
                    <option value="" disabled>(select a day)</option>
                  )}
                  {outOfRange && (
                    <option key={local.date} value={local.date}>{local.date} (outside tournament days)</option>
                  )}
                  {days.map((d, i) => (
                    <option key={d} value={d}>Day {i + 1}: {d}</option>
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
      {local.kind === "team" && (
        <div className="field">
          <label className="field__label">Team match format</label>
          {/* draw-ready + started lock: teamMatchType selects fixed vs kachinuki
              bout sequencing; changing it after draw-ready would desync the match
              structure from config, and flipping it on a STARTED comp would
              desync recorded bouts from the scoring paradigm (server 409s). */}
          <div className="radio-group">
            <button
              className={`radio-pill ${local.teamMatchType !== "kachinuki" ? "is-active" : ""}`}
              type="button"
              onClick={() => update("teamMatchType", "fixed")}
              disabled={isDrawReady || isStarted}
            >Regular</button>
            <button
              className={`radio-pill ${local.teamMatchType === "kachinuki" ? "is-active" : ""}`}
              type="button"
              onClick={() => update("teamMatchType", "kachinuki")}
              disabled={isDrawReady || isStarted}
            >Kachinuki (winner stays on)</button>
          </div>
          <div className="field__hint">
            {teamMatchTypeHint(local.teamMatchType === "kachinuki")}{(isDrawReady || isStarted) ? " Locked after draw." : ""}
          </div>
        </div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo (courts)</label>
        {/* draw-ready lock: courts is output-affecting. Discard the draw to reassign. */}
        {isDrawReady && (
          <div className="field__hint" style={{ marginBottom: 6, color: "var(--ink-2)", fontWeight: 500 }}>
            Discard the draw to change pools, courts, or format.
          </div>
        )}
        {/* Pills come from courtPillOptions, not from tournament.courts, so
            the rendered selection always equals local.courts: a shiaijo this
            competition still holds after the tournament dropped it gets its
            own flagged pill instead of vanishing from the screen while
            staying on disk. Deselecting it is the fix, so the pill stays
            clickable. */}
        <div className="radio-group">
          {courtOptions.map(({ court: cc, selected, inTournament }) => (
            <button
              key={cc}
              className={`radio-pill ${selected ? "is-active" : ""}`}
              type="button"
              onClick={() => toggleCourt(cc)}
              disabled={isDrawReady}
              style={inTournament ? undefined : { borderColor: "var(--red)", color: selected ? undefined : "var(--red)" }}
              data-testid={inTournament ? undefined : `orphan-court-${cc}`}
            >Shiaijo (court) {cc}{inTournament ? "" : " (not in tournament)"}</button>
          ))}
        </div>
        <window.FieldError testId="orphan-shiaijo-hint">{orphanedCourtsErr}</window.FieldError>
        {/* Shiaijo-count rule, shown with the other court hints below so the
            operator reads cap, suggestion and count rule in one place. The
            error is rendered for ANY invalid selection, staged or already
            saved, so the field never looks fine while the draw is blocked;
            Save itself is gated on blockingCourtsErr (a CHANGE to an invalid
            count), not on this hint. The hint is STANDING: it teaches the rule
            before it can block anything, so the operator meets "you may pick 1
            or 2 here" rather than learning it from a refusal, and is shown for
            every valid AND invalid selection; only league/Swiss (out of scope)
            drop it. Same component as the create form, which has to render
            both notes identically. */}
        <window.ShiaijoCountNotes error={courtsErr} hint={courtsHint} />
        {(local.format === "league" || local.poolFormat === "partial") ? (() => {
          const playerCount = (c.players || []).length;
          const ct = (n) => n === 1 ? "1 court" : `${n} courts`;
          const pt = (n) => n === 1 ? "1 player" : `${n} players`;
          if (playerCount < 2) return <div className="field__hint">Suggested: up to {ct(Math.max(1, Math.floor(playerCount / 2) - 1))} for {pt(playerCount)}</div>;
          // `local.courts` is seeded from the competition record, which Go
          // ships as `courts: null` when it was stored without a courts key.
          // Every other read on this screen already defaults it; this one did
          // not, so the league/partial-pool branch alone crashed the settings
          // tab - the very screen an operator opens to assign the shiaijo.
          const numCourts = (local.courts || []).length;
          const hardCap = Math.max(1, Math.floor(playerCount / 2));
          const suggestedCourts = Math.max(1, hardCap - 1);
          if (numCourts > hardCap) return <window.FieldError>Too many courts. {hardCap} max for {pt(playerCount)} (suggested: {suggestedCourts})</window.FieldError>;
          if (numCourts === hardCap && hardCap > suggestedCourts) return <div className="field__hint field__hint--warn">No rest between fights at {numCourts} courts. Consider {ct(suggestedCourts)} for {pt(playerCount)}</div>;
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
              <button
                className={`radio-pill ${local.poolSizeMode === "max" ? "is-active" : ""}`}
                type="button"
                onClick={() => {
                  // bc-qual LP-5a: leaving minimum-players-per-pool sizing
                  // hides the "Knockout qualifiers" radio below; reset its
                  // value to standard so it can't persist as a stale
                  // non-standard selection under a mode it's no longer
                  // valid for (same reset admin_setup.jsx's create form
                  // applies on the same transition).
                  update("poolSizeMode", "max");
                  update("extraQualifiers", resetExtraQualifiersOnPoolModeChange("max", local.extraQualifiers));
                }}
                disabled={isDrawReady}
              >maximum</button>
              <button className={`radio-pill ${local.poolSizeMode === "min" ? "is-active" : ""}`} type="button" onClick={() => update("poolSizeMode", "min")} disabled={isDrawReady}>minimum</button>
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
            <div className="field">
              <label className="field__label">Winners per pool</label>
              <input
                className="input"
                type="number"
                min="1"
                value={Number.isFinite(local.poolWinners) ? local.poolWinners : ""}
                onChange={(e) => updateNumber("poolWinners", e.target.value, 1)}
                disabled={isDrawReady || winnersInputDisabled(local.extraQualifiers)}
              />
              {/* bc-qual LP-5a: same coupling hint as the create form; */}
              {/* draw-ready already has its own standing note above the */}
              {/* "Pool size is a" pills, so it isn't repeated per-field here. */}
              {!isDrawReady && winnersInputDisabled(local.extraQualifiers) && (
                <div className="field__hint">Set to 1 by the knockout qualifiers setting below.</div>
              )}
            </div>
          </div>

          {/* Knockout qualifiers (bc-qual LP-5a): only meaningful under
              minimum-players-per-pool sizing (poolSizeMode === "min"); see
              extraQualifiersRadioVisible. Same three options, same copy,
              and the same draw-ready lock as poolSizeMode/poolSize/
              poolWinners immediately above (this field is in the
              server-side outputAffectingChanged set alongside them).
              Federation-neutral copy per operator ruling: no federation
              names anywhere in this UI. */}
          {extraQualifiersRadioVisible(local.format, local.poolSizeMode) && (() => {
            // The EFFECTIVE draw roster (c.players, not local.players,
            // masked by the same check-in opt-in rule the engine applies):
            // settings pool-config edits don't change the roster, c is the
            // server-confirmed competition, and generate-draw both counts
            // entrants and drops absent players' seeds AFTER check-in
            // filtering -- so a preview computed off the raw list promises
            // a cut the draw will not make whenever a seeded participant is
            // a no-show.
            const drawPlayers = effectiveDrawPlayers(c.players, c.checkInEnabled);
            // Seed RANKS, not a count: fill-bracket's supply rule only
            // credits a rank low enough to land its own pool, so that
            // mode's preview genuinely changes as the operator seeds.
            // seededRanks (admin_helpers) is the one owner of "which ranks
            // has this roster actually got" -- the same reader the seeding
            // blocker validates with.
            const preview = computeQualifierPreview(drawPlayers.length, local.poolSize, local.poolWinners, seededRanks(drawPlayers));
            const activeShape = local.extraQualifiers === EXTRA_QUALIFIERS_LARGER_POOLS
              ? preview.largerPools
              : local.extraQualifiers === EXTRA_QUALIFIERS_FILL_BRACKET
                ? preview.fillBracket
                : preview.standard;
            const previewLine = formatQualifierPreviewLine(activeShape);
            return (
              <div className="field">
                <label className="field__label">Knockout qualifiers</label>
                <div className="radio-group">
                  <button
                    className={`radio-pill ${!local.extraQualifiers ? "is-active" : ""}`}
                    type="button"
                    onClick={() => update("extraQualifiers", EXTRA_QUALIFIERS_STANDARD)}
                    disabled={isDrawReady}
                  >{extraQualifiersLabel(EXTRA_QUALIFIERS_STANDARD)}</button>
                  <button
                    className={`radio-pill ${local.extraQualifiers === EXTRA_QUALIFIERS_LARGER_POOLS ? "is-active" : ""}`}
                    type="button"
                    onClick={() => {
                      update("extraQualifiers", EXTRA_QUALIFIERS_LARGER_POOLS);
                      update("poolWinners", winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_LARGER_POOLS, local.poolWinners));
                    }}
                    disabled={isDrawReady}
                  >{extraQualifiersLabel(EXTRA_QUALIFIERS_LARGER_POOLS)}</button>
                  <button
                    className={`radio-pill ${local.extraQualifiers === EXTRA_QUALIFIERS_FILL_BRACKET ? "is-active" : ""}`}
                    type="button"
                    onClick={() => {
                      update("extraQualifiers", EXTRA_QUALIFIERS_FILL_BRACKET);
                      update("poolWinners", winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_FILL_BRACKET, local.poolWinners));
                    }}
                    disabled={isDrawReady}
                  >{extraQualifiersLabel(EXTRA_QUALIFIERS_FILL_BRACKET)}</button>
                </div>
                <div className="field__hint">
                  {extraQualifiersHint(local.extraQualifiers, local.poolSize)}
                </div>
                <div className="field__hint" data-testid="qualifier-preview-line">
                  {previewLine || "Preview appears once this competition has participants."}
                </div>
              </div>
            );
          })()}
        </>
      )}
      {/* FR-052..FR-054 / T047: per-phase match-duration inputs. */}
      {/* Render rules: */}
      {(local.format === "mixed" || local.format === "league" || local.format === "playoffs" || local.format === "swiss") && (
        <div className="row">
          {(local.format === "mixed" || local.format === "league" || local.format === "swiss") &&
            durationField(
              local.format === "swiss" ? "Round match duration" : "Pool match duration",
              "poolMatchDurationSeconds",
              `Estimated time per ${local.format === "swiss" ? "Swiss-round" : "pool"} match, as m:ss (e.g. 2:30).`
            )}
          {(local.format === "playoffs" || local.format === "mixed") &&
            durationField(
              "Playoff match duration",
              "playoffMatchDurationSeconds",
              "Estimated time per playoff/knockout match, as m:ss (e.g. 2:30)."
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
            // mp-gmcg: kachinuki has a variable bout count, so the server
            // returns a best/average/worst range (it knows the competition's
            // teamMatchType; no extra param needed here). The headline
            // totalDurationMinutes is the AVERAGE scenario; when the range
            // collapses (fixed/individual) the single total renders as before.
            // EstimateHeadline (admin_schedule_utils.jsx) owns that decision
            // for both this panel and the Overview footer.
            if (!total) {
              return <div style={{ fontSize: 12, color: "var(--ink-3, #6b7280)" }}>No estimate yet. Add participants and configure duration to see a projection.</div>;
            }
            return (
              <div style={{ fontSize: 12.5, color: "var(--ink-1, #111827)" }}>
                <EstimateHeadline estimate={compEstimate} total={total} format={formatCompMinutes} testId="comp-est-range" />
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
        <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => update("roundRobin", e.target.checked)} disabled={isDrawReady} /> Round-robin in pools</label>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => update("withZekkenName", e.target.checked)} disabled={isDrawReady || local.kind === "team"} /> Use Zekken display name</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : "When enabled, participant CSV uses three columns: Name, Zekken, Dojo."}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.naginata} onChange={(e) => update("naginata", e.target.checked)} disabled={isDrawReady || isStarted} /> Naginata competition</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Adds the Sune (S) ippon button to the score editor. Use for Naginata divisions.{(isDrawReady || isStarted) ? " Locked after draw." : ""}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.engi} onChange={(e) => update("engi", e.target.checked)} disabled={isDrawReady || isStarted || local.kind === "team"} /> Engi (kata competition)</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : `Flag-count scoring for Engi-Kyogi pairs. Enter each pair as one participant: Name 1 - Name 2, Dojo.${(isDrawReady || isStarted) ? " Locked after draw." : ""}`}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.checkInEnabled} onChange={(e) => update("checkInEnabled", e.target.checked)} /> Check-in tracking</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Show check-in column and counter. Disable for competitions that don't need attendance tracking.</div>
        </div>
      </div>
      {/* League standings settings. The joint-3rd convention applies to ALL
          leagues (team + individual); the "Break ties for top" band is a
          team-league tie-breaker knob and stays team-only. */}
      {local.format === "league" && (
        <div style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 8, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
          {(local.teamSize > 0 || local.kind === "team") && (
            <div className="field">
              <label className="field__label">Break ties for top</label>
              <div className="radio-group">
                <button
                  className={`radio-pill ${(local.leagueTiebreakTopN || 0) === 0 || local.leagueTiebreakTopN === 3 ? "is-active" : ""}`}
                  type="button"
                  disabled={isDrawReady || isStarted}
                  onClick={() => update("leagueTiebreakTopN", 3)}
                >Top 3</button>
                <button
                  className={`radio-pill ${local.leagueTiebreakTopN === 4 ? "is-active" : ""}`}
                  type="button"
                  disabled={isDrawReady || isStarted}
                  onClick={() => update("leagueTiebreakTopN", 4)}
                >Top 4</button>
              </div>
              <div className="field__hint">Tied teams within this finishing band require an operator-run tie-breaker before standings are finalised.{(isDrawReady || isStarted) ? " Locked after draw." : ""}</div>
            </div>
          )}
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <label className="checkbox">
              <input type="checkbox" checked={!!local.leagueTwoThirdPlaces} disabled={isDrawReady || isStarted} onChange={(e) => update("leagueTwoThirdPlaces", e.target.checked)} />
              {" "}Award two joint 3rd places
            </label>
            <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>When enabled, competitors genuinely tied for 3rd share bronze (standard kendo convention). Leave off for naginata, which awards a single 3rd place.{(isDrawReady || isStarted) ? " Locked after draw." : ""}</div>
          </div>
        </div>
      )}
      {/* Repeat Save at the foot of the long settings form so the operator
          doesn't have to scroll back to the header after editing. Same
          onClick, the SAME `saveDisabled` value and the SAME `saveBlockMessage`
          as the header: the two cannot drift because neither the block
          condition nor the blocking message is restated here. The transient
          "Saving…"/"✓ Saved at" states are the header's alone, by choice. */}
      <div style={{ marginTop: 20, display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 10 }}>
        {saveBlocked && <span style={{ fontSize: 12.5, color: "var(--red)", fontWeight: 600 }}>{saveBlockMessage}</span>}
        {!saveBlocked && isDirty && !saving && <span style={{ fontSize: 12.5, color: "var(--warn)", fontWeight: 600 }}>● Unsaved changes</span>}
        <button type="button" className="btn btn--primary" onClick={saveNow} disabled={saveDisabled}>
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
                    // a competition object. Don't call onUpdate: that would
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
