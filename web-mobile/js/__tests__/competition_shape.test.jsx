import { describe, it, expect } from 'vitest';
import {
  KIND_INDIVIDUAL, KIND_TEAM,
  FORMAT_KNOCKOUT, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS,
  POOL_FORMAT_FULL, POOL_FORMAT_PARTIAL,
  LABEL_KIND, KIND_OPTIONS,
  LABEL_FORMAT, FORMAT_OPTIONS, formatHint,
  LABEL_POOL_FORMAT, POOL_FORMAT_OPTIONS, poolFormatVisible,
  LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS, swissRoundsVisible,
  LABEL_ROUND_ROBIN, roundRobinVisible,
  LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK, LEAGUE_TIEBREAK_OPTIONS, leagueTiebreakVisible,
  LABEL_POOL_DURATION, HINT_POOL_DURATION, poolDurationLabel, poolDurationHint,
  LABEL_KNOCKOUT_DURATION, HINT_KNOCKOUT_DURATION,
  poolDurationVisible, knockoutDurationVisible,
  LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES, twoThirdPlacesVisible, effectiveTwoThirdPlaces,
  teamFieldsVisible, zekkenApplies, engiApplies,
  normalizeConfigForFormat,
  normalizeConfigForKind, DEFAULT_TEAM_SIZE, MIN_TEAM_SIZE, LABEL_TEAM_SIZE,
  kindChangeBlockedReason,
  poolSettingsError,
  swissSettingsError, MIN_SWISS_ROUNDS,
  resolveTeamSize, teamSizeError,
  resolvePoolSizeMode, POOL_SIZE_MODE_MAX, POOL_SIZE_MODE_MIN,
  configShapeChangeStaged, shapeConfigForSave,
  pendingConfigClears,
} from '../competition_shape.jsx';

// The four format values a competition can hold. Used to sweep every
// visibility predicate across every format so a new format added to one
// surface but not this module's predicates shows up as a table gap rather
// than being silently treated as "not visible".
const ALL_FORMATS = [FORMAT_KNOCKOUT, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS, '', undefined];
const ALL_KINDS = [KIND_INDIVIDUAL, KIND_TEAM, '', undefined];

describe('wire-value constants', () => {
  it('mirror state.Competition Kind/Format/PoolFormat values byte-for-byte', () => {
    expect(KIND_INDIVIDUAL).toBe('individual');
    expect(KIND_TEAM).toBe('team');
    expect(FORMAT_KNOCKOUT).toBe('knockout');
    expect(FORMAT_MIXED).toBe('mixed');
    expect(FORMAT_LEAGUE).toBe('league');
    expect(FORMAT_SWISS).toBe('swiss');
    expect(POOL_FORMAT_FULL).toBe('full');
    expect(POOL_FORMAT_PARTIAL).toBe('partial');
  });
});

describe('KIND_OPTIONS', () => {
  it('has exactly Individual and Team, verbatim copy from admin_setup.jsx', () => {
    expect(LABEL_KIND).toBe('Competition type');
    expect(KIND_OPTIONS).toEqual([
      { value: KIND_INDIVIDUAL, label: 'Individual' },
      { value: KIND_TEAM, label: 'Team' },
    ]);
  });
});

describe('FORMAT_OPTIONS / formatHint', () => {
  it('has exactly the four formats with verbatim create-form copy', () => {
    expect(LABEL_FORMAT).toBe('Format');
    expect(FORMAT_OPTIONS.map((o) => o.value)).toEqual([FORMAT_KNOCKOUT, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS]);
    expect(FORMAT_OPTIONS.map((o) => o.label)).toEqual(['Knockout only', 'Pools + Knockout', 'League', 'Swiss']);
  });

  const cases = [
    [FORMAT_KNOCKOUT, 'Direct single-elimination knockout.'],
    [FORMAT_MIXED, 'Round-robin pools first, then top finishers advance to a knockout bracket.'],
    [FORMAT_LEAGUE, 'Single round-robin across all participants; final standings determine the winner (no knockout).'],
    [FORMAT_SWISS, 'Swiss-system: fixed number of rounds, pairing players with equal win counts; cumulative standings decide the winner.'],
    ['', ''],
    [undefined, ''],
    ['bogus-format', ''],
  ];
  it.each(cases)('formatHint(%j) -> %j', (format, expected) => {
    expect(formatHint(format)).toBe(expected);
  });
});

describe('POOL_FORMAT_OPTIONS / poolFormatVisible', () => {
  it('has exactly full and partial, verbatim create-form copy', () => {
    expect(LABEL_POOL_FORMAT).toBe('Round-robin shape');
    expect(POOL_FORMAT_OPTIONS).toEqual([
      { value: POOL_FORMAT_FULL, label: 'Full round-robin', hint: 'Every participant plays every other participant in their pool.' },
      { value: POOL_FORMAT_PARTIAL, label: 'Partial / neighbour-only', hint: 'Each participant plays a neighbourhood subset: useful when a full round-robin would not fit in the day\'s schedule.' },
    ]);
  });

  // bc-symm Gap 1: visible for every format that builds a pool phase --
  // "mixed" AND "league", per internal/engine/pools.go:149's
  // `switch comp.PoolFormat`, which dispatches for both (see
  // poolFormatVisible's own comment for the pools.go/competition.go
  // evidence). Before the fix this returned false for "mixed", so a mixed
  // competition's pool shape could only ever be set to "partial" by
  // hand-editing config.md; the FORMAT_MIXED row below is the one that
  // pins the fix.
  const table = [
    [FORMAT_KNOCKOUT, false],
    [FORMAT_MIXED, true],
    [FORMAT_LEAGUE, true],
    [FORMAT_SWISS, false],
    ['', false],
    [undefined, false],
  ];
  it.each(table)('poolFormatVisible(%j) -> %j', (format, expected) => {
    expect(poolFormatVisible(format)).toBe(expected);
  });
});

describe('Swiss rounds label/hint/visibility', () => {
  it('picks the settings wording ("Number of Swiss rounds"), self-contained out of pill context', () => {
    expect(LABEL_SWISS_ROUNDS).toBe('Number of Swiss rounds');
  });
  it('hint is the shared copy (identical on both surfaces already)', () => {
    expect(HINT_SWISS_ROUNDS).toBe('Typical: 4 rounds for 16 players, 5 for 32, 6 for 64 (≈ log₂ of field size).');
  });

  const table = [
    [FORMAT_KNOCKOUT, false],
    [FORMAT_MIXED, false],
    [FORMAT_LEAGUE, false],
    [FORMAT_SWISS, true],
    ['', false],
    [undefined, false],
  ];
  it.each(table)('swissRoundsVisible(%j) -> %j', (format, expected) => {
    expect(swissRoundsVisible(format)).toBe(expected);
  });
});

describe('LABEL_ROUND_ROBIN / roundRobinVisible', () => {
  it('is the settings-only checkbox copy verbatim', () => {
    expect(LABEL_ROUND_ROBIN).toBe('Round-robin in pools');
  });

  // bc-symm Gap 2: visible ONLY for "mixed" with poolFormat !== "partial" --
  // the one combination internal/engine/pools.go:157 actually reads
  // comp.RoundRobin for (see roundRobinVisible's own comment). Full sweep,
  // every format x every poolFormat value, not just the interesting cells:
  // before the fix this control had no visibility predicate at all (both
  // surfaces rendered it unconditionally), so every row where `expected`
  // is false below is a row the OLD behaviour got wrong.
  const ALL_POOL_FORMATS = [POOL_FORMAT_FULL, POOL_FORMAT_PARTIAL, '', undefined];
  const table = [];
  for (const format of ALL_FORMATS) {
    for (const poolFormat of ALL_POOL_FORMATS) {
      const expected = format === FORMAT_MIXED && poolFormat !== POOL_FORMAT_PARTIAL;
      table.push([format, poolFormat, expected]);
    }
  }
  it.each(table)('roundRobinVisible(%j, %j) -> %j', (format, poolFormat, expected) => {
    expect(roundRobinVisible(format, poolFormat)).toBe(expected);
  });

  // Named regression cases, restated outside the sweep so the two rules
  // (format gate, poolFormat gate) each fail on their own if either one
  // is dropped later.
  it('is visible for mixed with an unset/full poolFormat', () => {
    expect(roundRobinVisible(FORMAT_MIXED, POOL_FORMAT_FULL)).toBe(true);
    expect(roundRobinVisible(FORMAT_MIXED, '')).toBe(true);
    expect(roundRobinVisible(FORMAT_MIXED, undefined)).toBe(true);
  });

  it('disappears the moment a mixed competition switches to partial pools', () => {
    expect(roundRobinVisible(FORMAT_MIXED, POOL_FORMAT_PARTIAL)).toBe(false);
  });

  it('is never visible for league, even though RoundRobin is stored there too: competition.go:896 forces it true regardless', () => {
    expect(roundRobinVisible(FORMAT_LEAGUE, POOL_FORMAT_FULL)).toBe(false);
    expect(roundRobinVisible(FORMAT_LEAGUE, POOL_FORMAT_PARTIAL)).toBe(false);
  });

  it('is never visible for knockout or swiss: neither runs the PoolFormat switch\'s default branch', () => {
    expect(roundRobinVisible(FORMAT_KNOCKOUT, POOL_FORMAT_FULL)).toBe(false);
    expect(roundRobinVisible(FORMAT_SWISS, POOL_FORMAT_FULL)).toBe(false);
  });
});

describe('league tie-break band', () => {
  it('copy is verbatim from admin_competition_settings.jsx', () => {
    expect(LABEL_LEAGUE_TIEBREAK).toBe('Break ties for top');
    expect(HINT_LEAGUE_TIEBREAK).toBe('Tied teams within this finishing band require an operator-run tie-breaker before standings are finalised.');
    expect(LEAGUE_TIEBREAK_OPTIONS).toEqual([
      { value: 3, label: 'Top 3' },
      { value: 4, label: 'Top 4' },
    ]);
  });

  // Full (format, kind, teamSize) cross product. The teamSize term is the
  // legacy-data allowance: settings shows this control for a league whose
  // teamSize > 0 even when kind is not "team" (models.go:885-886 names that
  // pairing as real stored data), so a two-argument narrowing would hide a
  // control those records show today.
  const table = [];
  for (const format of ALL_FORMATS) {
    for (const kind of ALL_KINDS) {
      for (const teamSize of [0, 3, undefined]) {
        const isTeam = kind === KIND_TEAM || teamSize > 0;
        table.push([format, kind, teamSize, format === FORMAT_LEAGUE && isTeam]);
      }
    }
  }
  it.each(table)('leagueTiebreakVisible(%j, %j, %j) -> %j', (format, kind, teamSize, expected) => {
    expect(leagueTiebreakVisible(format, kind, teamSize)).toBe(expected);
  });

  // The regression the widening exists to prevent, stated on its own so a
  // future narrowing back to (format, kind) fails here by name.
  it('shows the tie-break band for a drifted league record: kind individual, teamSize > 0', () => {
    expect(leagueTiebreakVisible(FORMAT_LEAGUE, KIND_INDIVIDUAL, 3)).toBe(true);
  });
});

describe('per-phase match duration', () => {
  it('base (mixed/league) copy is verbatim from admin_competition_settings.jsx', () => {
    expect(LABEL_POOL_DURATION).toBe('Pool match duration');
    expect(HINT_POOL_DURATION).toBe('Estimated time per pool match, as m:ss (e.g. 2:30).');
    expect(LABEL_KNOCKOUT_DURATION).toBe('Knockout match duration');
    expect(HINT_KNOCKOUT_DURATION).toBe('Estimated time per knockout match, as m:ss (e.g. 2:30).');
  });

  const labelTable = [
    [FORMAT_MIXED, 'Pool match duration'],
    [FORMAT_LEAGUE, 'Pool match duration'],
    [FORMAT_SWISS, 'Round match duration'],
    [FORMAT_KNOCKOUT, 'Pool match duration'],
    ['', 'Pool match duration'],
  ];
  it.each(labelTable)('poolDurationLabel(%j) -> %j', (format, expected) => {
    expect(poolDurationLabel(format)).toBe(expected);
  });

  const hintTable = [
    [FORMAT_MIXED, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
    [FORMAT_LEAGUE, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
    [FORMAT_SWISS, 'Estimated time per Swiss-round match, as m:ss (e.g. 2:30).'],
    [FORMAT_KNOCKOUT, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
  ];
  it.each(hintTable)('poolDurationHint(%j) -> %j', (format, expected) => {
    expect(poolDurationHint(format)).toBe(expected);
  });

  const poolVisTable = [
    [FORMAT_KNOCKOUT, false],
    [FORMAT_MIXED, true],
    [FORMAT_LEAGUE, true],
    [FORMAT_SWISS, true],
    ['', false],
    [undefined, false],
  ];
  it.each(poolVisTable)('poolDurationVisible(%j) -> %j', (format, expected) => {
    expect(poolDurationVisible(format)).toBe(expected);
  });

  const knockoutVisTable = [
    [FORMAT_KNOCKOUT, true],
    [FORMAT_MIXED, true],
    [FORMAT_LEAGUE, false],
    [FORMAT_SWISS, false],
    ['', false],
    [undefined, false],
  ];
  it.each(knockoutVisTable)('knockoutDurationVisible(%j) -> %j', (format, expected) => {
    expect(knockoutDurationVisible(format)).toBe(expected);
  });

  // mixed is the one format where BOTH duration fields render together
  // (it runs both a pool phase and a knockout phase).
  it('both duration fields are visible together only for "mixed"', () => {
    expect(poolDurationVisible(FORMAT_MIXED) && knockoutDurationVisible(FORMAT_MIXED)).toBe(true);
    for (const format of [FORMAT_KNOCKOUT, FORMAT_LEAGUE, FORMAT_SWISS]) {
      expect(poolDurationVisible(format) && knockoutDurationVisible(format)).toBe(false);
    }
  });
});

describe('two-thirds-places (joint bronze, bc-3rdp)', () => {
  it('copy matches verbatim between admin_setup.jsx and admin_competition_settings.jsx (no drift found)', () => {
    expect(LABEL_TWO_THIRD_PLACES).toBe('Award two joint 3rd places');
    expect(HINT_TWO_THIRD_PLACES).toBe('When enabled, two beaten semi-finalists share 3rd place and no bronze match is played (standard kendo convention). Leave it off to decide a single 3rd place: a knockout plays a bronze match, and a league awards one 3rd rather than a shared rank.');
  });

  // Gated on format alone (not kind): an individual league can still award
  // joint bronze under the kendo convention. Visible for every format that
  // can produce a 3rd place at all; hidden only for Swiss (no bracket, no
  // bronze match to suppress).
  const table = [];
  for (const format of ALL_FORMATS) {
    table.push([format, format !== FORMAT_SWISS]);
  }
  it.each(table)('twoThirdPlacesVisible(%j) -> %j', (format, expected) => {
    expect(twoThirdPlacesVisible(format)).toBe(expected);
  });

  // effectiveTwoThirdPlaces mirrors state.Competition.EffectiveTwoThirdPlaces
  // (Go), pinned here with the same four behaviour-preservation cases plus
  // the trap case bc-3rdp names, so the JS and Go resolvers cannot drift.
  describe('effectiveTwoThirdPlaces (Go mirror)', () => {
    it('naginata knockout resolves to false (single 3rd, bronze match)', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_KNOCKOUT, naginata: true })).toBe(false);
    });
    it('non-naginata knockout resolves to true (joint 3rd, no bronze match)', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_KNOCKOUT, naginata: false })).toBe(true);
    });
    it('league with the legacy flag on resolves to true', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_LEAGUE, leagueTwoThirdPlaces: true })).toBe(true);
    });
    it('league with the legacy flag off resolves to false', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_LEAGUE, leagueTwoThirdPlaces: false })).toBe(false);
    });
    it('trap case: a non-naginata knockout ignores a stray legacy leagueTwoThirdPlaces value', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_KNOCKOUT, naginata: false, leagueTwoThirdPlaces: false })).toBe(true);
      expect(effectiveTwoThirdPlaces({ format: FORMAT_KNOCKOUT, naginata: false, leagueTwoThirdPlaces: true })).toBe(true);
    });
    it('an explicit twoThirdPlaces always wins over both legacy fallbacks', () => {
      expect(effectiveTwoThirdPlaces({ format: FORMAT_KNOCKOUT, naginata: true, twoThirdPlaces: true })).toBe(true);
      expect(effectiveTwoThirdPlaces({ format: FORMAT_LEAGUE, leagueTwoThirdPlaces: true, twoThirdPlaces: false })).toBe(false);
    });
  });
});

describe('kind-gated fields', () => {
  const teamTable = [];
  const zekkenTable = [];
  const engiTable = [];
  // The expectation for "" and undefined is the point of this table, not a
  // straggler it tolerates. state.ValidateCompetitionKind blesses "" as a
  // first-class member of the kind set MEANING individual (an import
  // manifest with no `kind:` key decodes to it, and so does
  // state.Competition's Go zero value), so a record carrying it IS an
  // individual competition and the zekken / engi controls apply to it.
  // These three predicates read through resolveKind for exactly that
  // reason; before they did, both controls rendered DISABLED on such a
  // record under the hint "(Only applicable for individual competitions)".
  for (const kind of ALL_KINDS) {
    const isTeam = kind === KIND_TEAM;
    teamTable.push([kind, isTeam]);
    zekkenTable.push([kind, !isTeam]);
    engiTable.push([kind, !isTeam]);
  }
  it.each(teamTable)('teamFieldsVisible(%j) -> %j', (kind, expected) => {
    expect(teamFieldsVisible(kind)).toBe(expected);
  });
  it.each(zekkenTable)('zekkenApplies(%j) -> %j', (kind, expected) => {
    expect(zekkenApplies(kind)).toBe(expected);
  });
  it.each(engiTable)('engiApplies(%j) -> %j', (kind, expected) => {
    expect(engiApplies(kind)).toBe(expected);
  });

  // teamFieldsVisible and zekkenApplies/engiApplies partition kind into
  // complementary halves: a competition is never both "team fields
  // visible" and "zekken/engi applies" at once, and never NEITHER. The
  // second half is the one that used to fail: "" and undefined fell
  // through both predicates, so a legacy record got the team fields hidden
  // (right) and the individual-only ones disabled (wrong), which is a
  // competition the screen could describe as neither kind. resolveKind
  // makes the halves exhaustive, so the table covers every value in
  // ALL_KINDS rather than the two spelled-out ones.
  it('team-only and individual-only fields partition every kind value, with no overlap and no gap', () => {
    for (const kind of ALL_KINDS) {
      expect(teamFieldsVisible(kind) && zekkenApplies(kind), `kind ${JSON.stringify(kind)} claims both halves`).toBe(false);
      expect(teamFieldsVisible(kind) && engiApplies(kind), `kind ${JSON.stringify(kind)} claims both halves`).toBe(false);
      expect(teamFieldsVisible(kind) || zekkenApplies(kind), `kind ${JSON.stringify(kind)} falls through both halves`).toBe(true);
      expect(teamFieldsVisible(kind) || engiApplies(kind), `kind ${JSON.stringify(kind)} falls through both halves`).toBe(true);
    }
  });
});

describe('normalizeConfigForFormat', () => {
  it('does not mutate its argument', () => {
    const cfg = { format: FORMAT_LEAGUE, poolSize: 4, poolWinners: 2, extraQualifiers: 'larger-pools' };
    const frozen = JSON.parse(JSON.stringify(cfg));
    normalizeConfigForFormat(cfg);
    expect(cfg).toEqual(frozen);
  });

  it('returns a new object, not the same reference', () => {
    const cfg = { format: FORMAT_MIXED, poolSize: 4, poolWinners: 2, extraQualifiers: '' };
    const result = normalizeConfigForFormat(cfg);
    expect(result).not.toBe(cfg);
  });

  it('league zeroes poolSize/poolWinners and clears extraQualifiers', () => {
    const cfg = { format: FORMAT_LEAGUE, poolSize: 5, poolWinners: 2, extraQualifiers: 'fill-bracket' };
    expect(normalizeConfigForFormat(cfg)).toEqual({ format: FORMAT_LEAGUE, poolSize: 0, poolWinners: 0, extraQualifiers: '' });
  });

  it('knockout zeroes poolSize/poolWinners and clears extraQualifiers', () => {
    const cfg = { format: FORMAT_KNOCKOUT, poolSize: 3, poolWinners: 1, extraQualifiers: 'larger-pools' };
    expect(normalizeConfigForFormat(cfg)).toEqual({ format: FORMAT_KNOCKOUT, poolSize: 0, poolWinners: 0, extraQualifiers: '' });
  });

  it('swiss clears extraQualifiers but leaves poolSize/poolWinners untouched', () => {
    const cfg = { format: FORMAT_SWISS, poolSize: 4, poolWinners: 2, extraQualifiers: 'fill-bracket', swissRounds: 5 };
    expect(normalizeConfigForFormat(cfg)).toEqual({ format: FORMAT_SWISS, poolSize: 4, poolWinners: 2, extraQualifiers: '', swissRounds: 5 });
  });

  it('mixed leaves poolSize/poolWinners/extraQualifiers untouched (the only format where they mean something)', () => {
    const cfg = { format: FORMAT_MIXED, poolSize: 4, poolWinners: 2, extraQualifiers: 'larger-pools' };
    expect(normalizeConfigForFormat(cfg)).toEqual(cfg);
  });

  it('preserves fields it does not own', () => {
    const cfg = { format: FORMAT_LEAGUE, poolSize: 4, poolWinners: 2, extraQualifiers: '', name: 'Men\'s Individual', courts: ['A', 'B'] };
    const result = normalizeConfigForFormat(cfg);
    expect(result.name).toBe('Men\'s Individual');
    expect(result.courts).toEqual(['A', 'B']);
  });

  it('an already-normalized league config round-trips unchanged (idempotent)', () => {
    const cfg = { format: FORMAT_LEAGUE, poolSize: 0, poolWinners: 0, extraQualifiers: '' };
    expect(normalizeConfigForFormat(normalizeConfigForFormat(cfg))).toEqual(cfg);
  });
});

describe('kindChangeBlockedReason', () => {
  it('is free ("") when the roster is empty', () => {
    expect(kindChangeBlockedReason(0)).toBe('');
  });

  it('is free for non-finite/negative input (treated as no roster)', () => {
    expect(kindChangeBlockedReason(NaN)).toBe('');
    expect(kindChangeBlockedReason(undefined)).toBe('');
    expect(kindChangeBlockedReason(-3)).toBe('');
  });

  it('is blocked with an operator-facing reason once a roster exists', () => {
    const reason = kindChangeBlockedReason(5);
    expect(reason).not.toBe('');
    expect(typeof reason).toBe('string');
  });

  it('states the count and the way out (clearing the roster)', () => {
    const reason = kindChangeBlockedReason(5);
    expect(reason).toMatch(/5 participants/);
    expect(reason.toLowerCase()).toMatch(/clear/);
  });

  it('singular vs plural participant count', () => {
    expect(kindChangeBlockedReason(1)).toMatch(/1 participant\b/);
    expect(kindChangeBlockedReason(1)).not.toMatch(/1 participants/);
    expect(kindChangeBlockedReason(2)).toMatch(/2 participants/);
  });

  it('never mentions mats: kendo has no mats, only shiaijo (courts)', () => {
    expect(kindChangeBlockedReason(5).toLowerCase()).not.toMatch(/\bmat\b|\bmats\b/);
  });
});

// bc-symm Gap 3: the ONE place the team-size floor is written down.
// state.ValidateCompetitionTeamSize (internal/state/models.go) rejects
// teamSize == 1 unconditionally, so 2 is the true legal floor for a team
// competition, not 1. Both surfaces' Team size <input> used a literal
// min="1" and floored a typed/stepped value at 1 before this constant
// existed, letting the UI construct a request the server always 400s.
describe('MIN_TEAM_SIZE', () => {
  it('is 2: teamSize 1 is rejected by the server for every kind (neither the individual-only 0 nor a valid team value)', () => {
    expect(MIN_TEAM_SIZE).toBe(2);
  });
});

// Each case here is a dead end the settings screen could reach before
// normalizeConfigForKind existed: the server rejects the pairing, and the
// control that would fix it is hidden or disabled by the kind just chosen.
describe('normalizeConfigForKind', () => {
  it('zeroes teamSize going individual: the Team size input is hidden by then', () => {
    expect(normalizeConfigForKind({ kind: KIND_INDIVIDUAL, teamSize: 5 }).teamSize).toBe(0);
  });

  it('stages "fixed" (never "") for teamMatchType going individual', () => {
    // "" would be re-filled from the stored value by the PUT body's
    // `effective.teamMatchType || latestC.teamMatchType` fallback, so the
    // rejected kachinuki would come straight back.
    const out = normalizeConfigForKind({ kind: KIND_INDIVIDUAL, teamSize: 5, teamMatchType: 'kachinuki' });
    expect(out.teamMatchType).toBe('fixed');
    expect(out.teamSize).toBe(0);
  });

  it('clears engi going team: the checkbox is disabled for team, so nobody else can', () => {
    expect(normalizeConfigForKind({ kind: KIND_TEAM, engi: true }).engi).toBe(false);
  });

  it('supplies a usable team size going team, since 0 and 1 are both rejected', () => {
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: 0 }).teamSize).toBe(DEFAULT_TEAM_SIZE);
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: 1 }).teamSize).toBe(DEFAULT_TEAM_SIZE);
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: NaN }).teamSize).toBe(DEFAULT_TEAM_SIZE);
  });

  it('keeps an already-valid team size rather than resetting it to the default', () => {
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: 3 }).teamSize).toBe(3);
  });

  // bc-symm Gap 3: MIN_TEAM_SIZE is the boundary itself (2), not just a
  // value below it -- kept as-is rather than bumped to DEFAULT_TEAM_SIZE,
  // pinning that the floor check is `>= MIN_TEAM_SIZE` and not an
  // off-by-one (`> MIN_TEAM_SIZE`). Behaviourally identical to the old
  // hardcoded `>= 2` this replaced, so this alone is not a red/green
  // regression pin -- see the MIN_TEAM_SIZE describe block below for the
  // assertions that changed behaviour (the number input's min/floor).
  it('keeps a team size exactly at MIN_TEAM_SIZE rather than treating it as invalid', () => {
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: MIN_TEAM_SIZE }).teamSize).toBe(MIN_TEAM_SIZE);
  });

  it('clears withZekkenName going team, matching what the create form sends', () => {
    // Not cosmetic: EffectiveWithZekkenName has no kind term, so a team
    // competition carrying this parses participants.csv with the 4-column
    // zekken layout -- a roster shape the create form can never produce,
    // because it forces the flag false for a team.
    expect(normalizeConfigForKind({ kind: KIND_TEAM, withZekkenName: true }).withZekkenName).toBe(false);
  });

  it('leaves withZekkenName alone going individual, where it is the operator\'s real choice', () => {
    expect(normalizeConfigForKind({ kind: KIND_INDIVIDUAL, withZekkenName: true }).withZekkenName).toBe(true);
  });

  it('does not mutate its argument', () => {
    const cfg = { kind: KIND_INDIVIDUAL, teamSize: 5, teamMatchType: 'kachinuki' };
    normalizeConfigForKind(cfg);
    expect(cfg).toEqual({ kind: KIND_INDIVIDUAL, teamSize: 5, teamMatchType: 'kachinuki' });
  });
});

// bc-symm: poolSettingsError is the shared rule behind admin_setup.jsx's
// validatePoolSettings (create form) AND admin_competition_settings.jsx's
// blockingPoolSettingsErr (Settings screen). Taken verbatim from
// validatePoolSettings so the thresholds cannot diverge; the exact strings
// (including the "≥" character and trailing full stops) are asserted below,
// not just truthiness, because the Settings screen renders this string
// directly to the operator.
describe('poolSettingsError', () => {
  it.each([FORMAT_KNOCKOUT, FORMAT_LEAGUE, FORMAT_SWISS, '', undefined])(
    'is always null for a non-mixed format (%j), even with a 0/NaN pool size',
    (format) => {
      expect(poolSettingsError(format, 0, 0)).toBeNull();
      expect(poolSettingsError(format, NaN, NaN)).toBeNull();
      expect(poolSettingsError(format, 3, 1)).toBeNull();
    }
  );

  // The reproduced bug: normalizePoolConfig zeroes poolSize/poolWinners on
  // every stored league/knockout competition, and flipping such a
  // competition to "mixed" on the Settings screen leaves that 0/0 staged
  // with nothing else touched.
  it('flags the stored-league/knockout 0/0 combination once format is mixed', () => {
    expect(poolSettingsError(FORMAT_MIXED, 0, 0)).toBe('Players per pool must be a whole number ≥ 3.');
  });

  it('flags a NaN poolSize (cleared input) as the players message', () => {
    expect(poolSettingsError(FORMAT_MIXED, NaN, 2)).toBe('Players per pool must be a whole number ≥ 3.');
  });

  it('flags a fractional poolSize as the players message', () => {
    expect(poolSettingsError(FORMAT_MIXED, 2.5, 1)).toBe('Players per pool must be a whole number ≥ 3.');
  });

  it('flags poolSize 2 (one below the floor) as the players message', () => {
    expect(poolSettingsError(FORMAT_MIXED, 2, 1)).toBe('Players per pool must be a whole number ≥ 3.');
  });

  it('flags an invalid winners count (0 or NaN) once poolSize is valid', () => {
    expect(poolSettingsError(FORMAT_MIXED, 3, 0)).toBe('Winners per pool must be a whole number ≥ 1.');
    expect(poolSettingsError(FORMAT_MIXED, 3, NaN)).toBe('Winners per pool must be a whole number ≥ 1.');
  });

  it('is null for the smallest legal combination', () => {
    expect(poolSettingsError(FORMAT_MIXED, 3, 1)).toBeNull();
  });
});

// bc-symm-settings-create-parity: pendingConfigClears is the other half of
// the fix for a reproduced round-trip defect. Settings used to STAGE
// normalizeConfigForFormat/normalizeConfigForKind's result straight into
// `local` on the Format/Kind pill tap itself. On a stored mixed competition
// (poolSize: 4, poolWinners: 2), tapping "Knockout only" staged
// poolSize/poolWinners: 0; tapping "Pools + Knockout" to go straight back
// was a no-op for those fields going back INTO mixed (normalizeConfigForFormat
// only clears them on the way OUT of "mixed"), so two taps that cancelled out
// on `format` destroyed the operator's real values with no way to recover
// them, and Save was blocked by poolSettingsError. Operator ruling: a config
// change must never quietly overwrite or delete the operator's data -- it
// must surface what will happen and let the operator decide.
// pendingConfigClears computes what a save WOULD clear, without staging
// anything, so the Settings screen can show it before the operator commits.
describe('pendingConfigClears', () => {
  it('returns [] when neither format nor kind changed, even with values present that a stray call would flag', () => {
    const cfg = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 4, poolWinners: 2, extraQualifiers: '' };
    expect(pendingConfigClears(cfg, { ...cfg })).toEqual([]);
  });

  // The property that matters is not that the two functions agree today but
  // that they CANNOT disagree: pendingConfigClears reports the diff against
  // shapeConfigForSave's output, which is the value the payload boundary
  // sends. Swept over the shapes that reach the normalizers at all, plus the
  // stored-but-inconsistent ones (team + withZekkenName, individual +
  // teamSize) that an unscoped normalization used to clear without a word.
  it('names every field shapeConfigForSave will actually change, on every staged shape', () => {
    const stores = [
      { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 4, poolWinners: 2, extraQualifiers: 'larger-pools', teamSize: 0, teamMatchType: 'fixed', engi: true, withZekkenName: true },
      { format: FORMAT_MIXED, kind: KIND_TEAM, poolSize: 4, poolWinners: 2, extraQualifiers: '', teamSize: 3, teamMatchType: 'kachinuki', engi: false, withZekkenName: true },
      { format: FORMAT_KNOCKOUT, kind: KIND_INDIVIDUAL, poolSize: 0, poolWinners: 0, extraQualifiers: '', teamSize: 3, teamMatchType: 'fixed', engi: false, withZekkenName: false },
    ];
    for (const stored of stores) {
      for (const format of [FORMAT_KNOCKOUT, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS]) {
        for (const kind of [KIND_INDIVIDUAL, KIND_TEAM]) {
          const staged = { ...stored, format, kind };
          const sent = shapeConfigForSave(stored, staged);
          const named = new Set(pendingConfigClears(stored, staged).map((x) => x.key));
          for (const key of Object.keys(sent)) {
            if (sent[key] === staged[key]) continue;
            // Only a MEANINGFUL staged value is reportable: a field already
            // at 0/""/false had nothing in it to lose.
            if (!staged[key]) continue;
            expect(
              named.has(key),
              `save changes ${key} (${JSON.stringify(staged[key])} -> ${JSON.stringify(sent[key])}) for ${kind}/${format} but the notice does not name it`
            ).toBe(true);
          }
        }
      }
    }
  });

  it('mixed(poolSize 4, poolWinners 2) -> knockout reports both keys with their "from" values', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 4, poolWinners: 2, extraQualifiers: '' };
    const staged = { ...stored, format: FORMAT_KNOCKOUT };
    expect(pendingConfigClears(stored, staged)).toEqual([
      { key: 'poolSize', from: 4 },
      { key: 'poolWinners', from: 2 },
    ]);
  });

  it('a field already 0/""/false is NOT reported: it had nothing in it to lose', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 0, poolWinners: 0, extraQualifiers: '' };
    const staged = { ...stored, format: FORMAT_KNOCKOUT };
    expect(pendingConfigClears(stored, staged)).toEqual([]);
  });

  it('team(teamSize 5) -> individual reports teamSize', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_TEAM, teamSize: 5, teamMatchType: 'fixed', engi: false, withZekkenName: false };
    const staged = { ...stored, kind: KIND_INDIVIDUAL };
    expect(pendingConfigClears(stored, staged)).toEqual([{ key: 'teamSize', from: 5 }]);
  });

  it('individual -> team reports engi/withZekkenName only when they were truthy', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, teamSize: 0, teamMatchType: 'fixed', engi: true, withZekkenName: true };
    const staged = { ...stored, kind: KIND_TEAM };
    expect(pendingConfigClears(stored, staged)).toEqual([
      { key: 'engi', from: true },
      { key: 'withZekkenName', from: true },
    ]);
  });

  it('individual -> team reports neither engi nor withZekkenName when both were already false', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, teamSize: 0, teamMatchType: 'fixed', engi: false, withZekkenName: false };
    const staged = { ...stored, kind: KIND_TEAM };
    expect(pendingConfigClears(stored, staged)).toEqual([]);
  });

  it('extraQualifiers reported when leaving mixed with a non-standard value', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 4, poolWinners: 2, extraQualifiers: 'larger-pools' };
    const staged = { ...stored, format: FORMAT_LEAGUE };
    expect(pendingConfigClears(stored, staged)).toEqual(
      expect.arrayContaining([{ key: 'extraQualifiers', from: 'larger-pools' }])
    );
  });
});

// bc-symm-settings-create-parity, review round: resolveTeamSize exists
// because the guard it replaces was DEAD. saveNow read
// `safeNonNegInt(shaped.teamSize, latestC.teamSize)`, but
// normalizeConfigForKind rewrites teamSize on BOTH branches, so its output
// is always a finite integer and the fallback could never fire. The visible
// consequence: clearing the Team size input on a stored 3 saved 5, while the
// input's own comment promised the last-saved value.
// teamSizeError is the visible half of the same rule resolveTeamSize
// enforces silently. resolveTeamSize decides what the PAYLOAD carries;
// this decides whether a save is offered at all. Both screens read it --
// the create form at submit (where it replaced two inline branches) and
// the settings screen as an inline error plus a change-scoped Save gate,
// which it had NO equivalent of: an out-of-range team size was discarded
// by resolveTeamSize and reported as "✓ Saved".
describe('teamSizeError', () => {
  it('is null for every kind the field does not apply to, whatever the number', () => {
    for (const kind of [KIND_INDIVIDUAL, '', undefined]) {
      for (const size of [0, 1, NaN, 5, -3]) {
        expect(teamSizeError(kind, size, 10), `kind ${JSON.stringify(kind)} / size ${size}`).toBeNull();
      }
    }
  });

  it('accepts the legal domain for a team competition', () => {
    for (const size of [MIN_TEAM_SIZE, 3, 5, 10]) {
      expect(teamSizeError(KIND_TEAM, size, 10)).toBeNull();
    }
  });

  it('rejects a cleared, fractional, zero, one or negative size, naming the field', () => {
    for (const size of [NaN, undefined, null, 1.5, 0, 1, -2]) {
      const err = teamSizeError(KIND_TEAM, size, 10);
      expect(err, `size ${JSON.stringify(size)} must be refused`).toBeTruthy();
      expect(err, 'the message must name the control the operator is looking at').toContain(LABEL_TEAM_SIZE);
    }
  });

  // teamSize 1 is the one an operator can reach by typing and the one
  // ValidateCompetitionTeamSize (state/models.go) rejects outright, on the
  // grounds that TeamSize > 0 makes it read as a team elsewhere in the
  // engine. It must never leave the client.
  it('rejects teamSize 1 specifically, the value the server refuses unconditionally', () => {
    expect(teamSizeError(KIND_TEAM, 1, 10)).toBeTruthy();
  });

  // The ceiling is a parameter because it belongs to the scoring UI's
  // position list (MAX_TEAM_SIZE, admin_helpers.jsx), not to this module.
  // Omitting it checks the floor alone rather than silently inventing a
  // second copy of that number here.
  it('checks the ceiling only when the caller supplies one', () => {
    expect(teamSizeError(KIND_TEAM, 99, 10)).toBeTruthy();
    expect(teamSizeError(KIND_TEAM, 99, undefined)).toBeNull();
  });
});

describe('resolveTeamSize', () => {
  it('keeps a usable staged value', () => {
    expect(resolveTeamSize(2, 5)).toBe(2);
    expect(resolveTeamSize(7, 5)).toBe(7);
  });

  it('falls back to the stored value for every unusable staged value', () => {
    // NaN is what decideNumericUpdate stores for a CLEARED input; 0 and 1
    // are what it stores for a typed value below the field's own min (it
    // stages what was typed so the input can show it, and reports
    // shouldSave: false). All three mean "no usable team size supplied".
    for (const staged of [NaN, 0, 1, -3, 2.5, undefined, null]) {
      expect(resolveTeamSize(staged, 3)).toBe(3);
    }
  });

  it('resolves to 0 when the stored value is unusable too, so the kind normalizer decides', () => {
    // An individual competition stores teamSize 0. Flipping it to team
    // leaves nothing to fall back to, and normalizeConfigForKind's floor
    // then supplies DEFAULT_TEAM_SIZE -- which is the create form's rule,
    // not a value invented here.
    expect(resolveTeamSize(NaN, 0)).toBe(0);
    expect(resolveTeamSize(1, undefined)).toBe(0);
    expect(normalizeConfigForKind({ kind: KIND_TEAM, teamSize: resolveTeamSize(NaN, 0) }).teamSize).toBe(DEFAULT_TEAM_SIZE);
  });

  it('never defeats the deliberate 0 a team -> individual flip stages', () => {
    // The 0 comes from normalizeConfigForKind's kind branch, which runs
    // AFTER this function, so resolving first cannot re-inflate it.
    const shaped = normalizeConfigForKind({ kind: KIND_INDIVIDUAL, teamSize: resolveTeamSize(NaN, 5) });
    expect(shaped.teamSize).toBe(0);
  });
});

// The Swiss twin of poolSettingsError, shared for the same reason: the
// settings screen's Format editor makes "swiss" reachable for a stored
// competition whose swissRounds is 0, and nothing there blocked the save.
describe('swissSettingsError', () => {
  it('is null for every non-swiss format, whatever the round count', () => {
    for (const format of [FORMAT_KNOCKOUT, FORMAT_MIXED, FORMAT_LEAGUE, '', undefined]) {
      for (const rounds of [NaN, 0, -1, 4]) {
        expect(swissSettingsError(format, rounds)).toBeNull();
      }
    }
  });

  it('rejects the values the server rejects, and names the field it is about', () => {
    for (const rounds of [NaN, 0, -3, 4.5, undefined, null]) {
      const err = swissSettingsError(FORMAT_SWISS, rounds);
      expect(err).toBeTruthy();
      // The copy has to name the field, because the settings screen prints
      // it directly rather than behind a fixed short label.
      expect(err).toContain(LABEL_SWISS_ROUNDS);
    }
  });

  it('accepts the floor and above', () => {
    expect(swissSettingsError(FORMAT_SWISS, MIN_SWISS_ROUNDS)).toBeNull();
    expect(swissSettingsError(FORMAT_SWISS, 6)).toBeNull();
  });

  it('mirrors validateSwissConfig, whose floor is 1', () => {
    expect(MIN_SWISS_ROUNDS).toBe(1);
  });
});

// resolvePoolSizeMode: the stored field has a third state the pills do not.
// Nothing on the server fills PoolSizeMode in on POST, so a competition
// authored outside the SPA sits on disk with "" -- and every consumer reads
// that as minimum sizing via `isMax := PoolSizeMode == "max"`.
describe('resolvePoolSizeMode', () => {
  it('resolves the unset/legacy/unknown value to minimum, matching the engine', () => {
    for (const stored of ['', undefined, null, 'MAX', 'maximum', 'nonsense']) {
      expect(resolvePoolSizeMode(stored)).toBe(POOL_SIZE_MODE_MIN);
    }
  });

  it('passes the two canonical values through', () => {
    expect(resolvePoolSizeMode(POOL_SIZE_MODE_MAX)).toBe(POOL_SIZE_MODE_MAX);
    expect(resolvePoolSizeMode(POOL_SIZE_MODE_MIN)).toBe(POOL_SIZE_MODE_MIN);
  });

  it('always lights exactly one of the two pills', () => {
    for (const stored of ['', undefined, POOL_SIZE_MODE_MAX, POOL_SIZE_MODE_MIN, 'nonsense']) {
      const resolved = resolvePoolSizeMode(stored);
      const lit = [POOL_SIZE_MODE_MAX, POOL_SIZE_MODE_MIN].filter((v) => v === resolved);
      expect(lit).toHaveLength(1);
    }
  });

  it('mirrors the wire values byte-for-byte', () => {
    expect(POOL_SIZE_MODE_MAX).toBe('max');
    expect(POOL_SIZE_MODE_MIN).toBe('min');
  });
});

// shapeConfigForSave scopes the two normalizers to a staged format/kind
// change. Unscoped, a save that touched only the start time forced
// withZekkenName false on a stored team competition that carried it -- which
// is in the PUT's output-affecting set, so at draw-ready the operator's own
// edit died on a 409 about a change they never made, on every attempt, with
// the zekken checkbox disabled at that status.
describe('shapeConfigForSave', () => {
  const TEAM_WITH_ZEKKEN = {
    format: FORMAT_MIXED, kind: KIND_TEAM, teamSize: 5, teamMatchType: 'fixed',
    engi: false, withZekkenName: true, poolSize: 4, poolWinners: 2, extraQualifiers: '',
  };

  it('leaves a stored-but-inconsistent record untouched when no flip is staged', () => {
    const staged = { ...TEAM_WITH_ZEKKEN, startTime: '10:00' };
    expect(shapeConfigForSave(TEAM_WITH_ZEKKEN, staged).withZekkenName).toBe(true);
    expect(shapeConfigForSave(TEAM_WITH_ZEKKEN, staged)).toEqual(staged);
  });

  it('still normalizes when a kind change IS staged', () => {
    const staged = { ...TEAM_WITH_ZEKKEN, kind: KIND_INDIVIDUAL };
    const sent = shapeConfigForSave(TEAM_WITH_ZEKKEN, staged);
    expect(sent.teamSize).toBe(0);
    expect(sent.teamMatchType).toBe('fixed');
  });

  it('still normalizes when a format change IS staged', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_INDIVIDUAL, poolSize: 4, poolWinners: 2, extraQualifiers: 'larger-pools' };
    const sent = shapeConfigForSave(stored, { ...stored, format: FORMAT_KNOCKOUT });
    expect(sent.poolSize).toBe(0);
    expect(sent.poolWinners).toBe(0);
    expect(sent.extraQualifiers).toBe('');
  });

  it('returns a copy, never the staged object itself', () => {
    const staged = { ...TEAM_WITH_ZEKKEN };
    expect(shapeConfigForSave(TEAM_WITH_ZEKKEN, staged)).not.toBe(staged);
  });

  it('configShapeChangeStaged is the gate, and reads both fields', () => {
    const stored = { format: FORMAT_MIXED, kind: KIND_TEAM };
    expect(configShapeChangeStaged(stored, { ...stored })).toBe(false);
    expect(configShapeChangeStaged(stored, { ...stored, format: FORMAT_LEAGUE })).toBe(true);
    expect(configShapeChangeStaged(stored, { ...stored, kind: KIND_INDIVIDUAL })).toBe(true);
  });

  // The two normalizers are gated INDEPENDENTLY, which is a stronger
  // property than "gated at all" and the one a review round found missing.
  // Running both whenever EITHER field moved reproduced the very lockout
  // the scoping exists to prevent, through the other door: a format-only
  // save on a stored team competition ran the KIND normalizer too and
  // forced withZekkenName / engi false on a kind that had not moved, with
  // the zekken checkbox disabled behind teamFieldsVisible so nothing on
  // screen could put it back. Reverting shapeConfigForSave to
  // `if (configShapeChangeStaged(...)) return normalizeConfigForKind(
  // normalizeConfigForFormat(staged))` reddens both subtests below.
  it('a format-only change does not run the kind normalizer', () => {
    const staged = { ...TEAM_WITH_ZEKKEN, format: FORMAT_LEAGUE };
    const sent = shapeConfigForSave(TEAM_WITH_ZEKKEN, staged);

    // The format half DID run.
    expect(sent.poolSize, 'league has no pool phase to size').toBe(0);
    expect(sent.poolWinners).toBe(0);

    // The kind half did NOT: kind never moved, so nothing it owns may be
    // touched by this save.
    expect(sent.withZekkenName, 'a format change must not clear a zekken setting the operator did not touch').toBe(true);
    expect(sent.teamSize, 'nor rewrite the team size').toBe(5);
    expect(sent.engi).toBe(false);
  });

  // The mirror case, and it needs a LEAGUE fixture to discriminate: with a
  // stored "mixed", normalizeConfigForFormat is a no-op, so an unscoped
  // call looks identical to a scoped one. A stored league carrying a
  // non-zero poolSize is not a contrived shape -- /api/tournament/import
  // defaults PoolSize on every format and never runs normalizePoolConfig,
  // so an imported league sits on disk with exactly this pairing.
  it('a kind-only change does not run the format normalizer', () => {
    const stored = {
      format: FORMAT_LEAGUE, kind: KIND_TEAM, teamSize: 5, teamMatchType: 'fixed',
      engi: false, withZekkenName: false, poolSize: 4, poolWinners: 2,
      extraQualifiers: '',
    };
    const sent = shapeConfigForSave(stored, { ...stored, kind: KIND_INDIVIDUAL });

    // The kind half DID run.
    expect(sent.teamSize).toBe(0);
    expect(sent.teamMatchType).toBe('fixed');

    // The format half did NOT: format never moved, so a save about kind may
    // not quietly rewrite fields format owns.
    expect(sent.poolSize, 'a kind change must not zero a stored pool size').toBe(4);
    expect(sent.poolWinners).toBe(2);
  });

  // pendingConfigClears derives from shapeConfigForSave, so the notice
  // inherits the scoping rather than restating it -- which is what stops it
  // announcing a clear the save will not make (or, worse, staying silent
  // about one it will).
  it('the pending-clears notice follows the same scoping, naming no kind field on a format-only change', () => {
    const staged = { ...TEAM_WITH_ZEKKEN, format: FORMAT_LEAGUE };
    const named = pendingConfigClears(TEAM_WITH_ZEKKEN, staged).map((e) => e.key);
    expect(named, 'the notice must not blame a format change for clearing a kind-owned field').not.toContain('withZekkenName');
    expect(named, 'nor for a team size the save leaves alone').not.toContain('teamSize');
    expect(named, 'it should still name what the format change really does clear').toEqual(
      expect.arrayContaining(['poolSize', 'poolWinners'])
    );
  });
});
