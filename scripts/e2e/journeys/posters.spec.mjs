// The venue posters (/admin/posters): what jsdom cannot measure, judged in a
// real browser. Every size inside a poster is in cqw, so only a layout engine
// can say that the court letter is the largest thing on a court poster, that
// the sponsors sit on one row at one height for any count from 1 to 6, that a
// court code is over 8 cm wide on A4 and the viewer code larger still, and
// that the longest text the server allows still fits the sheet. Print is
// judged on the PDF Chromium makes of the print root: one A4 portrait page per
// poster, no blank page after the last, and the band's colour on paper with
// background graphics off (print-color-adjust: exact), against a control
// print with that rule taken away. The browser's own Print (page.pdf() with no
// Print button, as Cmd+P) prints the poster on screen, or only a note while a
// text is over its limit.
//
// Everything is seeded through the interface: the first-run form, a primary
// colour, the logo and six sponsors on Edit tournament details, the poster text
// on the Posters page. The images are PNGs written at run time.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament, login } from '../fixtures/setup.mjs';
import { ensureCoarse } from '../fixtures/pointer.mjs';
import { addSponsor, openEditTournament, pngBytes, uploadLogo } from '../fixtures/organiser.mjs';
import {
  A4_CONTENT_PX, A4_POINTS, choosePoster, hasFill, imagesSettled, openPosters, posterBoxes, printWithoutDialog, pxToMm,
  readPdf, rgbUnit,
} from '../fixtures/posters.mjs';

// Long enough to take two lines in the band: the court sizes were chosen for it.
const TOURNAMENT = 'The Northern Counties Open Kendo Championship 2026';
const COURTS = 3;
// A light primary colour: the paper ink (court letter, heading) is darkened
// from it, so the band's fill is the one place this exact colour is painted,
// and seeing it in the PDF means the band's background printed.
const PRIMARY = '#f4a261';
// Six sponsors of six shapes, so "one height" is not an accident of equal
// images: a square, a banner, a tall one, and the rest between.
const SPONSORS = [
  ['Alpha Bogu', [6, 6]], ['Beta Shinai Works', [60, 3]], ['Gamma Hakama', [4, 12]],
  ['Delta Tsuba', [20, 10]], ['Epsilon Dojo Supply', [40, 4]], ['Zeta Travel', [9, 7]],
];
const HEADING_80 = 'Scan the code to follow every bout on this shiaijo, live, from wherever you sit.';
const CTA_120 = 'Results, the next pairs called and each pool table update the moment a judge records them, on any phone, no apps needed.';

test.describe.configure({ mode: 'serial' });

test.describe('venue posters', () => {
  let shot;
  let work;
  const audit = auditLog('posters');
  const { record } = audit;

  test.beforeAll(() => {
    shot = journeyShots('posters');
    work = fs.mkdtempSync(path.join(os.tmpdir(), 'bc-e2e-posters-'));
  });
  test.afterAll(() => {
    audit.flush();
    if (work) fs.rmSync(work, { recursive: true, force: true });
  });

  test('layout: the court letter leads, the sponsors keep one row for 1 to 6, the viewer code is the larger', async ({ page }) => {
    await test.step('seed the tournament, its colour and its logo', async () => {
      await enterTournament(page, { name: TOURNAMENT, courts: COURTS });
      await openEditTournament(page);
      await page.locator('#branding-primary-color').fill(PRIMARY);
      await page.getByRole('button', { name: 'Save changes' }).tap();
      await expect(page.getByRole('heading', { level: 1, name: 'Edit tournament details' })).toHaveCount(0);
      await openEditTournament(page);
      const logo = path.join(work, 'logo.png');
      fs.writeFileSync(logo, pngBytes(30, 20, [30, 120, 60]));
      await uploadLogo(page, logo);
    });

    const heights = [];
    for (let n = 1; n <= SPONSORS.length; n += 1) {
      await test.step(`${n} sponsor${n > 1 ? 's' : ''}: one row of logos at one height`, async () => {
        const [name, [w, h]] = SPONSORS[n - 1];
        const file = path.join(work, `sponsor-${n}.png`);
        fs.writeFileSync(file, pngBytes(w, h, [20 * n, 90, 200]));
        await openEditTournament(page);
        await addSponsor(page, { name, file });
        await openPosters(page);
        const poster = await choosePoster(page, 'Shiaijo A');
        await imagesSettled(poster);
        const b = await posterBoxes(poster);
        record({ journey: 'posters', step: 'sponsor strip', count: n, strip: b.strip, sponsors: b.sponsors.map((s) => [s.width, s.height, s.top]) });
        expect(b.sponsors, 'every sponsor is drawn').toHaveLength(n);
        const [first] = b.sponsors;
        for (const s of b.sponsors) {
          expect(Math.abs(s.height - first.height), 'one height').toBeLessThan(0.5);
          expect(Math.abs(s.top - first.top), 'one row').toBeLessThan(0.5);
          expect(s.width, 'never squeezed to nothing').toBeGreaterThan(1);
          expect(s.left, 'inside the strip').toBeGreaterThanOrEqual(b.strip.left - 0.5);
          expect(s.right, 'inside the strip').toBeLessThanOrEqual(b.strip.right + 0.5);
        }
        heights.push(first.height / b.poster.width);
        expect(b.strip.height, 'the strip is one row tall').toBeLessThan(first.height * 2);
        expect(b.strip.bottom, 'the strip is on the sheet').toBeLessThanOrEqual(b.poster.bottom + 0.5);
        if (n === 1 || n === SPONSORS.length) await shot(page, `court-poster-${n}-sponsors`);
      });
    }

    await test.step('the logos grow into the space the poster leaves, taller for fewer sponsors', async () => {
      record({ journey: 'posters', step: 'sponsor heights', shareOfWidth: heights });
      for (let i = 1; i < heights.length; i += 1) {
        expect(heights[i], `${i + 1} sponsors are no taller than ${i}`).toBeLessThanOrEqual(heights[i - 1] + 0.002);
      }
      expect(heights[0], 'one sponsor is taller than six').toBeGreaterThan(heights[heights.length - 1]);
      expect(heights[0], 'one sponsor is at least twice the floor').toBeGreaterThanOrEqual(2 * 0.065);
      expect(heights[heights.length - 1], 'six sponsors are over the floor').toBeGreaterThanOrEqual(0.065 - 0.002);
    });

    await test.step('the address sits just under its code', async () => {
      for (const label of ['Viewer home', 'Shiaijo A']) {
        const b = await posterBoxes(await choosePoster(page, label));
        const gap = (b.address.top - b.code.bottom) / b.poster.width;
        record({ journey: 'posters', step: 'address gap', poster: label, shareOfWidth: gap });
        expect(gap, `${label}: the address is under the code`).toBeGreaterThanOrEqual(0);
        expect(gap, `${label}: and close to it`).toBeLessThan(0.02);
      }
    });

    await test.step('the court letter is the largest element of a court poster', async () => {
      const poster = await choosePoster(page, 'Shiaijo A');
      const b = await posterBoxes(poster);
      record({ journey: 'posters', step: 'court letter', letter: b.letter, letterFontPx: b.letterFontPx, pieces: b.pieces.map((p) => [p.cls, p.height]) });
      for (const p of b.pieces) expect(b.letter.height, `taller than ${p.cls}`).toBeGreaterThan(p.height);
      for (const f of b.fontPx) expect(b.letterFontPx, `set larger than ${f.cls}`).toBeGreaterThan(f.px);
      await expect(poster.locator('.poster__court-letter')).toHaveText('A');
    });

    await test.step('the viewer code is larger than a court code', async () => {
      const court = await posterBoxes(await choosePoster(page, 'Shiaijo A'));
      const viewerPoster = await choosePoster(page, 'Viewer home');
      await imagesSettled(viewerPoster);
      const viewer = await posterBoxes(viewerPoster);
      record({ journey: 'posters', step: 'code sizes', viewer: viewer.code.width, court: court.code.width, poster: viewer.poster.width });
      expect(viewer.poster.width, 'the two previews are drawn at one width').toBeCloseTo(court.poster.width, 0);
      expect(viewer.code.width).toBeGreaterThan(court.code.width);
      await shot(page, 'viewer-poster');
    });
  });

  test('the longest text the server allows, the logo in the corner and six sponsors, still fit a court poster', async ({ page }) => {
    await login(page);
    await openPosters(page);
    const poster = await choosePoster(page, 'Shiaijo B');
    await page.getByLabel('Heading', { exact: true }).fill(HEADING_80);
    await page.getByLabel(/^Call to action/).fill(CTA_120);
    await page.getByRole('radio', { name: 'Logo in the top corner' }).check();
    await expect(page.getByText('80 of 80 characters')).toBeVisible();
    await expect(page.getByText('120 of 120 characters')).toBeVisible();
    await expect(poster).toHaveAttribute('data-placement', 'above');
    await page.getByRole('button', { name: 'Save', exact: true }).tap();
    await expect(page.getByTestId('posters-status')).toHaveText('Saved');
    await imagesSettled(poster);
    const b = await posterBoxes(poster);
    record({ journey: 'posters', step: 'longest court poster', poster: b.poster, address: b.address, strip: b.strip, pieces: b.pieces.map((p) => [p.cls, p.top, p.bottom]) });
    // In order down the sheet, nothing overlapping the next, and the strip on
    // the sheet below the address.
    const body = b.pieces.filter((p) => !/poster__sponsor\b/.test(p.cls));
    for (let i = 1; i < body.length; i += 1) {
      expect(body[i].top, `${body[i].cls} starts below ${body[i - 1].cls}`).toBeGreaterThanOrEqual(body[i - 1].bottom - 0.5);
    }
    expect(b.strip.top, 'the sponsor strip clears the address').toBeGreaterThanOrEqual(b.address.bottom);
    expect(b.strip.bottom, 'the sponsor strip is on the sheet').toBeLessThanOrEqual(b.poster.bottom + 0.5);
    for (const p of b.pieces) expect(b.letter.height, `the letter still leads ${p.cls}`).toBeGreaterThan(p.height);
    // The corner logo: beside the letter and clear of it, under the band, above
    // the code, and smaller than the letter.
    const band = b.pieces.find((p) => /poster__band/.test(p.cls));
    record({ journey: 'posters', step: 'corner logo', corner: b.corner, letter: b.letter });
    expect(b.corner, 'a corner logo is drawn').not.toBeNull();
    expect(b.corner.right, 'the corner logo clears the letter').toBeLessThanOrEqual(b.letter.left);
    expect(b.corner.left, 'the corner logo is on the sheet').toBeGreaterThanOrEqual(b.poster.left);
    expect(b.corner.top, 'the corner logo is under the band').toBeGreaterThanOrEqual(band.bottom);
    expect(b.corner.bottom, 'the corner logo is above the code').toBeLessThanOrEqual(b.code.top);
    expect(b.letter.height).toBeGreaterThan(b.corner.height);
    await shot(page, 'court-poster-longest');
  });

  test('Print all: one A4 portrait page per poster, in colour with background graphics off', async ({ page }) => {
    await login(page);
    await openPosters(page);
    const root = await printWithoutDialog(page, 'Print all posters');
    const posters = root.locator('.poster');
    await expect(posters).toHaveCount(1 + COURTS);
    await imagesSettled(root);

    await test.step('laid out for paper, a court code is over 8 cm and the viewer code larger', async () => {
      // The page box @page poster leaves inside its margins, as the viewport,
      // with print media: what the printer lays out.
      await page.setViewportSize(A4_CONTENT_PX);
      await page.emulateMedia({ media: 'print' });
      try {
        const boxes = [];
        for (let i = 0; i < 1 + COURTS; i += 1) boxes.push(await posterBoxes(posters.nth(i)));
        record({ journey: 'posters', step: 'print layout', page: A4_CONTENT_PX, posters: boxes.map((b) => ({ w: b.poster.width, h: b.poster.height, codeMm: pxToMm(b.code.width) })) });
        const [viewer, ...courts] = boxes;
        for (const b of boxes) {
          expect(b.poster.height, 'a poster fits one page').toBeLessThanOrEqual(A4_CONTENT_PX.height + 0.5);
          expect(b.poster.width, 'a poster fits one page').toBeLessThanOrEqual(A4_CONTENT_PX.width + 0.5);
        }
        for (const c of courts) {
          expect(pxToMm(c.code.width), 'a court code is over 8 cm wide').toBeGreaterThan(80);
          expect(viewer.code.width, 'the viewer code is larger').toBeGreaterThan(c.code.width);
        }
      } finally {
        await page.emulateMedia({ media: null });
        await page.setViewportSize(OPERATOR_DEVICE.viewport);
        await ensureCoarse(page);
      }
    });

    await test.step('the PDF: one A4 portrait page a poster, no trailing blank, the band and the letter in colour', async () => {
      const band = rgbUnit((await posterBoxes(posters.first())).band);
      // The court letter is text in the poster ink (the primary darkened until
      // it reads on white), never black.
      const { ink } = await posterBoxes(posters.nth(1));
      expect(ink, 'the court letter is set in the poster ink').not.toBe('rgb(0, 0, 0)');
      const pdf = readPdf(await page.pdf({ preferCSSPageSize: true, printBackground: false }));
      record({ journey: 'posters', step: 'print all', pages: pdf.pages, boxes: pdf.boxes, band, ink });
      expect(pdf.pages, 'one page per poster, and none after the last').toBe(1 + COURTS);
      expect(pdf.boxes).toHaveLength(1 + COURTS);
      for (const box of pdf.boxes) {
        expect(Math.abs(box.w - A4_POINTS.w), 'A4 width').toBeLessThan(2);
        expect(Math.abs(box.h - A4_POINTS.h), 'A4 height, portrait').toBeLessThan(2);
      }
      expect(hasFill(pdf, band), `the band's ${PRIMARY} prints without background graphics`).toBe(true);
      expect(hasFill(pdf, rgbUnit(ink)), 'the court letter prints in its colour').toBe(true);
      // Printing fires afterprint, and the page takes its print root down.
      await expect(page.locator('body > .poster-print-root')).toHaveCount(0);
    });

    await test.step('the control: without print-color-adjust the band would not print', async () => {
      // The same print with the rule taken away, so the check above is about
      // that rule and not about a fill Chromium draws whatever it is told.
      const again = await printWithoutDialog(page, 'Print all posters');
      await imagesSettled(again);
      const band = rgbUnit((await posterBoxes(again.locator('.poster').first())).band);
      const control = await page.addStyleTag({ content: '.poster, .poster * { print-color-adjust: economy !important; -webkit-print-color-adjust: economy !important; }' });
      const plain = readPdf(await page.pdf({ preferCSSPageSize: true, printBackground: false }));
      await control.evaluate((el) => el.remove());
      record({ journey: 'posters', step: 'print control', pages: plain.pages, bandPrinted: hasFill(plain, band) });
      expect(plain.pages).toBe(1 + COURTS);
      expect(hasFill(plain, band)).toBe(false);
    });
  });
  test('the browser\'s own Print (Cmd+P) prints the poster on screen, and only a note while a text is over its limit', async ({ page }) => {
    await login(page);
    await openPosters(page);
    const poster = await choosePoster(page, 'Shiaijo A');
    await imagesSettled(poster);
    const band = rgbUnit((await posterBoxes(poster)).band);
    // page.pdf() prints the way Cmd+P does: no Print button, beforeprint and
    // afterprint around it.
    const pdf = readPdf(await page.pdf({ preferCSSPageSize: true, printBackground: false }));
    record({ journey: 'posters', step: 'browser print', pages: pdf.pages, bandPrinted: hasFill(pdf, band) });
    expect(pdf.pages, 'one sheet: the poster on screen').toBe(1);
    expect(hasFill(pdf, band), 'the poster itself printed').toBe(true);
    await expect(page.locator('body.printing-posters')).toHaveCount(0);

    await page.getByLabel('Heading', { exact: true }).fill(`${HEADING_80}!`);
    await expect(page.getByTestId('posters-too-long')).toHaveText('Over a limit on Shiaijo A: shorten it to save or print.');
    const note = readPdf(await page.pdf({ preferCSSPageSize: true, printBackground: false }));
    record({ journey: 'posters', step: 'browser print over a limit', pages: note.pages, bandPrinted: hasFill(note, band) });
    expect(note.pages, 'one sheet: the note').toBe(1);
    expect(hasFill(note, band), 'no poster printed').toBe(false);
  });
});
