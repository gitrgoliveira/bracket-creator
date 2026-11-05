// competition_put_body.jsx: The one builder of a competition settings PUT body,
// used by Settings and (later) by the calendar's move with { date, startTime, courts }
// as edits and a fresh read's version. Pure module: no window, no React, no setState.

import { validateAndNormalizeDate } from './admin_helpers.jsx';
import { formatDuration } from './duration.jsx';
import { extraQualifiersLabel } from './qualifier_preview.jsx';
import {
  shapeConfigForSave,
  resolveTeamSize,
  effectiveTwoThirdPlaces,
  cutNumberPrefix,
  optionLabel,
  KIND_OPTIONS,
  FORMAT_OPTIONS,
  POOL_FORMAT_OPTIONS,
  POOL_SIZE_MODE_OPTIONS,
  TEAM_MATCH_TYPE_OPTIONS,
  LABEL_KIND,
  LABEL_FORMAT,
  LABEL_POOL_FORMAT,
  LABEL_SWISS_ROUNDS,
  LABEL_ROUND_ROBIN,
  LABEL_LEAGUE_TIEBREAK,
  LABEL_KNOCKOUT_DURATION,
  LABEL_POOL_DURATION,
  LABEL_TWO_THIRD_PLACES,
  LABEL_POOL_SIZE,
  LABEL_POOL_WINNERS,
  LABEL_EXTRA_QUALIFIERS,
  LABEL_TEAM_SIZE,
  LABEL_TEAM_MATCH_TYPE,
  LABEL_ZEKKEN,
  LABEL_ENGI,
  LABEL_POOL_SIZE_MODE,
  LABEL_NAGINATA,
  LABEL_CHECK_IN,
  LABEL_NUMBER_PREFIX,
  LABEL_COURTS,
} from './competition_shape.jsx';

export const LABEL_EXPECTED_PARTICIPANTS = "Expected participants";
export const LABEL_EXPECTED_TEAMS = "Expected teams";
// Mirrors engine.MaxEstimateParticipants; pinned by internal/engine/expected_cap_test.go.
export const MAX_EXPECTED_PARTICIPANTS = 1024;

export function expectedParticipantsHint(expectedOrOpts, maybeOpts) {
  const opts = (typeof expectedOrOpts === "object" && expectedOrOpts !== null)
    ? expectedOrOpts
    : { expectedParticipants: expectedOrOpts, ...maybeOpts };

  const expectedRaw = opts.expectedParticipants;
  const status = opts.status || "setup";
  const isTeam = !!opts.isTeam;
  const rosterCount = typeof opts.rosterCount === "number" ? opts.rosterCount : 0;
  const checkedInCount = typeof opts.checkedInCount === "number" ? opts.checkedInCount : 0;
  const checkIn = !!opts.checkInApplies;
  const drawnCount = typeof opts.drawnCount === "number" ? opts.drawnCount : rosterCount;

  if (status !== "setup") {
    const d = drawnCount;
    const unit = isTeam ? (d === 1 ? "team" : "teams") : (d === 1 ? "participant" : "participants");
    return `The draw has been made, so estimates use the ${d} ${unit} in it.`;
  }

  const hasN = Number.isFinite(expectedRaw) && expectedRaw > 0;
  if (!hasN) {
    return "How many you expect. Estimates use this number until more register. Kept with the competition.";
  }

  const n = expectedRaw;
  const c = checkIn ? checkedInCount : rosterCount;

  if (c === 0) {
    if (checkIn) {
      return `Estimates use ${n} until more than ${n} check in. ${rosterCount} registered, 0 checked in so far.`;
    }
    return `Estimates use ${n} until more than ${n} register. 0 registered so far.`;
  }

  if (c < n) {
    if (checkIn) {
      return `${c} checked in, fewer than the ${n} you expected. Estimates still use ${n}; lower this number to use the check-ins.`;
    }
    return `${c} registered, fewer than the ${n} you expected. Estimates still use ${n}; lower this number to use the registrations.`;
  }

  if (c === n) {
    if (checkIn) {
      return `Check-ins match the ${n} you expected.`;
    }
    return `Registrations match the ${n} you expected.`;
  }

  // c > n
  if (checkIn) {
    return `Estimates use the ${c} checked in, more than the ${n} you expected.`;
  }
  return `Estimates use the ${c} registered, more than the ${n} you expected.`;
}

const SETTINGS_FIELD_LABELS = {
  name: "Name",
  date: "Date",
  startTime: "Start time",
  courts: LABEL_COURTS,
  kind: LABEL_KIND,
  format: LABEL_FORMAT,
  poolFormat: LABEL_POOL_FORMAT,
  poolSize: LABEL_POOL_SIZE,
  poolWinners: LABEL_POOL_WINNERS,
  poolSizeMode: LABEL_POOL_SIZE_MODE,
  roundRobin: LABEL_ROUND_ROBIN,
  swissRounds: LABEL_SWISS_ROUNDS,
  teamSize: LABEL_TEAM_SIZE,
  teamMatchType: LABEL_TEAM_MATCH_TYPE,
  withZekkenName: LABEL_ZEKKEN,
  engi: LABEL_ENGI,
  naginata: LABEL_NAGINATA,
  checkInEnabled: LABEL_CHECK_IN,
  twoThirdPlaces: LABEL_TWO_THIRD_PLACES,
  leagueTiebreakTopN: LABEL_LEAGUE_TIEBREAK,
  extraQualifiers: LABEL_EXTRA_QUALIFIERS,
  numberPrefix: LABEL_NUMBER_PREFIX,
  poolMatchDurationSeconds: LABEL_POOL_DURATION,
  knockoutMatchDurationSeconds: LABEL_KNOCKOUT_DURATION,
  expectedParticipants: LABEL_EXPECTED_PARTICIPANTS,
};

function formatEditValue(k, v) {
  if (k === "poolMatchDurationSeconds" || k === "knockoutMatchDurationSeconds") {
    if (v === "" || v === null || v === undefined || (typeof v === "number" && Number.isNaN(v))) {
      return "blank";
    }
    const formatted = formatDuration(v);
    return formatted || (v === 0 ? "blank" : String(v));
  }
  if (k === "kind") {
    return optionLabel(KIND_OPTIONS, v) || (v === "" ? "blank" : String(v));
  }
  if (k === "format") {
    return optionLabel(FORMAT_OPTIONS, v) || (v === "" ? "blank" : String(v));
  }
  if (k === "poolFormat") {
    return optionLabel(POOL_FORMAT_OPTIONS, v) || (v === "" ? "blank" : String(v));
  }
  if (k === "poolSizeMode") {
    return optionLabel(POOL_SIZE_MODE_OPTIONS, v) || (v === "" ? "blank" : String(v));
  }
  if (k === "teamMatchType") {
    return optionLabel(TEAM_MATCH_TYPE_OPTIONS, v) || (v === "" ? "blank" : String(v));
  }
  if (k === "extraQualifiers") {
    return extraQualifiersLabel(v) || "Standard";
  }
  if (typeof v === "boolean") {
    return v ? "on" : "off";
  }
  if (Array.isArray(v)) {
    return v.length > 0 ? v.join(", ") : "blank";
  }
  if (v === "" || v === null || v === undefined || (typeof v === "number" && Number.isNaN(v))) {
    return "blank";
  }
  return String(v);
}

export function describeSettingsEdits(edits) {
  if (!edits || typeof edits !== "object") return "";
  const keys = Object.keys(edits);
  if (keys.length === 0) return "";
  const parts = keys.map((k) => {
    const label = SETTINGS_FIELD_LABELS[k] || k;
    const val = formatEditValue(k, edits[k]);
    return `${label} to ${val}`;
  });
  return parts.join(", ") + ".";
}

export function settingsPutBody(base, edits, configVersion) {
  const effective = { ...base, ...edits };

  // twoThirdPlaces (bc-3rdp): if the operator touched it this session, their
  // choice wins (already staged as a real boolean in localSnap by the
  // checkbox's onChange). Otherwise DERIVE it via effectiveTwoThirdPlaces
  // rather than trusting latestC.twoThirdPlaces raw -- a legacy competition
  // has no stored value at all, and sending `false` for one on every
  // unrelated settings save (this field is round-tripped unconditionally
  // below, like naginata/teamMatchType) would silently flip its
  // effective rule from "on" (via the !naginata fallback) to an explicit
  // "off" the operator never chose.
  effective.twoThirdPlaces = (edits && Object.prototype.hasOwnProperty.call(edits, "twoThirdPlaces"))
    ? !!edits.twoThirdPlaces
    : effectiveTwoThirdPlaces(base);

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
      return { error: dateError };
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
    return { error: "Competition name is required." };
  }

  // cutNumberPrefix (competition_shape.jsx,): the TextField's
  // onChange already runs this per keystroke, so `effective.numberPrefix`
  // should already be trimmed and character-safe-capped -- this is a
  // defensive re-application at the payload boundary, the same posture
  // normalizeConfigForFormat/normalizeConfigForKind take (only ever
  // called here, never a second time from the control's own onChange).
  // Historically this line only trimmed (`.trim()`), because the
  // per-keystroke cut used `.substring(0, 3)` and never trimmed, so
  // typing "  A" stored "  A" in local state and (without this) landed
  // "  A" on the server; the CREATE flow (AdminCreateCompetition.create's
  // deriveCompetitionName + trim chain in admin_setup.jsx) already
  // trimmed at create time, so this mirrored that for the SETTINGS edit
  // flow. cutNumberPrefix now does both jobs in one call.
  const trimmedPrefix = cutNumberPrefix(effective.numberPrefix);

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
  const shaped = shapeConfigForSave(base, {
    ...effective,
    teamSize: resolveTeamSize(effective.teamSize, base.teamSize),
  });

  const resolveExpectedParticipants = () => {
    if (edits && Object.prototype.hasOwnProperty.call(edits, "expectedParticipants")) {
      const v = edits.expectedParticipants;
      if (v === "" || v === null || v === undefined || (typeof v === "number" && Number.isNaN(v))) {
        return 0;
      }
      return safeNonNegInt(v, safeNonNegInt(base.expectedParticipants, 0));
    }
    return safeNonNegInt(base.expectedParticipants, 0);
  };

  const finalNext = {
    id: base.id,
    name: trimmedName,
    date: dateNorm,
    startTime: effective.startTime,
    // poolSize/poolWinners read from `shaped`, not `effective`:
    // normalizeConfigForFormat is the function that zeroes them on the way
    // out of "mixed", and shaped is where that now runs (see the `shaped`
    // comment above). safeNonNegInt, not safeInt, so the deliberate 0
    // survives -- see safeNonNegInt's own comment.
    poolSize: safeNonNegInt(shaped.poolSize, base.poolSize),
    poolWinners: safeNonNegInt(shaped.poolWinners, base.poolWinners),
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
    poolMatchDurationSeconds: safeNonNegInt(effective.poolMatchDurationSeconds, base.poolMatchDurationSeconds || 0),
    knockoutMatchDurationSeconds: safeNonNegInt(effective.knockoutMatchDurationSeconds, base.knockoutMatchDurationSeconds || 0),
    // T190 (FR-050a): swissRounds is editable pre-start; safeInt
    // preserves the previously-saved value when the input is
    // cleared (so the cleared display doesn't clobber the disk
    // value before the user types a valid replacement).
    swissRounds: safeInt(effective.swissRounds, base.swissRounds || 0),
    naginata: !!effective.naginata,
    // Engi (flag-scoring kata pairs). Round-tripped for the same reason as
    // `teamMatchType`: the backend transform unconditionally
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
    leagueTiebreakTopN: safeInt(effective.leagueTiebreakTopN, base.leagueTiebreakTopN || 0),
    // twoThirdPlaces (bc-3rdp): round-tripped unconditionally, like
    // naginata above (and teamMatchType below), so an untouched control
    // never clobbers the stored rule -- `effective.twoThirdPlaces` was already
    // resolved to a real boolean above (the operator's edit, or the
    // derived effective default), never the possibly-absent raw value.
    // LeagueTwoThirdPlaces itself is legacy read-only and is never sent.
    twoThirdPlaces: !!effective.twoThirdPlaces,
    // teamMatchType is edited via the Team match format pills above; the
    // merge is a full replace: omitting it would clobber a kachinuki
    // competition's value to "" (fixed) on any save. Round-trip it like
    // `engi` above to preserve the stored value. Reads from `shaped`:
    // normalizeConfigForKind stages "fixed" (never "") going individual --
    // see that function's own comment for why "" would be re-filled from
    // latestC.teamMatchType here and the rejected kachinuki would come
    // straight back.
    teamMatchType: shaped.teamMatchType || base.teamMatchType || "",
    // bc-qual LP-5a: knockout qualifiers, edited by the "Knockout
    // qualifiers" pills below. Included for the same reason as
    // teamMatchType/naginata above: the backend transform
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
    expectedParticipants: resolveExpectedParticipants(),
    configVersion: configVersion || "",
  };

  return { body: finalNext };
}

// mergeSavedCompetition: merges a competition PUT's answer (from the server)
// into the client's held competition copy. Handles omitempty fields where the
// server's answer omits keys (e.g. expectedParticipants: 0, or missing configVersion
// when reload fails). Knowledge of which fields are omitempty lives here next to
// the body builder.
export function mergeSavedCompetition(held, answer, sent) {
  const merged = { ...held, ...answer };
  if (!("expectedParticipants" in (answer || {}))) {
    const sentCleared = sent != null && (
      Number(sent.expectedParticipants) === 0 ||
      Number.isNaN(sent.expectedParticipants)
    );
    if (sentCleared) {
      merged.expectedParticipants = 0;
    }
  }
  if (!answer || !answer.configVersion) {
    merged.configVersion = "";
  }
  return merged;
}
