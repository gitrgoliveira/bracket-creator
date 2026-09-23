#!/usr/bin/env node
// Guards the competitor-NUMBER match rule: a picker that filters a list by
// what the reader typed must ASK that rule rather than re-derive it.
//
// A rule, not a monopoly (operator correction 2026-09-22). The app has other
// people-searches on purpose -- the registration desk's fuzzy RANKED search
// answers "who did they most likely mean" with a score, and Participants &
// seeds keeps its own haystack -- and both are ruled exemptions, carried as
// data in ALLOWED below rather than left to prose.
//
// The rule lives in js/competitor_search.jsx as matchesCompetitorNumber (and
// the wider competitorMatchesQuery it composes into). Every consumer must ask
// that rather than re-deriving a number test from the raw `.number` field or
// its accessor, competitor_identity.jsx's numberOf.
//
// This check exists because the codebase has already paid for that once. The
// question was spelled three different ways in three files: viewer_schedule
// used .includes(q), viewer_watchlist used .startsWith(q), and
// admin_participants did not search numbers at all. For the string "K12",
// includes("12") is true and startsWith("12") is false, so the same keystroke
// found a person on one surface and nobody on its sibling, under a
// placeholder promising both. A predicate a caller must remember to re-derive
// is a predicate that drifts, and the drift is invisible: every one of those
// sites compiled, linted and passed its tests while being subtly wrong.
//
// So the convention is enforced rather than documented: a hand-rolled number
// test, anywhere except the module that owns the rule, fails the build and
// names itself. See the SCOPE note below for what is and is not policed, and
// why policing more than this was a mistake.
//
// If you are adding a genuinely new number-matching need, add it to
// competitor_search.jsx and let every consumer inherit it. That is the
// entire point.
//
// No npm dependencies, Node.js built-ins only. The walk, the comment stripper
// and the line scan are shared with check-write-result.mjs through
// check-helpers.mjs; this file owns only the rule.
//
// Usage:   node web-mobile/check-competitor-search.mjs
// Exit 0   no hand-rolled number checks outside the owning module
// Exit 1   at least one site re-derives the rule
//
// FORBIDDEN is exported and the run is guarded on being the entry script, so
// js/__tests__/rule_gates.test.jsx can import the rule and prove a fixture
// line trips it. The gate itself had no test, which is how the accessor form
// (`numberOf(p).startsWith(q)`) shipped past its first regex unpoliced.

import { readFileSync } from 'node:fs';
import { resolve, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { walk, scanSource, printViolations } from './check-helpers.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)));
const JS_DIR = resolve(ROOT, 'js');

// The module that OWNS the rule, and is therefore the one place allowed to
// state a number comparison in terms of the raw `.number` field.
const OWNER = 'competitor_search.jsx';

// The two surfaces the operator ruled OUT of this rule on 2026-09-21. They are
// listed here, not only in the owner's prose, because the enforcer is what a
// future maintainer will actually consult:
//
//   admin_participants.jsx      the roster search does not search numbers at all
//   admin_registration_desk.jsx keeps its fuzzy ranked search, so it still finds
//                               K12 from a bare "12"
//
// Without this the desk escapes only by SHAPE -- rdHaystack joins the number
// into a string and scores it as a subsequence, which the regex below cannot
// see. Refactor that haystack to a plain `.includes` and the build would break,
// and the obvious repair (route it through the owner) would silently overturn
// the ruling. Named exemptions fail loudly instead.
const ALLOWED = new Set(['admin_participants.jsx', 'admin_registration_desk.jsx']);

// Tests are out of scope (check-helpers.mjs's SKIP_DIRS): a fixture may
// legitimately construct these shapes, and the thing being guarded is a
// PRODUCTION site branching on a hand-rolled number test.

// SCOPE, deliberately narrow. This does not try to police every possible way
// of comparing a number -- check-write-result.mjs's own SCOPE note explains
// why that is a mistake: an earlier draft of that check policed a second
// field too and flagged ten sites, every one of them legitimate, and a check
// that cries wolf gets switched off, which is strictly worse than no check.
//
// What IS worth enforcing is the exact shape that caused the incident: a
// read of the number -- the raw `.number` field or the `numberOf(` accessor
// that wraps it -- landing on the same line as `.includes(` or `.startsWith(`,
// which is precisely how the three surfaces drifted apart. The accessor arm
// is not a widening of the scope: the first regex policed the field alone,
// so the moment a consumer read the number through numberOf the very same
// hand-rolled test walked past the gate.
//
// The exact comparison `numberOf(p) === q` is NOT policed, on purpose: the
// deep link and the permalink compare a machine-generated number whole, which
// is the identity read the accessor exists for, not a re-derived match rule.
export const FORBIDDEN = [
  {
    re: /(\.number\b|numberOf\().*\.(includes|startsWith)\(/,
    why: 'hand-rolls a competitor-number match; call matchesCompetitorNumber(p, q) (or competitorMatchesQuery) from competitor_search.jsx instead',
  },
];

export function findViolations() {
  const violations = [];
  for (const file of walk(JS_DIR)) {
    if (file.endsWith(OWNER)) continue;
    if ([...ALLOWED].some((a) => file.endsWith(a))) continue; // exempt by operator ruling
    const rel = relative(ROOT, file);
    for (const hit of scanSource(readFileSync(file, 'utf8'), FORBIDDEN)) {
      violations.push({ rel, ...hit });
    }
  }
  return violations;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const violations = findViolations();
  if (violations.length === 0) {
    console.log('  ✓ the competitor-number match rule is asked, never re-derived');
    console.log('All competitor-search checks OK.');
    process.exit(0);
  }
  console.error('Hand-rolled competitor-number match checks found.\n');
  console.error(`The rule belongs to js/${OWNER} (matchesCompetitorNumber).`);
  console.error('Re-deriving it at a call site is how the three surfaces drifted last time.\n');
  printViolations(violations);
  process.exit(1);
}
