// T1: unit tests for the pure validateCreateTournament helper extracted from app.jsx.
// These run without mounting the component, so they are fast and deterministic.
import { describe, it, expect, beforeAll } from 'vitest';

const MAX_COURTS = window.MAX_COURTS || 26;

// Import the exported helper (app.jsx exports it via named export).
let validateCreateTournament;
beforeAll(async () => {
  ({ validateCreateTournament } = await import('../app.jsx'));
});

describe('validateCreateTournament', () => {
  const valid = {
    name: 'Tokyo Cup 2026',
    pass: 'secret',
    date: '12-07-2026',
    courts: 2,
    isSelfRun: false,
    locked: false,
    adminPass: '',
    maxCourts: MAX_COURTS,
  };

  it('returns "" for valid input', () => {
    expect(validateCreateTournament(valid)).toBe('');
  });

  it('returns error when name is empty', () => {
    const msg = validateCreateTournament({ ...valid, name: '' });
    expect(msg).not.toBe('');
    expect(msg.length).toBeGreaterThan(0);
  });

  it('returns error when name is whitespace only', () => {
    const msg = validateCreateTournament({ ...valid, name: '   ' });
    expect(msg).not.toBe('');
  });

  it('returns error when pass is empty', () => {
    const msg = validateCreateTournament({ ...valid, pass: '' });
    expect(msg).not.toBe('');
  });

  it('returns error when self-run in file mode and adminPass is missing', () => {
    const msg = validateCreateTournament({ ...valid, isSelfRun: true, locked: false, adminPass: '' });
    expect(msg).not.toBe('');
  });

  it('returns "" when self-run in locked mode without adminPass (locked mode has env-var hash)', () => {
    const msg = validateCreateTournament({ ...valid, isSelfRun: true, locked: true, adminPass: '' });
    expect(msg).toBe('');
  });

  it('returns error for fractional courts', () => {
    const msg = validateCreateTournament({ ...valid, courts: 2.5 });
    expect(msg).not.toBe('');
  });

  it('returns error for courts = 0', () => {
    const msg = validateCreateTournament({ ...valid, courts: 0 });
    expect(msg).not.toBe('');
  });

  it('returns error for courts > maxCourts', () => {
    const msg = validateCreateTournament({ ...valid, courts: MAX_COURTS + 1 });
    expect(msg).not.toBe('');
  });

  it('returns "" for courts exactly at maxCourts', () => {
    const msg = validateCreateTournament({ ...valid, courts: MAX_COURTS });
    expect(msg).toBe('');
  });
});
