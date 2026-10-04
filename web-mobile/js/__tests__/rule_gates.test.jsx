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
import { stripComments, scanSource, scanWholeFile } from '../../check-helpers.mjs';
import { FORBIDDEN as NUMBER_RULES } from '../../check-competitor-search.mjs';
import { FORBIDDEN as WRITE_RULES } from '../../check-write-result.mjs';

// The two default-win rules (WRITE_RULES[2] and [3]) are the ones
// findViolations scans WHOLE-FILE (scanWholeFile), not line by line: see
// check-write-result.mjs's WHOLE_FILE_RULE_INDICES. Tests below that exercise
// a cross-line shape use scanWholeFile directly with just these two, rather
// than scanSource with the full WRITE_RULES array, since scanSource cannot
// see across a line break by construction.
const DEFAULT_WIN_RULES = [WRITE_RULES[2], WRITE_RULES[3]];

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
// directly (comment-stripped), never against a hand-rolled owner exemption.
// These single-line fixtures work the same way whichever scanner runs them
// (scanSource or scanWholeFile): there is no line break for the two to
// disagree about. See the next describe block for the shapes that only a
// whole-file scan -- findViolations' real mechanism for this rule -- can see.
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

// bc-cse FIX 2: the line-based scanner above never saw the shape that
// actually shipped (commit 76ffb044, admin_scoring_shared.jsx ~1878): a JSX
// text node the formatter wrapped so "the default" ends one line and "win
// when..." begins the next. Neither line contains the phrase alone, so a
// per-line regex missed it. These pin the whole-file scanner (scanWholeFile,
// what findViolations actually runs the default-win rules through) against
// that shape and its siblings: a JSX expression splice, string
// concatenation, and a non-breaking space, every one of which renders as the
// same two words a reader sees run together.
describe('the "default win" rule sees a wrap a line-based scan cannot', () => {
  const tripsWhole = (src) => scanWholeFile(src, DEFAULT_WIN_RULES).length === 1;

  it('the exact 76ffb044 shape: a JSX text node wrapped across two lines', () => {
    // Reproduces admin_scoring_shared.jsx as it shipped at 76ffb044, before
    // this fix: "default" is the last word of one line, "win" the first word
    // of the next.
    const src = [
      '            <p data-testid="clear-withdrawal-consequence" style={{ margin: "6px 0 0" }}>',
      '              This reopens the match: it goes back to running and {who || "the withdrawn side"} can',
      '              compete again. {winnerName || "The winner"}&apos;s points were replaced by the default',
      '              win when the withdrawal was recorded, so enter them again; {who ? `${who}\'s` : "the withdrawn side\'s"} points',
      '              are kept. Then score the rest and finish it.',
      '            </p>',
    ].join('\n');
    const hits = scanWholeFile(src, DEFAULT_WIN_RULES);
    expect(hits.length).toBe(1);
    // Reports the line the match STARTS on (the "default" line), not the
    // "win" line it ends on -- the same convention scanSource uses.
    expect(hits[0].line).toBe(3);
  });

  it('a JSX expression splice between the words', () => {
    expect(tripsWhole('const t = <>the default{" "}win stands</>;')).toBe(true);
  });

  it('string concatenation between the words', () => {
    expect(tripsWhole("const t = 'the default ' + 'win stands';")).toBe(true);
  });

  it('a non-breaking space between the words', () => {
    expect(tripsWhole('const t = "the default win stands";')).toBe(true);
  });

  it('still requires a real separator, even scanned whole-file', () => {
    // The identifier concern survives the switch to whole-file scanning:
    // nothing about scanning the whole file should make a zero-separator
    // camelCase identifier start tripping the rule.
    expect(tripsWhole('export function writeDefaultWinStands(res) {\n  return defaultWin;\n}\n')).toBe(false);
  });
});

// The second spelling (operator ruling 2026-10-04, "wins by default"):
// naming a side as winning "by default" is the same banned concept without
// the literal words "default win" adjacent. Same whole-file scanning, so a
// wrap between "win" and "by default" is caught too.
describe('the "wins ... by default" phrase rule', () => {
  const tripsWhole = (src) => scanWholeFile(src, DEFAULT_WIN_RULES).length === 1;

  it('catches a side named as winning by default', () => {
    expect(tripsWhole('return `${name} wins 2–0 by default.`;')).toBe(true);
    expect(tripsWhole('return "Kyoto win by default this round.";')).toBe(true);
  });

  it('does not reach across a line break either -- the [^.\\n] bound is deliberate', () => {
    // Unlike the "default ... win" pattern above, this one's gap is bounded
    // by [^.\n]{0,40}: a sentence a formatter wraps onto a second line is
    // exactly the shape the bound is there to stop short of, the same as a
    // period. Reported separately because this is NOT a gap in the rule --
    // "wins" and "by default" that far apart, even on one line, read as two
    // different claims, so widening the bound to swallow a line break would
    // widen it to swallow a whole paragraph too.
    const src = [
      'const msg = `${name} wins the encounter',
      '  by default.`;',
    ].join('\n');
    expect(tripsWhole(src)).toBe(false);
  });

  it('does not reach across a sentence break into an unrelated "by default"', () => {
    expect(tripsWhole('return "Kyoto wins the first bout. The second is forfeited by default.";')).toBe(false);
  });

  it('does not trip inside a comment', () => {
    expect(tripsWhole('// a side that wins by default is never shown that way')).toBe(false);
  });

  it('names the recorded decision instead, which never trips it', () => {
    expect(tripsWhole('return `Record fusensho for ${name}`;')).toBe(false);
  });
});
