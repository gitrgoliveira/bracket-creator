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

import { describe, it, expect } from 'vitest';
import { resolveBoutSideName } from '../lineup_resolver.jsx';

const TEAM = 'Musashi Dojo A';

describe('resolveBoutSideName: the team name is not a fighter name', () => {
  it('drops a stored fixed-order name that is the team name', () => {
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: TEAM, lineupName: '', teamName: TEAM,
    })).toBe('');
  });

  it('keeps a real fighter name that merely sits on the same row', () => {
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: 'Haruki Tanaka', lineupName: '', teamName: TEAM,
    })).toBe('Haruki Tanaka');
  });

  it('still prefers the lineup, which was always the fixed-order rule', () => {
    expect(resolveBoutSideName({
      isKachinuki: false, isDaihyosen: false,
      existingName: TEAM, lineupName: 'Ren Suzuki', teamName: TEAM,
    })).toBe('Ren Suzuki');
  });

  it('leaves kachinuki alone: the engine writes real fighter names there', () => {
    // A kachinuki row is server-first and a team name never lands in it, so
    // the guard must not reach in and blank a legitimate stored name.
    expect(resolveBoutSideName({
      isKachinuki: true, isDaihyosen: false,
      existingName: TEAM, lineupName: '', teamName: TEAM,
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
      existingName: '', lineupName: '', teamName: TEAM,
    })).toBe('');
  });
});
