// Unit tests for admin_schedule_export.jsx: buildXlsxBody pure-logic helper.
//
// Purpose: verify that the POST /create body is built correctly for engi and
// naginata competitions (mp-wvba gap closure). Imports buildXlsxBody directly
// so no component mounting or fetch mocking is required.

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

  const pairs = [
    pair('Aoi', 'Haru', 'DojoA'),
    pair('Bo', 'Cho', 'DojoB'),
    pair('Dai', 'Ebi', 'DojoC'),
  ];

  it('produces 3-column roster lines for engi competitions', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    const lines = body.get('playerList').split('\n');
    expect(lines).toHaveLength(3);
    // Each line must have exactly 3 comma-separated columns.
    for (const line of lines) {
      const cols = line.split(', ');
      expect(cols).toHaveLength(3);
    }
    // First pair: member1, member2, dojo.
    expect(lines[0]).toBe('Aoi, Haru, DojoA');
    expect(lines[1]).toBe('Bo, Cho, DojoB');
  });

  it('sends engi=on in the POST body', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('engi')).toBe('on');
  });

  it('sends withZekkenName=on in the POST body when engi=true', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('withZekkenName')).toBe('on');
  });

  it('does NOT send naginata when cfg.naginata is absent', () => {
    const body = buildXlsxBody(engiCfg, 'Test', pairs);
    expect(body.get('naginata')).toBeNull();
  });
});

// ── naginata: 3rd-place param ─────────────────────────────────────────────────

describe('buildXlsxBody naginata param', () => {
  const nagCfg = {
    format: 'playoffs',
    naginata: true,
    courts: ['A'],
  };

  const four = [
    player('Alice', 'DA'),
    player('Bob', 'DB'),
    player('Charlie', 'DC'),
    player('Dave', 'DD'),
  ];

  it('sends naginata=on in the POST body when cfg.naginata is true', () => {
    const body = buildXlsxBody(nagCfg, 'Test', four);
    expect(body.get('naginata')).toBe('on');
  });

  it('does NOT send naginata when cfg.naginata is absent', () => {
    const body = buildXlsxBody({ format: 'playoffs', courts: ['A'] }, 'Test', four);
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
