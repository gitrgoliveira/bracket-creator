// JS half of the shared Go/JS golden table for operator match labels. Go
// half: TestOperatorMatchLabel_GoldenTable (internal/engine/match_label_test.go).
// Schema and rationale: internal/engine/testdata/match_labels.json's own
// "_comment" field.
//
// Four label-producing surfaces are pinned against the ONE golden table:
//   - pool/league/swiss rows: scoreRowMatchLabel (pool_ids.jsx), the score
//     editor's own composition of poolNameOf + poolMatchNumberOf +
//     window.poolLabel-equivalent naming.
//   - pool daihyosen/tiebreaker rows: poolMatchNumberOf returns 0 for them,
//     so scoreRowMatchLabel renders no match-number label at all (the JS
//     side shows a separate "DH" tag badge instead -- see the fixture's own
//     comment for why this file does not try to reproduce the Go-only
//     "<pool> daihyosen"/"<pool> tiebreaker" spelling as a render).
//   - knockout rows: matchLabel (write_result.jsx) is the number/id prefix
//     of the golden label; the round qualifier ("(Semifinals)", "(Final)")
//     is composed server-side (engine.MatchLabel) and carried in `label`
//     verbatim, which matchLabel does not add.
//   - bronze: the one knockout row with NO round qualifier to strip, so
//     matchLabel matches the golden label exactly.
//
// Also pinned: poolPosition (viewer_utils.jsx's compMatches, derived by
// array-index within a poolName group) agrees with poolMatchNumberOf (a pure
// id-parse) once matches are supplied to compMatches in id order -- the
// precondition the server always meets in production.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { scoreRowMatchLabel, poolMatchNumberOf, poolNameOf } from '../pool_ids.jsx';
import { matchLabel } from '../write_result.jsx';
import { compMatches } from '../viewer_utils.jsx';

describe('match labels: the JS surfaces mirror the Go operator label (golden table)', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'match_labels.json'),
      'utf8'
    )
  );

  // Load-bearing: it.each over an empty array silently produces zero tests
  // (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/engine/testdata/match_labels.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  // bc-cse: poolMatchNumberOf alone is NOT the right filter -- it matches any
  // id ending in "-<digits>" (pool_ids.jsx's own documented caveat), which
  // also matches a knockout id like "m-r2-0". Gate on format too.
  describe('pool/league/swiss rows: scoreRowMatchLabel matches the golden label exactly', () => {
    const rows = table.cases.filter((c) => c.format !== 'knockout' && poolMatchNumberOf(c.id) > 0);

    it('the table exercises at least one pool/league/swiss row', () => {
      expect(rows.length).toBeGreaterThan(0);
    });

    it.each(rows)('$id ($format)', ({ id, format, label }) => {
      const m = { phase: 'pool', id, compFormat: format, poolName: poolNameOf(id) };
      expect(scoreRowMatchLabel(m)).toBe(label);
    });
  });

  describe('pool daihyosen/tiebreaker rows: no match-number label on the JS side', () => {
    const rows = table.cases.filter((c) => c.id.startsWith('Pool ') && poolMatchNumberOf(c.id) === 0);

    it('the table exercises at least one DH/TB row', () => {
      expect(rows.length).toBeGreaterThan(0);
    });

    it.each(rows)('$id', ({ id, format }) => {
      expect(poolMatchNumberOf(id)).toBe(0);
      const m = { phase: 'pool', id, compFormat: format, poolName: poolNameOf(id) };
      expect(scoreRowMatchLabel(m)).toBe('');
    });
  });

  describe('knockout rows: matchLabel is the golden label\'s number/id prefix', () => {
    const rows = table.cases.filter((c) => c.format === 'knockout' && c.id !== 'm-bronze');

    it('the table exercises at least one knockout row', () => {
      expect(rows.length).toBeGreaterThan(0);
    });

    it.each(rows)('$id', ({ id, label, matchNumber }) => {
      const prefix = matchLabel({ number: matchNumber || 0, id });
      expect(label.startsWith(prefix)).toBe(true);
    });
  });

  it('bronze: matchLabel matches the golden label exactly (no round qualifier to strip)', () => {
    const bronze = table.cases.find((c) => c.id === 'm-bronze');
    expect(bronze).toBeTruthy();
    expect(matchLabel({ id: bronze.id })).toBe(bronze.label);
  });

  describe('poolPosition (compMatches) agrees with poolMatchNumberOf over an id-ordered list', () => {
    const rows = table.cases.filter((c) => c.format !== 'knockout' && poolMatchNumberOf(c.id) > 0);

    it.each(rows)('$id', ({ id }) => {
      const pool = poolNameOf(id);
      const n = poolMatchNumberOf(id);
      // The id's own siblings, 0-indexed, in id order -- the shape
      // compMatches receives from a real server payload.
      const poolMatches = Array.from({ length: n }, (_, i) => ({ id: `${pool}-${i}` }));
      const matches = compMatches({ id: 'c1', name: 'Cup', status: 'active', poolMatches });
      const target = matches.find((x) => x.id === id);
      expect(target).toBeTruthy();
      expect(target.poolPosition).toBe(poolMatchNumberOf(id));
    });
  });
});
