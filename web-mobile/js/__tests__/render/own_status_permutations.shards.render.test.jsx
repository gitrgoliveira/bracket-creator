// The guard of the own-status permutation gate, which is split over the shard
// files `own_status_permutations.shard-<k>-of-<n>.render.test.jsx` (the
// harness is `../helpers/own_status_permutations.jsx`). It fails when a shard
// file is deleted, added, emptied or left over from another count, when the shards stop
// covering every combination exactly once, or when a loop's number of
// combinations changes. A change to a loop's combinations updates COUNTS here
// on purpose, in the same change.
import { describe, it, expect } from 'vitest';
import path from 'path';
import { fileURLToPath } from 'url';
import { shardOf, expectShardFiles } from '../helpers/shards.js';
import { combinations } from '../helpers/own_status_permutations.jsx';

const SHARDS = 7;
const STEM = 'own_status_permutations';
const SUFFIX = '.render.test.jsx';
const SHARD_BODY = `import { defineOwnStatusShard } from '../helpers/own_status_permutations.jsx';
defineOwnStatusShard(import.meta.url);
`;
// Each loop's number of combinations, in the order the gate runs the loops.
const COUNTS = [
  ['chain', 312],
  ['runningRep', 64],
  ['individual', 2161],
  ['team', 1257],
  ['kachinuki Record bout', 805],
  ['engi', 36],
];
const TOTAL = 4635;

const here = path.dirname(fileURLToPath(import.meta.url));
const key = (c) => `${c.loop}\t${JSON.stringify(c.sc)}`;

describe('the own-status permutation gate\'s shards', () => {
  it(`are exactly the ${SHARDS} shard files`, () => {
    expectShardFiles(here, STEM, SUFFIX, SHARDS, SHARD_BODY);
  });

  it('keep every loop\'s number of combinations, in the gate\'s order', () => {
    const all = combinations();
    const counts = [];
    for (const c of all) {
      if (!counts.length || counts.at(-1)[0] !== c.loop) counts.push([c.loop, 0]);
      counts.at(-1)[1] += 1;
    }
    expect(counts).toEqual(COUNTS);
    expect(all.length).toBe(TOTAL);
    expect(COUNTS.reduce((sum, [, n]) => sum + n, 0)).toBe(TOTAL);
  });

  it('run every combination exactly once between them', () => {
    const all = combinations();
    const slices = [];
    for (let k = 1; k <= SHARDS; k++) slices.push(shardOf(all, k, SHARDS));
    const ran = slices.flat();
    // Disjoint, and nothing left out: each combination is in one slice only.
    expect(new Set(ran).size).toBe(ran.length);
    expect(ran.length).toBe(all.length);
    expect(new Set(ran)).toEqual(new Set(all));
    // No combination is repeated within its loop either.
    expect(new Set(all.map(key)).size).toBe(all.length);
    // Every shard runs a share of every loop.
    for (const slice of slices) {
      expect(new Set(slice.map((c) => c.loop))).toEqual(new Set(COUNTS.map(([loop]) => loop)));
    }
  });
});
