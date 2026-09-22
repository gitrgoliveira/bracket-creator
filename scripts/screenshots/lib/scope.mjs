// Which families does a set of changed files reach?
//
// `SINCE=<ref>` asks the runner to seed and capture only the families whose
// inputs changed, instead of all of them. The unit is the FAMILY, not the
// capture: seeding is most of a run's cost (one family alone is ~85s of ~170s)
// and every capture in a family rides the same seed, so skipping a capture
// inside a family that still seeds saves nothing.
//
// "Changed" is git's answer, never a file's modified date. Git stores no dates;
// a working copy's dates record when THAT checkout last wrote each file, so a
// fresh clone stamps everything at once and a branch switch restamps whatever
// differs. A date compare would say "regenerate everything" after most
// checkouts and nothing useful the rest of the time.
//
// Three rules, in this order, and the runner prints which one fired for every
// path so a wrong scope is visible before the seeding starts:
//
//   1. A harness file (the runner, lib/, the registry, the Makefile) can alter
//      any capture, so it selects every family.
//   2. A path a family CLAIMS - by a `sources` prefix, or by being one of the
//      recipe files that declares it or holds its recipes - selects that
//      family. A family that declares no `sources` is taken to depend on all
//      application source, so a new family never silently sits out a scoped
//      run.
//   3. Application source that no family claims selects every family. The
//      shared modules (styles.css, app.jsx, the Go handlers, the scoreboard
//      primitives) are deliberately claimed by nobody, because a change there
//      can reach any surface. Unclaimed is the SAFE case, not the skip case.
//
// A path matching none of those (docs prose, a test) selects nothing, and a
// run whose scope is empty says so and exits 0 - a real outcome, not an error.
//
// Claims are plain path prefixes. A glob or import-graph walk would be more
// precise and would rot; a coarse prefix over-selects, which costs a seed, and
// never under-selects, which would cost a wrong screenshot.
import { execFileSync } from 'node:child_process';
import { REPO } from './server.mjs';

const HARNESS = [
  'scripts/screenshots/run.mjs',
  'scripts/screenshots/lib/',
  'scripts/screenshots/recipes/index.mjs',
  'scripts/screenshots/package',
  'Makefile',
];

const APPLICATION = ['web-mobile/', 'web/', 'internal/', 'cmd/', 'main.go', 'go.mod', 'go.sum'];

const under = (p, prefixes) => prefixes.some((x) => p.startsWith(x));

// Everything git considers changed: the branch against `since`, the index and
// worktree against HEAD, and files git does not know about yet. Ignored files
// (the build's version stamps, out/, node_modules) never appear.
export function changedPaths(since) {
  const git = (...argv) => execFileSync('git', argv, { cwd: REPO, encoding: 'utf8' })
    .split('\n').filter(Boolean);
  return new Set([
    ...git('diff', '--name-only', `${since}...HEAD`),
    ...git('diff', '--name-only', 'HEAD'),
    ...git('ls-files', '--others', '--exclude-standard'),
  ]);
}

// -> { selected: Set<family>, reasons: [[path, why], ...] }
export function scope(paths, families, recipeFiles) {
  const all = Object.keys(families);
  const undeclared = all.filter((f) => !families[f].sources);
  const claims = (f, p) => under(p, families[f].sources || [])
    || (recipeFiles.get(f) || new Set()).has(p);

  const selected = new Set();
  const reasons = [];
  const take = (p, names, why) => {
    names.forEach((f) => selected.add(f));
    reasons.push([p, why || names.join(', ')]);
  };

  for (const p of [...paths].sort()) {
    if (under(p, HARNESS)) {
      take(p, all, 'harness file: every family');
      continue;
    }
    const explicit = all.filter((f) => claims(f, p));
    if (under(p, APPLICATION)) {
      if (explicit.length) take(p, [...new Set([...explicit, ...undeclared])]);
      else take(p, all, 'application source no family claims: every family');
      continue;
    }
    take(p, explicit, explicit.length ? null : 'no capture depends on it');
  }
  return { selected, reasons };
}
