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
// bc-symm Phase 3: option lists, copy and coupling rules shared with the
// competition CREATE form (admin_setup.jsx's AdminCreateCompetition), so the
// two screens cannot drift apart. See competition_shape.jsx's header for the
// full split rationale. shapeConfigForSave, resolveTeamSize and
// kindChangeBlockedReason are settings-only imports: the create form builds
// a payload from scratch rather than re-shaping a stored one, and it has no
// roster that could ever block a kind change.
import {
  LABEL_KIND, KIND_OPTIONS, LABEL_FORMAT, FORMAT_OPTIONS, formatHint,
  LABEL_POOL_FORMAT, POOL_FORMAT_OPTIONS, poolFormatVisible,
  LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS, swissRoundsVisible,
  LABEL_ROUND_ROBIN, roundRobinVisible,
  LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK, LEAGUE_TIEBREAK_OPTIONS, leagueTiebreakVisible,
  LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,
  poolDurationLabel, poolDurationHint, poolDurationVisible, playoffDurationVisible,
  LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES,
  LABEL_POOL_SIZE, LABEL_POOL_WINNERS, LABEL_EXTRA_QUALIFIERS,
  LABEL_TEAM_SIZE, LABEL_TEAM_MATCH_TYPE, TEAM_MATCH_TYPE_OPTIONS,
  LABEL_ZEKKEN, LABEL_ENGI,
  teamFieldsVisible, zekkenApplies, engiApplies,
  shapeConfigForSave, resolveTeamSize, kindChangeBlockedReason,
  FORMAT_LEAGUE, FORMAT_MIXED, POOL_FORMAT_PARTIAL,
  MIN_TEAM_SIZE, poolSettingsError, pendingConfigClears, swissSettingsError,
  CLEARED_FIELD_LABELS, optionLabel, poolFormatHint, resolvePoolFormat,
  resolvePoolSizeMode, POOL_SIZE_MODE_MAX, POOL_SIZE_MODE_MIN,
  leagueTiebreakActive, twoThirdPlacesVisible,
  MIN_POOL_SIZE, MIN_POOL_WINNERS, MIN_SWISS_ROUNDS,
  HINT_ZEKKEN, HINT_ENGI, HINT_KIND_ONLY_INDIVIDUAL,
  LABEL_NAGINATA, HINT_NAGINATA,
  LABEL_CHECK_IN, HINT_CHECK_IN,
  LABEL_NUMBER_PREFIX, HINT_NUMBER_PREFIX,
} from './competition_shape.jsx';
import { seededRanks } from './admin_helpers.jsx';

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA, useMemo: useMemoA } = React;

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

// Human-readable rendering of a cleared field's CURRENT (about-to-be-lost)
// value. Booleans read "on" (pendingConfigClears only ever reports a
// truthy boolean -- see its "meaningful" filter -- so "off" is never
// reached here). teamMatchType and extraQualifiers resolve through their
// option lists rather than branching per field; everything else prints as-is.
function formatClearedValue(key, value) {
  if (typeof value === "boolean") return "on";
  // Resolve an enum through its own option list rather than branching per
  // field: teamMatchType went through TEAM_MATCH_TYPE_OPTIONS while
  // extraQualifiers -- also in CLEARED_FIELD_LABELS -- fell through to
  // String(value) and printed its raw wire value.
  if (key === "teamMatchType") {
    return optionLabel(TEAM_MATCH_TYPE_OPTIONS, value) || TEAM_MATCH_TYPE_OPTIONS[0].label;
  }
  if (key === "extraQualifiers") return extraQualifiersLabel(value);
  return String(value);
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

  // Knockout-qualifiers preview (bc-qual): computed off the EFFECTIVE draw
  // roster -- server-confirmed c.players (settings edits don't change the
  // roster) masked by the same check-in opt-in rule the engine applies
  // (effectiveDrawPlayers), read against the PENDING local.checkInEnabled so
  // the preview tracks the form exactly like its sibling pool-config inputs;
  // generate-draw both counts entrants and drops absent players' seeds AFTER
  // check-in filtering, so a preview computed off the raw list promises a
  // cut the draw will not make whenever a seeded participant is a no-show.
  // Seed RANKS, not a count: fill-bracket's supply rule only credits a rank
  // low enough to land its own pool; seededRanks (admin_helpers) is the one
  // owner of "which ranks has this roster actually got" -- the same reader
  // the seeding blocker validates with. Memoized because this walks the
  // roster and runs the full formation scan, and AdminSettings re-renders on
  // every keystroke of any settings field.
  const qualifierPreview = useMemoA(() => {
    const drawPlayers = effectiveDrawPlayers(c.players, local.checkInEnabled);
    return computeQualifierPreview(drawPlayers.length, local.poolSize, local.poolWinners, seededRanks(drawPlayers));
  }, [c.players, local.checkInEnabled, local.poolSize, local.poolWinners]);

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
    // layer can show "" instead of "0"). If the user clears swissRounds and
    // then clicks Save, the cleared swissRounds is still NaN in the edited
    // overlay. JSON.stringify({n: NaN}) produces '{"n":null}'. Go binds
    // JSON null to int as 0: backend transform writes 0 to disk,
    // clobbering the prior good value. Falling back to `latestC.<field>`
    // when the effective value isn't a usable positive integer preserves
    // the disk value until the user types a valid replacement.
    const safeInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 1 ? v : fallback;
    // safeNonNegInt is the >=0 sibling, for the per-phase duration fields and
    // for poolSize/poolWinners. Same NaN/fractional/negative guards as
    // safeInt; the only difference is the lower bound, and the bound is the
    // whole point: each of these fields has a legitimate 0 that safeInt's
    // >=1 floor would read as invalid input and replace with the stale
    // stored value.
    //
    // T047: on a duration, 0 means "unset, use the scheduler default", so
    // clearing one has to round-trip as 0 or it cannot be reset.
    // poolSize/poolWinners (below, read from `shaped`) joined it for the
    // same shape of reason: normalizeConfigForFormat's 0 going out of
    // "mixed" is a DECISION (no pool phase to size), not a cleared input,
    // and re-sending the stale mixed-only value would silently defeat the
    // clear.
    //
    // teamSize is NOT one of its callers, though it was until the review
    // round, and the reason it left is worth keeping: 0 is teamSize's
    // required value for an INDIVIDUAL kind, so it needed the >=0 bound for
    // exactly the reason above -- but the guard was reading
    // normalizeConfigForKind's OUTPUT, which is always a finite integer, so
    // it never fired at all. It is resolved against the stored value BEFORE
    // shaping instead (resolveTeamSize, below).
    //
    // The NaN fallback to latestC.<field> still protects a field the
    // operator never touched from being zeroed by an unrelated save.
    const safeNonNegInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 0 ? v : fallback;

    // The ONE place normalization runs now. It used to run on the
    // Format/Kind pill's onClick itself (staging
    // normalizeConfigForFormat/normalizeConfigForKind's result straight
    // into `local`), which meant the operator's real values were
    // overwritten before Save was ever clicked: reproduced on a stored
    // mixed competition (poolSize: 4, poolWinners: 2) by tapping "Knockout
    // only" (stages poolSize/poolWinners: 0) and then tapping "Pools +
    // Knockout" to go straight back -- a no-op for those fields going back
    // INTO mixed, per normalizeConfigForFormat's own comment -- so two taps
    // that cancelled out on `format` left poolSize/poolWinners at 0 with no
    // operator action able to recover them, and Save blocked by
    // poolSettingsError. Operator ruling: a config change must never
    // quietly overwrite or delete the operator's data -- it must surface
    // what will happen and let the operator decide. Normalizing only here,
    // at the payload boundary, means `local` (and therefore every input on
    // screen) always shows exactly what the operator staged, right up until
    // the instant Save actually sends it; pendingConfigClears
    // (competition_shape.jsx), rendered below the Format/Kind controls,
    // tells the operator what THIS save is about to clear before they
    // commit to it. shapeConfigForSave is also what SCOPES the two
    // normalizers to a staged format/kind change -- see its own comment for
    // the draw-ready 409 an unscoped run caused on a stored team
    // competition carrying withZekkenName.
    // teamSize is resolved against the stored value BEFORE shaping, never
    // after. normalizeConfigForKind rewrites it on BOTH branches (0 going
    // individual, DEFAULT_TEAM_SIZE for a team under the floor), so
    // shaped.teamSize is always a finite integer and the
    // `safeNonNegInt(shaped.teamSize, latestC.teamSize)` this line used to
    // pair with could never fire: clearing the Team size input on a stored 3
    // silently saved 5, and typing a 1 or a 0 did the same, while the input's
    // own comment promised a fall back to the last-saved value.
    // resolveTeamSize (competition_shape.jsx) is that fall back, and the
    // pendingClears notice below calls it too so the warning can never name a
    // value the save will not send.
    const shaped = shapeConfigForSave(latestC, {
      ...effective,
      teamSize: resolveTeamSize(effective.teamSize, latestC.teamSize),
    });
    const finalNext = {
      id: latestC.id,
      name: trimmedName,
      date: dateNorm,
      startTime: effective.startTime,
      // poolSize/poolWinners read from `shaped`, not `effective`:
      // normalizeConfigForFormat is the function that zeroes them on the way
      // out of "mixed", and shaped is where that now runs (see the `shaped`
      // comment above). safeNonNegInt, not safeInt, so the deliberate 0
      // survives -- see safeNonNegInt's own comment.
      poolSize: safeNonNegInt(shaped.poolSize, latestC.poolSize),
      poolWinners: safeNonNegInt(shaped.poolWinners, latestC.poolWinners),
      poolSizeMode: effective.poolSizeMode,
      courts: effective.courts,
      roundRobin: effective.roundRobin,
      // withZekkenName is normalizeConfigForKind's field (forced false going
      // team; see that function's own comment for why), so it reads from
      // `shaped` for the same reason poolSize/poolWinners do above.
      withZekkenName: shaped.withZekkenName,
      // No safeNonNegInt here: see the resolveTeamSize note on `shaped` above
      // -- the guard belongs before the normalizer, not after it.
      teamSize: shaped.teamSize,
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
      // those statuses. Reads from `shaped`: normalizeConfigForKind is the
      // function that forces this false going team (the checkbox is
      // disabled for team, so the operator cannot clear it themselves), and
      // shaped is where that now runs.
      engi: !!shaped.engi,
      checkInEnabled: !!effective.checkInEnabled,
      // Phase 3b (mp-8rc9): league tie-breaker config. Only meaningful for
      // team-league competitions; safe to include for all formats because
      // the backend's PUT allowlist ignores unknown fields.
      leagueTiebreakTopN: safeInt(effective.leagueTiebreakTopN, latestC.leagueTiebreakTopN || 0),
      leagueTwoThirdPlaces: !!effective.leagueTwoThirdPlaces,
      // teamMatchType is edited via the Team match format pills above; the
      // merge is a full replace: omitting it would clobber a kachinuki
      // competition's value to "" (fixed) on any save. Round-trip it like
      // `mirror` above to preserve the stored value. Reads from `shaped`:
      // normalizeConfigForKind stages "fixed" (never "") going individual --
      // see that function's own comment for why "" would be re-filled from
      // latestC.teamMatchType here and the rejected kachinuki would come
      // straight back.
      teamMatchType: shaped.teamMatchType || latestC.teamMatchType || "",
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
      //
      // Reads from `shaped`, not `effective`: normalizeConfigForFormat is the
      // function that forces this to "" once the format leaves "mixed", and
      // shaped is where that now runs (see the `shaped` comment above). The
      // "no `|| latestC.extraQualifiers` fallback" rule above still holds --
      // shaped already carries the stored value for a format that didn't
      // change, exactly as effective did.
      extraQualifiers: shaped.extraQualifiers || "",
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

  // The Format and Kind pills below call `update("format", ...)` /
  // `update("kind", ...)` directly -- staging ONLY the field itself, never
  // re-normalizing on the tap. See normalizeConfigForFormat's and
  // normalizeConfigForKind's own doc comments (competition_shape.jsx) for
  // why that matters: this file used to run normalization straight from
  // the pill's onClick and it quietly destroyed operator data on a
  // round-trip flip. Do not reintroduce a normalizing wrapper here.

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

  // The draw-lock condition and its copy, named once. Both were repeated
  // across this component (16 spellings of the condition, 8 of the note),
  // and three of the note's occurrences had to be hand-copied correctly
  // when they were added -- the ninth is the one that says something
  // slightly different.
  const lockedAfterDraw = isDrawReady || isStarted;
  const lockedNote = lockedAfterDraw ? " Locked after draw." : "";

  // Kind lock: a ROSTER lock, not the isDrawReady/isStarted pair above.
  // Individual and team rosters don't translate (competition_shape.jsx's
  // kindChangeBlockedReason), and the roster exists long before any draw
  // does, so this has to block earlier than every other output-affecting
  // field on this screen. "" (falsy) means unblocked.
  const kindLockReason = kindChangeBlockedReason((c.players || []).length);

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
  // Save is blocked for `courtsErr && (courtsChanged || formatChanged)`. The
  // server's own gate is exactly this OR -- it revalidates the shiaijo count
  // whenever the courts change OR the format does -- and bc-symm Phase 3 gave
  // this screen a Format control (below), so local.format can now genuinely
  // diverge from c.format the same way local.courts already could. courtsErr
  // is computed against local.format (not c.format), so switching a league on
  // 3 shiaijo to mixed WITHOUT touching a single court pill flips courtsErr to
  // non-null on its own; gating on courtsChanged alone would miss exactly that
  // case and offer a live Save that takes a 400 -- the scenario this comment
  // used to warn about before the control existed.
  //
  // Kind needs no equivalent term. The shiaijo-count rule is a function of
  // FORMAT alone (shiaijoPickerError / shiaijoCountErrorFor never take kind),
  // so a kind-only edit can never move courtsErr, whatever else it locks.
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
  const formatChanged = local.format !== c.format;
  const courtsErr = window.shiaijoPickerError(local.format, local.courts, courtsChanged, (tournament.courts || []).length);
  const savedCourtsErr = window.resolvedShiaijoCountError(c.format, savedCourts, tournament.courts);
  const blockingCourtsErr = !!courtsErr && (courtsChanged || formatChanged);

  // Same shape as the courts trio directly above, for the same reproduced
  // failure: normalizePoolConfig (handlers_competition.go) zeroes poolSize/
  // poolWinners on every stored league/playoffs competition, and
  // normalizeConfigForFormat only clears those fields on the way OUT of
  // "mixed" -- flipping a stored playoffs competition (poolSize: 0,
  // poolWinners: 0) back INTO "mixed" here is a no-op for them. So
  // poolSettingsErr is computed against local.format, not c.format: toggling
  // the "Pools + Knockout" pill flips it non-null on its own, with no pool
  // field touched, which is exactly that case.
  //
  // Save is gated on `(formatChanged || poolFieldsChanged)`, not on the bare
  // error, following savedCourtsErr's rule immediately below: a competition
  // that ALREADY stores an invalid combination (hand-edited config.md, or one
  // saved before this guard existed) must stay editable for every unrelated
  // field -- name, date, durations, check-in. Locking the operator out of the
  // whole form because of a pool-size value they never touched this session
  // is the one outcome this must not cause.
  //
  // The concrete failure this closes: before this guard, switching a stored
  // playoffs competition to "mixed" left "Players per pool" showing 0, both
  // Save buttons enabled, and a click took an HTTP 400 whose raw server
  // string ("mixed format requires a pool size of at least 1") reached the
  // operator verbatim -- while the create form already refused the identical
  // combination client-side with the friendlier copy poolSettingsError
  // returns.
  const poolFieldsChanged = local.poolSize !== c.poolSize || local.poolWinners !== c.poolWinners;
  const poolSettingsErr = poolSettingsError(local.format, local.poolSize, local.poolWinners);
  const blockingPoolSettingsErr = !!poolSettingsErr && (formatChanged || poolFieldsChanged);

  // The Swiss twin of the pool gate directly above, and it exists for the
  // same reason and by the same route: the Format editor makes "swiss"
  // reachable for a stored competition that has never had a round count, and
  // swissRounds is 0 on every non-Swiss record. Tapping the "Swiss" pill
  // rendered "Number of Swiss rounds" showing 0 with Save still live, and
  // the PUT took validateSwissConfig's raw server string. The create form
  // has blocked the identical combination client-side since T190; this is
  // the settings side of that guard, over the SAME rule
  // (swissSettingsError, competition_shape.jsx).
  //
  // Change-scoped like the pool gate, and for the same reason: a stored
  // Swiss competition whose round count is already 0 must not have every
  // unrelated edit blocked by a value the operator did not touch.
  const swissFieldsChanged = local.swissRounds !== c.swissRounds;
  const swissSettingsErr = swissSettingsError(local.format, local.swissRounds);
  const blockingSwissSettingsErr = !!swissSettingsErr && (formatChanged || swissFieldsChanged);

  // What THIS save is about to clear, per the operator ruling that a config
  // change must never quietly overwrite or delete the operator's data --
  // it must surface what will happen and let the operator decide.
  // pendingConfigClears (competition_shape.jsx) reports the diff between
  // what is staged and what shapeConfigForSave will actually send, so it
  // returns [] unless a format or kind change is staged; rendered as a
  // WARNING (never a blocker -- see saveDisabled below, which does not
  // include it) directly under the Format/Kind controls that caused it.
  //
  // Staged teamSize is resolved exactly as saveNow resolves it. Without
  // this the notice reads the raw input: type a 1 into Team size while
  // flipping to Team and it announced "Team size (1)" as about to be
  // cleared, when the save neither keeps 1 nor clears it -- it sends the
  // stored value, or the default when there is none.
  const pendingClears = pendingConfigClears(c, {
    ...local,
    teamSize: resolveTeamSize(local.teamSize, c.teamSize),
  });
  // Names the format/kind the operator is switching TO, for the notice's
  // "which does not apply to <x>" clause. Only the sides that actually
  // changed are named, so a kind-only flip doesn't claim a format change
  // that never happened (and vice versa).
  // optionLabel (competition_shape.jsx) is the one option-list lookup in
  // this codebase; these two used to respell its `.find(...)?.label` inline,
  // ten lines from formatClearedValue which already calls it. The `||` tails
  // keep the raw wire value as a last resort, which optionLabel's own ""
  // return does not supply.
  const pendingClearsTarget = [
    formatChanged ? (optionLabel(FORMAT_OPTIONS, local.format) || local.format) : null,
    local.kind !== c.kind ? (optionLabel(KIND_OPTIONS, local.kind) || local.kind) : null,
  ].filter(Boolean).join(" / ");
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
  const saveDisabled = !isDirty || saving || hasDurationError || blockingCourtsErr || blockingPoolSettingsErr || blockingSwissSettingsErr;

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
      : blockingCourtsErr ? "⚠ Fix shiaijo allocation"
        // Unlike the courts arm above, poolSettingsErr already names the
        // field and the exact rule (e.g. "Players per pool must be a whole
        // number ≥ 3."), so it is printed directly rather than behind a
        // fixed short label.
        : blockingPoolSettingsErr ? `⚠ ${poolSettingsErr}`
          // Same reasoning as the pool arm above: swissSettingsError already
          // names the field and the rule, so it prints directly.
          : blockingSwissSettingsErr ? `⚠ ${swissSettingsErr}` : "";
  const saveBlocked = !!saveBlockMessage;

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{
            // 12.5 was a stray off the documented scale (DESIGN.md Typography).
            // This chip is a badge (padding + borderRadius + background, same
            // shape as .badge--*), so it takes the "Buttons, inputs, badges"
            // 13-13.5px role, not the 12px hint role.
            fontSize: 13,
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
      {/* row-3 for the same reason as the create form's identity group: three
          fields in `.row`'s two-column grid wrap 2 + 1 and orphan Start time
          beside an empty cell. Kept identical to admin_setup.jsx so the two
          screens lay the group out the same way, not just order it the same. */}
      <div className="row-3">
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
      <div className="field">
        <label className="field__label">{LABEL_KIND}</label>
        {/* Roster lock (kindLockReason above), NOT just isDrawReady/isStarted:
            a kind flip invalidates the roster itself, which exists long
            before any draw does, so this locks earlier than every other
            output-affecting field below -- Format included, which is why
            Kind sits directly above it: the two are the form's only
            shape-gating controls (bc-symm Group 2). Team size and Team
            match type, which Kind gates via teamFieldsVisible, are grouped
            further down (after Format, Group 3): Kind still has to resolve
            first for their own visibility to be decidable, whatever else
            renders between here and there. */}
        <div className="radio-group">
          {KIND_OPTIONS.map((o) => (
            <button
              key={o.value}
              className={`radio-pill ${local.kind === o.value ? "is-active" : ""}`}
              type="button"
              onClick={() => update("kind", o.value)}
              disabled={lockedAfterDraw || !!kindLockReason}
            >{o.label}</button>
          ))}
        </div>
        <div className="field__hint">
          {kindLockReason}{lockedNote}
        </div>
      </div>
      <div className="field">
        <label className="field__label">{LABEL_FORMAT}</label>
        {/* draw-ready + started lock: format is output-affecting exactly like
            courts/poolSizeMode below -- it picks which shape (bracket, pools
            + bracket, league, Swiss) the draw builds, and every downstream
            control on this screen (courts, pool sizing, per-phase durations,
            Swiss rounds, league tie-breaks) reads local.format to decide
            what it shows. Placed above the shiaijo pills it governs:
            blockingCourtsErr (above) now tracks a staged format change
            alongside a staged courts change, for exactly this reason. */}
        <div className="radio-group">
          {FORMAT_OPTIONS.map((o) => (
            <button
              key={o.value}
              className={`radio-pill ${local.format === o.value ? "is-active" : ""}`}
              type="button"
              onClick={() => update("format", o.value)}
              disabled={lockedAfterDraw}
            >{o.label}</button>
          ))}
        </div>
        <div className="field__hint">
          {formatHint(local.format)}{lockedNote}
        </div>
      </div>
      {/* Data-loss notice for a staged Format/Competition-type change,
          placed immediately under the two controls that can trigger it
          (Competition type above, Format directly above this). A WARNING,
          not a blocker: `field__hint--warn`, not `window.FieldError`'s red
          style, which this file reserves for a combination Save will
          actually refuse (blockingPoolSettingsErr / blockingCourtsErr
          above) -- saveDisabled does NOT include pendingClears, because the
          save these fields describe is valid and intentional, just lossy.
          See pendingClears' own comment for the reproduced round-trip
          failure this exists to surface instead of silently causing. */}
      {pendingClears.length > 0 && (
        <div className="alert alert--warn" style={{ marginBottom: 12 }} data-testid="config-clears-notice">
          <div style={{ fontWeight: 600, marginBottom: 6 }}>
            Saving will clear these settings, which do not apply to {pendingClearsTarget || "the new selection"}:
          </div>
          <ul style={{ margin: "0 0 8px 16px", padding: 0 }}>
            {pendingClears.map(({ key, from }) => (
              <li key={key}>{CLEARED_FIELD_LABELS[key] || key} ({formatClearedValue(key, from)})</li>
            ))}
          </ul>
          <div className="field__hint field__hint--warn" style={{ margin: 0 }}>
            Switch back to keep them.
          </div>
        </div>
      )}
      {teamFieldsVisible(local.kind) && (
        <div className="field">
          <label className="field__label">{LABEL_TEAM_SIZE}</label>
          {/* Cap is MAX_TEAM_SIZE (admin_helpers.jsx). TEAM_POSITIONS in */}
          {/* admin_scoring_modal.jsx is built from the same constant, so */}
          {/* this input can't allow a value the scoring UI doesn't render. */}
          {/* Floor is MIN_TEAM_SIZE (competition_shape.jsx): teamSize == 1 */}
          {/* is rejected unconditionally by ValidateCompetitionTeamSize */}
          {/* (state/models.go), so this field's legal domain starts at 2, */}
          {/* not 1, whenever it's visible at all (team-only, see */}
          {/* teamFieldsVisible above). */}
          {/* Render NaN as "" so clearing the input stays empty instead of */}
          {/* collapsing to "0"; saveNow resolves the staged value through */}
          {/* resolveTeamSize (competition_shape.jsx), so a cleared input -- */}
          {/* or a typed 0 or 1, both below this field's own min -- falls */}
          {/* back to the last-saved teamSize rather than landing on the */}
          {/* backend as a clobbering 0 or as a silent 5. */}
          {/* draw-ready lock: teamSize is output-affecting. */}
          <input
            className="input"
            type="number"
            min={MIN_TEAM_SIZE}
            max={MAX_TEAM_SIZE}
            value={Number.isFinite(local.teamSize) ? local.teamSize : ""}
            onChange={(e) => updateNumber("teamSize", e.target.value, MIN_TEAM_SIZE)}
            disabled={isDrawReady}
          />
        </div>
      )}
      {teamFieldsVisible(local.kind) && (
        <div className="field">
          <label className="field__label">{LABEL_TEAM_MATCH_TYPE}</label>
          {/* draw-ready + started lock: teamMatchType selects fixed vs kachinuki
              bout sequencing; changing it after draw-ready would desync the match
              structure from config, and flipping it on a STARTED comp would
              desync recorded bouts from the scoring paradigm (server 409s). */}
          <div className="radio-group">
            {/* Active check is asymmetric ON PURPOSE, not a plain o.value ===
                local.teamMatchType: local.teamMatchType can be a legacy/
                stored value that is neither exactly "fixed" nor "kachinuki"
                (see normalizeConfigForKind's own comment on the legacy ""
                sentinel), and this field defaults to Regular in that case --
                mirroring ValidateTeamMatchType's own "" == fixed reading.
                Only the "kachinuki" option is checked for an exact match;
                "fixed" is active whenever the stored value is anything else. */}
            {TEAM_MATCH_TYPE_OPTIONS.map((o) => (
              <button
                key={o.value}
                className={`radio-pill ${(o.value === "kachinuki" ? local.teamMatchType === "kachinuki" : local.teamMatchType !== "kachinuki") ? "is-active" : ""}`}
                type="button"
                onClick={() => update("teamMatchType", o.value)}
                disabled={lockedAfterDraw}
              >{o.label}</button>
            ))}
          </div>
          <div className="field__hint">
            {teamMatchTypeHint(local.teamMatchType === "kachinuki")}{lockedNote}
          </div>
        </div>
      )}
      {poolFormatVisible(local.format) && (
        <div className="field">
          <label className="field__label">{LABEL_POOL_FORMAT}</label>
          {/* Same output-affecting lock as Format above: this is
              the shape of the round-robin the draw itself builds. */}
          <div className="radio-group">
            {POOL_FORMAT_OPTIONS.map((o) => (
              <button
                key={o.value}
                className={`radio-pill ${resolvePoolFormat(local.poolFormat) === o.value ? "is-active" : ""}`}
                type="button"
                onClick={() => update("poolFormat", o.value)}
                disabled={lockedAfterDraw}
              >{o.label}</button>
            ))}
          </div>
          <div className="field__hint">
            {/* Stored/legacy "" resolves to "full" for display: the server's
                own unset default is "full" (see poolFormatVisible's comment
                in competition_shape.jsx), and this file already reads an
                empty poolFormat as non-partial at the courts-suggestion hint
                below (was: `local.poolFormat === "partial"`). */}
            {poolFormatHint(local.poolFormat)}
            {lockedNote}
          </div>
        </div>
      )}
      {/* bc-symm Gap 2: gated on roundRobinVisible (competition_shape.jsx)
          -- true only for "mixed" with poolFormat !== "partial", the one
          combination internal/engine/pools.go actually reads
          comp.RoundRobin for. Rendering it unconditionally (as this used
          to) showed a live-looking control that a league or a
          partial-pool mixed competition silently ignored.
          The guard wraps the .field, not just the checkbox inside it: an
          empty .field still carries the class's own margin, and the sibling
          poolFormatVisible / swissRoundsVisible blocks on this screen all
          gate the wrapper too.
          draw-ready lock: roundRobin is output-affecting. */}
      {roundRobinVisible(local.format, local.poolFormat) && (
        <div className="field">
          <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => update("roundRobin", e.target.checked)} disabled={isDrawReady} /> {LABEL_ROUND_ROBIN}</label>
        </div>
      )}
      {local.format === FORMAT_MIXED && (
        <>
          <div className="field">
            <label className="field__label">Pool size is a</label>
            {/* Stored/legacy "" resolves to "minimum", which is what the
                engine does with it (`isMax := PoolSizeMode == "max"`
                everywhere) -- see resolvePoolSizeMode in
                competition_shape.jsx. Nothing on the server fills this
                field in on POST, so a competition authored outside the SPA
                reaches the Format editor with "" and a bare equality lit
                NEITHER pill while the draw ran minimum sizing. Same fix, and
                the same reason, as resolvePoolFormat on the sibling field
                above. */}
            {/* draw-ready lock: poolSizeMode, poolSize, poolWinners are output-affecting. */}
            <div className="radio-group">
              <button
                className={`radio-pill ${resolvePoolSizeMode(local.poolSizeMode) === POOL_SIZE_MODE_MAX ? "is-active" : ""}`}
                type="button"
                onClick={() => {
                  // bc-qual LP-5a: leaving minimum-players-per-pool sizing
                  // hides the "Knockout qualifiers" radio below; reset its
                  // value to standard so it can't persist as a stale
                  // non-standard selection under a mode it's no longer
                  // valid for (same reset admin_setup.jsx's create form
                  // applies on the same transition).
                  update("poolSizeMode", POOL_SIZE_MODE_MAX);
                  update("extraQualifiers", resetExtraQualifiersOnPoolModeChange(POOL_SIZE_MODE_MAX, local.extraQualifiers));
                }}
                disabled={isDrawReady}
              >maximum</button>
              <button className={`radio-pill ${resolvePoolSizeMode(local.poolSizeMode) === POOL_SIZE_MODE_MIN ? "is-active" : ""}`} type="button" onClick={() => update("poolSizeMode", POOL_SIZE_MODE_MIN)} disabled={isDrawReady}>minimum</button>
            </div>
          </div>
          <div className="row">
            {/* Same NaN-as-"" + gated-save pattern as Team size above. */}
            {/* min=3 is a PRODUCT floor, not the backend's, which this */}
            {/* comment used to assert: the engine rejects only poolSize <= 0 */}
            {/* (engine/pools.go). 3 is the smallest pool a round-robin is */}
            {/* worth running; the server stays looser on purpose so a */}
            {/* hand-edited or imported config is not retroactively invalid. */}
            {/* Shared with the create form via poolSettingsError. */}
            <div className="field"><label className="field__label">{LABEL_POOL_SIZE}</label><input
              className="input"
              type="number"
              min={MIN_POOL_SIZE}
              value={Number.isFinite(local.poolSize) ? local.poolSize : ""}
              onChange={(e) => updateNumber("poolSize", e.target.value, MIN_POOL_SIZE)}
              disabled={isDrawReady}
            /></div>
            <div className="field">
              <label className="field__label">{LABEL_POOL_WINNERS}</label>
              <input
                className="input"
                type="number"
                min={MIN_POOL_WINNERS}
                value={Number.isFinite(local.poolWinners) ? local.poolWinners : ""}
                onChange={(e) => updateNumber("poolWinners", e.target.value, MIN_POOL_WINNERS)}
                disabled={isDrawReady || winnersInputDisabled(local.extraQualifiers)}
              />
              {/* bc-qual LP-5a: same coupling hint as the create form; */}
              {/* draw-ready already has its own standing note on the */}
              {/* Assigned shiaijo field below, so it isn't repeated per-field here. */}
              {!isDrawReady && winnersInputDisabled(local.extraQualifiers) && (
                <div className="field__hint">Set to 1 by the knockout qualifiers setting below.</div>
              )}
            </div>
          </div>
          {/* Rendered for ANY invalid poolSize/poolWinners, staged or already
              saved, mirroring courtsErr's own rule (see the ShiaijoCountNotes
              comment on the Assigned shiaijo field below): the field must never
              look fine while a Save would 400, even before this session
              touched it. Save itself is gated on the narrower
              blockingPoolSettingsErr (a CHANGE to an invalid combination),
              not on this.

              Wrapped in a `.field` rather than dropped in bare. FieldError
              carries no spacing of its own by design (see its comment in
              ui.jsx: spacing is the container's job, and a style prop is the
              crack per-site drift returns through), so as a bare sibling of
              the .row above it rendered flush against the "Pool match
              duration" label below and read as that field's error instead of
              this row's. `.field`'s own margin-bottom is the sanctioned way
              to separate it. Full width under BOTH inputs, not inside either
              one: the message names whichever of the two is wrong, and the
              .row is a two-column grid that would wrap it to half width. */}
          {poolSettingsErr && (
            <div className="field">
              <window.FieldError>{poolSettingsErr}</window.FieldError>
            </div>
          )}

          {/* Knockout qualifiers (bc-qual LP-5a): only meaningful under
              minimum-players-per-pool sizing (poolSizeMode === "min"); see
              extraQualifiersRadioVisible. Same three options, same copy,
              and the same draw-ready lock as poolSizeMode/poolSize/
              poolWinners immediately above (this field is in the
              server-side outputAffectingChanged set alongside them).
              Federation-neutral copy per operator ruling: no federation
              names anywhere in this UI. */}
          {/* Resolved, not raw: extraQualifiersRadioVisible's gate is a
              strict `poolMode === "min"`, so an unresolved "" hid the radio
              on exactly the records the SERVER would accept a non-standard
              qualifier setting for (its own test is `isMax := ... == "max"`).
              The predicate keeps its strict contract; the call site hands it
              a resolved value. */}
          {extraQualifiersRadioVisible(local.format, resolvePoolSizeMode(local.poolSizeMode)) && (() => {
            const activeShape = local.extraQualifiers === EXTRA_QUALIFIERS_LARGER_POOLS
              ? qualifierPreview.largerPools
              : local.extraQualifiers === EXTRA_QUALIFIERS_FILL_BRACKET
                ? qualifierPreview.fillBracket
                : qualifierPreview.standard;
            const previewLine = formatQualifierPreviewLine(activeShape);
            return (
              <div className="field">
                <label className="field__label">{LABEL_EXTRA_QUALIFIERS}</label>
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
      {/* T190 (FR-050a): swissRounds settings editor. Only rendered */}
      {/* when format=swiss. The backend allows editing pre-start; */}
      {/* changing rounds after start is allowed too (the next */}
      {/* "Generate next round" call will respect the new cap). */}
      {swissRoundsVisible(local.format) && (
        <div className="field">
          <label className="field__label">{LABEL_SWISS_ROUNDS}</label>
          {/* Floor is MIN_SWISS_ROUNDS (competition_shape.jsx), the same
              constant swissSettingsError reads, so the input's own min and
              the message that blocks Save cannot state different numbers. */}
          <input
            className="input"
            type="number"
            min={MIN_SWISS_ROUNDS}
            step="1"
            value={Number.isFinite(local.swissRounds) ? local.swissRounds : ""}
            onChange={(e) => updateNumber("swissRounds", e.target.value, MIN_SWISS_ROUNDS)}
            style={{ maxWidth: 120 }}
          />
          {/* Inline, like the pool block's error above: the header/footer
              chip states the blocker, but the operator's eye is on the field
              they just emptied. */}
          {swissSettingsErr && <window.FieldError>{swissSettingsErr}</window.FieldError>}
          <div className="field__hint">{HINT_SWISS_ROUNDS}</div>
        </div>
      )}
      {/* League standings settings. The joint-3rd convention applies to ALL
          leagues (team + individual); the "Break ties for top" band is a
          team-league tie-breaker knob and stays team-only. */}
      {twoThirdPlacesVisible(local.format) && (
        <div style={{ display: "flex", flexDirection: "column", gap: 12, marginTop: 8, paddingTop: 12, borderTop: "1px solid var(--line)" }}>
          {/* leagueTiebreakVisible folds format === "league" into its own
              check, so this re-evaluates that half of the outer wrapper's
              condition -- redundant (A && A) but not wrong, and it keeps
              this call site identical to the create form's, which has no
              format wrapper of its own to lean on. */}
          {leagueTiebreakVisible(local.format, local.kind, local.teamSize) && (
            <div className="field">
              <label className="field__label">{LABEL_LEAGUE_TIEBREAK}</label>
              <div className="radio-group">
                {LEAGUE_TIEBREAK_OPTIONS.map((o) => (
                  <button
                    key={o.value}
                    className={`radio-pill ${leagueTiebreakActive(o.value, local.leagueTiebreakTopN) ? "is-active" : ""}`}
                    type="button"
                    disabled={lockedAfterDraw}
                    onClick={() => update("leagueTiebreakTopN", o.value)}
                  >{o.label}</button>
                ))}
              </div>
              <div className="field__hint">{HINT_LEAGUE_TIEBREAK}{lockedNote}</div>
            </div>
          )}
          {/* `.field` for its margin-bottom, matching admin_setup.jsx's wrapper
              around this same control. The inline flex/gap:4 stays and wins over
              .field's gap:6, so the checkbox-to-hint spacing is unchanged; the
              only thing the class adds is the 14px separation from whatever
              follows. Bare, this rendered flush against the next control's label
              (measured: 0px to "Assigned shiaijo (courts)", against 14px+ at
              every other transition on the screen). It was invisible until the
              bc-symm reorder, because this checkbox used to be the LAST control
              on the form and so had nothing to collide with. */}
          <div className="field" style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <label className="checkbox">
              <input type="checkbox" checked={!!local.leagueTwoThirdPlaces} disabled={lockedAfterDraw} onChange={(e) => update("leagueTwoThirdPlaces", e.target.checked)} />
              {" "}{LABEL_TWO_THIRD_PLACES}
            </label>
            <div className="field__hint field__hint--checkbox">{HINT_TWO_THIRD_PLACES}{lockedNote}</div>
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
        {(local.format === FORMAT_LEAGUE || local.poolFormat === POOL_FORMAT_PARTIAL) ? (() => {
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
      {/* FR-052..FR-054 / T047: per-phase match-duration inputs. Visibility
          and label/hint text now come from competition_shape.jsx
          (poolDurationVisible/playoffDurationVisible, poolDurationLabel/
          poolDurationHint) so this row can't drift from the create form's
          copy of the same two fields. */}
      {(poolDurationVisible(local.format) || playoffDurationVisible(local.format)) && (
        <div className="row">
          {poolDurationVisible(local.format) &&
            durationField(poolDurationLabel(local.format), "poolMatchDurationSeconds", poolDurationHint(local.format))}
          {playoffDurationVisible(local.format) &&
            durationField(LABEL_PLAYOFF_DURATION, "playoffMatchDurationSeconds", HINT_PLAYOFF_DURATION)}
        </div>
      )}
      {/* mp-zoh Phase 4: inline schedule estimate. Shown below duration inputs */}
      {/* so the operator can immediately see the impact of duration changes */}
      {/* after the save lands. Re-fetches from the server on every */}
      {/* c-prop update (SSE schedule_updated / competition_updated). */}
      {/* Its marginBottom matches .field's 14px. The panel is bordered and */}
      {/* filled, so it reads as a box, and with only a marginTop it sat flush */}
      {/* against the next control's label (measured: 0px). Like the joint-3rd */}
      {/* places checkbox above, that was invisible until the bc-symm reorder */}
      {/* gave it a successor -- it used to be followed by a gap-bearing */}
      {/* container rather than by a field. */}
      {(compEstimate || compEstimateLoading || compEstimateErr) && (
        <div className="settings-estimate">
          {/* "Schedule estimate" is the title of this hint-style panel, not a
              button/input/badge, so it takes the 12px "hints, secondary meta"
              role (DESIGN.md Typography) -- matching every other text node in
              this panel rather than the former 12.5 stray. */}
          <div style={{ fontSize: 12, fontWeight: 600, color: "var(--ink-2)", marginBottom: 4 }}>
            Schedule estimate
            {compEstimateLoading && <span className="spinner" style={{ marginLeft: 6, verticalAlign: "middle" }} />}
          </div>
          {compEstimateErr && (
            <div style={{ fontSize: 12, color: "var(--red)" }}>{compEstimateErr}</div>
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
              return <div style={{ fontSize: 12, color: "var(--ink-3)" }}>No estimate yet. Add participants and configure duration to see a projection.</div>;
            }
            return (
              <div style={{ fontSize: 12, color: "var(--ink-1)" }}>
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
        <label className="field__label">{LABEL_NUMBER_PREFIX} <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={local.numberPrefix || ""} onChange={(e) => update("numberPrefix", e.target.value.substring(0, 3))} disabled={isDrawReady} style={{ maxWidth: 80 }} />
        <div className="field__hint">{HINT_NUMBER_PREFIX}</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          {/* zekkenApplies/engiApplies express the RULE (competition_shape.jsx);
              this screen keeps its own PRESENTATION of it -- render both
              controls always and disable+explain, rather than hide them like
              the create form does -- because kind is locked here once a
              roster exists (kindLockReason above), so hiding would leave the
              operator unable to see, let alone recover, a setting they
              remember configuring. */}
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => update("withZekkenName", e.target.checked)} disabled={isDrawReady || !zekkenApplies(local.kind)} /> {LABEL_ZEKKEN}</label>
          <div className="field__hint field__hint--checkbox">{!zekkenApplies(local.kind) ? HINT_KIND_ONLY_INDIVIDUAL : HINT_ZEKKEN}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.engi} onChange={(e) => update("engi", e.target.checked)} disabled={lockedAfterDraw || !engiApplies(local.kind)} /> {LABEL_ENGI}</label>
          <div className="field__hint field__hint--checkbox">{!engiApplies(local.kind) ? HINT_KIND_ONLY_INDIVIDUAL : `${HINT_ENGI}${lockedNote}`}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.naginata} onChange={(e) => update("naginata", e.target.checked)} disabled={lockedAfterDraw} /> {LABEL_NAGINATA}</label>
          <div className="field__hint field__hint--checkbox">{HINT_NAGINATA}{lockedNote}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.checkInEnabled} onChange={(e) => update("checkInEnabled", e.target.checked)} /> {LABEL_CHECK_IN}</label>
          <div className="field__hint field__hint--checkbox">{HINT_CHECK_IN}</div>
        </div>
      </div>
      {/* Repeat Save at the foot of the long settings form so the operator
          doesn't have to scroll back to the header after editing. Same
          onClick, the SAME `saveDisabled` value and the SAME `saveBlockMessage`
          as the header: the two cannot drift because neither the block
          condition nor the blocking message is restated here. The transient
          "Saving…"/"✓ Saved at" states are the header's alone, by choice. */}
      <div style={{ marginTop: 20, display: "flex", justifyContent: "flex-end", alignItems: "center", gap: 10 }}>
        {/* Same badge-like status role as the header chip above (13px, "Buttons,
            inputs, badges" per DESIGN.md Typography): both are a bold coloured
            save-state indicator sitting beside the Save button, not a hint. */}
        {saveBlocked && <span style={{ fontSize: 13, color: "var(--red)", fontWeight: 600 }}>{saveBlockMessage}</span>}
        {!saveBlocked && isDirty && !saving && <span style={{ fontSize: 13, color: "var(--warn)", fontWeight: 600 }}>● Unsaved changes</span>}
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
