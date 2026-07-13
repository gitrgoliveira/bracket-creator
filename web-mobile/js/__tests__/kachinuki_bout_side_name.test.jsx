// resolveBoutSideName: format-aware priority for a sub-bout side name.
// Regression guard for the UAT bug where the modal rewrote the server's
// winner-stays pairings: playerNamesForBout resolved lineup-position
// FIRST, so the engine's appended bout 5 "Ryu Shiro vs Tora Goro"
// persisted as "Ryu Goro vs Tora Goro" (taisho vs taisho). Kachinuki
// numbered bouts must be server-bout-log first; the lineup only seeds
// the bootstrapped bout 1. Fixed-format matches and the daihyosen row
// keep lineup-first.

import { describe, it, expect } from 'vitest';
import { resolveBoutSideName } from '../admin_scoring_team.jsx';

describe('resolveBoutSideName', () => {
  it('kachinuki bout: the existing server name wins over the lineup position', () => {
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: false,
      existingName: 'Ryu Shiro', lineupName: 'Ryu Goro',
    })).toBe('Ryu Shiro');
  });

  it('kachinuki bout: falls back to the lineup only when the server has no name (bootstrapped bout 1)', () => {
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: false,
      existingName: '', lineupName: 'Ryu Senpo',
    })).toBe('Ryu Senpo');
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: false,
      existingName: undefined, lineupName: 'Ryu Senpo',
    })).toBe('Ryu Senpo');
  });

  it('fixed-format bout: lineup-first (lineups are editable and drive fixed pairings)', () => {
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: 'Old Name', lineupName: 'New Pick',
    })).toBe('New Pick');
  });

  it('fixed-format bout: falls back to the recorded name when no lineup pick exists', () => {
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: 'Recorded', lineupName: '',
    })).toBe('Recorded');
  });

  it('daihyosen row is lineup-first even in a kachinuki match', () => {
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: true,
      existingName: 'Ryu', lineupName: 'Rep Pick',
    })).toBe('Rep Pick');
  });

  it('returns empty string when neither source has a name', () => {
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: false,
      existingName: '', lineupName: '',
    })).toBe('');
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: undefined, lineupName: undefined,
    })).toBe('');
  });
});
