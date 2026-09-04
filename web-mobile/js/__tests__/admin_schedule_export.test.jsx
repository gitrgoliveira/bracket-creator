// Unit tests for admin_schedule_export.jsx: buildXlsxBody pure-logic helper.
//
// Purpose: verify that the POST /create body is built correctly for engi and
// naginata competitions (mp-wvba gap closure), and that the 3rd-place field
// it sends (thirdPlaceMatch, bc-3rdp gap closure) follows
// effectiveTwoThirdPlaces rather than the raw naginata flag. Imports
// buildXlsxBody directly so no component mounting or fetch mocking is
// required.

import { describe, it, expect } from 'vitest';
import { buildXlsxBody } from '../admin_schedule_export.jsx';

// Minimal player stubs used across all tests.
const pair = (name, displayName, dojo) => ({ name, displayName, dojo });
const player = (name, dojo) => ({ name, dojo });

// ── engi: 3-column roster lines ──────────────────────────────────────────────

describe('buildXlsxBody engi roster lines', () => {
  const engiCfg = {
    format: 'pools',
    engi: true,
    poolSize: 3,
    poolWinners: 2,
    courts: ['A'],
  };

  // Pairs carry both member names combined in the name field.
  const pairs = [
    player('Aoi - Haru', 'DojoA'),
    player('Bo - Cho', 'DojoB'),
    player('Dai - Ebi', 'DojoC'),
  ];

  it('produces standard 2-column roster lines for engi competitions', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    const lines = body.get('playerList').split('\n');
    expect(lines).toHaveLength(3);
    // Combined pair name + dojo: same layout as any non-zekken competition.
    expect(lines[0]).toBe('Aoi - Haru, DojoA');
    expect(lines[1]).toBe('Bo - Cho, DojoB');
  });

  it('sends engi=on in the POST body', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('engi')).toBe('on');
  });

  it('does NOT force withZekkenName for engi', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('withZekkenName')).toBeNull();
  });

  it('does NOT send thirdPlaceMatch when cfg.naginata is absent (kendo default: joint 3rds)', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('thirdPlaceMatch')).toBeNull();
  });
});

// ── thirdPlaceMatch: 3rd-place param, driven by effectiveTwoThirdPlaces ──────
//
// bc-3rdp gap closure: the exported blank workbook must carry a bronze
// (3rd-place) match exactly when the competition does NOT award joint 3rd
// places (RequiresSingleThirdPlace, the negation of effectiveTwoThirdPlaces),
// not merely when cfg.naginata happens to be true. The two cross cases below
// are the whole point: a NON-naginata competition that opted into a single
// 3rd must still get the block, and a NAGINATA competition that opted INTO
// joint 3rds must NOT get it.

describe('buildXlsxBody thirdPlaceMatch param', () => {
  const four = [
    player('Alice', 'DA'),
    player('Bob', 'DB'),
    player('Charlie', 'DC'),
    player('Dave', 'DD'),
  ];

  it('sends thirdPlaceMatch=on for a naginata competition with no explicit twoThirdPlaces (legacy fallback: !naginata)', () => {
    const body = buildXlsxBody({ format: 'knockout', naginata: true, courts: ['A'] }, 'Test', four);
    expect(body.get('thirdPlaceMatch')).toBe('on');
  });

  it('does NOT send thirdPlaceMatch for a non-naginata competition with no explicit twoThirdPlaces (kendo default: joint 3rds)', () => {
    const body = buildXlsxBody({ format: 'knockout', courts: ['A'] }, 'Test', four);
    expect(body.get('thirdPlaceMatch')).toBeNull();
  });

  // Cross case 1: a NON-naginata competition that explicitly opted into a
  // single 3rd place (twoThirdPlaces: false) must still get the bronze
  // block. The old cfg.naginata check would have missed this entirely.
  it('sends thirdPlaceMatch=on for a NON-naginata competition that opted into a single 3rd place', () => {
    const body = buildXlsxBody({ format: 'knockout', naginata: false, twoThirdPlaces: false, courts: ['A'] }, 'Test', four);
    expect(body.get('thirdPlaceMatch')).toBe('on');
  });

  // Cross case 2: a NAGINATA competition that explicitly opted INTO joint
  // 3rd places (twoThirdPlaces: true) must NOT get the bronze block. The old
  // cfg.naginata check would have wrongly sent it.
  it('does NOT send thirdPlaceMatch for a NAGINATA competition that opted into joint 3rd places', () => {
    const body = buildXlsxBody({ format: 'knockout', naginata: true, twoThirdPlaces: true, courts: ['A'] }, 'Test', four);
    expect(body.get('thirdPlaceMatch')).toBeNull();
  });

  it('never sends the legacy naginata field', () => {
    const body = buildXlsxBody({ format: 'knockout', naginata: true, courts: ['A'] }, 'Test', four);
    expect(body.get('naginata')).toBeNull();
  });
});

// ── regression: non-engi roster uses 2-column layout ────────────────────────

describe('buildXlsxBody non-engi roster lines', () => {
  const cfg = {
    format: 'pools',
    poolSize: 3,
    poolWinners: 2,
    courts: ['A'],
  };

  const players = [
    player('Alice', 'DA'),
    player('Bob', 'DB'),
    player('Charlie', 'DC'),
  ];

  it('produces 2-column roster lines for non-engi competitions', () => {
    const body = buildXlsxBody(cfg, 'Test', players);
    const lines = body.get('playerList').split('\n');
    expect(lines).toHaveLength(3);
    for (const line of lines) {
      const cols = line.split(', ');
      expect(cols).toHaveLength(2);
    }
    expect(lines[0]).toBe('Alice, DA');
  });

  it('does NOT send engi in the POST body', () => {
    const body = buildXlsxBody(cfg, 'Test', players);
    expect(body.get('engi')).toBeNull();
  });

  it('does NOT send withZekkenName when neither engi nor withZekkenName is set', () => {
    const body = buildXlsxBody(cfg, 'Test', players);
    expect(body.get('withZekkenName')).toBeNull();
  });
});

// ── Fix D: cfg.courts guard (Array.isArray) ──────────────────────────────────

describe('buildXlsxBody courts guard', () => {
  const baseCfg = {
    format: 'knockout',
  };

  const four = [
    { name: 'Alice', dojo: 'DA' },
    { name: 'Bob', dojo: 'DB' },
    { name: 'Charlie', dojo: 'DC' },
    { name: 'Dave', dojo: 'DD' },
  ];

  it('uses courts=1 when cfg.courts is undefined', () => {
    const body = buildXlsxBody({ ...baseCfg }, 'Test', four);
    expect(body.get('courts')).toBe('1');
  });

  it('uses courts=1 when cfg.courts is a string (not an array)', () => {
    const body = buildXlsxBody({ ...baseCfg, courts: 'A' }, 'Test', four);
    expect(body.get('courts')).toBe('1');
  });

  it('uses the array length when cfg.courts is a valid array', () => {
    const body = buildXlsxBody({ ...baseCfg, courts: ['A', 'B'] }, 'Test', four);
    expect(body.get('courts')).toBe('2');
  });
});

// ── effectiveZekken: withZekkenName alone also triggers 3-column ─────────────

describe('buildXlsxBody effectiveZekken from withZekkenName', () => {
  const cfg = {
    format: 'pools',
    withZekkenName: true,
    poolSize: 3,
    poolWinners: 2,
    courts: ['A'],
  };

  const players = [
    pair('Yamada', 'YAMADA', 'Dojo1'),
    pair('Tanaka', 'TANAKA', 'Dojo2'),
    pair('Suzuki', 'SUZUKI', 'Dojo3'),
  ];

  it('produces 3-column roster lines when withZekkenName=true (no engi)', () => {
    const body = buildXlsxBody(cfg, 'Test', players);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe('Yamada, YAMADA, Dojo1');
  });

  it('sends withZekkenName=on but NOT engi in the POST body', () => {
    const body = buildXlsxBody(cfg, 'Test', players);
    expect(body.get('withZekkenName')).toBe('on');
    expect(body.get('engi')).toBeNull();
  });
});

// ── csvField sanitizing via roster lines (CR/LF collapse, quoting, coercion) ─

describe('buildXlsxBody roster field sanitizing', () => {
  const cfg = { format: 'pools', poolSize: 3, poolWinners: 2, courts: ['A'] };

  it('collapses an embedded carriage return to a space (server splits on newlines pre-CSV)', () => {
    const body = buildXlsxBody(cfg, 'Test', [player('Yama\rda', 'Dojo1')]);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe('Yama da, Dojo1');
  });

  it('collapses an embedded CRLF run to a single space', () => {
    const body = buildXlsxBody(cfg, 'Test', [player('Yama\r\n\nda', 'Dojo1')]);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe('Yama da, Dojo1');
  });

  it('still RFC-4180-quotes commas and double-quotes', () => {
    const body = buildXlsxBody(cfg, 'Test', [player('Yamada, Taro', 'Do"jo')]);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe('"Yamada, Taro", "Do""jo"');
  });

  it('string-coerces non-string field values', () => {
    const body = buildXlsxBody(cfg, 'Test', [player(42, 'Dojo1')]);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe('42, Dojo1');
  });

  it('coerces nullish field values to an empty field, not "undefined"/"null"', () => {
    // Copilot round: String(undefined) exported the literal text "undefined";
    // an empty field lets the server reject the missing name instead.
    const body = buildXlsxBody(cfg, 'Test', [player(undefined, 'Dojo1'), player(null, 'Dojo2')]);
    const lines = body.get('playerList').split('\n');
    expect(lines[0]).toBe(', Dojo1');
    expect(lines[1]).toBe(', Dojo2');
  });
});

// ── seeds: dojo must ride along so a same-name pair resolves (domain.SeedKey
// matches (name, dojo) composite; without dojo, /create 400s "seeded
// participant not found" for two competitors sharing a display name) ────────

describe('buildXlsxBody seeded participant dojo', () => {
  const cfg = { format: 'pools', poolSize: 3, poolWinners: 2, courts: ['A'] };

  it('includes each seeded participant\'s dojo in the seeds JSON', () => {
    const players = [
      { name: 'Alice', dojo: 'DojoA', seed: 1 },
      { name: 'Alice', dojo: 'DojoB', seed: 2 },
    ];
    const body = buildXlsxBody(cfg, 'Test', players);
    const seeds = JSON.parse(body.get('seeds'));
    expect(seeds).toEqual([
      { name: 'Alice', dojo: 'DojoA', seedRank: 1 },
      { name: 'Alice', dojo: 'DojoB', seedRank: 2 },
    ]);
  });

  it('falls back to "NA" when a seeded participant has no dojo (matches rosterLine\'s fallback)', () => {
    const players = [{ name: 'Solo', dojo: '', seed: 1 }];
    const body = buildXlsxBody(cfg, 'Test', players);
    const seeds = JSON.parse(body.get('seeds'));
    expect(seeds).toEqual([{ name: 'Solo', dojo: 'NA', seedRank: 1 }]);
  });

  it('omits the seeds param entirely when no participant is seeded', () => {
    const players = [{ name: 'Alice', dojo: 'DojoA' }];
    const body = buildXlsxBody(cfg, 'Test', players);
    expect(body.get('seeds')).toBeNull();
  });
});
