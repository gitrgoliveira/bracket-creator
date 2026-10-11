// side_marks.jsx is a LEAF: the court console (admin_shiaijo.jsx) imports the
// result-mark placement helpers from it, and the console must not reach
// bracket.jsx. Its render suite stubs window.BracketTree before importing the
// console, the console reads that global at module eval, and bracket.jsx
// reassigns window.BracketTree in its own module body; an import's
// dependencies evaluate FIRST, so a path from the console to bracket.jsx would
// overwrite the stub (see the header of admin_shiaijo.jsx).
//
// So the leaf's one import is competitor_identity.jsx, itself a leaf. This test
// reads the source rather than the module graph because a graph walk would need
// a bundler; the rule is small enough to state as "the only `import` lines in
// the file name competitor_identity.jsx", and a second import fails it loudly.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { stripComments } from '../../check-helpers.mjs';

const JS = resolve(__dirname, '..');
const read = (name) => readFileSync(resolve(JS, name), 'utf8');

function importSpecifiers(src) {
  const out = [];
  const re = /^\s*import\s[^;]*?from\s+['"]([^'"]+)['"]|^\s*import\s+['"]([^'"]+)['"]/gm;
  let m;
  while ((m = re.exec(stripComments(src))) !== null) out.push(m[1] || m[2]);
  return out;
}

describe('side_marks.jsx stays a leaf', () => {
  it('its only import is ./competitor_identity.jsx', () => {
    expect(importSpecifiers(read('side_marks.jsx'))).toEqual(['./competitor_identity.jsx']);
  });

  it('competitor_identity.jsx, the one module it reaches, imports nothing', () => {
    expect(importSpecifiers(read('competitor_identity.jsx'))).toEqual([]);
  });

  it('exports the six helpers the console and bracket.jsx read', () => {
    const src = read('side_marks.jsx');
    for (const name of ['isKikenDecisionBC', 'joinSp', 'placeMarks', 'sideMarks', 'winnerSideLR', 'teamMatchMarks']) {
      expect(src, name).toMatch(new RegExp(`export\\s+(?:function|const)\\s+${name}\\b|export\\s*\\{[^}]*\\b${name}\\b`));
    }
  });
});
