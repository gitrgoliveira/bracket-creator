import { describe, it, expect } from 'vitest';
import jsQR from 'jsqr';
import { _buildQR_test as buildQR } from '../qr.jsx';

// Roundtrip decode test: encode a URL with our QR generator, decode with
// jsQR (devDependency only — never shipped), assert decoded text matches.
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
});
