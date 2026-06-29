#!/usr/bin/env node
// Static cross-module import/export checker for mp-zac3 split modules.
//
// Validates that every named import from a sibling .jsx module is actually
// exported by that module. Catches the ESM blind spot: esbuild transpile-only
// and vitest do NOT fail on a missing named export, only native browser ESM
// does. This script gives the same check deterministically in CI.
//
// No npm dependencies, uses Node.js built-ins only.
//
// Usage:   node web-mobile/check-imports.mjs
// Exit 0   all imports resolved
// Exit 1   one or more named imports missing from their source module

import { readFileSync, existsSync } from 'node:fs';
import { resolve, dirname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const JS_DIR = resolve(dirname(fileURLToPath(import.meta.url)), 'js');

// Strip block (/* */) and line (//) comments from a captured import/export
// brace body. Without this, an inline comment on a line ending in a comma
// swallows the following identifier when we split on commas (the trimmed token
// becomes "// comment\n  name", which fails the ^\w+ match and is dropped),
// silently defeating the check for that name.
function stripComments(s) {
  return s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/[^\n]*/g, '');
}

// Modules introduced by the mp-zac3 split, the ones whose imports we verify.
// Add entries here if more sibling splits are made.
const CHECK_MODULES = [
  'admin_scoring_modal.jsx',
  'admin_scoring_individual.jsx',
  'admin_scoring_team.jsx',
  'admin_scoring_autosave.jsx',
];

// ---------------------------------------------------------------------------
// Export extraction
// ---------------------------------------------------------------------------
// Returns a Set of every name the file makes available to importers.
// Handles:
//   export { a, b, localC as exportedD }
//   export function Foo / export async function Foo
//   export const / export let / export var / export class
// Does NOT handle `export default` (not used in these modules).

function extractExports(src) {
  const names = new Set();

  // export { a, b as c, … }, possibly multiline
  for (const m of src.matchAll(/export\s*\{([^}]+)\}/gs)) {
    for (const item of stripComments(m[1]).split(',')) {
      // 'local as exported' → exported is what importers see
      const parts = item.trim().match(/^(\w+)(?:\s+as\s+(\w+))?/);
      if (parts) names.add(parts[2] ?? parts[1]);
    }
  }

  // export [async] function/const/let/var/class Name
  for (const m of src.matchAll(/^export\s+(?:async\s+)?(?:function|const|let|var|class)\s+(\w+)/gm)) {
    names.add(m[1]);
  }

  return names;
}

// ---------------------------------------------------------------------------
// Import extraction
// ---------------------------------------------------------------------------
// Returns an array of { importedNames: string[], fromPath: string } for every
// `import { … } from './…'` in the source (relative paths only).

function extractSiblingImports(src) {
  const results = [];
  for (const m of src.matchAll(/import\s*\{([^}]+)\}\s*from\s*['"](\.[^'"]+)['"]/gs)) {
    const names = stripComments(m[1]).split(',').map(s => {
      // 'exported as local' → exported is the name we're requesting from the source
      const parts = s.trim().match(/^(\w+)(?:\s+as\s+\w+)?$/);
      return parts ? parts[1] : null;
    }).filter(Boolean);
    if (names.length > 0) results.push({ importedNames: names, fromPath: m[2] });
  }
  return results;
}

// ---------------------------------------------------------------------------
// Main check loop
// ---------------------------------------------------------------------------

const exportCache = new Map();

function getCachedExports(absPath) {
  if (!exportCache.has(absPath)) {
    if (!existsSync(absPath)) {
      console.error(`  ERROR: source file not found: ${absPath}`);
      exportCache.set(absPath, new Set());
    } else {
      exportCache.set(absPath, extractExports(readFileSync(absPath, 'utf8')));
    }
  }
  return exportCache.get(absPath);
}

let totalErrors = 0;

for (const modName of CHECK_MODULES) {
  const modPath = resolve(JS_DIR, modName);
  if (!existsSync(modPath)) {
    // A listed split module that has vanished almost always means a bad
    // rename/move, failing the check is correct, not a soft skip, otherwise
    // js/validate would go green on a broken refactor.
    console.error(`  ✗ ${modName}: listed in CHECK_MODULES but file not found, bad rename/move?`);
    totalErrors++;
    continue;
  }

  const src = readFileSync(modPath, 'utf8');
  const siblingImports = extractSiblingImports(src);
  if (siblingImports.length === 0) {
    // A module with no relative imports (e.g. admin_scoring_autosave.jsx, which
    // pulls everything from React window globals) has nothing to verify, but
    // say so explicitly, so the output accounts for every CHECK_MODULES entry
    // rather than silently omitting it.
    console.log(`  – ${modName}: no sibling imports to check`);
    continue;
  }

  let modErrors = 0;
  for (const { importedNames, fromPath } of siblingImports) {
    const sibPath = resolve(JS_DIR, fromPath);
    const sibName = fromPath.replace(/^\.\//, '');
    // Containment guard: a `from` path that resolves outside js/ (e.g. a
    // committed `../../etc/...` traversal) must fail the check, not silently
    // read an arbitrary file. JS_DIR is computed from import.meta.url, so the
    // separator is platform-correct.
    if (sibPath !== JS_DIR && !sibPath.startsWith(JS_DIR + sep)) {
      console.error(`  ✗ ${modName}: import path escapes js/ directory: ${fromPath}`);
      modErrors++;
      totalErrors++;
      continue;
    }
    const exported = getCachedExports(sibPath);
    for (const name of importedNames) {
      if (!exported.has(name)) {
        console.error(`  ✗ ${modName}: imports "${name}" from "${sibName}", not exported by "${sibName}"`);
        modErrors++;
        totalErrors++;
      }
    }
  }

  if (modErrors === 0) {
    console.log(`  ✓ ${modName}: all sibling imports resolved`);
  }
}

if (totalErrors > 0) {
  console.error(`\n${totalErrors} import mismatch(es), fix exports or imports before merging.`);
  process.exit(1);
}

console.log('\nAll sibling imports OK.');
