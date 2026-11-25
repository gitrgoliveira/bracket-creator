import { describe, it, expect } from 'vitest';
import jsQR from 'jsqr';
import { qrMatrix, qrFits } from '../qr.jsx';

// Level H decode tests. The poster paints a logo over the centre of a level-H
// code, so the contract is: the code still reads with the largest pad the
// poster may draw. Each test rasterises qrMatrix into a pixel buffer itself
// (no canvas) and decodes it with jsQR (devDependency only; never shipped).

const QUIET = 4;
const SCALE = 4;

// The pixel buffer of a matrix, SCALE pixels a module, with a white quiet zone.
// `pad` whitens a square of whole modules centred on the code, as a logo's white
// backing does.
function rasterise({ size, modules }, pad = 0) {
  const start = Math.floor((size - pad) / 2);
  const n = (size + 2 * QUIET) * SCALE;
  const data = new Uint8ClampedArray(n * n * 4).fill(255);
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      const inPad = pad > 0 && r >= start && r < start + pad && c >= start && c < start + pad;
      if (modules[r * size + c] !== 1 || inPad) continue;
      for (let dy = 0; dy < SCALE; dy++) {
        for (let dx = 0; dx < SCALE; dx++) {
          const i = (((r + QUIET) * SCALE + dy) * n + (c + QUIET) * SCALE + dx) * 4;
          data[i] = 0;
          data[i + 1] = 0;
          data[i + 2] = 0;
        }
      }
    }
  }
  return { data, width: n, height: n };
}

function decode(matrix, pad = 0) {
  const img = rasterise(matrix, pad);
  return jsQR(img.data, img.width, img.height);
}

// The largest whole-module square whose area is at most `share` of the code:
// sqrt(share) of the side, rounded DOWN.
function maxPad(size, share = 0.2) { return Math.floor(Math.sqrt(share) * size); }

// The share of the code a white centre pad may cover and still decode, MEASURED
// with jsQR on level-H codes filled to capacity (the largest pad that decoded,
// against the 20% limit, in modules):
//   v2 8 of 11, v3 10 of 12, v4 13 of 14   (the 20% pad does NOT decode)
//   v5 17 of 16, v6 18 of 18, v7 20 of 20, v8 21 of 21, v9 23 of 23, v10 27 of 25
// A small code has few codewords to spare, so from v5 up the 20% limit holds
// and at v2 to v4 only a pad of at most 10% of the area does.
function padShare(version) { return version >= 5 ? 0.2 : 0.1; }

// Deterministic URL-like text of an exact byte length.
function textOfLength(len, seed) {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789/-.:';
  let state = seed;
  let text = '';
  while (text.length < len) {
    state = (state * 1103515245 + 12345) % 2147483648;
    text += alphabet[state % alphabet.length];
  }
  return text;
}

// Level-H byte capacity of v2 to v10 (ISO 18004 Table 7 data codewords 16, 26,
// 36, 46, 60, 66, 86, 100, 122, less the mode and count bits).
const H_CAPACITY = [14, 24, 34, 44, 58, 64, 84, 98, 119];

describe('QR level H', () => {
  it('decodes a viewer-home URL', () => {
    const url = 'https://tournament.example.org/';
    const result = decode(qrMatrix(url, { level: 'H' }));
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  it('decodes a court URL', () => {
    const url = 'https://tournament.example.org/#/display/court/B';
    const result = decode(qrMatrix(url, { level: 'H' }));
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  it('marks the code level H in the format information', () => {
    const url = 'https://tournament.example.org/';
    const result = decode(qrMatrix(url, { level: 'H' }));
    expect(result.version).toBe(Math.floor((qrMatrix(url, { level: 'H' }).size - 17) / 4));
  });

  it('differs from level M for the same text', () => {
    const url = 'https://tournament.example.org/';
    expect(Array.from(qrMatrix(url, { level: 'H' }).modules)).not.toEqual(Array.from(qrMatrix(url).modules));
  });

  it('decodes the largest input that fits (119 bytes) at version 10', () => {
    const text = textOfLength(119, 3);
    expect(qrFits(text, { level: 'H' })).toBe(true);
    const matrix = qrMatrix(text, { level: 'H' });
    expect(matrix.size).toBe(17 + 4 * 10);
    const result = decode(matrix);
    expect(result).not.toBeNull();
    expect(result.data).toBe(text);
  });

  it('does not fit one byte more', () => {
    const text = textOfLength(120, 3);
    expect(qrFits(text, { level: 'H' })).toBe(false);
    expect(() => qrMatrix(text, { level: 'H' })).toThrow(/too long/i);
    // Level M still carries it.
    expect(qrFits(text)).toBe(true);
    expect(qrFits(text, { level: 'M' })).toBe(true);
  });

  it('counts encoded bytes, not characters', () => {
    // 60 two-byte characters are 120 bytes.
    expect(qrFits('é'.repeat(60), { level: 'H' })).toBe(false);
    expect(qrFits('é'.repeat(59), { level: 'H' })).toBe(true);
  });

  it('refuses an empty text and an unknown level', () => {
    expect(qrFits('', { level: 'H' })).toBe(false);
    expect(() => qrMatrix('abc', { level: 'Q' })).toThrow(/level/i);
    expect(() => qrFits('abc', { level: 'L' })).toThrow(/level/i);
  });

  describe('with the centre white pad at its area limit', () => {
    H_CAPACITY.forEach((len, i) => {
      const version = i + 2;
      it(`still decodes exactly at version ${version}`, () => {
        const text = textOfLength(len, 11 * version);
        const matrix = qrMatrix(text, { level: 'H' });
        expect(matrix.size).toBe(17 + 4 * version);
        const share = padShare(version);
        const pad = maxPad(matrix.size, share);
        expect(pad * pad).toBeLessThanOrEqual(share * matrix.size * matrix.size);
        expect((pad + 1) * (pad + 1)).toBeGreaterThan(share * matrix.size * matrix.size);
        const result = decode(matrix, pad);
        expect(result).not.toBeNull();
        expect(result.data).toBe(text);
      });
    });

    it('decodes a short URL with the pad', () => {
      const url = 'https://a.example/b';
      const matrix = qrMatrix(url, { level: 'H' });
      const result = decode(matrix, maxPad(matrix.size, padShare((matrix.size - 17) / 4)));
      expect(result).not.toBeNull();
      expect(result.data).toBe(url);
    });
  });
});

describe('QR level M', () => {
  it('decodes with no pad', () => {
    const url = 'https://tournament.example.org/#/display/court/B';
    const result = decode(qrMatrix(url));
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });
});

describe('QR version information (version 7 and up)', () => {
  // ISO 18004:2015 Table D.1.
  const EXPECTED = { 7: 0x07c94, 8: 0x085bc, 9: 0x09a99, 10: 0x0a4d3 };
  Object.entries(EXPECTED).forEach(([v, bits]) => {
    it(`version ${v} carries the standard 18 bits in both blocks`, () => {
      const version = Number(v);
      // Fill version v at level H to be sure it is chosen.
      const text = textOfLength(H_CAPACITY[version - 2], 5);
      const { size, modules } = qrMatrix(text, { level: 'H' });
      expect(size).toBe(17 + 4 * version);
      let topRight = 0;
      let bottomLeft = 0;
      for (let i = 0; i < 18; i++) {
        const a = size - 11 + (i % 3);
        const b = Math.floor(i / 3);
        topRight |= modules[b * size + a] << i;
        bottomLeft |= modules[a * size + b] << i;
      }
      expect(topRight).toBe(bits);
      expect(bottomLeft).toBe(bits);
    });
  });
});
