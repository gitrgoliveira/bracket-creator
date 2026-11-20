// check-helpers.mjs: the scanning primitives behind the two RULE gates,
// check-write-result.mjs and check-competitor-search.mjs.
//
// Each gate owns its rule: the regexes, the owner module, the ruled
// exemptions and the prose saying why. This file owns the mechanics -- how the
// tree is walked, how comments are blanked, how a line is judged and how a hit
// is printed -- so the two gates agree on those without either carrying a copy.
//
// They did carry copies, byte-identical ones, and both copies had the same
// faults: every violation either gate reported after a block comment named the
// wrong line, and a "/*" inside a line comment could hide code from both.
// stripComments is now the one comment blanker here (check-imports.mjs reads
// it too), and a parser reads the comments. A sibling lives in js/__tests__/helpers/source.js for the vitest
// sweeps; it deliberately keeps trailing // comments (an absence assertion must
// not be fooled by prose, but a trailing comment is never a render), so it is
// a different contract, not a copy.
//
// Node.js built-ins plus the parser rolldown ships (see stripComments).

import { readdirSync, statSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

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

// scanSource: the lines of one file that trip a rule; the first matching rule
// wins per line. `line` is 1-based and indexes the REAL file (see above).
export function scanSource(src, rules, kind = 'js') {
  const hits = [];
  stripComments(src, kind).split('\n').forEach((line, i) => {
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
// does for a single-line hit. Hits come back in line order (rules are the
// outer loop, so a later rule's early hit would otherwise print after an
// earlier rule's late one); the sort is stable, so same-line hits keep rule
// order.
export function scanWholeFile(src, rules, kind = 'js') {
  const stripped = stripComments(src, kind);
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
  return hits.sort((a, b) => a.line - b.line);
}

// printViolations: one block per hit, in the file:line form editors link.
export function printViolations(violations) {
  for (const v of violations) {
    console.error(`  ${v.rel}:${v.line}`);
    console.error(`    ${v.text}`);
    console.error(`    ${v.why}\n`);
  }
}

// stripComments: the source with every comment replaced by spaces, newlines
// kept, so line i of the result is line i of the file and offsets do not move.
//
// A comment DESCRIBING a rule (which every gate and every owner module does, at
// length) must not read as a use of it, but a gate that then reports the wrong
// line sends the reader to prose. Trailing comments are blanked too
// (`p.number; // .includes(` must not trip a rule).
//
// What is a comment is decided by a real JavaScript parser (the oxc parser that
// rolldown ships, a pinned devDependency, the one check-load-cycles.mjs reads
// too), never by scanning characters: a hand-written scanner had to guess where
// a "/" begins a regular expression, where a quote opens a string and whether
// "//" is a URL, and each guess was a way to blank code or keep prose. The parser
// knows. It is tolerant: a file it cannot read to the end keeps the comments it
// found, which a real source file (compiled by esbuild) never meets.
//
// `kind` names the language of the text: 'js' (the default, JSX included), 'css'
// (block comments), or 'html' (HTML comments, plus the comments of an inline
// <script> body, read by the same parser, and of a <style> body).
const { parseSync } = await loadParser();

async function loadParser() {
  const resolved = createRequire(import.meta.url).resolve('rolldown/utils');
  return import(pathToFileURL(resolved).href);
}

const blank = (s) => s.replace(/[^\n]/g, ' ');
const CSS_COMMENT = /\/\*[\s\S]*?\*\//g;

function stripJsComments(src) {
  const { comments } = parseSync('source.jsx', src, { lang: 'jsx', sourceType: 'module' });
  let out = '';
  let at = 0;
  for (const { start, end } of comments) {
    out += src.slice(at, start) + blank(src.slice(start, end));
    at = end;
  }
  return out + src.slice(at);
}

function stripHtmlComments(src) {
  return src
    .replace(/(<script\b(?![^>]*\bsrc=)[^>]*>)([\s\S]*?)(<\/script>)/gi, (_, open, body, close) => open + stripJsComments(body) + close)
    .replace(/(<style\b[^>]*>)([\s\S]*?)(<\/style>)/gi, (_, open, body, close) => open + body.replace(CSS_COMMENT, blank) + close)
    .replace(/<!--[\s\S]*?-->/g, blank);
}

export function stripComments(src, kind = 'js') {
  if (kind === 'css') return src.replace(CSS_COMMENT, blank);
  return kind === 'html' ? stripHtmlComments(src) : stripJsComments(src);
}
