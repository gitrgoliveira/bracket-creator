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
// and instance members run later, so reading a sibling there is safe, except
// where a load-time call runs one on the spot: an immediately invoked function,
// a function the module declares at its top level (followed through the calls
// it makes in turn), the callback of an array method such as `.map` (written
// inline or named after a function the module declares), and `new` of a class
// the module declares (its constructor and instance field initializers). A
// function reached any other way (a method, an alias, a superclass constructor,
// a method the constructor calls through `this`, a callback given to a timer or
// a listener) is not followed. A name the module imports from a module outside
// its cycle (a leaf) is always safe.
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

// The functions and the classes a module declares at its top level, by name: a
// function declaration, or a const or let holding a function or an arrow; a
// class declaration.
function readLocalDeclarations(program) {
  const locals = new Map();
  const classes = new Map();
  for (const node of program.body) {
    const decl = node.type === 'ExportNamedDeclaration' ? node.declaration : node;
    if (!decl) continue;
    if (decl.type === 'ClassDeclaration' && decl.id) classes.set(decl.id.name, decl);
    if (decl.type === 'FunctionDeclaration' && decl.id) locals.set(decl.id.name, decl);
    if (decl.type === 'VariableDeclaration') {
      for (const d of decl.declarations) {
        if (d.id.type === 'Identifier' && isFunction(d.init)) locals.set(d.id.name, d.init);
      }
    }
  }
  return { functions: locals, classes };
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

// The functions whose bodies a call runs on the spot: an immediately invoked
// function, a function the module declares at its top level, and the callback
// handed to an array method that calls it before it returns, inline or by the
// name of a function the module declares. A function reached any other way (a
// method, an alias, a callback given to a timer or a listener) is not followed,
// so a read inside one is not seen. `new` is handled by runNewNow.
const SYNC_CALLBACK_METHODS = new Set([
  'map', 'forEach', 'filter', 'reduce', 'reduceRight', 'flatMap', 'some', 'every', 'find', 'findIndex', 'findLast', 'findLastIndex', 'sort', 'toSorted',
]);
const isFunction = (n) => isNode(n) && (n.type === 'FunctionExpression' || n.type === 'ArrowFunctionExpression');

function functionsRunBy(call, ctx) {
  let { callee } = call;
  while (callee.type === 'ParenthesizedExpression') callee = callee.expression;
  if (isFunction(callee)) return [callee];
  if (callee.type === 'Identifier') return ctx.locals.has(callee.name) ? [ctx.locals.get(callee.name)] : [];
  if (callee.type === 'MemberExpression' && !callee.computed && SYNC_CALLBACK_METHODS.has(callee.property.name)) {
    // A callback written inline, or the name of a function the module declares.
    return call.arguments
      .map((a) => (a.type === 'Identifier' ? ctx.locals.get(a.name) : isFunction(a) && a))
      .filter(Boolean);
  }
  return [];
}

// What `new X(...)` runs on the spot: a function the module declares (as a
// constructor), or the constructor and the instance field initializers of a
// class it declares. The constructor of a superclass and the methods the
// constructor calls through `this` are not followed.
function runNewNow(node, ctx) {
  let { callee } = node;
  while (callee.type === 'ParenthesizedExpression') callee = callee.expression;
  if (callee.type !== 'Identifier') return;
  if (ctx.locals.has(callee.name)) {
    runNow(ctx.locals.get(callee.name), ctx);
    return;
  }
  const cls = ctx.classes.get(callee.name);
  if (!cls || ctx.seen.has(cls)) return;
  ctx.seen.add(cls);
  for (const member of cls.body.body) {
    if (member.static) continue;
    if (member.type === 'MethodDefinition' && member.kind === 'constructor') runNow(member.value, ctx);
    else if (member.type === 'PropertyDefinition') walkLoadTime(member.value, ctx);
  }
}

// Walk a function's parameter defaults and body as code that runs now, once.
function runNow(fn, ctx) {
  if (ctx.seen.has(fn)) return;
  ctx.seen.add(fn);
  walkPattern(fn.params, ctx);
  walkLoadTime(fn.body, ctx);
}

// The names a load-time walk of `node` reads from `ctx.watched` (a Map of local
// name -> import record), as { name, start }. `ctx.onHit` receives each.
// `ctx.locals` and `ctx.classes` map the module's top-level functions and
// classes by name, and `ctx.seen` holds the function and class nodes already
// walked.
function walkLoadTime(node, ctx) {
  if (Array.isArray(node)) {
    for (const n of node) walkLoadTime(n, ctx);
    return;
  }
  if (!isNode(node)) return;
  switch (node.type) {
    case 'FunctionDeclaration':
    case 'FunctionExpression':
    case 'ArrowFunctionExpression':
      return; // runs later
    case 'CallExpression':
      for (const fn of functionsRunBy(node, ctx)) runNow(fn, ctx);
      break; // the callee and the arguments are walked below
    case 'NewExpression':
      runNewNow(node, ctx);
      break;
    case 'ImportDeclaration':
    case 'ExportAllDeclaration':
      return;
    case 'ExportNamedDeclaration':
      if (node.declaration) walkLoadTime(node.declaration, ctx);
      return; // `export { a }` names a binding; it reads nothing
    case 'Identifier':
    case 'JSXIdentifier':
      if (ctx.watched.has(node.name)) ctx.onHit(node.name, node.start);
      return;
    case 'MemberExpression':
      walkLoadTime(node.object, ctx);
      if (node.computed) walkLoadTime(node.property, ctx);
      return;
    case 'Property':
    case 'ObjectProperty':
    case 'PropertyDefinition':
    case 'MethodDefinition':
    case 'AccessorProperty': {
      if (node.computed) walkLoadTime(node.key, ctx);
      // An instance field or a method runs when the class is used, not now.
      const runsNow = node.type === 'Property' || node.type === 'ObjectProperty'
        ? !node.method && node.kind !== 'get' && node.kind !== 'set'
        : node.static && node.type !== 'MethodDefinition';
      if (runsNow) walkLoadTime(node.value, ctx);
      return;
    }
    case 'JSXAttribute':
      walkLoadTime(node.value, ctx);
      return;
    case 'JSXMemberExpression':
      walkLoadTime(node.object, ctx);
      return;
    case 'LabeledStatement':
      walkLoadTime(node.body, ctx);
      return;
    case 'BreakStatement':
    case 'ContinueStatement':
      return;
    case 'VariableDeclarator':
      walkPattern(node.id, ctx);
      walkLoadTime(node.init, ctx);
      return;
    default:
  }
  for (const key of Object.keys(node)) {
    if (key === 'type' || key === 'start' || key === 'end' || key === 'loc' || key === 'range') continue;
    walkLoadTime(node[key], ctx);
  }
}

// A binding pattern introduces names; only its defaults and computed keys run.
function walkPattern(p, ctx) {
  if (Array.isArray(p)) {
    for (const el of p) walkPattern(el, ctx);
    return;
  }
  if (!isNode(p)) return;
  switch (p.type) {
    case 'ObjectPattern':
      for (const prop of p.properties) {
        if (prop.type === 'RestElement') walkPattern(prop.argument, ctx);
        else {
          if (prop.computed) walkLoadTime(prop.key, ctx);
          walkPattern(prop.value, ctx);
        }
      }
      return;
    case 'ArrayPattern':
      for (const el of p.elements) walkPattern(el, ctx);
      return;
    case 'RestElement':
      walkPattern(p.argument, ctx);
      return;
    case 'AssignmentPattern':
      walkPattern(p.left, ctx);
      walkLoadTime(p.right, ctx);
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
    const onHit = (name, start) => {
      const line = src.slice(0, start).split('\n').length;
      violations.push({ file, line, name, from: watched.get(name).from });
    };
    const { functions: locals, classes } = readLocalDeclarations(program);
    walkLoadTime(program.body, { watched, onHit, locals, classes, seen: new Set() });
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
