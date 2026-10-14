// The organiser's own pages: the Excel download on a competition's Export
// section, the folder import, the forgotten-password reset page, and the
// branding and sponsor uploads on Edit tournament details. Every selector for
// those lives here, with the runtime file makers they need (a PNG, an
// import bundle) and the small zip reader that looks inside a workbook.
import { expect } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import zlib from 'node:zlib';
import { dwell } from './pace.mjs';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';

// ------------------------------------------------------------- run files --

// A valid PNG of w x h in one flat colour, built here at run time: the
// journeys commit no binaries. A size nothing else in the app has (3 x 2) is
// what lets a journey tell an uploaded image from the bundled default.
export function pngBytes(w, h, [r, g, b] = [200, 30, 30]) {
  const chunk = (type, data) => {
    const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(zlib.crc32(body));
    return Buffer.concat([len, body, crc]);
  };
  const head = Buffer.alloc(13);
  head.writeUInt32BE(w, 0);
  head.writeUInt32BE(h, 4);
  head.writeUInt8(8, 8); // bit depth
  head.writeUInt8(2, 9); // colour type: RGB
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, () => [r, g, b]).flat())]);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', head),
    chunk('IDAT', zlib.deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

// ---------------------------------------------------------------- workbook --

// Every entry of a zip, by name, read from its central directory and inflated
// with node:zlib. Enough for an .xlsx, which is a plain deflated zip.
export function readZip(buf) {
  let eocd = -1;
  for (let i = buf.length - 22; i >= 0; i -= 1) {
    if (buf.readUInt32LE(i) === 0x06054b50) { eocd = i; break; }
  }
  if (eocd < 0) throw new Error('readZip: no end-of-central-directory record: not a zip');
  const count = buf.readUInt16LE(eocd + 10);
  let p = buf.readUInt32LE(eocd + 16);
  const entries = new Map();
  for (let n = 0; n < count; n += 1) {
    if (buf.readUInt32LE(p) !== 0x02014b50) throw new Error('readZip: bad central directory entry');
    const method = buf.readUInt16LE(p + 10);
    const size = buf.readUInt32LE(p + 20);
    const nameLen = buf.readUInt16LE(p + 28);
    const extraLen = buf.readUInt16LE(p + 30);
    const commentLen = buf.readUInt16LE(p + 32);
    const local = buf.readUInt32LE(p + 42);
    const name = buf.toString('utf8', p + 46, p + 46 + nameLen);
    const dataAt = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const packed = buf.subarray(dataAt, dataAt + size);
    entries.set(name, method === 0 ? packed : zlib.inflateRawSync(packed));
    p += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

// One pass, so a decoded '&' is never decoded again ("&amp;lt;" reads "&lt;").
const XML_ENTITIES = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" };
const xmlText = (s) => s.replace(/&(amp|lt|gt|quot|apos);/g, (_, e) => XML_ENTITIES[e]);

// A shared or inline string's text: its <t> runs joined (a rich-text string
// splits it across several), read rather than left over from stripping tags.
const runText = (s) => [...s.matchAll(/<t\b[^>]*>([\s\S]*?)<\/t>/g)].map((m) => xmlText(m[1])).join('');

// What a downloaded workbook holds: its sheet names, in order, and all of its
// cell text (shared strings and inline strings, joined), unescaped.
export function readWorkbook(buf) {
  const zip = readZip(buf);
  const xml = (name) => (zip.get(name) ? zip.get(name).toString('utf8') : '');
  if (!zip.has('xl/workbook.xml')) throw new Error(`not a workbook: parts are ${[...zip.keys()].join(', ')}`);
  const sheets = [...xml('xl/workbook.xml').matchAll(/<sheet\b[^>]*\bname="([^"]*)"/g)].map((m) => xmlText(m[1]));
  const parts = [...zip.keys()].filter((k) => k === 'xl/sharedStrings.xml' || /^xl\/worksheets\/[^/]+\.xml$/.test(k));
  const text = xmlText(parts.map((k) => xml(k).replace(/<[^>]+>/g, ' ')).join(' '));
  return { sheets, text, parts: [...zip.keys()] };
}

// Every non-empty cell of one sheet, as { "F4": "M", ... }: shared and inline
// strings resolved, numbers as written. A cell's place is what tells a played
// bout's mark from the same letter in a heading, which the flat text of
// readWorkbook cannot.
export function sheetCells(buf, sheetName) {
  const zip = readZip(buf);
  const xml = (name) => (zip.get(name) ? zip.get(name).toString('utf8') : '');
  const shared = [...xml('xl/sharedStrings.xml').matchAll(/<si>([\s\S]*?)<\/si>/g)]
    .map((m) => runText(m[1]));
  // Each <sheet> tag's attributes are read one by one, so their order does not matter.
  const entry = [...xml('xl/workbook.xml').matchAll(/<sheet\b[^>]*>/g)]
    .map((m) => ({ name: /\bname="([^"]*)"/.exec(m[0]), rid: /\br:id="([^"]*)"/.exec(m[0]) }))
    .find((s) => s.name && s.rid && xmlText(s.name[1]) === sheetName);
  if (!entry) throw new Error(`sheetCells: no sheet named ${sheetName}`);
  const rel = [...xml('xl/_rels/workbook.xml.rels').matchAll(/Id="([^"]*)"[^>]*Target="([^"]*)"|Target="([^"]*)"[^>]*Id="([^"]*)"/g)]
    .map((m) => ({ id: m[1] || m[4], target: m[2] || m[3] })).find((r) => r.id === entry.rid[1]);
  if (!rel) throw new Error(`sheetCells: no part for ${sheetName}`);
  const part = xml(`xl/${rel.target.replace(/^\/?(xl\/)?/, '')}`);
  const out = {};
  for (const m of part.matchAll(/<c\b([^>]*?)(?:\/>|>([\s\S]*?)<\/c>)/g)) {
    const ref = /\br="([A-Z]+\d+)"/.exec(m[1]);
    if (!ref || !m[2]) continue;
    const type = /\bt="(\w+)"/.exec(m[1]);
    const v = /<v>([\s\S]*?)<\/v>/.exec(m[2]);
    const inline = /<is>([\s\S]*?)<\/is>/.exec(m[2]);
    let text = null;
    if (inline) text = runText(inline[1]);
    else if (v) text = type && type[1] === 's' ? shared[Number(v[1])] : xmlText(v[1]);
    if (text !== null && text !== '') out[ref[1]] = text;
  }
  return out;
}

// Open a competition's Export section and download its results workbook.
// Resolves to { file, name, bytes } once Playwright's download event has fired.
export async function downloadResultsWorkbook(page, id) {
  await page.goto(`/admin/competition/${id}/export`);
  const button = page.getByRole('button', { name: 'Download results (.xlsx)' });
  await expect(button).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent('download'), button.tap()]);
  const file = await download.path();
  if (!file) throw new Error(`the download failed: ${await download.failure()}`);
  return { file, name: download.suggestedFilename(), bytes: fs.readFileSync(file) };
}

// ------------------------------------------------------------------ import --

// Write an import bundle to `dir`: manifest.yaml plus one participants CSV per
// competition. `comps` is [{ id, name, kind, format, date, roster: [[name, dojo]] }]
// (`date` is DD-MM-YYYY and may be left out).
// The app has no exporter that writes a manifest, so a bundle is assembled by
// hand, as the import page describes ("a folder containing manifest.yaml and
// participant CSV files"). Returns the file paths, manifest first.
export function writeImportBundle(dir, comps) {
  fs.mkdirSync(dir, { recursive: true });
  const files = [];
  const lines = ['competitions:'];
  for (const c of comps) {
    const csv = `${c.id}.csv`;
    fs.writeFileSync(path.join(dir, csv), c.roster.map((r) => r.join(', ')).join('\n') + '\n');
    lines.push(`  - id: ${c.id}`, `    name: ${c.name}`, `    kind: ${c.kind || 'individual'}`,
      `    format: ${c.format || 'mixed'}`, `    participants: ${csv}`);
    if (c.date) lines.push(`    date: ${c.date}`);
    files.push(path.join(dir, csv));
  }
  const manifest = path.join(dir, 'manifest.yaml');
  fs.writeFileSync(manifest, lines.join('\n') + '\n');
  return [manifest, ...files];
}

// Pick `files` on the import page ("Select files individually" opens the
// browser's file chooser), tap Import, answer the confirm, and return once the
// results list is up.
export async function importFiles(page, files) {
  await page.goto('/admin/import');
  await expect(page.getByRole('heading', { level: 1, name: 'Import competitions' })).toBeVisible();
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByRole('button', { name: 'Select files individually' }).tap(),
  ]);
  await chooser.setFiles(files);
  await expect(page.getByText('manifest found: manifest.yaml')).toBeVisible();
  await page.getByRole('button', { name: 'Import', exact: true }).tap();
  const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  await dialog.waitFor({ state: 'visible' });
  await dwell(page, Date.now());
  await dialog.getByRole('button', { name: 'Import competitions' }).tap();
  await expect(page.getByText('Import results')).toBeVisible();
}

export const importResultRows = (page) => page.getByTestId('import-result-row');

// ------------------------------------------------------------------- reset --

// Choose a new admin password on the /reset page (file mode, from the machine
// running the server). The page signs the browser straight in on success.
export async function resetPassword(page, password) {
  await page.getByLabel('New password', { exact: true }).fill(password);
  await page.getByLabel('Confirm new password').fill(password);
  await page.getByRole('button', { name: 'Reset password', exact: true }).tap();
}

// ------------------------------------------------------ branding, sponsors --

export async function openEditTournament(page) {
  await page.goto('/admin/edit-tournament');
  await expect(page.getByRole('heading', { level: 1, name: 'Edit tournament details' })).toBeVisible();
}

// Upload the tournament logo from `file` (a path). Returns once the page shows
// the logo as the current one (its Remove button is there).
export async function uploadLogo(page, file) {
  await page.locator('#branding-logo-file').setInputFiles(file);
  await page.getByRole('button', { name: 'Upload logo', exact: true }).tap();
  await expect(page.getByRole('button', { name: 'Remove' }).first()).toBeVisible();
}

// Add a sponsor by name and image. Returns once the sponsor is listed.
export async function addSponsor(page, { name, file }) {
  await page.locator('#sponsor-name').fill(name);
  await page.locator('#sponsor-logo').setInputFiles(file);
  await page.getByRole('button', { name: 'Add sponsor' }).tap();
  await expect(page.locator(`img[alt="${name}"]`)).toBeVisible();
}

// What an <img> really holds once loaded: its size in pixels and the request
// URL it was fetched from. The tournament logo <img> falls back to the bundled
// /logo.jpeg when none is uploaded, so "an image is on screen" proves nothing;
// the uploaded image's own size does.
export async function loadedImage(locator) {
  await expect.poll(() => locator.evaluate((i) => i.complete && i.naturalWidth > 0), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
  return locator.evaluate((i) => ({ w: i.naturalWidth, h: i.naturalHeight, src: i.getAttribute('src') }));
}

// Today as the manifest's DD-MM-YYYY date (the machine's own day, which is the
// day the first-run form offers the tournament).
export function todayDMY(now = new Date()) {
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(now.getDate())}-${pad(now.getMonth() + 1)}-${now.getFullYear()}`;
}

// The day `days` from now, as the date input's YYYY-MM-DD and as the
// manifest's DD-MM-YYYY: a tournament day that is not today.
export function dayFromNow(days, now = new Date()) {
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + days);
  const pad = (n) => String(n).padStart(2, '0');
  const [y, m, dd] = [d.getFullYear(), pad(d.getMonth() + 1), pad(d.getDate())];
  return { iso: `${y}-${m}-${dd}`, dmy: `${dd}-${m}-${y}` };
}
