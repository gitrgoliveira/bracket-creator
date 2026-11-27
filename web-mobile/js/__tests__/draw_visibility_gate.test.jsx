// Whether a draft's draw is hidden from the public has ONE client judge,
// drawHiddenFromPublic (draw_visibility.jsx), which applies draw-ready and the
// tournament's mode on top of the stored `drawHidden`. A screen that read
// `comp.drawHidden` itself would call a started competition, or a self-run
// one, hidden, so this gate reads every module under js/ (tests, dist and
// vendor skipped, comments blanked) and fails on a read of the field outside
// the owner and the two serializers that carry it off the wire
// (api_serializers.jsx and api_client_requests.jsx's normalizeViewerCompItem).
// A read is a property access (`.drawHidden`, `?.drawHidden`), a computed one
// (`["drawHidden"]`) or an object destructuring (`{ drawHidden } = ...`); a
// local variable that happens to be named drawHidden is not one.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { walk, scanSource } from '../../check-helpers.mjs';

const JS = resolve(__dirname, '..');

const DRAW_HIDDEN_OWNERS = new Set(['draw_visibility.jsx', 'api_serializers.jsx', 'api_client_requests.jsx']);

const DRAW_HIDDEN_READS = [
  { re: /\.drawHidden\b/, why: 'reads .drawHidden' },
  { re: /\[\s*["'`]drawHidden["'`]\s*\]/, why: 'reads ["drawHidden"]' },
  { re: /\{[^{}]*\bdrawHidden\b[^{}]*\}\s*=[^=>]/, why: 'destructures drawHidden' },
];

function hitsIn(src) {
  return scanSource(src, DRAW_HIDDEN_READS);
}

describe('drawHidden is read only through draw_visibility.jsx', () => {
  it('the detector flags a read and leaves the judge and a local name alone', () => {
    for (const s of ['if (c.drawHidden) {', 'comp?.drawHidden', 'c["drawHidden"]', "const { drawHidden } = comp;", 'const { status, drawHidden, name } = c;']) {
      expect(hitsIn(s), s).not.toEqual([]);
    }
    for (const s of ['drawHiddenFromPublic(t, c)', 'const drawHidden = drawHiddenFromPublic(t, c);', 'x.drawHiddenFromPublic', '{ drawHidden ? a : b }', '<Card drawHidden={hidden} />']) {
      expect(hitsIn(s), s).toEqual([]);
    }
  });

  it('no module outside the owner and the serializers reads the field', () => {
    const files = [...walk(JS)];
    expect(files.length).toBeGreaterThan(50);
    const hits = [];
    for (const f of files) {
      const rel = relative(JS, f);
      if (DRAW_HIDDEN_OWNERS.has(rel)) continue;
      for (const h of hitsIn(readFileSync(f, 'utf8'))) hits.push(`${rel}:${h.line}: ${h.why} in ${h.text.slice(0, 120)}`);
    }
    expect(hits, `Ask drawHiddenFromPublic (draw_visibility.jsx) instead:\n${hits.join('\n')}`).toEqual([]);
  });

  it('the owner itself still reads the field, so the list cannot go stale', () => {
    expect(hitsIn(readFileSync(resolve(JS, 'draw_visibility.jsx'), 'utf8'))).not.toEqual([]);
  });
});
