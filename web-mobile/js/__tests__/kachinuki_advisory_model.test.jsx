// bc-kfnl: the advisory line's pure model (kachinuki_advisory.jsx). The queue is
// the server's; this pins how it is shaped for display: the fighter on removed
// (member id first, name only when no id), every fighter named with their
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
  it('removes the fighter on by member id and lists the rest in queue order', () => {
    const out = kachinukiAdvisorySide({
      side: side(true, { name: 'Taki', memberId: 'b1' }, { name: 'Ueda', memberId: 'b2' }, { name: 'Sato', memberId: 'b3' }),
      on: { name: 'Someone else', memberId: 'b1' },
      squad: squadB, teamNumber: 'T1',
    });
    expect(out.text).toBe('T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato)');
  });
  it('removes the fighter on by name when neither carries an id', () => {
    const out = kachinukiAdvisorySide({
      side: side(true, { name: 'Taki' }, { name: 'Ueda' }),
      on: { name: 'Taki' },
      squad: [], teamNumber: '',
    });
    expect(out.text).toBe('Taki on, 1 left (Ueda)');
  });
  it('does not match by name when one of the two carries an id', () => {
    const out = kachinukiAdvisorySide({
      side: side(true, { name: 'Taki', memberId: 'x9' }),
      on: { name: 'Taki' },
      squad: [], teamNumber: '',
    });
    expect(out.left).toHaveLength(1);
  });
  it('reads "last fighter" when nobody is left behind the fighter on', () => {
    const out = kachinukiAdvisorySide({
      side: side(true, { name: 'Kudo', memberId: 'a1' }),
      on: { name: 'Kudo', memberId: 'a1' },
      squad: squadA, teamNumber: 'T2',
    });
    expect(out.text).toBe('T2.1 Kudo on, last fighter');
  });
  it('shows only the fighter on, with no count, for a side with no lineup', () => {
    const out = kachinukiAdvisorySide({
      side: side(false),
      on: { name: 'Kudo', memberId: 'a1' },
      squad: squadA, teamNumber: 'T2',
    });
    expect(out.text).toBe('T2.1 Kudo on');
    expect(out.left).toBeNull();
  });
  it('says nothing for a side with no lineup and no fighter on', () => {
    expect(kachinukiAdvisorySide({ side: side(false), on: { name: '', memberId: '' }, squad: [], teamNumber: '' })).toBeNull();
  });
});

describe('kachinukiAdvisoryModel', () => {
  const roster = {
    sideA: side(true, { name: 'Kudo', memberId: 'a1' }),
    sideB: side(true, { name: 'Taki', memberId: 'b1' }, { name: 'Ueda', memberId: 'b2' }, { name: 'Sato', memberId: 'b3' }),
  };
  it('reads Shiro (side B) first, then Aka (side A)', () => {
    const out = kachinukiAdvisoryModel({
      roster,
      onA: { name: 'Kudo', memberId: 'a1' },
      onB: { name: 'Taki', memberId: 'b1' },
      squadA, squadB, teamNumberA: 'T2', teamNumberB: 'T1',
    });
    expect(out.text).toBe('Shiro: T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato) · Aka: T2.1 Kudo on, last fighter');
  });
  it('is null when neither side has a lineup', () => {
    expect(kachinukiAdvisoryModel({
      roster: { sideA: side(false), sideB: side(false) },
      onA: { name: 'Kudo', memberId: 'a1' }, onB: { name: 'Taki', memberId: 'b1' },
      squadA, squadB, teamNumberA: 'T2', teamNumberB: 'T1',
    })).toBeNull();
  });
  it('is null with no roster', () => {
    expect(kachinukiAdvisoryModel({ roster: null })).toBeNull();
  });
  it('shows a side with no lineup by its fighter on alone', () => {
    const out = kachinukiAdvisoryModel({
      roster: { sideA: side(false), sideB: roster.sideB },
      onA: { name: 'Kudo', memberId: 'a1' }, onB: { name: 'Taki', memberId: 'b1' },
      squadA, squadB, teamNumberA: 'T2', teamNumberB: 'T1',
    });
    expect(out.text).toBe('Shiro: T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato) · Aka: T2.1 Kudo on');
  });
  it('never asks for a lineup, whatever the sides hold', () => {
    const shapes = [
      { sideA: side(false), sideB: roster.sideB },
      { sideA: roster.sideA, sideB: side(false) },
      roster,
      { sideA: side(true), sideB: side(true) },
    ];
    for (const r of shapes) {
      const out = kachinukiAdvisoryModel({
        roster: r, onA: { name: 'Kudo', memberId: 'a1' }, onB: { name: '', memberId: '' },
        squadA, squadB, teamNumberA: 'T2', teamNumberB: 'T1',
      });
      expect(out?.text || '').not.toMatch(/lineup|incomplete|add/i);
    }
  });
});

describe('kachinukiRosterKey', () => {
  const log = [
    { position: 1, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Taki', sideBMemberId: 'b1', winner: 'Kudo', decision: 'fought' },
    { position: 2, sideA: 'Kudo', sideAMemberId: 'a1', sideB: 'Ueda', sideBMemberId: 'b2' },
  ];
  it('is the same for an equal bout log in a new object', () => {
    expect(kachinukiRosterKey('m1', log)).toBe(kachinukiRosterKey('m1', JSON.parse(JSON.stringify(log))));
  });
  it('ignores the live bout outcome but follows a recorded one', () => {
    const live = log.map((s, i) => (i === 1 ? { ...s, winner: 'Ueda' } : s));
    expect(kachinukiRosterKey('m1', live)).toBe(kachinukiRosterKey('m1', log));
    const appended = [...live, { position: 3, sideA: 'Mori', sideAMemberId: 'a3', sideB: 'Ueda', sideBMemberId: 'b2' }];
    expect(kachinukiRosterKey('m1', appended)).not.toBe(kachinukiRosterKey('m1', log));
  });
  it('differs by match and leaves the representative bout out', () => {
    expect(kachinukiRosterKey('m2', log)).not.toBe(kachinukiRosterKey('m1', log));
    expect(kachinukiRosterKey('m1', [...log, { position: -1, sideA: 'R', sideB: 'T' }])).toBe(kachinukiRosterKey('m1', log));
  });
});
