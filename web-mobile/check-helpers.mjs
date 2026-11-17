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

// scanWholeFile: like scanSource, but tests each rule against the WHOLE
// comment-stripped file rather than one line at a time, so a rule whose
// production text can wrap across lines (a JSX text node split by the
// bundler's own line length, `{" "}`, string concatenation, or a literal
// newline inside a tag) is still caught. scanSource alone cannot see this:
// testing "default" and "win" on separate lines never trips a single-line
// regex even when they are one sentence apart. A rule meant for this scanner
// still reads naturally against a single line (there is no join inserted;
// stripComments already keeps every line where it was, so `\n` in the regex
// matches a real line break in the source).
//
// `line` is 1-based and counts the newlines in the stripped text BEFORE the
// match start, so it names where each match STARTS, exactly as scanSource
// does for a single-line hit.
export function scanWholeFile(src, rules) {
  const stripped = stripComments(src);
  const hits = [];
  for (const { re, why } of rules) {
    const flags = re.flags.includes('g') ? re.flags : `${re.flags}g`;
    const global = new RegExp(re.source, flags);
    let m;
    while ((m = global.exec(stripped)) !== null) {
      const line = stripped.slice(0, m.index).split('\n').length;
      hits.push({ line, text: m[0].replace(/\s+/g, ' ').trim(), why });
      if (m[0].length === 0) global.lastIndex += 1; // never loop on a zero-width match
    }
  }
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

// Whether a "/" at this point begins a regular-expression literal rather than a
// division, a JSX closing tag ("</div>") or JSX text: it does after an operator
// or an opening bracket, at the start of the file or after a keyword such as
// "return". "<" and ">" are left out on purpose, so "</div>" and "/>" never
// read as one; "=>" is the one exception.
function regexMayStart(out) {
  let tail = '';
  for (let k = out.length - 1; k >= 0 && tail.trimEnd().length < 8; k--) tail = out[k] + tail;
  tail = tail.trimEnd();
  if (!tail) return true;
  return /[(,=:[!&|?;{+\-*%~^]$/.test(tail) || tail.endsWith('=>')
    || /\b(?:return|typeof|case|in|of|delete|void|throw|else|do|yield|await)$/.test(tail);
}

// The end of the regular-expression literal that starts at src[i] (a "/"),
// flags included, or -1 when none closes on this line, so a stray "/" in JSX
// text is left to the normal scan.
function regexEnd(src, i) {
  let j = i + 1;
  let inClass = false;
  while (j < src.length && src[j] !== '\n') {
    const ch = src[j];
    if (ch === '\\') { j += 2; continue; }
    if (ch === '[') inClass = true;
    else if (ch === ']') inClass = false;
    else if (ch === '/' && !inClass) {
      j++;
      while (/[a-z]/i.test(src[j] || '')) j++;
      return j;
    }
    j++;
  }
  return -1;
}

// The source with every comment replaced by spaces (newlines kept, so offsets
// and line starts do not move). One pass over the text, aware of strings and
// template literals, so a "//" or "/*" inside a string is not a comment and a
// "/*" inside a // comment cannot open a phantom block. A whole-file regex
// strip is NOT used: it cuts string literals that hold those characters.
// A quote that is never closed ends at the line break (JSX text such as
// "Don't" holds a lone apostrophe), and a backslash in code protects the next
// character (the "\/*" of a regular expression is not a comment opener).
// A regular-expression literal is passed through whole, so a quote, a backtick
// or a "/*" inside one opens no string, template or comment (see
// regexMayStart for where a "/" can begin one).
export function maskComments(src) {
  const blank = (s) => s.replace(/[^\n]/g, ' ');
  const out = [];
  const n = src.length;
  const templateDepths = []; // brace depth at which each open ${ began
  let braceDepth = 0;
  let inTemplate = false;
  let i = 0;
  while (i < n) {
    const c = src[i];
    const d = src[i + 1];
    if (inTemplate) {
      if (c === '\\') { out.push(src.slice(i, i + 2)); i += 2; continue; }
      if (c === '`') { inTemplate = false; out.push(c); i++; continue; }
      if (c === '$' && d === '{') {
        templateDepths.push(braceDepth);
        braceDepth++;
        inTemplate = false;
        out.push('${');
        i += 2;
        continue;
      }
      out.push(c);
      i++;
      continue;
    }
    if (c === '/' && d === '/') {
      let j = src.indexOf('\n', i);
      if (j === -1) j = n;
      out.push(blank(src.slice(i, j)));
      i = j;
      continue;
    }
    if (c === '/' && d === '*') {
      let j = src.indexOf('*/', i + 2);
      j = j === -1 ? n : j + 2;
      out.push(blank(src.slice(i, j)));
      i = j;
      continue;
    }
    if (c === "'" || c === '"') {
      let j = i + 1;
      while (j < n && src[j] !== c && src[j] !== '\n') j += src[j] === '\\' ? 2 : 1;
      if (src[j] === c) j++;
      out.push(src.slice(i, j));
      i = j;
      continue;
    }
    if (c === '/' && regexMayStart(out)) {
      const j = regexEnd(src, i);
      if (j !== -1) { out.push(src.slice(i, j)); i = j; continue; }
    }
    if (c === '`') { inTemplate = true; out.push(c); i++; continue; }
    if (c === '\\') { out.push(src.slice(i, i + 2)); i += 2; continue; }
    if (c === '{') braceDepth++;
    if (c === '}') {
      braceDepth--;
      if (templateDepths.length && templateDepths[templateDepths.length - 1] === braceDepth) {
        templateDepths.pop();
        inTemplate = true;
      }
    }
    out.push(c);
    i++;
  }
  return out.join('');
}
