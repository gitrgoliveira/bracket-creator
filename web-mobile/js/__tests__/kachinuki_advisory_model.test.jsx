// bc-kfnl: the advisory line's pure model (kachinuki_advisory.jsx). The queue is
// the server's, split there into each side's fighter on and the queue behind
// them (the client never decides which entry is the fighter on); this pins how
// it is shaped for display: every fighter named with their
// team-member number as the bout rows name them, "last fighter" when the lineup
// has nobody behind the fighter on, a side with no lineup showing its fighter
// alone, and no line when neither side has a lineup. The line never asks for a
// lineup.
import { describe, it, expect } from 'vitest';
import {
  kachinukiAdvisoryModel,
  kachinukiAdvisorySide,
  kachinukiFighterLabel,
  kachinukiRosterKey,
} from '../kachinuki_advisory.jsx';

const squadA = [
  { id: 'a1', index: 1, name: 'Kudo' },
  { id: 'a2', index: 2, name: '' },
  { id: 'a3', index: 3, name: 'Mori' },
];
const squadB = [
  { id: 'b1', index: 1, name: 'Taki' },
  { id: 'b2', index: 2, name: 'Ueda' },
  { id: 'b3', index: 3, name: 'Sato' },
];

const side = (lineupFound, ...remaining) => ({ lineupFound, remaining });

describe('kachinukiFighterLabel', () => {
  it('names a fighter by team-member number then current name', () => {
    expect(kachinukiFighterLabel({ squad: squadB, teamNumber: 'T1', memberId: 'b2', name: 'Old name' })).toBe('T1.2 Ueda');
  });
  it('reads a member with no name by number alone', () => {
    expect(kachinukiFighterLabel({ squad: squadA, teamNumber: 'T2', memberId: 'a2', name: '' })).toBe('T2.2');
  });
  it('reads by name alone before the draw gives the team a number', () => {
    expect(kachinukiFighterLabel({ squad: squadA, teamNumber: '', memberId: 'a1', name: 'Kudo' })).toBe('Kudo');
  });
});

describe('kachinukiAdvisorySide', () => {
  it('names the server\'s fighter on and the queue behind them, next first', () => {
    const withOn = kachinukiAdvisorySide({
      side: { ...side(true, { name: 'Ueda', memberId: 'b2' }, { name: 'Sato', memberId: 'b3' }), on: { name: 'Old name', memberId: 'b1' } },
      squad: squadB, teamNumber: 'T1',
    });
    expect(withOn.text).toBe('T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato)');
  });
  it('takes the queue as the server split it, never matching the fighter on itself', () => {
    // A queue entry with the fighter on's name is listed: deciding which entry
    // is the fighter on is the server's (engine.KachinukiRoster).
    const out = kachinukiAdvisorySide({
      side: { ...side(true, { name: 'Taki', memberId: 'x9' }), on: { name: 'Taki' } },
      squad: [], teamNumber: '',
    });
    expect(out.text).toBe('Taki on, 1 left (Taki)');
  });
  it('reads "last fighter" when nobody is left behind the fighter on', () => {
    const out = kachinukiAdvisorySide({
      side: { ...side(true), on: { name: 'Kudo', memberId: 'a1' } },
      squad: squadA, teamNumber: 'T2',
    });
    expect(out.text).toBe('T2.1 Kudo on, last fighter');
  });
  it('reads "no fighters left" once a recorded bout has taken the side\'s last fighter off', () => {
    const out = kachinukiAdvisorySide({ side: { ...side(true), on: null }, squad: squadA, teamNumber: 'T2' });
    expect(out.text).toBe('no fighters left');
  });
  it('shows only the fighter on, with no count, for a side with no lineup', () => {
    const out = kachinukiAdvisorySide({
      side: { ...side(false), on: { name: 'Kudo', memberId: 'a1' } },
      squad: squadA, teamNumber: 'T2',
    });
    expect(out.text).toBe('T2.1 Kudo on');
    expect(out.left).toBeNull();
  });
  it('says nothing for a side with no lineup and no fighter on', () => {
    expect(kachinukiAdvisorySide({ side: { ...side(false), on: null }, squad: [], teamNumber: '' })).toBeNull();
    expect(kachinukiAdvisorySide({ side: { ...side(false), on: { name: '', memberId: '' } }, squad: [], teamNumber: '' })).toBeNull();
    expect(kachinukiAdvisorySide({ side: null, squad: [], teamNumber: '' })).toBeNull();
  });
});

describe('kachinukiAdvisoryModel', () => {
  const roster = {
    sideA: { ...side(true), on: { name: 'Kudo', memberId: 'a1' } },
    sideB: { ...side(true, { name: 'Ueda', memberId: 'b2' }, { name: 'Sato', memberId: 'b3' }), on: { name: 'Taki', memberId: 'b1' } },
  };
  const numbers = { squadA, squadB, teamNumberA: 'T2', teamNumberB: 'T1' };
  it('reads Shiro (side B) first, then Aka (side A)', () => {
    const out = kachinukiAdvisoryModel({ roster, ...numbers });
    expect(out.text).toBe('Shiro: T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato) · Aka: T2.1 Kudo on, last fighter');
  });
  it('is null when neither side has a lineup', () => {
    expect(kachinukiAdvisoryModel({
      roster: { sideA: { ...side(false), on: roster.sideA.on }, sideB: { ...side(false), on: roster.sideB.on } },
      ...numbers,
    })).toBeNull();
  });
  it('is null with no roster', () => {
    expect(kachinukiAdvisoryModel({ roster: null })).toBeNull();
  });
  it('shows a side with no lineup by its fighter on alone', () => {
    const out = kachinukiAdvisoryModel({
      roster: { sideA: { ...side(false), on: roster.sideA.on }, sideB: roster.sideB },
      ...numbers,
    });
    expect(out.text).toBe('Shiro: T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato) · Aka: T2.1 Kudo on');
  });
  it('never asks for a lineup, whatever the sides hold', () => {
    const shapes = [
      { sideA: { ...side(false), on: roster.sideA.on }, sideB: roster.sideB },
      { sideA: roster.sideA, sideB: { ...side(false), on: null } },
      roster,
      { sideA: { ...side(true), on: null }, sideB: { ...side(true), on: null } },
    ];
    for (const r of shapes) {
      const out = kachinukiAdvisoryModel({ roster: r, ...numbers });
      expect(out?.text || '').not.toMatch(/lineup|incomplete|add/i);
    }
  });
});

describe('kachinukiRosterKey', () => {
  const log = [
    { position: 1, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Taki', sideBMemberId: 'b1', winner: 'Kudo', decision: 'fought' },
    { position: 2, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Ueda', sideBMemberId: 'b2' },
  ];
  const key = kachinukiRosterKey;
  it('is the same for an equal bout log in a new object', () => {
    expect(key('m1', log)).toBe(key('m1', JSON.parse(JSON.stringify(log))));
  });
  it('follows the live bout\'s result, not its points', () => {
    // A point that leaves the live bout's result as it was reads nothing.
    const pointed = log.map((s, i) => (i === 1 ? { ...s, ipponsA: ['M'] } : s));
    expect(key('m1', pointed)).toBe(key('m1', log));
    // A result on the live bout (recorded with nothing appended, or a lead the
    // running autosave wrote) reads again: the server takes its loser off.
    const won = log.map((s, i) => (i === 1 ? { ...s, winner: 'Kudo', winnerMemberId: 'a1', decision: 'fought' } : s));
    expect(key('m1', won)).not.toBe(key('m1', log));
    // More points that keep the same winner read nothing.
    const more = won.map((s, i) => (i === 1 ? { ...s, ipponsA: ['M', 'K'] } : s));
    expect(key('m1', more)).toBe(key('m1', won));
    // A draw, or a correction to the other side, reads again.
    const drawn = log.map((s, i) => (i === 1 ? { ...s, decision: 'hikiwake' } : s));
    expect(key('m1', drawn)).not.toBe(key('m1', log));
    expect(key('m1', drawn)).not.toBe(key('m1', won));
    const corrected = won.map((s, i) => (i === 1 ? { ...s, winner: 'Ueda', winnerMemberId: 'b2' } : s));
    expect(key('m1', corrected)).not.toBe(key('m1', won));
  });
  it('follows an appended bout', () => {
    const appended = [...log, { position: 3, sideA: 'Mori', sideAMemberId: 'a3', sideB: 'Ueda', sideBMemberId: 'b2' }];
    expect(key('m1', appended)).not.toBe(key('m1', log));
  });
  it('differs by match and leaves the representative bout out', () => {
    expect(key('m2', log)).not.toBe(key('m1', log));
    expect(key('m1', [...log, { position: -1, sideA: 'R', sideB: 'T' }])).toBe(key('m1', log));
  });
});
