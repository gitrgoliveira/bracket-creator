// The shard helpers (helpers/shards.js) are the safety of every split
// permutation gate: a slice that dropped or doubled a combination, or a deleted
// shard file nobody noticed, would shrink a gate silently. This pins them.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { mkdtempSync, writeFileSync, rmSync } from 'fs';
import { tmpdir } from 'os';
import path from 'path';
import { shardOf, shardFromUrl, expectShardFiles } from './helpers/shards.js';

describe('shardOf', () => {
    const list = Array.from({ length: 23 }, (_, i) => `c${i}`);

    it.each([1, 2, 3, 7, 23, 30])('the %i slices are disjoint and cover the list', (n) => {
        const slices = Array.from({ length: n }, (_, i) => shardOf(list, i + 1, n));
        const all = slices.flat();
        expect(all).toHaveLength(list.length);
        expect(new Set(all).size).toBe(list.length);
        expect([...all].sort()).toEqual([...list].sort());
    });

    it('keeps the list order inside a slice and deals round-robin', () => {
        expect(shardOf(list, 1, 3)).toEqual(['c0', 'c3', 'c6', 'c9', 'c12', 'c15', 'c18', 'c21']);
        expect(shardOf(list, 3, 3)).toEqual(['c2', 'c5', 'c8', 'c11', 'c14', 'c17', 'c20']);
    });

    it('one shard is the whole list', () => {
        expect(shardOf(list, 1, 1)).toEqual(list);
    });

    it('rejects a k outside 1..n and a bad n', () => {
        expect(() => shardOf(list, 0, 3)).toThrow();
        expect(() => shardOf(list, 4, 3)).toThrow();
        expect(() => shardOf(list, 1, 0)).toThrow();
        expect(() => shardOf(list, 1.5, 3)).toThrow();
    });
});

describe('shardFromUrl', () => {
    it('reads k and n from the file name', () => {
        expect(shardFromUrl('file:///x/render/own_status_permutations.shard-3-of-7.render.test.jsx'))
            .toEqual({ k: 3, n: 7 });
        expect(shardFromUrl('file:///x/minted_member_permutations.shard-5-of-5.test.jsx'))
            .toEqual({ k: 5, n: 5 });
    });

    it('reads the file name only, never a folder named like a shard', () => {
        expect(shardFromUrl('file:///x.shard-1-of-2.tmp/render/a.shard-3-of-7.render.test.jsx'))
            .toEqual({ k: 3, n: 7 });
        expect(() => shardFromUrl('file:///x.shard-1-of-2.tmp/a.test.jsx')).toThrow();
    });

    it('throws on a name that is not a shard file', () => {
        expect(() => shardFromUrl('file:///x/own_status_permutations.shards.render.test.jsx')).toThrow();
        expect(() => shardFromUrl('file:///x/own_status_permutations.render.test.jsx')).toThrow();
        expect(() => shardFromUrl('file:///x/a.shard-x-of-7.test.jsx')).toThrow();
    });

    it('throws when k is outside 1..n', () => {
        expect(() => shardFromUrl('file:///x/a.shard-0-of-7.test.jsx')).toThrow();
        expect(() => shardFromUrl('file:///x/a.shard-8-of-7.test.jsx')).toThrow();
        expect(() => shardFromUrl('file:///x/a.shard-1-of-0.test.jsx')).toThrow();
    });
});

describe('expectShardFiles', () => {
    const STEM = 'gate';
    const SUFFIX = '.test.jsx';
    let dir;
    const BODY = 'defineShard(import.meta.url);\n';
    const touch = (...names) => names.forEach((n) => writeFileSync(path.join(dir, n), BODY));
    const full = (n) => Array.from({ length: n }, (_, i) => `${STEM}.shard-${i + 1}-of-${n}${SUFFIX}`);

    beforeEach(() => { dir = mkdtempSync(path.join(tmpdir(), 'shards-')); });
    afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

    it('passes when exactly the n shard files exist, ignoring the guard and other files', () => {
        touch(...full(3), `${STEM}.shards${SUFFIX}`, 'other.shard-1-of-9.test.jsx', `${STEM}.js`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).not.toThrow();
    });

    it('reports a missing shard', () => {
        touch(...full(3).slice(1), `${STEM}.shards${SUFFIX}`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
    });

    it('reports an extra shard', () => {
        touch(...full(3), `${STEM}.shard-4-of-3${SUFFIX}`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
    });

    it('reports a leftover from another n', () => {
        touch(...full(3), `${STEM}.shard-1-of-2${SUFFIX}`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
    });

    it('reports a different n entirely', () => {
        touch(...full(2));
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
    });

    it('reports a shard whose body was emptied or changed', () => {
        touch(...full(3));
        writeFileSync(path.join(dir, full(3)[1]), '');
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
        writeFileSync(path.join(dir, full(3)[1]), `// skipped\n${BODY}`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 3, BODY)).toThrow();
    });

    it.each([0, -1, 1.5, NaN, undefined])('rejects n = %s instead of passing with nothing checked', (n) => {
        expect(() => expectShardFiles(dir, STEM, SUFFIX, n, BODY)).toThrow(/positive integer/);
    });

    it('does not count the .shards guard file as a shard', () => {
        touch(`${STEM}.shards${SUFFIX}`);
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 1, BODY)).toThrow();
        touch(...full(1));
        expect(() => expectShardFiles(dir, STEM, SUFFIX, 1, BODY)).not.toThrow();
    });
});
