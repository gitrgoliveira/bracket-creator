// Load-order gate for the import cycles among web-mobile/js modules.
//
// ES modules in a cycle are safe only while no module runs code at load time
// that reaches a binding of a sibling that has not evaluated yet: reading an
// imported `const`, `let` or class before its module ran throws a TDZ
// ReferenceError in the browser. Vitest cannot show it (it rewrites imports to
// property reads, which give `undefined`), so a violation passed every test and
// failed only on a cold page load. This gate closes that hole statically:
//
//   A module that shares an import cycle with another module must not read,
//   in code that runs as it loads, a binding it imports from a module of the
//   same cycle.
//
// "Code that runs as it loads" is everything at the top level outside function
// bodies: initializers, calls, `API.x = ...` assignments, a class's `extends`
// and its computed keys, static field initializers and static blocks, JSX
// built at the top level. Function and arrow bodies, their parameter defaults
// and instance members run later, so reading a sibling there is safe. A name
// the module imports from a module outside its cycle (a leaf) is always safe.
//
// Parser: the oxc parser that rolldown ships (rolldown/utils parseSync, a
// dependency of vitest's vite, installed by `npm ci` in web-mobile).
//
// Usage:   node web-mobile/check-load-cycles.mjs
// Exit 0   no load-time read of a cycle sibling's binding
// Exit 1   at least one (file:line, the name and the module it comes from)

import { readFileSync, readdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { resolve, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const JS_DIR = resolve(HERE, 'js');

const isNode = (v) => v && typeof v === 'object' && typeof v.type === 'string';

// The imports of one parsed module: local name -> { from, imported }, plus the
// relative modules it names (a re-export is an edge too, but reads nothing).
function readImports(program) {
  const bindings = new Map();
  const edges = new Set();
  for (const node of program.body) {
    const fromRelative = node.source && typeof node.source.value === 'string' && node.source.value.startsWith('.');
    if (!fromRelative) continue;
    const from = node.source.value.replace(/^\.\//, '');
    if (node.type === 'ImportDeclaration') {
      edges.add(from);
      for (const s of node.specifiers) bindings.set(s.local.name, { from, imported: s.imported ? s.imported.name : s.type });
    } else if (node.type === 'ExportNamedDeclaration' || node.type === 'ExportAllDeclaration') {
      edges.add(from);
    }
  }
  return { bindings, edges };
}

// Tarjan: the strongly connected components of { module -> Set(module) },
// keyed by module, each module mapped to its component's id; a module alone
// in its component is in a cycle only when it imports itself.
function cycleIds(graph) {
  const index = new Map();
  const low = new Map();
  const onStack = new Set();
  const stack = [];
  const comp = new Map();
  let counter = 0;
  let nextComp = 0;
  const visit = (v) => {
    index.set(v, counter);
    low.set(v, counter);
    counter++;
    stack.push(v);
    onStack.add(v);
    for (const w of graph.get(v) || []) {
      if (!graph.has(w)) continue;
      if (!index.has(w)) {
        visit(w);
        low.set(v, Math.min(low.get(v), low.get(w)));
      } else if (onStack.has(w)) {
        low.set(v, Math.min(low.get(v), index.get(w)));
      }
    }
    if (low.get(v) === index.get(v)) {
      const members = [];
      let w;
      do {
        w = stack.pop();
        onStack.delete(w);
        members.push(w);
      } while (w !== v);
      const id = nextComp++;
      const cyclic = members.length > 1 || (graph.get(v) || new Set()).has(v);
      for (const m of members) comp.set(m, cyclic ? id : -1);
    }
  };
  for (const v of graph.keys()) if (!index.has(v)) visit(v);
  return comp;
}

// The names a load-time walk of `node` reads from `watched` (a Map of local
// name -> import record), as { name, start }. `onHit` receives each.
function walkLoadTime(node, watched, onHit) {
  if (Array.isArray(node)) {
    for (const n of node) walkLoadTime(n, watched, onHit);
    return;
  }
  if (!isNode(node)) return;
  switch (node.type) {
    case 'FunctionDeclaration':
    case 'FunctionExpression':
    case 'ArrowFunctionExpression':
      return; // runs later
    case 'ImportDeclaration':
    case 'ExportAllDeclaration':
      return;
    case 'ExportNamedDeclaration':
      if (node.declaration) walkLoadTime(node.declaration, watched, onHit);
      return; // `export { a }` names a binding; it reads nothing
    case 'Identifier':
    case 'JSXIdentifier':
      if (watched.has(node.name)) onHit(node.name, node.start);
      return;
    case 'MemberExpression':
      walkLoadTime(node.object, watched, onHit);
      if (node.computed) walkLoadTime(node.property, watched, onHit);
      return;
    case 'Property':
    case 'ObjectProperty':
    case 'PropertyDefinition':
    case 'MethodDefinition':
    case 'AccessorProperty': {
      if (node.computed) walkLoadTime(node.key, watched, onHit);
      // An instance field or a method runs when the class is used, not now.
      const runsNow = node.type === 'Property' || node.type === 'ObjectProperty'
        ? !node.method && node.kind !== 'get' && node.kind !== 'set'
        : node.static && node.type !== 'MethodDefinition';
      if (runsNow) walkLoadTime(node.value, watched, onHit);
      return;
    }
    case 'JSXAttribute':
      walkLoadTime(node.value, watched, onHit);
      return;
    case 'JSXMemberExpression':
      walkLoadTime(node.object, watched, onHit);
      return;
    case 'LabeledStatement':
      walkLoadTime(node.body, watched, onHit);
      return;
    case 'BreakStatement':
    case 'ContinueStatement':
      return;
    case 'VariableDeclarator':
      walkPattern(node.id, watched, onHit);
      walkLoadTime(node.init, watched, onHit);
      return;
    default:
  }
  for (const key of Object.keys(node)) {
    if (key === 'type' || key === 'start' || key === 'end' || key === 'loc' || key === 'range') continue;
    walkLoadTime(node[key], watched, onHit);
  }
}

// A binding pattern introduces names; only its defaults and computed keys run.
function walkPattern(p, watched, onHit) {
  if (!isNode(p)) return;
  switch (p.type) {
    case 'ObjectPattern':
      for (const prop of p.properties) {
        if (prop.type === 'RestElement') walkPattern(prop.argument, watched, onHit);
        else {
          if (prop.computed) walkLoadTime(prop.key, watched, onHit);
          walkPattern(prop.value, watched, onHit);
        }
      }
      return;
    case 'ArrayPattern':
      for (const el of p.elements) walkPattern(el, watched, onHit);
      return;
    case 'RestElement':
      walkPattern(p.argument, watched, onHit);
      return;
    case 'AssignmentPattern':
      walkPattern(p.left, watched, onHit);
      walkLoadTime(p.right, watched, onHit);
      return;
    default:
  }
}

// The load-time reads of cycle siblings' bindings across `sources`
// ({ 'a.jsx': text, ... }). Each violation is { file, line, name, from }.
export function findLoadTimeCycleReads(sources, parseSync) {
  const parsed = new Map();
  const graph = new Map();
  for (const [file, src] of Object.entries(sources)) {
    const r = parseSync(file, src, { lang: 'jsx', sourceType: 'module' });
    if (r.errors.length > 0) throw new Error(`${file} does not parse: ${r.errors.map((e) => e.message).join('; ')}`);
    const imports = readImports(r.program);
    parsed.set(file, { program: r.program, src, ...imports });
    graph.set(file, imports.edges);
  }
  const comp = cycleIds(graph);
  const violations = [];
  for (const [file, { program, src, bindings }] of parsed) {
    const mine = comp.get(file);
    if (mine === undefined || mine < 0) continue;
    const watched = new Map([...bindings].filter(([, b]) => comp.get(b.from) === mine));
    if (watched.size === 0) continue;
    walkLoadTime(program.body, watched, (name, start) => {
      const line = src.slice(0, start).split('\n').length;
      violations.push({ file, line, name, from: watched.get(name).from });
    });
  }
  return violations.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line);
}

async function loadParser() {
  const req = createRequire(resolve(HERE, 'package.json'));
  try {
    return (await import(pathToFileURL(req.resolve('rolldown/utils')).href)).parseSync;
  } catch (e) {
    console.error(`check-load-cycles: no parser: rolldown/utils not found under ${HERE}/node_modules (run npm ci there): ${e.message}`);
    process.exit(2);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const parseSync = await loadParser();
  const sources = {};
  for (const f of readdirSync(JS_DIR).filter((n) => n.endsWith('.jsx')).sort()) {
    sources[f] = readFileSync(resolve(JS_DIR, f), 'utf8');
  }
  const violations = findLoadTimeCycleReads(sources, parseSync);
  if (violations.length === 0) {
    console.log('  ✓ no module reads a cycle sibling\'s binding while it loads');
    console.log('All load-order checks OK.');
    process.exit(0);
  }
  console.error('Load-order checks failed.\n');
  console.error('A module in an import cycle reads, as it loads, a binding of a module in the same cycle.');
  console.error('That module may not have run yet, and the browser throws a TDZ error that vitest never shows.');
  console.error('Read it inside a function instead, or take the value from a leaf that imports nothing from the cycle.\n');
  for (const v of violations) console.error(`  ✗ js/${v.file}:${v.line}: reads "${v.name}" from ${v.from} at load time`);
  process.exit(1);
}
