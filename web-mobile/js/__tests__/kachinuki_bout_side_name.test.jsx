// resolveBoutSideName (lineup_resolver.jsx): format-aware priority for a
// sub-bout side name. Regression guard for the UAT bug where the modal
// rewrote the server's winner-stays pairings: playerNamesForBout resolved
// lineup-position FIRST, so the engine's appended bout 5 "Ryu Shiro vs
// Tora Goro" persisted as "Ryu Goro vs Tora Goro" (taisho vs taisho).
// Kachinuki numbered bouts must be server-bout-log first; the lineup only
// seeds the bootstrapped bout 1. Fixed-format matches and the daihyosen
// row keep lineup-first.

import { describe, it, expect } from 'vitest';
import { resolveBoutSideName } from '../lineup_resolver.jsx';

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

  // bc-dnst: a fixed-order bout row never reports the TEAM's name as the
  // fighter's name.
  //
  // The trap, found in a browser and not by any test: a fixed-order bout
  // settles at the match level, so buildPatch deliberately writes the team's
  // own name into every row's sideA/sideB, and standings depend on that. It is
  // therefore NOT a fighter name. resolveBoutSideName used to fall back to it,
  // so a team match with NO lineup came back with every position, including
  // ones nobody had scored, named after the team: on the score sheet, and in
  // the stored bout log that the viewer, the court display and the export read.
  // Setting a lineup hid it, because the lineup name wins.
  //
  // The TV board had guarded this for years in its own subSideName ("filter out
  // match-level team names"), which is why the bug was invisible there and live
  // in the editor and the streaming overlay. The rule now lives in the resolver
  // the three share.
  const TEAM = 'Musashi Dojo A';
  const OTHER = 'Kenshinkan A';

  describe('the team name is not a fighter name', () => {
    it('drops a stored fixed-order name that is the team name', () => {
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: TEAM, lineupName: '', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe('');
    });

    it('keeps a real fighter name that merely sits on the same row', () => {
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: 'Haruki Tanaka', lineupName: '', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe('Haruki Tanaka');
    });

    it('still prefers the lineup, which was always the fixed-order rule', () => {
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: TEAM, lineupName: 'Ren Suzuki', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe('Ren Suzuki');
    });

    it('leaves kachinuki alone: the engine writes real fighter names there', () => {
      // A kachinuki row is server-first and a team name never lands in it, so
      // the guard must not reach in and blank a legitimate stored name.
      expect(resolveBoutSideName({
        isKachinuki: true, isDaihyosen: false,
        existingName: TEAM, lineupName: '', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe(TEAM);
    });

    it('is inert when the caller passes no team name', () => {
      // Every caller threads one today, but the argument is optional and an
      // omitted team name must not change the answer for anyone.
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: TEAM, lineupName: '',
      })).toBe(TEAM);
    });

    it('does not blank an empty stored name into something else', () => {
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: '', lineupName: '', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe('');
    });

    it('drops the OTHER team name too, which is the check the TV board carried', () => {
      // A row whose sides are crossed by hand-edited data still holds a team
      // name where a fighter goes. Narrowing to the row's own side would have
      // dropped this, which the board had covered before the rule moved here.
      expect(resolveBoutSideName({
        isKachinuki: false, isDaihyosen: false,
        existingName: OTHER, lineupName: '', teamNameA: TEAM, teamNameB: OTHER,
      })).toBe('');
    });
  });
});
