// What both lineup editors (the at-court panel and the Lineups page) share, as
// the pure helpers in lineup_resolver.jsx: the fields a lineup read fills in,
// the wording that says where it was saved, and the wording for taking a
// match's own lineup away.

import { describe, it, expect, vi } from 'vitest';
// viewer_utils publishes window.poolLabel, which scoreRowMatchLabel reads when
// it names a pool match (in the app viewer.js evaluates it first).
import '../viewer_utils.jsx';
import {
  lineupFields, lineupSourceOf, lineupSourceLabel, isOwnLineup, STARTING_ROUND, PREVIOUS_LINEUP_LABEL, SAVE_QUEUED_REASON,
  REMOVED_UNREAD_NOTICE, previousLineupConfirm,
} from '../lineup_resolver.jsx';

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

describe('isOwnLineup', () => {
  it('is true only for a lineup saved for this very match', () => {
    expect(isOwnLineup({ matchId: 'Pool D-1' }, 'Pool D-1')).toBe(true);
    expect(isOwnLineup({ matchId: 'Pool D-0' }, 'Pool D-1')).toBe(false);
    expect(isOwnLineup({ round: 0 }, 'Pool D-1')).toBe(false);
    expect(isOwnLineup(null, 'Pool D-1')).toBe(false);
  });

  it('is false for the starting lineup, which has no match to own it', () => {
    expect(isOwnLineup({ matchId: 'Pool D-1' }, '')).toBe(false);
    expect(isOwnLineup({ matchId: '' }, '')).toBe(false);
  });

  it('is what the label calls "Lineup for this match"', () => {
    for (const [source, matchId] of [[{ matchId: 'a' }, 'a'], [{ matchId: 'b' }, 'a'], [{ round: 0 }, 'a'], [null, 'a'], [{ matchId: 'a' }, '']]) {
      expect(lineupSourceLabel(source, matchId, []) === 'Lineup for this match').toBe(isOwnLineup(source, matchId));
    }
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

  it('takes the matches as a function too, called only when the lineup is carried from another match', () => {
    const matches = vi.fn(() => ALL);
    expect(lineupSourceLabel(null, 'Pool D-1', matches)).toBe('No lineup saved yet');
    expect(lineupSourceLabel({ matchId: 'Pool D-1' }, 'Pool D-1', matches)).toBe('Lineup for this match');
    expect(lineupSourceLabel({ round: 0 }, 'Pool D-1', matches)).toBe('Starting lineup');
    expect(lineupSourceLabel({ round: 1 }, 'Pool D-1', matches)).toBe('From the Lineups page (Round 2)');
    expect(matches).not.toHaveBeenCalled();

    expect(lineupSourceLabel({ matchId: 'Pool D-0' }, 'Pool D-1', matches)).toBe('Same as Pool D · Match 1');
    expect(matches).toHaveBeenCalledTimes(1);
  });

  it('names the match by its id when there are no matches to look in', () => {
    expect(lineupSourceLabel({ matchId: 'gone-9' }, 'Pool D-1')).toBe('Same as gone-9');
    expect(lineupSourceLabel({ matchId: 'gone-9' }, 'Pool D-1', () => undefined)).toBe('Same as gone-9');
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

  it('says what happens to the team\'s first match, and that unsaved changes are discarded', () => {
    const { message, confirmLabel, cancelLabel } = previousLineupConfirm('Pool D · Match 1', 'Team A');
    expect(message).toBe(
      'Use the lineup Team A had before Pool D · Match 1? The lineup entered for Pool D · Match 1 is removed, '
      + 'so Team A carries the lineup of its previous match, or its starting lineup if this is its first match. '
      + 'Later matches that have no lineup of their own follow too. Unsaved changes here are discarded.',
    );
    expect(confirmLabel).toBe('Use previous lineup');
    expect(cancelLabel).toBe('Cancel');
  });

  it('reads on when the match or the team has no name', () => {
    expect(previousLineupConfirm('', '').message).toContain('before this match?');
    expect(previousLineupConfirm('', '').message).toContain('the team carries');
  });
});

describe('the other words the lineup editors share', () => {
  it('say why the button waits for a queued save, and what a removal whose re-read failed leaves', () => {
    expect(SAVE_QUEUED_REASON).toBe('A save of this lineup is still waiting to be sent.');
    expect(REMOVED_UNREAD_NOTICE).toBe('Removed. The lineup this match now uses could not be read: try again.');
  });

  it('keep the team\'s starting lineup as its round-0 entry', () => {
    expect(STARTING_ROUND).toBe(0);
  });
});
