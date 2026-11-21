// Sample printouts: prints the example workbooks the docs show as samples,
// compares every page with the committed sample, and copies into docs/ only
// the PDFs whose pages changed, with their thumbnails. See README.md.
//
//   make docs/samples
//
// A PDF is never compared by its bytes: `print` stamps every file it writes
// with its own date and ids, so two prints of one workbook always differ.
// Every page is turned into a picture instead, the committed sample's pages
// too, and the pictures are compared with the screenshot harness's own pixel
// compare, which allows the grey level or two of anti-aliasing noise a render
// can move by.
//
// Pages become pictures with the two tools this repository already needs:
// pdfcpu (a Go module the app is built with) splits a PDF into one file per
// page and reads each page's size, and LibreOffice (which `print` itself
// needs) draws each page as a PNG. Two things LibreOffice does are worked
// around. Its PNG export draws only a document's first page and ignores a page
// range, so the split comes first. And opening a PDF directly turns its text
// into editable text boxes that it lays out again, which moves letters (the
// pool tables' "PW" and "PL" headers ran into each other, measured
// 2026-11-21); so each page is instead placed as a picture on a drawing page
// of the same size, which LibreOffice draws as printed.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { pixelDiff } from '../screenshots/lib/png.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const BIN = path.join(ROOT, 'bin', 'bracket-creator');
const DOCS = path.join(ROOT, 'docs', 'samples');

// The samples, each printed on its own from the example workbook of the same
// name at the repository root (made by `make examples`). `thumbs` names the
// page of each PDF the docs page shows as its picture, chosen by eye: a page
// that shows what the printout is for. Pages are counted from 1. A PDF the
// print does not write for a sample (a team workbook has no competitor tags)
// is simply absent.
const SAMPLES = [
  {
    name: 'pools-example-medium',
    thumbs: {
      print_registration: 1,
      print_names_to_print: 2,
      print_tags: 2,
      print_pools_and_trees: 2,
      print_full_bracket: 3,
    },
  },
  {
    name: 'pools-example-medium-teams',
    thumbs: {
      print_registration: 1,
      print_names_to_print: 2,
      print_pools_and_trees: 2,
      print_full_bracket: 3,
    },
  },
];

function soffice() {
  const candidates = [
    process.env.LIBREOFFICE_PATH,
    'soffice',
    '/Applications/LibreOffice.app/Contents/MacOS/soffice',
    '/opt/homebrew/bin/soffice',
    '/usr/local/bin/soffice',
    '/usr/bin/soffice',
    '/opt/libreoffice/program/soffice',
  ].filter(Boolean);
  for (const c of candidates) {
    try {
      execFileSync(c, ['--version'], { stdio: 'ignore' });
      return c;
    } catch {
      // try the next one
    }
  }
  throw new Error('LibreOffice (soffice) not found: install it or set LIBREOFFICE_PATH');
}

const SOFFICE = soffice();
const WORK = fs.mkdtempSync(path.join(os.tmpdir(), 'bracket-creator-samples-'));
// A profile of its own, so a LibreOffice window the operator has open does
// not swallow the conversion, as `print` does for the same reason.
const PROFILE = `file://${path.join(WORK, 'lo-profile')}`;

// pdfcpu at the version go.mod names, run from the repository's module.
function pdfcpu(...args) {
  return execFileSync('go', ['run', 'github.com/pdfcpu/pdfcpu/cmd/pdfcpu', ...args], {
    cwd: ROOT,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
    maxBuffer: 64 * 1024 * 1024,
  });
}

// A flat OpenDocument drawing holding one PDF page as a picture that fills a
// drawing page of the PDF page's own size (in points). LibreOffice keeps the
// page size only with the empty office:styles element present: without it,
// every page came out A4 portrait (measured on LibreOffice 26.8, 2026-11-21).
function pageDrawing(pagePdf, width, height) {
  const data = fs.readFileSync(pagePdf).toString('base64');
  return `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.graphics">
<office:styles/>
<office:automatic-styles><style:page-layout style:name="page"><style:page-layout-properties fo:margin-top="0pt" fo:margin-bottom="0pt" fo:margin-left="0pt" fo:margin-right="0pt" fo:page-width="${width}pt" fo:page-height="${height}pt"/></style:page-layout></office:automatic-styles>
<office:master-styles><style:master-page style:name="Default" style:page-layout-name="page"/></office:master-styles>
<office:body><office:drawing><draw:page draw:name="page" draw:master-page-name="Default">
<draw:frame svg:x="0pt" svg:y="0pt" svg:width="${width}pt" svg:height="${height}pt"><draw:image><office:binary-data>${data}</office:binary-data></draw:image></draw:frame>
</draw:page></office:drawing></office:body></office:document>
`;
}

// Every page of one PDF as a PNG, in page order. LibreOffice draws at its
// default 96 dots per inch, so an A4 page is 794 by 1123 pixels.
function pagePictures(pdf, dir) {
  const pages = path.join(dir, 'pages');
  const drawings = path.join(dir, 'drawings');
  const pngs = path.join(dir, 'png');
  fs.mkdirSync(pages, { recursive: true });
  fs.mkdirSync(drawings, { recursive: true });
  pdfcpu('split', pdf, pages);
  const base = path.basename(pdf, '.pdf');
  const files = fs
    .readdirSync(pages)
    .map((f) => ({ f, n: Number(f.slice(base.length + 1, -'.pdf'.length)) }))
    .filter(({ n }) => Number.isInteger(n) && n > 0)
    .sort((a, b) => a.n - b.n)
    .map(({ f }) => path.join(pages, f));
  // One info call for every page: each split file holds one page, so its
  // single page size is that page's.
  const out = pdfcpu('info', '-j', ...files);
  const infos = JSON.parse(out.slice(out.indexOf('{'))).infos;
  const sizeOf = new Map(infos.map((i) => [path.resolve(i.source), i.pageSizes[0]]));
  const fodgs = files.map((f) => {
    const size = sizeOf.get(path.resolve(f));
    if (!size) throw new Error(`no page size for ${f}`);
    const fodg = path.join(drawings, `${path.basename(f, '.pdf')}.fodg`);
    fs.writeFileSync(fodg, pageDrawing(f, size.width, size.height));
    return fodg;
  });
  execFileSync(
    SOFFICE,
    [`-env:UserInstallation=${PROFILE}`, '--headless', '--convert-to', 'png', '--outdir', pngs, ...fodgs],
    { stdio: ['ignore', 'ignore', 'inherit'] },
  );
  const pictures = fodgs.map((f) => path.join(pngs, `${path.basename(f, '.fodg')}.png`));
  // LibreOffice can exit 0 having drawn nothing for a page: stop before a
  // missing picture reads as a changed page and half-updates docs/samples/.
  for (const png of pictures) {
    if (!fs.existsSync(png)) throw new Error(`LibreOffice drew no ${png} from ${pdf}`);
  }
  return pictures;
}

// Whether the new print's pages differ from the committed sample's, and how.
const pageCount = (n) => `${n} ${n === 1 ? 'page' : 'pages'}`;

function compare(newPages, oldPages) {
  if (!oldPages) return { changed: true, note: `NEW, ${pageCount(newPages.length)}` };
  if (newPages.length !== oldPages.length) {
    return { changed: true, note: `CHANGED, ${pageCount(oldPages.length)} before, ${newPages.length} now` };
  }
  const moved = [];
  newPages.forEach((p, i) => {
    const diff = pixelDiff(p, oldPages[i]);
    if (!diff || diff.changed) moved.push(i + 1);
  });
  if (moved.length === 0) return { changed: false, note: `unchanged, ${pageCount(newPages.length)}` };
  return { changed: true, note: `CHANGED, page ${moved.join(', ')} of ${newPages.length}` };
}

const copied = [];
const unchanged = [];
try {
  for (const sample of SAMPLES) {
    const workbook = path.join(ROOT, `${sample.name}.xlsx`);
    if (!fs.existsSync(workbook)) throw new Error(`${workbook} is missing: run make examples`);
    const input = path.join(WORK, sample.name, 'in');
    const printed = path.join(WORK, sample.name, 'pdf');
    fs.mkdirSync(input, { recursive: true });
    fs.copyFileSync(workbook, path.join(input, path.basename(workbook)));
    execFileSync(BIN, ['print', '--type=all', `--input=${input}`, `--output-dir=${printed}`], {
      stdio: ['ignore', 'ignore', 'inherit'],
    });

    const target = path.join(DOCS, sample.name);
    fs.mkdirSync(target, { recursive: true });
    for (const [kind, thumbPage] of Object.entries(sample.thumbs)) {
      const pdf = path.join(printed, `${kind}.pdf`);
      if (!fs.existsSync(pdf)) throw new Error(`${sample.name}: print wrote no ${kind}.pdf`);
      const newPages = pagePictures(pdf, path.join(WORK, sample.name, 'new', kind));
      const committed = path.join(target, `${kind}.pdf`);
      const oldPages = fs.existsSync(committed)
        ? pagePictures(committed, path.join(WORK, sample.name, 'old', kind))
        : null;
      const thumb = path.join(target, `${kind}.png`);
      const result = compare(newPages, oldPages);
      const label = `${sample.name}/${kind}.pdf`;
      if (thumbPage > newPages.length) {
        throw new Error(`${label} has ${newPages.length} pages; its thumbnail page ${thumbPage} is gone`);
      }
      if (!result.changed) {
        // The PDF stays as it is. A missing thumbnail (deleted to take
        // another page, or never made) is drawn from its committed pages.
        if (!fs.existsSync(thumb)) {
          fs.copyFileSync(oldPages[thumbPage - 1], thumb);
          copied.push(`${sample.name}/${kind}.png: thumbnail made from page ${thumbPage} of the committed PDF`);
        } else {
          unchanged.push(`${label}: ${result.note}`);
        }
        continue;
      }
      fs.copyFileSync(pdf, committed);
      fs.copyFileSync(newPages[thumbPage - 1], thumb);
      copied.push(`${label}: ${result.note} (thumbnail: page ${thumbPage})`);
    }
  }
} finally {
  fs.rmSync(WORK, { recursive: true, force: true });
}

console.log(`${unchanged.length} unchanged, ${copied.length} copied into docs/samples/`);
for (const line of unchanged) console.log(`  ${line}`);
if (copied.length > 0) {
  console.log('\ncopied - look at these, and at the page that shows them:');
  for (const line of copied) console.log(`  ${line}`);
}
