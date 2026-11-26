// The Posters page (/admin/posters): the poster picker, the text and logo
// choices, Print, and the measurements a person would otherwise take with a
// ruler. Every selector for the page and its poster lives here, with the reader
// that looks inside the PDF Chromium prints of it.
//
// Why a browser: every size inside a poster is in cqw (posters.css), which
// jsdom cannot lay out, so the vitest suite pins the numbers and only a real
// browser can say what they draw.
import { expect } from '@playwright/test';
import zlib from 'node:zlib';
import { settled } from './pace.mjs';

// The A4 page in PDF points, and the box @page poster leaves inside its 10 mm
// margins, in CSS px (96 per inch): 190 x 277 mm.
export const A4_POINTS = { w: 595, h: 842 };
const MM_PX = 96 / 25.4;
export const A4_CONTENT_PX = { width: Math.round(190 * MM_PX), height: Math.round(277 * MM_PX) };
export const pxToMm = (px) => px / MM_PX;

export async function openPosters(page) {
  await page.goto('/admin/posters');
  await expect(page.getByRole('heading', { level: 1, name: 'Posters' })).toBeVisible();
}

// Pick a poster in the picker by its label ("Viewer home", "Shiaijo A") and
// return the preview's poster for it.
export async function choosePoster(page, label) {
  const button = page.getByRole('group', { name: 'Poster' }).getByRole('button', { name: label, exact: true });
  await button.tap();
  await expect(button).toHaveAttribute('aria-pressed', 'true');
  const poster = page.getByTestId('posters-preview').getByRole('region', { name: `${label} poster` });
  await expect(poster).toBeVisible();
  return poster;
}

// Wait until every <img> inside `scope` has finished (loaded or failed), so a
// measurement never reads a logo still at zero size.
export async function imagesSettled(scope) {
  await expect.poll(() => scope.evaluate((root) => [...root.querySelectorAll('img')]
    .every((i) => i.complete))).toBe(true);
}

// The boxes of one poster's pieces, in CSS px, read in one pass. `poster` is a
// `.poster` element locator (the preview's, or the print root's).
export function posterBoxes(poster) {
  return poster.evaluate((root) => {
    const box = (el) => {
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
    };
    const q = (sel) => root.querySelector(sel);
    // Everything a reader sees on the sheet, the letter's own box aside: the
    // band, each piece of the body, the sponsor logos.
    const pieces = [...root.querySelectorAll(
      '.poster__band, .poster__court-label, .poster__titles, .poster__logo-above, .poster__code, .poster__address, .poster__sponsor',
    )].map((el) => ({ cls: el.className, ...box(el) }));
    const letter = q('.poster__court-letter');
    return {
      poster: box(root),
      letter: box(letter),
      letterFontPx: letter ? parseFloat(getComputedStyle(letter).fontSize) : 0,
      fontPx: [...root.querySelectorAll('.poster__tournament, .poster__court-label, .poster__heading, .poster__cta, .poster__address')]
        .map((el) => ({ cls: el.className, px: parseFloat(getComputedStyle(el).fontSize) })),
      code: box(q('.poster__code')),
      address: box(q('.poster__address')),
      corner: box(q('.poster__logo-corner')),
      strip: box(q('.poster__sponsors')),
      sponsors: [...root.querySelectorAll('.poster__sponsor')]
        .filter((el) => el.style.display !== 'none')
        .map((el) => ({ ...box(el), natural: [el.naturalWidth, el.naturalHeight] })),
      pieces,
      band: q('.poster__band') ? getComputedStyle(q('.poster__band')).backgroundColor : '',
      ink: letter ? getComputedStyle(letter).color : '',
    };
  });
}

// Print, with the browser's dialog stubbed: Print builds its print root and
// calls window.print() once the images decode, and leaves the root in place
// until "afterprint", which a stub never fires. Returns the print root, so a
// journey can lay it out for paper and print it with page.pdf().
export async function printWithoutDialog(page, buttonName) {
  await page.evaluate(() => {
    window.__posterPrints = 0;
    window.print = () => { window.__posterPrints += 1; };
  });
  await page.getByRole('button', { name: buttonName }).tap();
  // Print all asks first (the sheets, the address, scan one first).
  if (buttonName === 'Print all posters') {
    const ask = page.getByRole('dialog').filter({ hasText: 'Print all posters?' });
    await settled(ask);
    await expect(ask).toContainText(/This prints \d+ A4 sheets/);
    await ask.getByRole('button', { name: 'Print all', exact: true }).tap();
  }
  await expect.poll(() => page.evaluate(() => window.__posterPrints)).toBe(1);
  const root = page.locator('body > .poster-print-root');
  await expect(root).toHaveCount(1);
  return root;
}

// What a PDF holds, read without a PDF library: its pages (each /Type /Page
// object, never the /Pages tree node), each page's /MediaBox, and every
// non-stroking RGB fill colour (`r g b rg`) its content streams set, inflated
// where they are Flate-compressed. Chromium writes no object streams, so a
// plain scan finds every page object.
export function readPdf(buf) {
  const text = buf.toString('latin1');
  const pages = [...text.matchAll(/\/Type\s*\/Page(?![a-zA-Z])/g)].length;
  const boxes = [...text.matchAll(/\/MediaBox\s*\[\s*([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)\s+([-\d.]+)\s*\]/g)]
    .map((m) => ({ w: Number(m[3]) - Number(m[1]), h: Number(m[4]) - Number(m[2]) }));
  const fills = [];
  const streamRe = /(?<!end)stream\r?\n/g;
  let m;
  while ((m = streamRe.exec(text))) {
    const start = m.index + m[0].length;
    const end = text.indexOf('endstream', start);
    if (end < 0) break;
    const raw = buf.subarray(start, end);
    let body;
    try { body = zlib.inflateSync(raw).toString('latin1'); } catch { body = raw.toString('latin1'); }
    for (const f of body.matchAll(/(?:^|[\s\n])([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+rg\b/g)) {
      fills.push([Number(f[1]), Number(f[2]), Number(f[3])]);
    }
    streamRe.lastIndex = end + 'endstream'.length;
  }
  return { pages, boxes, fills };
}

// "rgb(244, 162, 97)" -> [0.957, 0.635, 0.380], the 0..1 triplet a PDF writes.
export const rgbUnit = (css) => (css.match(/\d+(\.\d+)?/g) || []).slice(0, 3).map((n) => Number(n) / 255);

// Does any fill colour in the PDF match `rgb` (0..1 each) to within a hair?
export const hasFill = (pdf, rgb) => pdf.fills.some((f) => f.every((v, i) => Math.abs(v - rgb[i]) < 0.006));
