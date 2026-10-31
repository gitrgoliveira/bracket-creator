#!/usr/bin/env node
// Forbids lint-disable comments: the Go linter's no-lint directive, gosec's
// suppression comment, and the JS linters' disable directives (line,
// next-line and block forms).
//
// A disable comment silences one finding where it is written and says nothing
// to a reviewer reading the lint config. The operator's rule is that a rule is
// either right or wrong for the whole tree: change the code, or change the rule
// in the lint config (.golangci.yml, web-mobile/.oxlintrc.json,
// web/.oxlintrc.json), where a reviewer sees it. Today the count is zero; this
// gate keeps it there. oxlint's --report-unused-disable-directives is the
// second line of defence (a directive nobody needs fails the lint too).
//
// Scope: git-tracked Go and JS/JSX/MJS/CJS/TS/TSX sources only, via
// `git ls-files`, so an untracked scratch file or a build output never counts.
// Markdown, YAML and the docs prose are not scanned: they may describe the
// directives. node_modules, dist and vendor trees are third-party or generated
// output and are skipped.
//
// The patterns are written so this file passes its own scan: one letter of
// each directive sits in a character class, so the pattern's source text is
// not itself a match (the regex /lint-disab[l]e/ matches "lint-disable" but
// the characters "lint-disab[l]e" do not). Keep it that way when editing, and
// never spell a directive out in a comment here.
//
// No npm dependencies, Node.js built-ins only. The error printing is shared
// with the other gates through check-helpers.mjs; the scan is NOT, because the
// other gates blank comments before judging a line and this one is looking AT
// comments.
//
// Usage:   node web-mobile/check-no-disable-comments.mjs [file ...]
//          (no arguments: every tracked source; with arguments: those files)
// Exit 0   no disable directive found
// Exit 1   at least one found
//
// GO_RULES, JS_RULES, rulesFor and scanText are exported, and the run is
// guarded on being the entry script, so js/__tests__/rule_gates.test.jsx can
// prove a fixture trips each rule.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { resolve, dirname, relative, extname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { printViolations } from './check-helpers.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)));
const REPO = resolve(ROOT, '..');

const FIX =
  'change the code, or change the rule in the lint config where a reviewer sees it; a disable comment is not allowed';

export const GO_RULES = [
  {
    re: /\/\/\s*no[l]int\b|\bno[l]int:/,
    why: `Go lint-disable directive: ${FIX} (.golangci.yml)`,
  },
  // gosec's two suppression spellings (analyzer.go, findNoSecDirective): its
  // tag, a hash and the directive word, matched anywhere with a rule id run
  // on or not; and its directive comment, "gosec:" and "disable" after the
  // slashes. A third spelling exists only when a -nosec-tag flag or config
  // sets one, which a reviewer sees where it is set.
  {
    re: /#\s*nos[e]c/,
    why: `gosec suppression comment: ${FIX} (.golangci.yml's gosec rules, and the gosec flags of the Makefile's go/sec and go/sec-tests)`,
  },
  {
    re: /\/\/\s*gosec:disab[l]e/,
    why: `gosec suppression directive: ${FIX} (.golangci.yml's gosec rules, and the gosec flags of the Makefile's go/sec and go/sec-tests)`,
  },
];

// (ox|es)lint-disable, with or without the -line / -next-line suffix, in a
// line comment or a block comment: the directive name is the same in both.
export const JS_RULES = [
  {
    re: /\b(?:ox|es)lint-disab[l]e/,
    why: `JS lint-disable directive: ${FIX} (web-mobile/.oxlintrc.json or web/.oxlintrc.json)`,
  },
];

const GO_EXTS = new Set(['.go']);
const JS_EXTS = new Set(['.js', '.jsx', '.mjs', '.cjs', '.ts', '.tsx']);

// Third-party or generated: never ours to edit, so never ours to police.
const SKIP_SEGMENTS = new Set(['node_modules', 'dist', 'vendor']);

export function rulesFor(file) {
  const ext = extname(file);
  if (GO_EXTS.has(ext)) return GO_RULES;
  if (JS_EXTS.has(ext)) return JS_RULES;
  return [];
}

export function isSkipped(file) {
  return file.split(/[\\/]/).some((seg) => SKIP_SEGMENTS.has(seg));
}

// scanText: the lines of one source text that trip a rule; 1-based line, first
// matching rule wins per line. Raw text, comments included, on purpose.
export function scanText(src, rules) {
  const hits = [];
  src.split('\n').forEach((line, i) => {
    for (const { re, why } of rules) {
      if (re.test(line)) {
        hits.push({ line: i + 1, text: line.trim(), why });
        break;
      }
    }
  });
  return hits;
}

function trackedSources() {
  const out = execFileSync('git', ['-C', REPO, 'ls-files', '-z'], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  });
  return out.split('\0').filter(Boolean).map((f) => resolve(REPO, f));
}

export function findViolations(files = trackedSources()) {
  const violations = [];
  for (const file of files) {
    const rel = relative(REPO, file);
    if (isSkipped(rel.split(sep).join('/'))) continue;
    const rules = rulesFor(file);
    if (rules.length === 0 || !existsSync(file)) continue; // tracked but deleted in the tree
    for (const hit of scanText(readFileSync(file, 'utf8'), rules)) {
      violations.push({ rel, ...hit });
    }
  }
  return violations;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2).map((f) => resolve(f));
  const violations = findViolations(args.length > 0 ? args : undefined);
  if (violations.length === 0) {
    console.log('  ✓ no lint-disable comments in tracked Go or JS sources');
    console.log('All no-disable-comment checks OK.');
    process.exit(0);
  }
  console.error('Lint-disable comments found.\n');
  console.error('A rule is right or wrong for the whole tree. Change the code, or change the');
  console.error('rule in the lint config, where a reviewer sees it.\n');
  printViolations(violations);
  process.exit(1);
}
