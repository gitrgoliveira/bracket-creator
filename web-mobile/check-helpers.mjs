// check-helpers.mjs: the scanning primitives behind the two RULE gates,
// check-write-result.mjs and check-competitor-search.mjs.
//
// Each gate owns its rule: the regexes, the owner module, the ruled
// exemptions and the prose saying why. This file owns the mechanics -- how the
// tree is walked, how comments are blanked, how a line is judged and how a hit
// is printed -- so the two gates agree on those without either carrying a copy.
//
// They did carry copies, byte-identical ones, and both copies had the same
// faults (see stripComments): every violation either gate reported after a
// block comment named the wrong line, and a "/*" inside a line comment could
// hide code from both. A third stripper lives in js/__tests__/helpers/source.js
// for the vitest sweeps; it deliberately keeps trailing // comments (an
// absence assertion must not be fooled by prose, but a trailing comment is
// never a render), so it is a sibling with a different contract, not a copy.
//
// No npm dependencies, Node.js built-ins only, like the gates themselves.

import { readdirSync, statSync } from 'node:fs';
import { resolve } from 'node:path';

// Tests may legitimately construct a forbidden shape as a fixture and assert
// on it; they are not consumers deciding control flow. dist/ is the compiled
// copy of js/, vendor/ and node_modules/ are not ours.
export const SKIP_DIRS = new Set(['__tests__', 'dist', 'vendor', 'node_modules']);

export function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = resolve(dir, entry);
    if (statSync(full).isDirectory()) {
      if (!SKIP_DIRS.has(entry)) yield* walk(full);
      continue;
    }
    if (entry.endsWith('.jsx') || entry.endsWith('.js')) yield full;
  }
}

// stripComments: blank out comments WITHOUT moving anything, so line i of the
// result is line i of the file. A comment DESCRIBING a rule (which every gate
// and every owner module does, at length) must not read as a use of it, but a
// gate that then reports the wrong line sends the reader to prose.
//
// The two faults the gates' old copies shared:
//
//   - a block comment was deleted outright, so every line after one shifted up
//     by the comment's height;
//   - line comments were matched with ^\s*, which under the m flag also eats
//     the blank lines ABOVE the comment -- another shift, one blank at a time.
//
// And a third, shared with an earlier copy in the test helpers: stripping
// block comments in a pass BEFORE line comments lets a "/*" inside a //
// comment (a route glob such as /api/competitions/:id/*) open a phantom block
// that swallows code up to the next "*/". Code a gate never sees is code it
// never judges. One alternation pass, line comments first, closes all three: a
// block comment is replaced by its own newlines, a line comment by nothing.
//
// Trailing comments ARE stripped here (`p.number; // .includes(` must not trip
// a rule), guarded by the character before the slashes so a URL in code, whose
// "//" follows a ":", survives.
export function stripComments(src) {
  return src.replace(
    /^[ \t]*\/\/.*$|\/\*[\s\S]*?\*\/|([^:])\/\/.*$/gm,
    (m, before) => (before !== undefined ? before : m.replace(/[^\n]/g, '')),
  );
}

// scanSource: the lines of one file that trip a rule; the first matching rule
// wins per line. `line` is 1-based and indexes the REAL file (see above).
export function scanSource(src, rules) {
  const hits = [];
  stripComments(src).split('\n').forEach((line, i) => {
    for (const { re, why } of rules) {
      if (re.test(line)) {
        hits.push({ line: i + 1, text: line.trim(), why });
        break;
      }
    }
  });
  return hits;
}

// printViolations: one block per hit, in the file:line form editors link.
export function printViolations(violations) {
  for (const v of violations) {
    console.error(`  ${v.rel}:${v.line}`);
    console.error(`    ${v.text}`);
    console.error(`    ${v.why}\n`);
  }
}
