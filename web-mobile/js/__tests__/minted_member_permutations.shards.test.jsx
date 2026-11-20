// The guard of the minted-member gate, which runs as shard files over the
// harness `helpers/minted_member_permutations.js`: the shard files on disk are
// exactly the five the gate expects, their slices are disjoint and together are
// every row, and the row count is pinned, so a row dropped from the harness, or
// a shard file deleted, added or left over from another count, fails here.
import { describe, it, expect } from 'vitest';
import { shardOf, expectShardFiles } from './helpers/shards.js';
import { ALL } from './helpers/minted_member_permutations.js';

const SHARDS = 5;
// Every state an add reaches crossed with every write naming its id, in one tab
// and in two (counted from the gate before it was split).
const ROWS = 504;

const label = (row) => row.join(' ');

describe('the minted-member gate runs every row, once, over its shard files', () => {
    it('has exactly the shard files the gate expects', () => {
        expectShardFiles(__dirname, 'minted_member_permutations', '.test.jsx', SHARDS);
    });

    it('pins the number of rows', () => {
        expect(ALL.length).toBe(ROWS);
        expect(new Set(ALL.map(label)).size).toBe(ROWS);
    });

    it('splits the rows into disjoint slices that together are every row', () => {
        const slices = [];
        for (let k = 1; k <= SHARDS; k++) slices.push(shardOf(ALL, k, SHARDS));
        for (const slice of slices) expect(slice.length).toBeGreaterThan(0);
        const seen = slices.flat().map(label);
        expect(seen.length).toBe(ALL.length);
        expect(new Set(seen).size).toBe(seen.length);
        expect([...seen].sort()).toEqual(ALL.map(label).sort());
    });
});
