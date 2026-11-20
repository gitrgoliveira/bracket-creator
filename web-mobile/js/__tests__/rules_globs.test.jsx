// The `paths:` globs of the .claude/rules files name web-mobile modules one by
// one (a Read, Edit or Write on a matching file loads the rule). A module split
// out of one the rule names is a new file the globs do not match, so its rule
// silently stops loading there. This pins both ways: every module a glob names
// exists, and every rule that names a split entry (SPLIT_ENTRIES) names each
// module of that entry's family too.
import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';
import { familyFiles, SPLIT_ENTRIES, modules } from './helpers/source.js';

const RULES_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', '.claude', 'rules');
const JS_GLOB = /^web-mobile\/js\/(?:\{([^}]*)\}|([^{}*]+))\.jsx$/;

// rule file -> the web-mobile/js module names its `paths:` globs list.
const listed = {};
for (const file of readdirSync(RULES_DIR).filter((f) => f.endsWith('.md')).sort()) {
  const front = /^---\n([\s\S]*?)\n---/.exec(readFileSync(resolve(RULES_DIR, file), 'utf8'));
  const names = new Set();
  for (const m of (front ? front[1] : '').matchAll(/^\s*-\s*"([^"]+)"\s*$/gm)) {
    const g = JS_GLOB.exec(m[1]);
    if (g) (g[1] ?? g[2]).split(',').forEach((n) => names.add(`${n.trim()}.jsx`));
  }
  if (names.size > 0) listed[file] = names;
}

describe('rules paths globs', () => {
  it('read at least one rule file', () => {
    expect(Object.keys(listed).length).toBeGreaterThan(0);
  });

  it.each(Object.entries(listed))('%s names only modules that exist', (file, names) => {
    const have = new Set(modules());
    expect([...names].filter((n) => !have.has(n))).toEqual([]);
  });

  it.each(Object.entries(listed))('%s names every module of a split entry it names', (file, names) => {
    const missing = [];
    for (const entry of SPLIT_ENTRIES) {
      if (!names.has(entry)) continue;
      for (const f of familyFiles(entry)) if (!names.has(f)) missing.push(f);
    }
    expect(missing).toEqual([]);
  });
});
