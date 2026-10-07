// What a lineup save carries and how two saves of one lineup join in the offline
// queue (operator decision 2026-10-07, "Only changed positions"): lineup_save.jsx
// is the one owner of both.

import { describe, it, expect } from 'vitest';
import { changedLineupSave, joinQueuedLineupSave } from '../lineup_save.jsx';

const LINEUP = { teamId: 't1', competitionId: 'c1', matchId: 'm1' };

describe('changedLineupSave', () => {
  const shown = { senpo: 'Kato', jiho: 'Ito', chuken: 'Mori' };
  const ids = { senpo: 'mem-k', jiho: '', chuken: 'mem-m' };

  it('carries the name and the id of each changed position, and nothing else', () => {
    expect(changedLineupSave(shown, ids, ['senpo', 'chuken'])).toEqual({
      positions: { senpo: 'Kato', chuken: 'Mori' },
      memberIds: { senpo: 'mem-k', chuken: 'mem-m' },
      changed: ['senpo', 'chuken'],
    });
  });

  it('names a changed position that has no id by its name alone', () => {
    expect(changedLineupSave(shown, ids, ['jiho'])).toEqual({
      positions: { jiho: 'Ito' }, memberIds: {}, changed: ['jiho'],
    });
  });

  it('says a cleared position by its empty name, which the server needs to be there, with no id', () => {
    const body = changedLineupSave({ senpo: '', jiho: 'Ito' }, { senpo: '', jiho: '' }, ['senpo']);
    expect(body).toEqual({ positions: { senpo: '' }, memberIds: {}, changed: ['senpo'] });
    expect('senpo' in body.positions).toBe(true);
  });

  it('says a position missing from the lineup as shown the same way: cleared', () => {
    expect(changedLineupSave({ jiho: 'Ito' }, {}, ['senpo'])).toEqual({
      positions: { senpo: '' }, memberIds: {}, changed: ['senpo'],
    });
  });

  it('keeps a picked member that has no name yet: the empty name with the id is a placement', () => {
    expect(changedLineupSave({ senpo: '' }, { senpo: 'mem-blank' }, ['senpo'])).toEqual({
      positions: { senpo: '' }, memberIds: { senpo: 'mem-blank' }, changed: ['senpo'],
    });
  });

  it('is a copy: the changed list it carries is not the one it was given', () => {
    const changed = ['senpo'];
    const body = changedLineupSave(shown, ids, changed);
    body.changed.push('jiho');
    expect(changed).toEqual(['senpo']);
  });
});

describe('joinQueuedLineupSave', () => {
  const save = (positions, memberIds, changed) => ({
    ...LINEUP, positions, ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
  });

  it('joins two saves into one that changes every position either changed', () => {
    const queued = save({ senpo: 'Kato' }, { senpo: 'mem-k' }, ['senpo']);
    const incoming = save({ jiho: 'Ito' }, { jiho: 'mem-i' }, ['jiho']);
    expect(joinQueuedLineupSave(queued, incoming)).toEqual(
      save({ senpo: 'Kato', jiho: 'Ito' }, { senpo: 'mem-k', jiho: 'mem-i' }, ['senpo', 'jiho']),
    );
  });

  it('gives a position both saves changed the value of the later one, name and id', () => {
    const queued = save({ senpo: 'Kato', jiho: 'Ito' }, { senpo: 'mem-k', jiho: 'mem-i' }, ['senpo', 'jiho']);
    const incoming = save({ senpo: 'Ota' }, { senpo: 'mem-o' }, ['senpo']);
    expect(joinQueuedLineupSave(queued, incoming)).toEqual(
      save({ senpo: 'Ota', jiho: 'Ito' }, { senpo: 'mem-o', jiho: 'mem-i' }, ['senpo', 'jiho']),
    );
  });

  it('drops the id a position had when the later save names it without one', () => {
    const queued = save({ senpo: 'Kato' }, { senpo: 'mem-k' }, ['senpo']);
    const incoming = save({ senpo: 'Ota' }, undefined, ['senpo']);
    const joined = joinQueuedLineupSave(queued, incoming);
    expect(joined.positions).toEqual({ senpo: 'Ota' });
    expect(joined.memberIds).toEqual({});
  });

  it('makes a position the later save cleared its empty name with no id, which stays a change', () => {
    const queued = save({ senpo: 'Kato', jiho: 'Ito' }, { senpo: 'mem-k' }, ['senpo', 'jiho']);
    const incoming = save({ senpo: '' }, {}, ['senpo']);
    const joined = joinQueuedLineupSave(queued, incoming);
    expect(joined.positions).toEqual({ senpo: '', jiho: 'Ito' });
    expect(joined.memberIds).toEqual({});
    expect(joined.changed).toEqual(['senpo', 'jiho']);
  });

  it('lists a changed position once, in the order it was first changed', () => {
    const queued = save({ jiho: 'Ito', senpo: 'Kato' }, undefined, ['jiho', 'senpo']);
    const incoming = save({ senpo: 'Ota', chuken: 'Mori' }, undefined, ['senpo', 'chuken']);
    expect(joinQueuedLineupSave(queued, incoming).changed).toEqual(['jiho', 'senpo', 'chuken']);
  });

  it('takes what names the lineup from the later save', () => {
    const queued = { ...save({ senpo: 'Kato' }, undefined, ['senpo']), teamId: 't-old' };
    const joined = joinQueuedLineupSave(queued, save({ jiho: 'Ito' }, undefined, ['jiho']));
    expect(joined.teamId).toBe('t1');
    expect(joined.competitionId).toBe('c1');
    expect(joined.matchId).toBe('m1');
  });

  it('applies the changes onto a save an earlier build queued, which has no changed list, and stays whole', () => {
    const queued = save({ senpo: 'Sato', jiho: 'Tanaka', taisho: 'Ito' }, { senpo: 'mem-s', jiho: 'mem-t' });
    const incoming = save({ senpo: 'Kato', jiho: '' }, { senpo: 'mem-k' }, ['senpo', 'jiho']);
    const joined = joinQueuedLineupSave(queued, incoming);
    expect(joined).toEqual(save({ senpo: 'Kato', taisho: 'Ito' }, { senpo: 'mem-k' }));
    expect('changed' in joined).toBe(false);
    // A whole save has no empty names: a cleared position is simply absent, as every
    // earlier build sent it.
    expect('jiho' in joined.positions).toBe(false);
  });

  it('keeps the ids a save an earlier build queued has for the positions nobody changed', () => {
    const queued = save({ senpo: 'Sato', jiho: 'Tanaka' }, { senpo: 'mem-s', jiho: 'mem-t' });
    const joined = joinQueuedLineupSave(queued, save({ jiho: 'Ito' }, { jiho: 'mem-i' }, ['jiho']));
    expect(joined.memberIds).toEqual({ senpo: 'mem-s', jiho: 'mem-i' });
  });

  it('takes a save that names no changed positions as the whole lineup: it replaces the queued one', () => {
    const queued = save({ senpo: 'Kato' }, { senpo: 'mem-k' }, ['senpo']);
    const incoming = save({ jiho: 'Ito' });
    expect(joinQueuedLineupSave(queued, incoming)).toEqual(incoming);
  });

  it('changes neither save it is given', () => {
    const queued = save({ senpo: 'Kato' }, { senpo: 'mem-k' }, ['senpo']);
    const incoming = save({ senpo: 'Ota', jiho: 'Ito' }, { senpo: 'mem-o' }, ['senpo', 'jiho']);
    const before = JSON.stringify([queued, incoming]);
    joinQueuedLineupSave(queued, incoming);
    expect(JSON.stringify([queued, incoming])).toBe(before);
  });
});
