import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'fs';
import { resolve, dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const JS_DIR = resolve(__dirname, '..');

// mp-hpe3 Phase 0 safety net; STATIC CROSS-MODULE EXPORT CHECKER.
//
// THE failure mode of splitting a monolith into ES modules is an import that
// names a symbol the target module does not actually export. Native ESM throws
// "does not provide an export named X" and blanks the SPA; but vitest's
// lenient resolver and esbuild's per-file transpile BOTH silently let it pass.
// Nothing in the JS gate catches it today.
//
// This test closes that gap: it walks every production .jsx module, finds each
//   import { a, b as c } from './sibling.jsx'
// (relative siblings only; npm packages are out of scope), and asserts the
// target file exports every named symbol. It validates the entire existing
// cross-module graph now, and becomes the guardrail for the admin_competition
// (mp-hpe3) and viewer (mp-pxxc) splits.
//
// Parsing is regex-based; deliberately simple, matching the project's
// readFileSync-introspection convention. It handles multi-line import/export
// blocks and `as` aliasing. All statement regexes are anchored to line-start
// (the `m` flag + `^\s*`): real ES import/export statements are module-level
// and begin their line, whereas illustrative `// import { … } from './x.jsx'`
// examples in comments have `//` first and are correctly ignored. It does not
// resolve `export *` (none in the tree); if one is introduced, extend
// collectExports rather than weakening the check.

function listProdModules(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === '__tests__' || entry.name === 'node_modules') continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...listProdModules(full));
    else if (entry.name.endsWith('.jsx')) out.push(full);
  }
  return out;
}

// Names a module makes importable: export function/const/let/var/class X,
// export { a, b as c }, and re-exports export { a, b as c } from './x'.
function collectExports(src) {
  const names = new Set();

  const declRe = /^\s*export\s+(?:async\s+)?(?:function\*?|const|let|var|class)\s+([A-Za-z_$][\w$]*)/gm;
  for (const m of src.matchAll(declRe)) names.add(m[1]);

  // export { ... } and export { ... } from '...'  ; the EXPORTED name is the
  // right-hand side of `as`, or the bare name otherwise.
  const blockRe = /^\s*export\s*\{([^}]*)\}/gm;
  for (const m of src.matchAll(blockRe)) {
    for (const piece of m[1].split(',')) {
      const part = piece.trim();
      if (!part || part === 'default') continue;
      const asMatch = part.match(/\bas\s+([A-Za-z_$][\w$]*)\s*$/);
      names.add(asMatch ? asMatch[1] : part.replace(/\s+/g, ''));
    }
  }
  return names;
}

// Each relative-.jsx import OR re-export edge: { specifiers } and the resolved
// target path. Both `import { X } from './y.jsx'` and `export { X } from
// './y.jsx'` are cross-module edges that must name a real export of the target;
// re-export drift (the entry re-exporting a helper a section module no longer
// exports) is exactly the failure the mp-hpe3 split could introduce, and native
// ESM throws on it just the same. The name we must find in the target is the
// left-hand side of `as` (the source binding), or the bare name otherwise.
function collectRelativeImports(src, fromFile) {
  const edges = [];
  const parseSpecifiers = (raw) => {
    const wanted = [];
    for (const piece of raw.split(',')) {
      const part = piece.trim();
      if (!part) continue;
      const asMatch = part.match(/^([A-Za-z_$][\w$]*)\s+as\s+/);
      wanted.push(asMatch ? asMatch[1] : part.replace(/\s+/g, ''));
    }
    return wanted;
  };
  // `import { … } from` and `export { … } from` share the same edge shape.
  const edgeRe = /^\s*(?:import|export)\s*\{([^}]*)\}\s*from\s*['"](\.\.?\/[^'"]+\.jsx)['"]/gm;
  for (const m of src.matchAll(edgeRe)) {
    const wanted = parseSpecifiers(m[1]);
    if (wanted.length) edges.push({ target: resolve(dirname(fromFile), m[2]), wanted, spec: m[2] });
  }
  return edges;
}

describe('cross-module ES export integrity (mp-hpe3 / mp-pxxc split guardrail)', () => {
  const modules = listProdModules(JS_DIR);
  const exportsByFile = new Map();
  const readSrc = (f) => readFileSync(f, 'utf8');
  const exportsOf = (f) => {
    if (!exportsByFile.has(f)) exportsByFile.set(f, collectExports(readSrc(f)));
    return exportsByFile.get(f);
  };

  it('discovers production modules to scan', () => {
    expect(modules.length).toBeGreaterThan(10);
  });

  it('every relative import/re-export names a symbol its target actually exports', () => {
    const violations = [];
    for (const file of modules) {
      const rel = file.slice(JS_DIR.length + 1);
      let edges;
      try {
        edges = collectRelativeImports(readSrc(file), file);
      } catch (e) {
        violations.push(`${rel}: failed to parse imports (${e.message})`);
        continue;
      }
      for (const { target, wanted, spec } of edges) {
        let targetExports;
        try {
          targetExports = exportsOf(target);
        } catch {
          violations.push(`${rel}: imports from '${spec}' but target file is unreadable`);
          continue;
        }
        for (const name of wanted) {
          if (!targetExports.has(name)) {
            violations.push(`${rel}: imports { ${name} } from '${spec}'; not exported there`);
          }
        }
      }
    }
    expect(violations, `cross-module export mismatches:\n${violations.join('\n')}`).toEqual([]);
  });
});
