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

  it('the accessors, which the first regex missed', () => {
    expect(trips('numberOf(p).startsWith(q)')).toBe(true);
    expect(trips('numberOf(p).toLowerCase().includes(q)')).toBe(true);
    // The draw-selecting arm reads the prefix; re-deriving it is the same drift.
    expect(trips('prefixOf(p).toLowerCase().startsWith(q)')).toBe(true);
  });

  it('but not asking the owner, and not the exact identity compare', () => {
    expect(trips('matchesCompetitorNumber(p, q)')).toBe(false);
    expect(trips('competitorMatchesQuery(p, q)')).toBe(false);
    // The permalink compares a machine-generated number whole; that is the
    // identity read the accessor exists for.
    expect(trips('numberOf(p) === q')).toBe(false);
  });
});

describe('the write-result rule is importable without running the gate', () => {
  it('still names both spellings of the drift', () => {
    const trips = (line) => scanSource(line + '\n', WRITE_RULES).length === 1;
    expect(trips('if (res.applied === false) return;')).toBe(true);
    expect(trips('window.writeDidNotLand(res)')).toBe(true);
    expect(trips('window.writeWasRefused(res)')).toBe(true);
    expect(trips('window.writeRetryable(res)')).toBe(true);
    // The held-write copy (bc-offl): a script-tagged surface reaching for it
    // off window would render "undefined" where the notice belongs.
    expect(trips('<span>{window.QUEUED_NOTICE}</span>')).toBe(true);
    expect(trips('window.heldWritesText(status, counts)')).toBe(true);
    expect(trips('window.queuedWritesNoun(1, 1)')).toBe(true);
    // bc-mrgc: the kept-in-history copy and the partial-apply predicates.
    expect(trips('window.writePartlyHeld(res)')).toBe(true);
    expect(trips('window.writeHeldGroups(res)')).toBe(true);
    expect(trips('<span>{window.SUPERSEDED_LEAD}</span>')).toBe(true);
    expect(trips('window.supersededAlertText(1, true)')).toBe(true);
    // Whether a queued write reached browser storage decides what its notice
    // may promise: asked through queuedNotice, never compared by hand.
    expect(trips('if (res.persisted === false) warn();')).toBe(true);
    expect(trips('<span>{window.queuedNotice(res)}</span>')).toBe(true);
    expect(trips('<span>{window.QUEUED_UNSAVED_NOTICE}</span>')).toBe(true);
    expect(trips('<span>{queuedNotice(pendingWrite)}</span>')).toBe(false);
    // The needs-winner reading: a held change and a later change moved to
    // the history are told apart by the owner alone.
    expect(trips('window.writeNeedsWinner(res)')).toBe(true);
    expect(trips('window.writeDisplacedGroups(res)')).toBe(true);
    expect(trips('<span>{QUEUED_NOTICE}</span>')).toBe(false);
    expect(trips('if (writeDidNotLand(res)) return;')).toBe(false);
  });
});

// The "default win" phrase rule (operator ruling 2026-10-04): "default win"
// is not a kendo term, so no production string may say it. Unlike the two
// rules above it, this one is deliberately global -- it has no owner allowed
// to state it directly -- so it is checked here only against scanSource
// directly (comment-stripped), the same mechanism findViolations uses, never
// against a hand-rolled owner exemption.
describe('the "default win" phrase rule has no legitimate spelling', () => {
  const trips = (line) => scanSource(line + '\n', WRITE_RULES).length === 1;

  it('catches the space and the hyphen, in either case', () => {
    expect(trips('return "Record default win for Tora";')).toBe(true);
    expect(trips('return "Clear default-win";')).toBe(true);
    expect(trips('return "DEFAULT WIN recorded";')).toBe(true);
  });

  it('does not trip on an identifier -- the space/hyphen is required', () => {
    expect(trips('export function writeDefaultWinStands(res) { return defaultWin; }')).toBe(false);
    expect(trips('export const DEFAULT_WIN_STANDS_REASON = 1;')).toBe(false);
  });

  it('does not trip inside a comment', () => {
    expect(trips('// "default win" does not exist in kendo')).toBe(false);
  });

  it('names the recorded decision instead, which never trips it', () => {
    expect(trips('return `Record fusensho for ${name}`;')).toBe(false);
    expect(trips('return decisionWord(decision);')).toBe(false);
  });
});
