#!/usr/bin/env node
// Guards the ONE rule that answers "did this score write actually land?".
//
// The rule lives in js/write_result.jsx as writeDidNotLand (queued OR
// superseded) and writeWasSuperseded (superseded only). Every consumer must ask
// one of those rather than re-deriving the test from the response shape.
//
// This check exists because the codebase has already paid for that twice. The
// question was originally spelled `res.queued` inline at five call sites; when
// the server gained a SECOND not-landed shape (200 {"applied": false}, a write
// the timestamp guard dropped because a newer result won), the conversion
// reached five sites and missed a sixth -- which happened to be the guard on a
// hard prerequisite, so a dependent request ran against server state the
// operator had never seen. A predicate a caller must remember to re-derive is a
// predicate that drifts, and the drift is invisible: every one of those sites
// compiles, lints and passes its tests while being subtly wrong.
//
// So the convention is enforced rather than documented: a hand-rolled test of
// the refusal field, anywhere except the module that owns the rule, fails the
// build and names itself. See the SCOPE note below for what is and is not
// policed, and why policing more than this was a mistake.
//
// If you are adding a genuinely new shape, add it to write_result.jsx and let
// every consumer inherit it. That is the entire point.
//
// No npm dependencies, Node.js built-ins only.
//
// Usage:   node web-mobile/check-write-result.mjs
// Exit 0   no hand-rolled checks outside the owning module
// Exit 1   at least one site re-derives the rule

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { resolve, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)));
const JS_DIR = resolve(ROOT, 'js');

// The module that OWNS the rule, and is therefore the one place allowed to
// state it in terms of the raw response fields.
const OWNER = 'write_result.jsx';

// Tests may legitimately construct these shapes as fixtures ({applied:false} as
// a mocked response body) and assert on them. They are not consumers deciding
// control flow, so they are out of scope -- the thing being guarded is a
// PRODUCTION site branching on a hand-rolled test.
const SKIP_DIRS = new Set(['__tests__', 'dist', 'vendor', 'node_modules']);

// SCOPE, deliberately narrow. An earlier draft of this check policed `.queued`
// too and flagged ten sites, every one of them legitimate: lineup writes (a
// different endpoint with a different response shape), the pending-write
// banners (which are about queued specifically, since pending is not failed),
// and the override path. A check that cries wolf gets switched off, which is
// strictly worse than no check.
//
// What IS worth enforcing is the shape that caused the incident: `applied`,
// the field that says the server refused the write. Outside the module that
// owns the rule and the client that parses the response, nothing should be
// comparing it by hand -- that comparison is the one that has to stay in step
// with a rule the owner might extend.
const FORBIDDEN = [
  {
    re: /\.applied\s*(===|==|!==|!=)\s*(false|true)/,
    why: 'compares .applied by hand; ask writeDidNotLand(res) or writeWasSuperseded(res) instead',
  },
  {
    // The SECOND way the rule drifts, learned the hard way. These names were
    // once mirrored on `window` for script-tagged surfaces; every consumer now
    // ES-imports the leaf instead, and the mirrors are gone. Reading one off
    // `window` therefore yields undefined -- which is not merely broken but
    // SILENTLY broken at the two admin_shiaijo call sites that sit inside
    // `catch (_e) {}`, where the TypeError is swallowed and the surface
    // degrades to exactly the "looks saved but was refused" behaviour this
    // whole rule exists to prevent. A copy constant read off `window` is worse
    // still: it renders as the literal text "undefined" in an operator banner.
    // Import from write_result.jsx; do not re-add a mirror.
    re: /window\.(writeDidNotLand|writeWasSuperseded|writeWasRefusedForClock|notLandedBanner|SUPERSEDED_REASON|SUPERSEDED_ADVICE|CLOCK_SKEW_REASON_TEXT|CLOCK_SKEW_ADVICE)\b/,
    why: 'reads an owned predicate/copy off window; those mirrors are deleted, import from write_result.jsx instead',
  },
];

// api_client.jsx is the collaborator that turns an HTTP response INTO the
// discriminated result the predicates read, so it necessarily touches the raw
// field. It is not a consumer deciding what a write meant.
//
// The exemption is per-RULE, not per-file: api_client may compare `.applied`
// (it is the parser) but may NOT re-publish a window mirror, since restoring
// one would re-open the drift the migration closed.
const ALLOWED = new Set(['api_client.jsx']);
const ALLOWED_RULE_INDEX = 0;

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = resolve(dir, entry);
    if (statSync(full).isDirectory()) {
      if (!SKIP_DIRS.has(entry)) yield* walk(full);
      continue;
    }
    if (entry.endsWith('.jsx') || entry.endsWith('.js')) yield full;
  }
}

// Strip line and block comments so a comment DESCRIBING the rule (which several
// of these files legitimately do, at length) is not mistaken for a use of it.
function stripComments(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')
    .replace(/([^:])\/\/.*$/gm, '$1');
}

const violations = [];

for (const file of walk(JS_DIR)) {
  const rel = relative(ROOT, file);
  if (file.endsWith(OWNER)) continue;
  const exempt = [...ALLOWED].some((a) => file.endsWith(a));
  const lines = stripComments(readFileSync(file, 'utf8')).split('\n');
  lines.forEach((line, i) => {
    for (let r = 0; r < FORBIDDEN.length; r++) {
      if (exempt && r === ALLOWED_RULE_INDEX) continue;
      const { re, why } = FORBIDDEN[r];
      if (re.test(line)) {
        violations.push({ rel, line: i + 1, text: line.trim(), why });
        break;
      }
    }
  });
}

if (violations.length === 0) {
  console.log('  ✓ the not-landed rule is asked, never re-derived');
  console.log('All write-result checks OK.');
  process.exit(0);
}

console.error('Hand-rolled "did the write land?" checks found.\n');
console.error(`The rule belongs to js/${OWNER} (writeDidNotLand / writeWasSuperseded).`);
console.error('Re-deriving it at a call site is how the sixth site was missed last time.\n');
for (const v of violations) {
  console.error(`  ${v.rel}:${v.line}`);
  console.error(`    ${v.text}`);
  console.error(`    ${v.why}\n`);
}
process.exit(1);
