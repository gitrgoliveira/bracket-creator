// What can be checked about a capture without a human looking at it: its pixel
// dimensions, and how far its pixels have moved from the committed original.
//
// Dimensions are the one property of a capture that IS deterministic across
// machines: fonts and renderers differ, width and height do not. Comparing them
// against the committed file catches a wrong viewport, DPR or crop selector at
// capture time instead of leaving it for a human to notice at review.
//
// The pixel compare answers a different and narrower question: did THIS surface
// change since the committed image was taken? It is only meaningful because
// docs/screenshots holds this harness's own output, so a capture of an
// unchanged surface reproduces the committed one. That is what keeps a 30
// capture run reviewable - the operator eyeballs what moved, not all of them.
import fs from 'node:fs';
import zlib from 'node:zlib';

// Why a tolerance rather than a byte compare, which would be one line: two runs
// of the SAME code on the SAME machine do not produce identical bytes. Measured
// by re-running the whole suite against the committed images: two of the thirty
// come back a single grey level away, on a few pixels each. Small, but a byte
// compare calls them changed on every run, which is worse than saying nothing.
//
// The two thresholds are set from that measurement, with the real differences
// earlier runs turned up for scale: a focus ring left on an input and a button
// captured in its disabled state both moved hundreds of pixels by 36+ levels.
// So there are two clear orders of magnitude between noise and signal here, and
// these sit between them rather than being tuned to either.
const CHANNEL_TOLERANCE = 16;
const MIN_DIFFERING_PIXELS = 16;

export function pngSize(file) {
  // A recipe with no committed counterpart is the normal way this harness
  // grows, so an absent file is "nothing to compare", not an error. Throwing
  // here made every NEW capture report FAILED after writing a perfectly good
  // PNG, which trains the operator to ignore FAILED lines.
  if (!fs.existsSync(file)) return null;
  const fd = fs.openSync(file, 'r');
  try {
    const head = Buffer.alloc(24);
    // A short read means a truncated file; reporting garbage dimensions from
    // an uninitialised buffer would be worse than reporting nothing.
    if (fs.readSync(fd, head, 0, 24, 0) < 24) return null;
    if (head.toString('ascii', 1, 4) !== 'PNG') return null;
    return { width: head.readUInt32BE(16), height: head.readUInt32BE(20) };
  } finally {
    fs.closeSync(fd);
  }
}

// Decode to raw samples. Deliberately supports only what Playwright writes -
// 8 bits per sample, no interlacing - and returns null for anything else rather
// than guessing. pixelDiff passes that null on as "no opinion", which the
// runner reads as changed, so an undecodable capture is sent for review rather
// than waved through.
function decode(file) {
  if (!fs.existsSync(file)) return null;
  const buf = fs.readFileSync(file);
  if (buf.length < 8 || buf.toString('ascii', 1, 4) !== 'PNG') return null;

  let header = null;
  const parts = [];
  for (let pos = 8; pos + 8 <= buf.length;) {
    const len = buf.readUInt32BE(pos);
    const type = buf.toString('ascii', pos + 4, pos + 8);
    const data = buf.subarray(pos + 8, pos + 8 + len);
    if (type === 'IHDR') {
      header = {
        width: data.readUInt32BE(0),
        height: data.readUInt32BE(4),
        depth: data[8],
        colorType: data[9],
        interlace: data[12],
      };
    } else if (type === 'IDAT') {
      parts.push(data);
    } else if (type === 'IEND') {
      break;
    }
    pos += 12 + len;
  }
  // Samples per pixel by PNG colour type: 0 grey, 2 RGB, 4 grey+alpha, 6 RGBA.
  const samples = { 0: 1, 2: 3, 4: 2, 6: 4 }[header?.colorType];
  if (!header || !samples || header.depth !== 8 || header.interlace !== 0) return null;

  const raw = zlib.inflateSync(Buffer.concat(parts));
  const { width, height } = header;
  const stride = width * samples;
  const out = Buffer.alloc(stride * height);
  let read = 0;
  for (let y = 0; y < height; y++) {
    const filter = raw[read++];
    const rowStart = y * stride;
    for (let x = 0; x < stride; x++) {
      const left = x >= samples ? out[rowStart + x - samples] : 0;
      const up = y > 0 ? out[rowStart - stride + x] : 0;
      const upLeft = y > 0 && x >= samples ? out[rowStart - stride + x - samples] : 0;
      const value = raw[read++];
      let restored;
      switch (filter) {
        case 0: restored = value; break;
        case 1: restored = value + left; break;
        case 2: restored = value + up; break;
        case 3: restored = value + ((left + up) >> 1); break;
        case 4: {
          // Paeth: pick whichever neighbour the linear prediction is closest to.
          const predicted = left + up - upLeft;
          const dl = Math.abs(predicted - left);
          const du = Math.abs(predicted - up);
          const dul = Math.abs(predicted - upLeft);
          restored = value + (dl <= du && dl <= dul ? left : du <= dul ? up : upLeft);
          break;
        }
        default: return null;
      }
      out[rowStart + x] = restored & 0xff;
    }
  }
  return { width, height, samples, data: out };
}

// How far apart two captures are. Returns null when either cannot be decoded,
// which the caller reads as "no opinion" exactly as it does a missing file.
export function pixelDiff(fileA, fileB) {
  const a = decode(fileA);
  const b = decode(fileB);
  if (!a || !b) return null;
  if (a.width !== b.width || a.height !== b.height || a.samples !== b.samples) {
    return { sizeDiffers: true, differing: null, maxDelta: null };
  }
  let differing = 0;
  let maxDelta = 0;
  for (let i = 0; i < a.data.length; i += a.samples) {
    let worst = 0;
    for (let s = 0; s < a.samples; s++) {
      const delta = Math.abs(a.data[i + s] - b.data[i + s]);
      if (delta > worst) worst = delta;
    }
    if (worst > maxDelta) maxDelta = worst;
    if (worst > CHANNEL_TOLERANCE) differing++;
  }
  return {
    sizeDiffers: false,
    differing,
    maxDelta,
    changed: differing >= MIN_DIFFERING_PIXELS,
    total: a.width * a.height,
  };
}
