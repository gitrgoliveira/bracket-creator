// qr.js — Zero-dependency QR Code Model 2 generator.
// Encodes byte-mode data, error correction level M, versions 1-10.
// Implements ISO/IEC 18004:2015.
//
// Public API: renderQR(canvas, text, opts)

// ---------------------------------------------------------------------------
// GF(256) arithmetic over the QR Code field (prime poly x^8+x^4+x^3+x^2+1)
// ---------------------------------------------------------------------------
const GF_PRIME = 0x11d;

const gfExp = new Uint8Array(512);
const gfLog = new Uint8Array(256);
(function buildTables() {
  let x = 1;
  for (let i = 0; i < 255; i++) {
    gfExp[i] = x;
    gfLog[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= GF_PRIME;
  }
  for (let i = 255; i < 512; i++) gfExp[i] = gfExp[i - 255];
})();

function gfMul(a, b) {
  if (a === 0 || b === 0) return 0;
  return gfExp[(gfLog[a] + gfLog[b]) % 255];
}

function gfPolyMul(p, q) {
  const r = new Uint8Array(p.length + q.length - 1);
  for (let i = 0; i < p.length; i++) {
    for (let j = 0; j < q.length; j++) {
      r[i + j] ^= gfMul(p[i], q[j]);
    }
  }
  return r;
}

function rsGeneratorPoly(n) {
  let g = new Uint8Array([1]);
  for (let i = 0; i < n; i++) {
    g = gfPolyMul(g, new Uint8Array([1, gfExp[i]]));
  }
  return g;
}

function rsEncode(data, ecCount) {
  const gen = rsGeneratorPoly(ecCount);
  const msg = new Uint8Array(data.length + ecCount);
  msg.set(data);
  for (let i = 0; i < data.length; i++) {
    const coeff = msg[i];
    if (coeff === 0) continue;
    for (let j = 1; j < gen.length; j++) {
      msg[i + j] ^= gfMul(gen[j], coeff);
    }
  }
  return msg.slice(data.length);
}

// ---------------------------------------------------------------------------
// Block structure for EC level M, versions 1-10
// Source: ISO/IEC 18004:2015 Table 9 (verified against nayuki's reference)
// Columns: [ecCWperBlock, nBlocks1, dcPerBlock1, nBlocks2, dcPerBlock2]
// ---------------------------------------------------------------------------
const QR_BLOCKS_M = [
  null,                  // v0 (unused)
  [6,  1, 10, 0,  0],   // v1
  [10, 1, 16, 0,  0],   // v2
  [15, 1, 26, 0,  0],   // v3
  [20, 2, 18, 0,  0],   // v4
  [26, 2, 24, 0,  0],   // v5
  [36, 4, 16, 0,  0],   // v6
  [40, 4, 19, 0,  0],   // v7
  [48, 2, 22, 2, 23],   // v8
  [60, 3, 16, 2, 17],   // v9
  [72, 4, 15, 1, 16],   // v10
];

function totalDataCW(v) {
  const [, n1, dc1, n2, dc2] = QR_BLOCKS_M[v];
  return n1 * dc1 + n2 * dc2;
}

// Character count indicator bit length for byte mode (ISO 18004 Table 3)
function charCountBits(v) { return v >= 10 ? 16 : 8; }

// Maximum byte-mode payload for version v (after mode indicator + count bits)
function byteCapacity(v) {
  return Math.floor((totalDataCW(v) * 8 - 4 - charCountBits(v)) / 8);
}

function selectVersion(byteLen) {
  for (let v = 1; v <= 10; v++) {
    if (byteCapacity(v) >= byteLen) return v;
  }
  throw new Error(`Text too long for QR versions 1-10 (max ~${byteCapacity(10)} bytes)`);
}

// ---------------------------------------------------------------------------
// Data encoding — byte mode
// ---------------------------------------------------------------------------
function encodeByte(text, version) {
  const bytes = new TextEncoder().encode(text);
  const dataCW = totalDataCW(version);
  const totalBits = dataCW * 8;

  const bits = [];
  function pushBits(val, len) {
    for (let i = len - 1; i >= 0; i--) bits.push((val >> i) & 1);
  }

  pushBits(0b0100, 4);     // byte mode indicator
  pushBits(bytes.length, charCountBits(version)); // 8 bits for v1-9, 16 for v10
  for (const b of bytes) pushBits(b, 8);

  // Terminator (up to 4 zeros)
  for (let i = 0; i < Math.min(4, totalBits - bits.length); i++) bits.push(0);
  // Pad to byte boundary
  while (bits.length % 8 !== 0) bits.push(0);
  // Pad codewords alternating 0xEC / 0x11
  const padBytes = [0xEC, 0x11];
  for (let pi = 0; bits.length < totalBits; pi++) pushBits(padBytes[pi % 2], 8);

  const cw = new Uint8Array(dataCW);
  for (let i = 0; i < dataCW; i++) {
    let b = 0;
    for (let j = 0; j < 8; j++) b = (b << 1) | bits[i * 8 + j];
    cw[i] = b;
  }
  return cw;
}

// ---------------------------------------------------------------------------
// Interleave data and EC codewords across blocks
// ---------------------------------------------------------------------------
function interleave(version, dataCW) {
  const [ecPerBlock, n1, dc1, n2, dc2] = QR_BLOCKS_M[version];

  const blocks = [];
  let offset = 0;
  for (let i = 0; i < n1; i++) { blocks.push(dataCW.slice(offset, offset + dc1)); offset += dc1; }
  for (let i = 0; i < n2; i++) { blocks.push(dataCW.slice(offset, offset + dc2)); offset += dc2; }

  const ecBlocks = blocks.map(b => rsEncode(b, ecPerBlock));

  const result = [];
  const maxDC = Math.max(dc1, dc2 || 0);
  for (let i = 0; i < maxDC; i++) {
    for (const blk of blocks) { if (i < blk.length) result.push(blk[i]); }
  }
  for (let i = 0; i < ecPerBlock; i++) {
    for (const ec of ecBlocks) result.push(ec[i]);
  }
  return new Uint8Array(result);
}

// ---------------------------------------------------------------------------
// QR Matrix construction
// ---------------------------------------------------------------------------
function matrixSize(version) { return 17 + version * 4; }

// Alignment pattern centers for versions 1-10 (ISO 18004:2015 Annex E)
const ALIGN_POS = [
  [],           // v1 (none)
  [6, 18],      // v2
  [6, 22],      // v3
  [6, 26],      // v4
  [6, 30],      // v5
  [6, 34],      // v6
  [6, 22, 38],  // v7
  [6, 24, 42],  // v8
  [6, 26, 46],  // v9
  [6, 28, 50],  // v10
];

function buildMatrix(version) {
  const size = matrixSize(version);
  const EMPTY = 255;
  const mat = new Uint8Array(size * size).fill(EMPTY);

  function set(r, c, v) { mat[r * size + c] = v; }
  function get(r, c) { return mat[r * size + c]; }

  // Place a 7×7 finder pattern with 1-wide separator at (r0, c0)
  function placeFinder(r0, c0) {
    for (let dr = -1; dr <= 7; dr++) {
      for (let dc = -1; dc <= 7; dc++) {
        const r = r0 + dr, c = c0 + dc;
        if (r < 0 || r >= size || c < 0 || c >= size) continue;
        if (dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6) {
          const isBorder = dr === 0 || dr === 6 || dc === 0 || dc === 6;
          const isCenter = dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4;
          set(r, c, (isBorder || isCenter) ? 1 : 0);
        } else {
          if (get(r, c) === EMPTY) set(r, c, 0); // separator
        }
      }
    }
  }
  placeFinder(0, 0);
  placeFinder(0, size - 7);
  placeFinder(size - 7, 0);

  // Timing patterns
  for (let i = 8; i < size - 8; i++) {
    set(6, i, i % 2 === 0 ? 1 : 0);
    set(i, 6, i % 2 === 0 ? 1 : 0);
  }

  // Dark module
  set(4 * version + 9, 8, 1);

  // Alignment patterns (version ≥ 2)
  if (version >= 2) {
    const pos = ALIGN_POS[version - 1];
    for (const ar of pos) {
      for (const ac of pos) {
        if (get(ar, ac) !== EMPTY) continue; // overlaps finder
        for (let dr = -2; dr <= 2; dr++) {
          for (let dc = -2; dc <= 2; dc++) {
            const isBorder = dr === -2 || dr === 2 || dc === -2 || dc === 2;
            const isCenter = dr === 0 && dc === 0;
            set(ar + dr, ac + dc, (isBorder || isCenter) ? 1 : 0);
          }
        }
      }
    }
  }

  // Mark format info areas as reserved (value 2)
  function reserve(r, c) { if (get(r, c) === EMPTY) set(r, c, 2); }
  for (let i = 0; i <= 8; i++) { reserve(8, i); reserve(i, 8); }           // top-left
  for (let i = size - 8; i < size; i++) reserve(8, i);                      // top-right
  for (let i = size - 8; i < size; i++) reserve(i, 8);                      // bottom-left (includes dark module row)

  return mat;
}

// isFunction[i] = 1 for all non-data modules (finder, timing, alignment, format, dark)
function makeFuncMask(mat, size) {
  const f = new Uint8Array(size * size);
  for (let i = 0; i < mat.length; i++) {
    if (mat[i] !== 255) f[i] = 1;
  }
  return f;
}

// Place data bits in the zigzag pattern
function placeData(mat, size, cw) {
  const totalBits = cw.length * 8;
  let bitIdx = 0;
  let goingUp = true;
  let col = size - 1;

  while (col > 0) {
    if (col === 6) col--; // skip timing column
    for (let ri = 0; ri < size; ri++) {
      const r = goingUp ? (size - 1 - ri) : ri;
      for (let dc = 0; dc <= 1; dc++) {
        const c = col - dc;
        if (mat[r * size + c] === 255) {
          let bit = 0;
          if (bitIdx < totalBits) {
            bit = (cw[bitIdx >> 3] >> (7 - (bitIdx & 7))) & 1;
            bitIdx++;
          }
          mat[r * size + c] = bit;
        }
      }
    }
    goingUp = !goingUp;
    col -= 2;
  }
}

// ---------------------------------------------------------------------------
// Masking
// ---------------------------------------------------------------------------
const MASK_FN = [
  (r, c) => (r + c) % 2 === 0,
  (r, c) => r % 2 === 0,
  (r, c) => c % 3 === 0,
  (r, c) => (r + c) % 3 === 0,
  (r, c) => (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0,
  (r, c) => (r * c) % 2 + (r * c) % 3 === 0,
  (r, c) => ((r * c) % 2 + (r * c) % 3) % 2 === 0,
  (r, c) => ((r + c) % 2 + (r * c) % 3) % 2 === 0,
];

function applyMask(src, size, maskId, funcMask) {
  const fn = MASK_FN[maskId];
  const out = new Uint8Array(src);
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      if (!funcMask[r * size + c] && fn(r, c)) out[r * size + c] ^= 1;
    }
  }
  return out;
}

function penaltyScore(mat, size) {
  let score = 0;

  // Rule 1: runs of 5+ same color
  for (let r = 0; r < size; r++) {
    for (let isCol = 0; isCol <= 1; isCol++) {
      let run = 1, prev = isCol ? mat[r] : mat[r * size];
      for (let i = 1; i < size; i++) {
        const v = isCol ? mat[i * size + r] : mat[r * size + i];
        if (v === prev) { run++; } else { if (run >= 5) score += run - 2; run = 1; prev = v; }
      }
      if (run >= 5) score += run - 2;
    }
  }

  // Rule 2: 2x2 blocks
  for (let r = 0; r < size - 1; r++) {
    for (let c = 0; c < size - 1; c++) {
      const v = mat[r * size + c];
      if (v === mat[r * size + c + 1] && v === mat[(r + 1) * size + c] && v === mat[(r + 1) * size + c + 1]) score += 3;
    }
  }

  // Rule 3: finder-like patterns
  const P1 = [1, 0, 1, 1, 1, 0, 1, 0, 0, 0, 0];
  const P2 = [0, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1];
  for (let r = 0; r < size; r++) {
    for (let c = 0; c <= size - 11; c++) {
      let m1 = true, m2 = true;
      for (let k = 0; k < 11; k++) {
        const v = mat[r * size + c + k];
        if (v !== P1[k]) m1 = false;
        if (v !== P2[k]) m2 = false;
      }
      if (m1 || m2) score += 40;
    }
  }
  for (let c = 0; c < size; c++) {
    for (let r = 0; r <= size - 11; r++) {
      let m1 = true, m2 = true;
      for (let k = 0; k < 11; k++) {
        const v = mat[(r + k) * size + c];
        if (v !== P1[k]) m1 = false;
        if (v !== P2[k]) m2 = false;
      }
      if (m1 || m2) score += 40;
    }
  }

  // Rule 4: dark module proportion
  let dark = 0;
  for (const v of mat) if (v === 1) dark++;
  const pct = (dark * 100) / (size * size);
  const p5 = Math.floor(pct / 5) * 5;
  score += Math.min(Math.abs(p5 - 50), Math.abs(p5 + 5 - 50)) * 2;

  return score;
}

// ---------------------------------------------------------------------------
// Format information placement
// EC level M = binary 00 in QR format bits
// Generator: G(x) = x^10 + x^8 + x^5 + x^4 + x^2 + x + 1 = 0x537
// XOR mask: 101010000010010 = 0x5412
// ---------------------------------------------------------------------------
function computeFormatBits(maskId) {
  // 5-bit data: EC level M (00) concatenated with 3-bit mask pattern
  const data = maskId; // (0b00 << 3) | maskId = maskId since EC bits are 0
  let rem = data << 10;
  // Polynomial division (15-bit format word, generator degree 10)
  for (let i = 14; i >= 10; i--) {
    if ((rem >> i) & 1) rem ^= 0x537 << (i - 10);
  }
  return ((data << 10) | rem) ^ 0x5412;
}

function placeFormatInfo(mat, size, maskId) {
  const fmt = computeFormatBits(maskId);

  // First copy: around top-left finder.
  // Bit 0 (LSB) at position (8,0), bit 14 (MSB) at position (0,8).
  // Position sequence per ISO 18004:2015 Table C.1:
  const tlR = [8, 8, 8, 8, 8, 8, 8, 8, 7, 5, 4, 3, 2, 1, 0];
  const tlC = [0, 1, 2, 3, 4, 5, 7, 8, 8, 8, 8, 8, 8, 8, 8];
  for (let i = 0; i < 15; i++) {
    mat[tlR[i] * size + tlC[i]] = (fmt >> i) & 1;
  }

  // Second copy — bottom-left vertical: bits 0-7
  // bit 0 at (size-1, 8), bit 7 at (size-8, 8). Bit 7 is overwritten
  // by the always-dark module at (4v+9, 8), which is set in buildMatrix.
  for (let i = 0; i <= 7; i++) {
    mat[(size - 1 - i) * size + 8] = (fmt >> i) & 1;
  }

  // Second copy — top-right horizontal: bits 8-14
  // bit 8 at (8, size-7), bit 14 at (8, size-1).
  for (let i = 8; i < 15; i++) {
    mat[8 * size + (size - 15 + i)] = (fmt >> i) & 1;
  }
}

// ---------------------------------------------------------------------------
// Build a complete QR matrix
// ---------------------------------------------------------------------------
function buildQR(text) {
  const bytes = new TextEncoder().encode(text);
  const version = selectVersion(bytes.length);
  const size = matrixSize(version);

  const mat = buildMatrix(version);
  const funcMask = makeFuncMask(mat, size);

  const dataCW = encodeByte(text, version);
  const allCW = interleave(version, dataCW);

  placeData(mat, size, allCW);

  let bestMask = 0, bestScore = Infinity, bestMat = null;
  for (let m = 0; m < 8; m++) {
    const candidate = applyMask(mat, size, m, funcMask);
    const s = penaltyScore(candidate, size);
    if (s < bestScore) { bestScore = s; bestMask = m; bestMat = candidate; }
  }

  placeFormatInfo(bestMat, size, bestMask);
  // Re-set the always-dark module — placeFormatInfo's bottom-left second copy
  // writes bit 7 to position (size-8, 8) = (4v+9, 8), which overlaps it.
  bestMat[(4 * version + 9) * size + 8] = 1;
  return { mat: bestMat, size };
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Render a QR code for `text` onto the given `<canvas>` element.
 * @param {HTMLCanvasElement} canvas  Target canvas
 * @param {string}           text    Text to encode (byte mode, EC level M)
 * @param {object}           opts
 * @param {number}           [opts.moduleSize=6]  CSS pixels per module
 * @param {number}           [opts.quietZone=4]   Quiet zone width in modules
 */
export function renderQR(canvas, text, { moduleSize = 6, quietZone = 4 } = {}) {
  const { mat, size } = buildQR(text);
  const dpr = Math.min(window.devicePixelRatio || 1, 3);
  const totalModules = size + 2 * quietZone;
  const cssSize = totalModules * moduleSize;

  canvas.style.width = cssSize + "px";
  canvas.style.height = cssSize + "px";
  canvas.width = Math.round(cssSize * dpr);
  canvas.height = Math.round(cssSize * dpr);

  const ctx = canvas.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, cssSize, cssSize);
  ctx.fillStyle = "#000000";

  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      if (mat[r * size + c] === 1) {
        ctx.fillRect((c + quietZone) * moduleSize, (r + quietZone) * moduleSize, moduleSize, moduleSize);
      }
    }
  }
}
