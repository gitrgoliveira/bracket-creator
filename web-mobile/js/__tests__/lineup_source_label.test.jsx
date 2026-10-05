// What both lineup editors (the at-court panel and the Lineups page) share, as
// the pure helpers in lineup_resolver.jsx: the fields a lineup read fills in,
// the wording that says where it was saved, and the wording for taking a
// match's own lineup away.

import { describe, it, expect } from 'vitest';
// viewer_utils publishes window.poolLabel, which scoreRowMatchLabel reads when
// it names a pool match (in the app viewer.js evaluates it first).
import '../viewer_utils.jsx';
import { lineupFields, lineupSourceOf, lineupSourceLabel, PREVIOUS_LINEUP_LABEL, previousLineupConfirm } from '../lineup_resolver.jsx';

const ALL = [
  { id: 'Pool D-0', phase: 'pool', poolName: 'Pool D' },
  { id: 'k-r0-m3', phase: 'bracket', matchNumber: 7 },
  { id: 'bronze', phase: 'bracket' },
];

describe('lineupFields', () => {
  it('names every position, vacant ones as empty strings', () => {
    const lineup = { positions: { 1: 'Aoki', 3: 'Ito' }, memberIds: { 1: 'mem-1', 3: 'mem-3' } };
    expect(lineupFields(lineup, ['1', '2', '3'])).toEqual({
      positions: { 1: 'Aoki', 2: '', 3: 'Ito' },
      memberIds: { 1: 'mem-1', 2: '', 3: 'mem-3' },
    });
  });

  it('keeps a member fielded by number alone, with no name', () => {
    const lineup = { positions: { 1: '' }, memberIds: { 1: 'mem-1' } };
    expect(lineupFields(lineup, ['1'])).toEqual({ positions: { 1: '' }, memberIds: { 1: 'mem-1' } });
  });

  it('reads a lineup saved before members had ids, and no lineup at all, as blank', () => {
    expect(lineupFields({ positions: { 1: 'Aoki' } }, ['1', '2'])).toEqual({
      positions: { 1: 'Aoki', 2: '' },
      memberIds: { 1: '', 2: '' },
    });
    expect(lineupFields(null, ['1'])).toEqual({ positions: { 1: '' }, memberIds: { 1: '' } });
  });

  it('ignores a position the team has no slot for', () => {
    expect(lineupFields({ positions: { 1: 'Aoki', 9: 'Ghost' } }, ['1']).positions).toEqual({ 1: 'Aoki' });
  });
});

describe('lineupSourceOf', () => {
  it('reads the source the server names', () => {
    expect(lineupSourceOf({ sourceMatchId: 'm0' })).toEqual({ matchId: 'm0' });
    expect(lineupSourceOf({ sourceRound: 0 })).toEqual({ round: 0 });
    expect(lineupSourceOf({ sourceRound: 2 })).toEqual({ round: 2 });
    expect(lineupSourceOf({ positions: {} })).toBeNull();
    expect(lineupSourceOf(null)).toBeNull();
  });
});

describe('lineupSourceLabel', () => {
  it.each([
    ['nothing in force', null, 'No lineup saved yet'],
    ['its own', { matchId: 'Pool D-1' }, 'Lineup for this match'],
    ['carried from a pool match', { matchId: 'Pool D-0' }, 'Same as Pool D · Match 1'],
    ['carried from a knockout match', { matchId: 'k-r0-m3' }, 'Same as Match 7'],
    ['carried from a match the list does not hold', { matchId: 'gone-9' }, 'Same as gone-9'],
    ['the starting lineup', { round: 0 }, 'Starting lineup'],
    ['a later round\'s Lineups-page lineup', { round: 1 }, 'From the Lineups page (Round 2)'],
  ])('%s', (_name, source, label) => {
    expect(lineupSourceLabel(source, 'Pool D-1', ALL)).toBe(label);
  });
});

describe('the wording for taking a match\'s own lineup away', () => {
  it('names the button and, in the confirm, the match, the team and what else follows', () => {
    expect(PREVIOUS_LINEUP_LABEL).toBe('Use the previous match\'s lineup');
    const confirm = previousLineupConfirm('Pool D · Match 2', 'Team A');
    expect(confirm.message).toContain('Pool D · Match 2');
    expect(confirm.message).toContain('Team A carries the lineup of its previous match');
    expect(confirm.message).toContain('Later matches that have no lineup of their own follow too.');
  });

  it('reads on when the match or the team has no name', () => {
    expect(previousLineupConfirm('', '').message).toContain('for this match?');
    expect(previousLineupConfirm('', '').message).toContain('the team carries');
  });
});
