import { describe, it, expect } from 'vitest';
import {
  KIND_INDIVIDUAL, KIND_TEAM,
  FORMAT_PLAYOFFS, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS,
  POOL_FORMAT_FULL, POOL_FORMAT_PARTIAL,
  LABEL_KIND, KIND_OPTIONS,
  LABEL_FORMAT, FORMAT_OPTIONS, formatHint,
  LABEL_POOL_FORMAT, POOL_FORMAT_OPTIONS, poolFormatVisible,
  LABEL_SWISS_ROUNDS, HINT_SWISS_ROUNDS, swissRoundsVisible,
  LABEL_ROUND_ROBIN, roundRobinVisible,
  LABEL_LEAGUE_TIEBREAK, HINT_LEAGUE_TIEBREAK, LEAGUE_TIEBREAK_OPTIONS, leagueTiebreakVisible,
  LABEL_POOL_DURATION, HINT_POOL_DURATION, poolDurationLabel, poolDurationHint,
  LABEL_PLAYOFF_DURATION, HINT_PLAYOFF_DURATION,
  poolDurationVisible, playoffDurationVisible,
  LABEL_TWO_THIRD_PLACES, HINT_TWO_THIRD_PLACES, twoThirdPlacesVisible,
  teamFieldsVisible, zekkenApplies, engiApplies,
  normalizeConfigForFormat,
  normalizeConfigForKind, DEFAULT_TEAM_SIZE, MIN_TEAM_SIZE,
  kindChangeBlockedReason,
  poolSettingsError,
} from '../competition_shape.jsx';

// The four format values a competition can hold. Used to sweep every
// visibility predicate across every format so a new format added to one
// surface but not this module's predicates shows up as a table gap rather
// than being silently treated as "not visible".
const ALL_FORMATS = [FORMAT_PLAYOFFS, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS, '', undefined];
const ALL_KINDS = [KIND_INDIVIDUAL, KIND_TEAM, '', undefined];

describe('wire-value constants', () => {
  it('mirror state.Competition Kind/Format/PoolFormat values byte-for-byte', () => {
    expect(KIND_INDIVIDUAL).toBe('individual');
    expect(KIND_TEAM).toBe('team');
    expect(FORMAT_PLAYOFFS).toBe('playoffs');
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
    expect(FORMAT_OPTIONS.map((o) => o.value)).toEqual([FORMAT_PLAYOFFS, FORMAT_MIXED, FORMAT_LEAGUE, FORMAT_SWISS]);
    expect(FORMAT_OPTIONS.map((o) => o.label)).toEqual(['Knockout only', 'Pools + Knockout', 'League', 'Swiss']);
  });

  const cases = [
    [FORMAT_PLAYOFFS, 'Direct single-elimination knockout.'],
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
    [FORMAT_PLAYOFFS, false],
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
    [FORMAT_PLAYOFFS, false],
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

  it('is never visible for playoffs or swiss: neither runs the PoolFormat switch\'s default branch', () => {
    expect(roundRobinVisible(FORMAT_PLAYOFFS, POOL_FORMAT_FULL)).toBe(false);
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
    expect(LABEL_PLAYOFF_DURATION).toBe('Playoff match duration');
    expect(HINT_PLAYOFF_DURATION).toBe('Estimated time per playoff/knockout match, as m:ss (e.g. 2:30).');
  });

  const labelTable = [
    [FORMAT_MIXED, 'Pool match duration'],
    [FORMAT_LEAGUE, 'Pool match duration'],
    [FORMAT_SWISS, 'Round match duration'],
    [FORMAT_PLAYOFFS, 'Pool match duration'],
    ['', 'Pool match duration'],
  ];
  it.each(labelTable)('poolDurationLabel(%j) -> %j', (format, expected) => {
    expect(poolDurationLabel(format)).toBe(expected);
  });

  const hintTable = [
    [FORMAT_MIXED, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
    [FORMAT_LEAGUE, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
    [FORMAT_SWISS, 'Estimated time per Swiss-round match, as m:ss (e.g. 2:30).'],
    [FORMAT_PLAYOFFS, 'Estimated time per pool match, as m:ss (e.g. 2:30).'],
  ];
  it.each(hintTable)('poolDurationHint(%j) -> %j', (format, expected) => {
    expect(poolDurationHint(format)).toBe(expected);
  });

  const poolVisTable = [
    [FORMAT_PLAYOFFS, false],
    [FORMAT_MIXED, true],
    [FORMAT_LEAGUE, true],
    [FORMAT_SWISS, true],
    ['', false],
    [undefined, false],
  ];
  it.each(poolVisTable)('poolDurationVisible(%j) -> %j', (format, expected) => {
    expect(poolDurationVisible(format)).toBe(expected);
  });

  const playoffVisTable = [
    [FORMAT_PLAYOFFS, true],
    [FORMAT_MIXED, true],
    [FORMAT_LEAGUE, false],
    [FORMAT_SWISS, false],
    ['', false],
    [undefined, false],
  ];
  it.each(playoffVisTable)('playoffDurationVisible(%j) -> %j', (format, expected) => {
    expect(playoffDurationVisible(format)).toBe(expected);
  });

  // mixed is the one format where BOTH duration fields render together
  // (it runs both a pool phase and a knockout phase).
  it('both duration fields are visible together only for "mixed"', () => {
    expect(poolDurationVisible(FORMAT_MIXED) && playoffDurationVisible(FORMAT_MIXED)).toBe(true);
    for (const format of [FORMAT_PLAYOFFS, FORMAT_LEAGUE, FORMAT_SWISS]) {
      expect(poolDurationVisible(format) && playoffDurationVisible(format)).toBe(false);
    }
  });
});

describe('two-thirds-places (league joint bronze)', () => {
  it('copy matches verbatim between admin_setup.jsx and admin_competition_settings.jsx (no drift found)', () => {
    expect(LABEL_TWO_THIRD_PLACES).toBe('Award two joint 3rd places');
    expect(HINT_TWO_THIRD_PLACES).toBe('When enabled, competitors genuinely tied for 3rd share bronze (standard kendo convention). Leave off for naginata, which awards a single 3rd place.');
  });

  // Gated on format alone (not kind): an individual league can still award
  // joint bronze under the kendo convention.
  const table = [];
  for (const format of ALL_FORMATS) {
    table.push([format, format === FORMAT_LEAGUE]);
  }
  it.each(table)('twoThirdPlacesVisible(%j) -> %j', (format, expected) => {
    expect(twoThirdPlacesVisible(format)).toBe(expected);
  });
});

describe('kind-gated fields', () => {
  const teamTable = [];
  const zekkenTable = [];
  const engiTable = [];
  for (const kind of ALL_KINDS) {
    teamTable.push([kind, kind === KIND_TEAM]);
    zekkenTable.push([kind, kind === KIND_INDIVIDUAL]);
    engiTable.push([kind, kind === KIND_INDIVIDUAL]);
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
  // visible" and "zekken/engi applies" at once, for every kind value this
  // module recognises (not for the "" / undefined stragglers, which are
  // neither).
  it('team-only and individual-only fields never overlap for a real kind value', () => {
    for (const kind of [KIND_INDIVIDUAL, KIND_TEAM]) {
      expect(teamFieldsVisible(kind) && zekkenApplies(kind)).toBe(false);
      expect(teamFieldsVisible(kind) && engiApplies(kind)).toBe(false);
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

  it('playoffs zeroes poolSize/poolWinners and clears extraQualifiers', () => {
    const cfg = { format: FORMAT_PLAYOFFS, poolSize: 3, poolWinners: 1, extraQualifiers: 'larger-pools' };
    expect(normalizeConfigForFormat(cfg)).toEqual({ format: FORMAT_PLAYOFFS, poolSize: 0, poolWinners: 0, extraQualifiers: '' });
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
  it.each([FORMAT_PLAYOFFS, FORMAT_LEAGUE, FORMAT_SWISS, '', undefined])(
    'is always null for a non-mixed format (%j), even with a 0/NaN pool size',
    (format) => {
      expect(poolSettingsError(format, 0, 0)).toBeNull();
      expect(poolSettingsError(format, NaN, NaN)).toBeNull();
      expect(poolSettingsError(format, 3, 1)).toBeNull();
    }
  );

  // The reproduced bug: normalizePoolConfig zeroes poolSize/poolWinners on
  // every stored league/playoffs competition, and flipping such a
  // competition to "mixed" on the Settings screen leaves that 0/0 staged
  // with nothing else touched.
  it('flags the stored-league/playoffs 0/0 combination once format is mixed', () => {
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
