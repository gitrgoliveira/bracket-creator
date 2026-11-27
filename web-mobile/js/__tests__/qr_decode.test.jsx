import { describe, it, expect } from 'vitest';
import jsQR from 'jsqr';
import { _buildQR_test as buildQR, qrMatrix, renderQR } from '../qr.jsx';
import levelMPin from './fixtures/qr_level_m_matrix.json';

// Roundtrip decode test: encode a URL with our QR generator, decode with
// jsQR (devDependency only; never shipped), assert decoded text matches.
// This proves our encoder faithfully encodes the input and cannot
// substitute a different URL.

/**
 * Convert our QR matrix (Uint8Array of 0/1 values, row-major) to the
 * RGBA pixel data format jsQR expects. Each module becomes one pixel.
 */
function matrixToImageData(mat, size) {
  const data = new Uint8ClampedArray(size * size * 4);
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      const idx = (r * size + c) * 4;
      const isDark = mat[r * size + c] === 1;
      const v = isDark ? 0 : 255;
      data[idx] = v;     // R
      data[idx + 1] = v; // G
      data[idx + 2] = v; // B
      data[idx + 3] = 255; // A (opaque)
    }
  }
  return { data, width: size, height: size };
}

/**
 * Wrap in a quiet zone (white border) to help the decoder find the code.
 * jsQR sometimes struggles without a quiet zone around the matrix.
 */
function addQuietZone(mat, size, zone = 4) {
  const newSize = size + 2 * zone;
  const newMat = new Uint8Array(newSize * newSize); // defaults to 0 (white)
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      newMat[(r + zone) * newSize + (c + zone)] = mat[r * size + c];
    }
  }
  return { mat: newMat, size: newSize };
}

function decodeQR(text) {
  const { mat, size } = buildQR(text);
  const padded = addQuietZone(mat, size);
  const img = matrixToImageData(padded.mat, padded.size);
  const result = jsQR(img.data, img.width, img.height);
  return result;
}

describe('QR roundtrip decode', () => {
  it('decodes a short URL correctly', () => {
    const url = 'http://localhost:8080/register/abc';
    const result = decodeQR(url);
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  it('decodes a typical registration URL', () => {
    const url = 'http://localhost:8083/register/men-individual';
    const result = decodeQR(url);
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  it('decodes an HTTPS URL with IP address', () => {
    const url = 'https://192.168.1.5:8080/register/comp-123';
    const result = decodeQR(url);
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  it('decodes plain text', () => {
    const text = 'Hello, World!';
    const result = decodeQR(text);
    expect(result).not.toBeNull();
    expect(result.data).toBe(text);
  });

  it('decodes a longer URL near version boundary', () => {
    // ~70 chars, should push to version 5 or 6
    const url = 'https://tournament.example.com/register/championship-2026-men-individual';
    const result = decodeQR(url);
    expect(result).not.toBeNull();
    expect(result.data).toBe(url);
  });

  // Every version the encoder can choose, filled to its level-M byte capacity
  // (ISO 18004 Table 7: 26, 42, 62, 84, 106, 122, 152, 180, 213 for v2 to v10).
  // v7 and up carry version information blocks and their own block tables.
  const M_CAPACITY = [26, 42, 62, 84, 106, 122, 152, 180, 213];
  M_CAPACITY.forEach((len, i) => {
    const version = i + 2;
    it(`decodes a full level-M code at version ${version}`, () => {
      const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789/-.:';
      let seed = 7 * version;
      let text = '';
      while (text.length < len) {
        seed = (seed * 1103515245 + 12345) % 2147483648;
        text += alphabet[seed % alphabet.length];
      }
      const { size } = qrMatrix(text);
      expect(size).toBe(17 + 4 * version);
      const result = decodeQR(text);
      expect(result).not.toBeNull();
      expect(result.data).toBe(text);
    });
  });
});

// The level-M pin. The fixture holds the version 3 and 5 matrices the encoder
// produced BEFORE level H was added, so these tests prove those two kept level
// M byte for byte, not merely that the encoder agrees with itself. Level M from
// version 7 up changes on purpose (block table, version information, alignment
// order) and is covered by the decode round trips above instead.
describe('QR level M is unchanged', () => {
  const toRows = (modules, size) => {
    const rows = [];
    for (let r = 0; r < size; r++) {
      let row = '';
      for (let c = 0; c < size; c++) row += modules[r * size + c] === 1 ? '1' : '0';
      rows.push(row);
    }
    return rows;
  };

  levelMPin.cases.forEach(({ text, size, rows }) => {
    it(`qrMatrix gives the recorded matrix for ${text}`, () => {
      const m = qrMatrix(text);
      expect(m.size).toBe(size);
      expect(toRows(m.modules, m.size)).toEqual(rows);
    });

    it(`qrMatrix with level "M" is the default for ${text}`, () => {
      expect(Array.from(qrMatrix(text, { level: 'M' }).modules)).toEqual(Array.from(qrMatrix(text).modules));
    });

    it(`the canvas path paints the recorded matrix for ${text}`, () => {
      const dark = [];
      const ctx = {
        fillStyle: '',
        setTransform() {},
        scale() {},
        fillRect(x, y, w, h) { if (this.fillStyle === '#000000') dark.push([x, y, w, h]); },
      };
      const canvas = { style: {}, getContext: () => ctx };
      renderQR(canvas, text, { moduleSize: 1, quietZone: 0 });
      const modules = new Uint8Array(size * size);
      for (const [x, y, w, h] of dark) {
        expect([w, h]).toEqual([1, 1]);
        modules[y * size + x] = 1;
      }
      expect(toRows(modules, size)).toEqual(rows);
    });
  });

  it('hands back a fresh copy each call', () => {
    const a = qrMatrix('http://localhost:8080/register/abc');
    a.modules.fill(0);
    expect(qrMatrix('http://localhost:8080/register/abc').modules.some((v) => v === 1)).toBe(true);
  });
});
