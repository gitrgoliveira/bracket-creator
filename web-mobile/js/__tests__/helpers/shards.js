// Helpers for a permutation gate that is split over several test files (shards)
// so vitest can run the slices in parallel workers. One job per function:
//   shardOf          which slice of a list a shard runs;
//   shardFromUrl     which shard a file is, read from the file's own name;
//   expectShardFiles the guard: the shard files on disk are exactly the N the
//                    gate expects, so deleting or adding one fails loudly.
import { readdirSync } from 'fs';
import { expect } from 'vitest';

// The k-th of n round-robin slices of `list` (k is 1-based), in list order.
// Slice k holds the items at indexes k-1, k-1+n, k-1+2n, ... so the n slices
// are disjoint and together cover the whole list.
export function shardOf(list, k, n) {
    if (!Number.isInteger(n) || n < 1) throw new Error(`shardOf: n must be a positive integer, got ${n}`);
    if (!Number.isInteger(k) || k < 1 || k > n) throw new Error(`shardOf: k must be within 1..${n}, got ${k}`);
    return list.filter((_, i) => i % n === k - 1);
}

// k and n from a shard file's own name, `<stem>.shard-<k>-of-<n>.<ext>`, taken
// from `import.meta.url`, so a shard file carries no numbers in its body.
export function shardFromUrl(url) {
    const m = /\.shard-(\d+)-of-(\d+)\./.exec(String(url));
    if (!m) throw new Error(`shardFromUrl: "${url}" is not named <stem>.shard-<k>-of-<n>.<ext>`);
    const k = Number(m[1]);
    const n = Number(m[2]);
    if (n < 1 || k < 1 || k > n) throw new Error(`shardFromUrl: shard ${k} of ${n} in "${url}" is outside 1..${n}`);
    return { k, n };
}

const escapeRegExp = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// Asserts the shard files of one gate in `dir` are exactly
// `<stem>.shard-<i>-of-<n><suffix>` for i = 1..n. A missing shard, an extra one
// or a leftover `-of-<other n>` file fails. The guard file `<stem>.shards<suffix>`
// does not match the pattern, so it is ignored.
export function expectShardFiles(dir, stem, suffix, n) {
    const pattern = new RegExp(`^${escapeRegExp(stem)}\\.shard-(\\d+)-of-(\\d+)${escapeRegExp(suffix)}$`);
    const found = readdirSync(dir).filter((name) => pattern.test(name)).sort();
    const expected = [];
    for (let i = 1; i <= n; i++) expected.push(`${stem}.shard-${i}-of-${n}${suffix}`);
    expect(found).toEqual(expected.sort());
}
