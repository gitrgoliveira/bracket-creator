#!/usr/bin/env node
// Static cross-module import/export checker for mp-zac3 split modules.
//
// Validates that every named import from a sibling .jsx module is actually
// exported by that module. Catches the ESM blind spot: esbuild transpile-only
// and vitest do NOT fail on a missing named export — only native browser ESM
// does. This script gives the same check deterministically in CI.
//
// No npm dependencies — uses Node.js built-ins only.
//
// Usage:   node web-mobile/check-imports.mjs
// Exit 0   all imports resolved
// Exit 1   one or more named imports missing from their source module

import { readFileSync, existsSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const JS_DIR = resolve(dirname(fileURLToPath(import.meta.url)), 'js');

// Modules introduced by the mp-zac3 split — the ones whose imports we verify.
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

  // export { a, b as c, … }  — possibly multiline
  for (const m of src.matchAll(/export\s*\{([^}]+)\}/gs)) {
    for (const item of m[1].split(',')) {
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
    const names = m[1].split(',').map(s => {
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
    // rename/move — failing the check is correct, not a soft skip, otherwise
    // js/validate would go green on a broken refactor.
    console.error(`  ✗ ${modName}: listed in CHECK_MODULES but file not found — bad rename/move?`);
    totalErrors++;
    continue;
  }

  const src = readFileSync(modPath, 'utf8');
  const siblingImports = extractSiblingImports(src);
  if (siblingImports.length === 0) continue;

  let modErrors = 0;
  for (const { importedNames, fromPath } of siblingImports) {
    const sibPath = resolve(JS_DIR, fromPath);
    const sibName = fromPath.replace(/^\.\//, '');
    const exported = getCachedExports(sibPath);
    for (const name of importedNames) {
      if (!exported.has(name)) {
        console.error(`  ✗ ${modName}: imports "${name}" from "${sibName}" — not exported by "${sibName}"`);
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
  console.error(`\n${totalErrors} import mismatch(es) — fix exports or imports before merging.`);
  process.exit(1);
}

console.log('\nAll sibling imports OK.');
