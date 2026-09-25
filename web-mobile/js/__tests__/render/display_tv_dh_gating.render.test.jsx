import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { TvDisplay } from '../../display.jsx';

// bc-cse: the TV board's Daihyosen-row gate (TvDisplay's showDH, a tied
// knockout team match with every regular bout DONE) used to hand-roll its own
// "has a result" test inline, missing the encho-only branch subBoutHasResult
// (team_default_credit.jsx) already covers: a bout fought into overtime with
// no ippon letters, winner, decision or hansoku recorded yet (an operator
// working the score sheet who has marked the period without a strike). The
// consolidated gate treats that bout as done, same as every other surface
// asking subBoutHasResult the same question.
describe('TvDisplay: Daihyosen banner gate counts an overtime bout as done', () => {
  function knockoutTeamComp(subResults) {
    return {
      id: 'cT', name: 'Team Cup', kind: 'team', teamSize: 1, withZekkenName: false,
      poolMatches: [],
      bracket: { rounds: [
        [{ id: 'm-r1-0', court: 'A', status: 'running',
           sideA: { name: 'Red Dojo' }, sideB: { name: 'White Dojo' },
           subResults }],
      ] },
    };
  }

  it('shows the Daihyosen banner once the lone regular bout is in overtime, no other marker', () => {
    const comp = knockoutTeamComp([
      { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [], encho: { periodCount: 1 } },
    ]);
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} connected />
    );
    expect(container.querySelector('[data-testid="tvd-dh-pending"]')).toBeTruthy();
  });

  it('does NOT show it while the lone regular bout carries no marker at all', () => {
    const comp = knockoutTeamComp([
      { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [] },
    ]);
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} connected />
    );
    expect(container.querySelector('[data-testid="tvd-dh-pending"]')).toBeNull();
  });
});

// The TV board asks only whether every regular bout is done; whether the
// encounter is TIED is TeamScoreboard's own test, which reads the server's
// teamResult first (teamIVPWFrom), the same source the viewer card and the
// board's own headline IV/PW read. It used to repeat the tie test from the
// bout rows alone, so wherever those disagreed with the server's figures the
// TV hid a Daihyosen row the card showed.
describe('TvDisplay: the Daihyosen row follows the same tie as every other surface', () => {
  function tiedByServer(subResults, teamResult) {
    return {
      id: 'cT', name: 'Team Cup', kind: 'team', teamSize: 1, withZekkenName: false,
      poolMatches: [],
      bracket: { rounds: [
        [{ id: 'm-r1-0', court: 'A', status: 'running',
           sideA: { name: 'Red Dojo' }, sideB: { name: 'White Dojo' },
           subResults, teamResult }],
      ] },
    };
  }

  it('shows it when the server reads the encounter tied, whatever the bout rows alone would count', () => {
    // The rows and teamResult disagree on purpose: the row credits Aka a
    // point, the server's figures are level. The server's figure is the one
    // every surface shows, so the TV follows it.
    const comp = tiedByServer(
      [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Red Dojo' }],
      { shiroIV: 0, akaIV: 0, shiroPW: 0, akaPW: 0 },
    );
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} connected />
    );
    expect(container.querySelector('[data-testid="tvd-dh-pending"]')).toBeTruthy();
  });

  it('does NOT show it when the encounter is not tied, with no server figures to read', () => {
    const comp = tiedByServer(
      [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Red Dojo' }],
      undefined,
    );
    const { container } = render(
      <TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} connected />
    );
    expect(container.querySelector('[data-testid="tvd-dh-pending"]')).toBeNull();
  });
});
