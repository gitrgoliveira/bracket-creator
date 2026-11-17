// check-load-cycles.mjs: the gate for the one thing vitest cannot show. Vitest
// rewrites imports to property reads, so a module that reads a cycle sibling's
// binding while it loads sees `undefined` here and throws a TDZ ReferenceError
// on a cold page load. The gate finds those reads statically.
import { describe, it, expect } from 'vitest';
import { createRequire } from 'node:module';
import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { findLoadTimeCycleReads } from '../../check-load-cycles.mjs';

const WEB = resolve(__dirname, '..', '..');
const { parseSync } = await import(pathToFileURL(createRequire(resolve(WEB, 'package.json')).resolve('rolldown/utils')).href);

// a.jsx and b.jsx import each other; leaf.jsx imports nothing; c.jsx imports a
// but is not imported back, so it is outside the cycle.
const find = (a, extra = {}) => findLoadTimeCycleReads({
  'a.jsx': a,
  'b.jsx': "import { fromA } from './a.jsx';\nexport const fromB = () => fromA();\nexport const B_CONST = 1;\n",
  'leaf.jsx': 'export const LEAF = 1;\n',
  'c.jsx': "import { A_CONST } from './a.jsx';\nexport const fromC = A_CONST + 1;\n",
  ...extra,
}, parseSync).map(({ file, line, name, from }) => `${file}:${line} ${name} <- ${from}`);

describe('findLoadTimeCycleReads', () => {
  const head = "import { B_CONST, fromB } from './b.jsx';\nimport { LEAF } from './leaf.jsx';\nexport const A_CONST = 2;\nexport const fromA = () => 1;\n";

  it('accepts reads inside function bodies, from a leaf, and from a module outside the cycle', () => {
    expect(find(`${head}export const f = () => B_CONST + fromB();\nexport function g(x = B_CONST) { return x + LEAF; }\nexport const init = LEAF + 1;\n`)).toEqual([]);
  });

  it.each([
    ['an initializer', 'export const x = B_CONST + 1;\n'],
    ['a call', 'export const x = fromB();\n'],
    ['a statement', 'fromB();\n'],
    ['a member write', 'B_CONST.k = 1;\n'],
    ['a destructure default', 'export const { y = B_CONST } = {};\n'],
    ['a class extends', 'export class K extends B_CONST {}\n'],
    ['a static field', 'export class K { static v = B_CONST; }\n'],
    ['a computed key', 'export const o = { [B_CONST]: 1 };\n'],
    ['an object value', 'export const o = { k: B_CONST };\n'],
    ['a shorthand property', 'export const o = { B_CONST };\n'],
    ['a template literal', 'export const t = `${B_CONST}`;\n'],
    ['an if at the top level', 'if (LEAF) { fromB(); }\n'],
  ])('reports a load-time read in %s', (_, code) => {
    const hits = find(head + code);
    expect(hits).toHaveLength(1);
    expect(hits[0]).toMatch(/^a\.jsx:\d+ (B_CONST|fromB) <- b\.jsx$/);
  });

  it('does not read an object key, a member name, an instance field or a method body as a use', () => {
    expect(find(`${head}export const o = { B_CONST: 1, fromB() { return fromB; } };\nexport const p = o.B_CONST;\nexport class K { m() { return fromB(); } v = B_CONST; }\n`)).toEqual([]);
  });

  it('reports a read from a module that imports itself back through a longer cycle', () => {
    const hits = findLoadTimeCycleReads({
      'x.jsx': "import { Y } from './y.jsx';\nexport const X = Y;\n",
      'y.jsx': "import { Z } from './z.jsx';\nexport const Y = () => Z;\n",
      'z.jsx': "import { X } from './x.jsx';\nexport const Z = () => X;\n",
    }, parseSync);
    expect(hits).toEqual([{ file: 'x.jsx', line: 2, name: 'Y', from: 'y.jsx' }]);
  });

  it('stops on a file that does not parse, never comparing a partial tree', () => {
    expect(() => findLoadTimeCycleReads({ 'a.jsx': 'export const = ;' }, parseSync)).toThrow(/does not parse/);
  });
});

describe('the real modules', () => {
  it('read no cycle sibling at load time', () => {
    const dir = resolve(WEB, 'js');
    const sources = {};
    for (const f of readdirSync(dir).filter((n) => n.endsWith('.jsx')).sort()) sources[f] = readFileSync(resolve(dir, f), 'utf8');
    expect(findLoadTimeCycleReads(sources, parseSync)).toEqual([]);
  });
});
