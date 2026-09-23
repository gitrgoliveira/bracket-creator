// The two rule gates (web-mobile/check-competitor-search.mjs and
// check-write-result.mjs) fail the build when a call site re-derives a rule
// its owner module states. Until this file they had no test of their own,
// and that is how the accessor form of the competitor-number test --
// `numberOf(p).startsWith(q)`, the very shape the rule forbids, read through
// the accessor the identity leaf provides -- walked past a regex that policed
// the raw `.number` field alone.
//
// Each gate exports its FORBIDDEN rules and runs only as the entry script, so
// importing one here executes nothing. The scanning mechanics live in
// check-helpers.mjs, shared by both, and are pinned here too: a gate that
// reports the wrong line sends the reader to prose, and a comment stripper
// that hides code hides violations.
import { describe, it, expect } from 'vitest';
import { stripComments, scanSource } from '../../check-helpers.mjs';
import { FORBIDDEN as NUMBER_RULES } from '../../check-competitor-search.mjs';
import { FORBIDDEN as WRITE_RULES } from '../../check-write-result.mjs';

const lines = (src) => src.split('\n').length;

describe('stripComments keeps every line where it was', () => {
  it('a block comment becomes its own newlines, not nothing', () => {
    const src = 'a\n/* one\n   two\n   three */\nb\n';
    const out = stripComments(src);
    expect(lines(out)).toBe(lines(src));
    expect(out.split('\n')[4]).toBe('b');
  });

  it('a line comment under blank lines does not take the blanks with it', () => {
    // The old ^\s* under the m flag swallowed the blank lines ABOVE a comment.
    const src = 'a\n\n\n// note\nb\n';
    const out = stripComments(src);
    expect(lines(out)).toBe(lines(src));
    expect(out.split('\n')[4]).toBe('b');
  });

  it('a "/*" inside a line comment opens no phantom block', () => {
    // Block-first stripping read the "/*" in a route glob as a block opener
    // and swallowed everything to the next "*/" -- code the gate never saw.
    const src = '// serves /api/competitions/:id/*\nconst hit = p.number.includes(q);\n// close */\n';
    const out = stripComments(src);
    expect(out.split('\n')[1]).toBe('const hit = p.number.includes(q);');
  });

  it('a trailing comment goes, a URL in code stays', () => {
    // The character before the slashes is kept (it is what proved this was
    // not a URL), so the line ends in the space that preceded the comment.
    expect(stripComments('x = 1; // .includes(\n')).toBe('x = 1; \n');
    expect(stripComments('u = "https://k.example/x";\n')).toBe('u = "https://k.example/x";\n');
  });
});

describe('scanSource reports the real line', () => {
  it('a hit after a multi-line block comment is numbered from the file', () => {
    const src = '/* a\n b\n c */\nconst x = 1;\nconst hit = p.number.includes(q);\n';
    const hits = scanSource(src, NUMBER_RULES);
    expect(hits.map((h) => h.line)).toEqual([5]);
  });
});

describe('the competitor-number rule catches every spelling of the test', () => {
  const trips = (line) => scanSource(line + '\n', NUMBER_RULES).length === 1;

  it('the raw field, as the three surfaces originally drifted', () => {
    expect(trips('(p.number || "").toLowerCase().includes(q)')).toBe(true);
    expect(trips('String(p.number || "").toLowerCase().startsWith(q)')).toBe(true);
  });

  it('the accessor, which the first regex missed', () => {
    expect(trips('numberOf(p).startsWith(q)')).toBe(true);
    expect(trips('numberOf(p).toLowerCase().includes(q)')).toBe(true);
  });

  it('but not asking the owner, and not the exact identity compare', () => {
    expect(trips('matchesCompetitorNumber(p, q)')).toBe(false);
    expect(trips('competitorMatchesQuery(p, q)')).toBe(false);
    // The deep link and the permalink compare a machine-generated number
    // whole; that is the identity read the accessor exists for.
    expect(trips('numberOf(p) === q')).toBe(false);
  });
});

describe('the write-result rule is importable without running the gate', () => {
  it('still names both spellings of the drift', () => {
    const trips = (line) => scanSource(line + '\n', WRITE_RULES).length === 1;
    expect(trips('if (res.applied === false) return;')).toBe(true);
    expect(trips('window.writeDidNotLand(res)')).toBe(true);
    expect(trips('if (writeDidNotLand(res)) return;')).toBe(false);
  });
});
