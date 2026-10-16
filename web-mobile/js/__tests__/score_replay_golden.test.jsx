// JS half of the shared Go/JS golden table for the score event replay (bc-evts).
// Go half: TestReplayScoring_GoldenTable (internal/engine/score_replay_golden_test.go),
// which this file mirrors step for step: every order of a case's `lines` is
// replayed after its `head`, want.match is a subset of the result (scoreVersion,
// modifiedAt and groupStamps compared whole), want.outcomes each id's outcomes
// sorted, and the output replayed again with the same lines changes nothing.
//
// Only the individual and engi cases marked jsMirror are mirrored: team bouts
// fold in JS from stage 5, and a case whose lines hold a baseline is Go only (a
// screen folds its taps on top of the server's last confirmed match and never
// folds a baseline; see the table's own comment).
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { replayHead } from '../score_events.jsx';

const table = JSON.parse(
  readFileSync(resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'score_replay.json'), 'utf8')
);

const MIRRORED_KINDS = new Set(['individual', 'engi']);
const cases = (table.cases || []).filter((c) => c.jsMirror && MIRRORED_KINDS.has(c.shape?.kind));

// emptyToNil reads an empty array as null, so a golden [] matches a field the
// wire omits (Go's emptyToNil); undefined reads as null too.
function emptyToNil(v) {
  if (v === undefined) return null;
  if (Array.isArray(v)) return v.length === 0 ? null : v.map(emptyToNil);
  if (v && typeof v === 'object') {
    const out = {};
    for (const [k, x] of Object.entries(v)) out[k] = emptyToNil(x);
    return out;
  }
  return v;
}

// subsetOf: every key want names is in got with the same value (Go's subsetOf).
function subsetOf(want, got, path) {
  if (want && typeof want === 'object' && !Array.isArray(want)) {
    if (!got || typeof got !== 'object' || Array.isArray(got)) return `${path}: want an object, got ${JSON.stringify(got)}`;
    for (const k of Object.keys(want)) {
      const err = subsetOf(want[k], got[k] ?? null, `${path}.${k}`);
      if (err) return err;
    }
    return null;
  }
  if (Array.isArray(want)) {
    if (!Array.isArray(got) || got.length !== want.length) return `${path}: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`;
    for (let i = 0; i < want.length; i++) {
      const err = subsetOf(want[i], got[i], `${path}[${i}]`);
      if (err) return err;
    }
    return null;
  }
  return String(want) === String(got) ? null : `${path}: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`;
}

function permutations(n) {
  if (n === 0) return [[]];
  const out = [];
  for (const rest of permutations(n - 1)) {
    for (let i = 0; i <= rest.length; i++) out.push([...rest.slice(0, i), n - 1, ...rest.slice(i)]);
  }
  return out;
}

// The wire form: a JSON round trip drops undefined fields, as omitempty does.
const wire = (m) => JSON.parse(JSON.stringify(m));

function byId(outcomes) {
  const got = {};
  for (const o of outcomes) (got[o.id] = got[o.id] || []).push(o.outcome);
  return got;
}

describe('score replay: the JS mirror answers the Go golden table', () => {
  // Load-bearing: it.each over an empty array silently produces zero tests.
  it('the table holds mirrored individual and engi cases', () => {
    expect(cases.length, 'score_replay.json has no jsMirror individual/engi case: the mirror would assert nothing').toBeGreaterThan(0);
  });

  it.each(cases.map((c) => [c.name, c]))('%s', (_name, c) => {
    const pinned = ['scoreVersion', 'modifiedAt', 'groupStamps'];
    for (const k of pinned) expect(c.want.match, `every case pins ${k}`).toHaveProperty(k);
    const opts = { engi: c.shape.kind === 'engi' };
    const wantMatch = emptyToNil(c.want.match);
    for (const order of permutations(c.lines.length)) {
      const lines = order.map((i) => c.lines[i]);
      const r = replayHead(c.match, c.head, lines, opts);
      const got = emptyToNil(wire(r.match));
      expect(subsetOf(wantMatch, got, 'match'), `order ${order}`).toBeNull();
      for (const k of pinned) expect(got[k], `order ${order}, ${k}`).toEqual(wantMatch[k]);
      const gotOutcomes = byId(r.outcomes);
      for (const [id, want] of Object.entries(c.want.outcomes)) {
        expect([...(gotOutcomes[id] || [])].sort(), `order ${order}, event ${id}`).toEqual([...want].sort());
      }
      // The next write replays from this output: doing so changes nothing.
      const again = replayHead(wire(r.match), c.head, lines, opts);
      expect(wire(again.match), `order ${order}, replayed from its own output`).toEqual(wire(r.match));
      expect(again.outcomes, `order ${order}, replayed from its own output`).toEqual(r.outcomes);
    }
  });

  it('never mutates the match or the lines it is given', () => {
    const c = cases[0];
    const before = JSON.stringify([c.match, c.head, c.lines]);
    replayHead(c.match, c.head, c.lines, { engi: c.shape.kind === 'engi' });
    expect(JSON.stringify([c.match, c.head, c.lines])).toBe(before);
  });
});
