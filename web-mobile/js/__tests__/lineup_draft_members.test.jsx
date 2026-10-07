// mergeMembers (lineup_draft.jsx) is the ONE rule every list of a team's members goes
// through, in both lineup editors and on the team score sheet: the server stamps a member
// (`modifiedAt`) each time it is created or named, and a list that arrives is merged with
// the one shown by member id, keeping for each member the copy with the larger stamp. Which
// list arrived last, or which read began first, says nothing about which copy is newer.

import { describe, it, expect } from 'vitest';
import { mergeMembers } from '../lineup_draft.jsx';

const member = (n, name = '', modifiedAt = 0) => ({ id: `m${n}`, index: n, name, modifiedAt });
const names = (members) => members.map((m) => `${m.index}:${m.name}`);

describe('mergeMembers: which members are in the list', () => {
  it('holds every member of both lists, once, in member number order', () => {
    const shown = [member(1), member(2, 'Kai'), member(6, 'Added elsewhere')];
    const arriving = [member(2, 'Kai'), member(1), member(4, 'Newcomer')];

    expect(names(mergeMembers(shown, arriving))).toEqual(['1:', '2:Kai', '4:Newcomer', '6:Added elsewhere']);
  });

  it('keeps a member shown that the list lacks: no screen removes a member', () => {
    expect(names(mergeMembers([member(1, 'Ren'), member(2, 'Ito', 7)], [member(1, 'Ren')]))).toEqual(['1:Ren', '2:Ito']);
  });

  it('brings in a member the list holds that is not shown', () => {
    expect(names(mergeMembers([member(1, 'Ren')], [member(1, 'Ren'), member(2, 'Mei', 9)]))).toEqual(['1:Ren', '2:Mei']);
  });

  it('gives an empty list as it is merged with nothing', () => {
    expect(mergeMembers([], [])).toEqual([]);
    expect(names(mergeMembers([], [member(2, 'Kai'), member(1)]))).toEqual(['1:', '2:Kai']);
    expect(names(mergeMembers([member(2, 'Kai'), member(1)], []))).toEqual(['1:', '2:Kai']);
  });

  it('changes neither list it is given', () => {
    const shown = [member(2, 'Kai', 3), member(1)];
    const arriving = [member(1, 'Ito', 5)];
    const before = JSON.stringify([shown, arriving]);

    mergeMembers(shown, arriving);

    expect(JSON.stringify([shown, arriving])).toBe(before);
  });
});

describe('mergeMembers: the copy of a member that is kept', () => {
  it('is the arriving one when its stamp is larger', () => {
    const merged = mergeMembers([member(1, 'Ito', 100)], [member(1, 'Itoh', 200)]);

    expect(merged).toEqual([member(1, 'Itoh', 200)]);
  });

  it('is the one shown when its stamp is larger: an older list never undoes a newer rename', () => {
    const merged = mergeMembers([member(1, 'Itoh', 200)], [member(1, 'Ito', 100)]);

    expect(merged).toEqual([member(1, 'Itoh', 200)]);
  });

  it('is the arriving one on a tie', () => {
    const shown = [member(1, 'Ito', 100)];
    const arriving = [member(1, 'Ito', 100)];

    const merged = mergeMembers(shown, arriving);

    expect(merged).toEqual([member(1, 'Ito', 100)]);
    expect(merged[0]).toBe(arriving[0]);
  });

  it('is the arriving one when neither carries a stamp: members nobody has touched', () => {
    const shown = [{ id: 'm1', index: 1, name: 'Old' }];
    const arriving = [{ id: 'm1', index: 1, name: 'New' }];

    expect(mergeMembers(shown, arriving)).toEqual(arriving);
  });

  it('reads a missing stamp as 0: any stamped copy outranks one with none, either way round', () => {
    const stamped = member(1, 'Ito', 5);
    const unstamped = { id: 'm1', index: 1, name: 'Before' };

    expect(mergeMembers([stamped], [unstamped])).toEqual([stamped]);
    expect(mergeMembers([unstamped], [stamped])).toEqual([stamped]);
  });

  it('reads a stamp of 0 as no stamp at all', () => {
    expect(mergeMembers([member(1, 'Ito', 0)], [member(1, '', 0)])).toEqual([member(1, '', 0)]);
    expect(mergeMembers([member(1, 'Ito', 3)], [member(1, '', 0)])).toEqual([member(1, 'Ito', 3)]);
  });

  it('keeps a cleared name when it is the newer copy: a list that still names the member does not bring the name back', () => {
    const cleared = member(1, '', 50);

    expect(mergeMembers([cleared], [member(1, 'Ito', 40)])).toEqual([cleared]);
    expect(mergeMembers([member(1, 'Ito', 40)], [cleared])).toEqual([cleared]);
  });

  it('is judged member by member', () => {
    const shown = [member(1, 'Ito', 10), member(2, 'Mori', 3)];
    const arriving = [member(1, 'Old', 5), member(2, 'Mori Kato', 8)];

    expect(names(mergeMembers(shown, arriving))).toEqual(['1:Ito', '2:Mori Kato']);
  });

  it('settles three copies the same whichever order they arrive in', () => {
    const own = member(1, 'Ito', 120);
    const elsewhereLater = member(1, 'Itoh', 150);
    const read = member(1, 'Old', 0);
    const orders = [
      [own, elsewhereLater, read],
      [read, own, elsewhereLater],
      [elsewhereLater, read, own],
      [own, read, elsewhereLater],
    ];

    orders.forEach((order) => {
      const merged = order.reduce((shown, copy) => mergeMembers(shown, [copy]), []);
      expect(merged).toEqual([elsewhereLater]);
    });
  });
});
