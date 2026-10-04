#!/usr/bin/env node
// Guards two things: the ONE rule that answers "did this score write
// actually land?", and the ONE phrase no production string may say.
//
// The landed rule lives in js/write_result.jsx as writeDidNotLand (queued OR
// superseded) and writeWasSuperseded (superseded only). Every consumer must ask
// one of those rather than re-deriving the test from the response shape.
//
// The phrase rule (operator ruling 2026-10-04) is unrelated in subject but
// shares this file's machinery and its Makefile wiring (js/check-imports):
// "default win" is not a term kendo has, so no production string may say it,
// the write_result.jsx held-write copy included -- name the recorded
// decision instead (kiken, fusenpai, fusensho) through decisionWord.
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
// No npm dependencies, Node.js built-ins only. The walk, the comment stripper
// and the line scan are shared with check-competitor-search.mjs through
// check-helpers.mjs; this file owns only the rule. FORBIDDEN is exported and
// the run is guarded on being the entry script so a test can import the rule.
//
// Usage:   node web-mobile/check-write-result.mjs
// Exit 0   no hand-rolled checks outside the owning module
// Exit 1   at least one site re-derives the rule

import { readFileSync } from 'node:fs';
import { resolve, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { walk, scanSource, printViolations } from './check-helpers.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)));
const JS_DIR = resolve(ROOT, 'js');

// The module that OWNS the rule, and is therefore the one place allowed to
// state it in terms of the raw response fields.
const OWNER = 'write_result.jsx';

// Tests are out of scope (check-helpers.mjs's SKIP_DIRS): a fixture may
// legitimately construct these shapes ({applied:false} as a mocked response
// body), and the thing being guarded is a PRODUCTION site branching on a
// hand-rolled test.

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
export const FORBIDDEN = [
  {
    // `persisted` is the same kind of field: whether a queued write reached
    // browser storage, which decides what the held-write notice may promise
    // (queuedNotice).
    re: /\.(applied|persisted)\s*(===|==|!==|!=)\s*(false|true)/,
    why: 'compares .applied/.persisted by hand; ask writeDidNotLand(res), writeWasSuperseded(res) or queuedNotice(res) instead',
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
    re: /window\.(writeDidNotLand|writeWasSuperseded|writeWasRefused|writeWasRefusedForClock|writeRetryable|notLandedBanner|SUPERSEDED_LEAD|SUPERSEDED_REASON|SUPERSEDED_ADVICE|supersededAlertText|writeHeldGroups|writePartlyHeld|CLOCK_SKEW_REASON_TEXT|CLOCK_SKEW_ADVICE|QUEUED_NOTICE|QUEUED_UNSAVED_NOTICE|queuedNotice|queuedWritesNoun|heldWritesText|writeNeedsWinner|writeDisplacedGroups|supersededBanner|displacedAlertText)\b/,
    why: 'reads an owned predicate/copy off window; those mirrors are deleted, import from write_result.jsx instead',
  },
  {
    // "Default win" is not a kendo term (operator ruling 2026-10-04): every
    // finished match has a result, a scoreline or a registered decision that
    // NAMES the winner. This rule is unlike the two above: it has no owner
    // allowed to state it directly, the OWNER module (write_result.jsx)
    // included, so it is NOT exempted for OWNER or for api_client.jsx below
    // (see the per-rule exemption sets). An identifier such as `defaultWin`
    // or `DEFAULT_WIN_STANDS_*` still passes: the space/hyphen is required,
    // and stripComments already removes comments before this runs, so a
    // comment explaining the ruling (this one included) is never a hit.
    re: /default[ -]win/i,
    why: 'says "default win", a term kendo does not have; name the recorded decision instead (kiken, fusenpai, fusensho) through decisionWord (write_result.jsx)',
  },
];

// api_client.jsx is the collaborator that turns an HTTP response INTO the
// discriminated result the predicates read, so it necessarily touches the raw
// field. It is not a consumer deciding what a write meant.
//
// The exemption is per-RULE, not per-file: api_client may compare `.applied`
// (it is the parser) but may NOT re-publish a window mirror, since restoring
// one would re-open the drift the migration closed, and it may not say
// "default win" either.
const ALLOWED = new Set(['api_client.jsx']);
const ALLOWED_RULE_INDEX = 0;

// The OWNER module is exempt from the two rules ABOVE it (it is allowed to
// state its own abstraction in the raw terms those rules forbid everywhere
// else), but not from the default-win rule: nothing, the owner included, may
// say "default win".
const OWNER_EXEMPT_RULE_INDICES = new Set([0, 1]);

export function findViolations() {
  const violations = [];
  for (const file of walk(JS_DIR)) {
    const isOwner = file.endsWith(OWNER);
    const isParser = [...ALLOWED].some((a) => file.endsWith(a));
    const rules = FORBIDDEN.filter((_, r) => {
      if (isOwner && OWNER_EXEMPT_RULE_INDICES.has(r)) return false;
      if (isParser && r === ALLOWED_RULE_INDEX) return false;
      return true;
    });
    if (rules.length === 0) continue;
    const rel = relative(ROOT, file);
    for (const hit of scanSource(readFileSync(file, 'utf8'), rules)) {
      violations.push({ rel, ...hit });
    }
  }
  return violations;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const violations = findViolations();
  if (violations.length === 0) {
    console.log('  ✓ the not-landed rule is asked, never re-derived');
    console.log('  ✓ no production string says "default win"');
    console.log('All write-result checks OK.');
    process.exit(0);
  }
  console.error('Write-result checks failed.\n');
  console.error(`The landed/superseded rule belongs to js/${OWNER} (writeDidNotLand / writeWasSuperseded);`);
  console.error('re-deriving it at a call site is how the sixth site was missed last time.');
  console.error('"default win" is not a kendo term; name the recorded decision instead (kiken, fusenpai, fusensho).\n');
  printViolations(violations);
  process.exit(1);
}
