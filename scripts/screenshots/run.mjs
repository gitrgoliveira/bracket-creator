#!/usr/bin/env node
// Docs capture runner.
//
//   make docs/media                               # every recipe
//   make docs/screenshots                         # the screenshots only
//   make docs/videos                              # the videos only
//   make docs/screenshots NAME=viewer-home
//   make docs/screenshots FAMILY=editors
//   make docs/screenshots SINCE=main
//
// Through make, not `node run.mjs` directly: the targets build the binary,
// install this directory's npm dependencies and download its browser, and
// none of those exists in a fresh worktree.
//
// Output lands in scripts/screenshots/out/, never straight into docs/, and the
// runner deliberately does not overwrite a committed file: what to publish is
// the operator's call.
//
// Each still is compared pixel by pixel against its committed twin, which works
// because docs/screenshots holds this harness's own output. A run therefore ends
// with the SHORT list of surfaces that actually changed - those are the ones to
// eyeball and copy across. An "unchanged" capture is indistinguishable from the
// committed file to a reader and needs neither.
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from 'playwright-core';
import { start, REPO } from './lib/server.mjs';
import { client } from './lib/api.mjs';
import { pngSize, pixelDiff } from './lib/png.mjs';
import { changedPaths, scope } from './lib/scope.mjs';
import { authAdmin } from './lib/ui.mjs';
import { recipes, families, recipeFiles } from './recipes/index.mjs';

const OUT = path.join(REPO, 'scripts', 'screenshots', 'out');
const COMMITTED = path.join(REPO, 'docs', 'screenshots');

// Every token must be KEY=value with a known KEY and a non-empty value. The
// filter-then-validate form this replaced dropped `SINCE-main` (no `=`) before
// it could be checked and read `NAME=` as "not given", so both ran all 33
// captures as though the command had been obeyed.
const KNOWN_ARGS = ['KIND', 'NAME', 'FAMILY', 'SINCE'];
const args = Object.fromEntries(process.argv.slice(2).map((token) => {
  const m = /^([A-Z]+)=(.+)$/s.exec(token);
  if (!m || !KNOWN_ARGS.includes(m[1])) {
    throw new Error(`bad argument "${token}" - use KEY=value with one of: ${KNOWN_ARGS.join(', ')}`);
  }
  return [m[1], m[2]];
}));

// Chromium's text rasterisation follows the HOST's font and colour
// configuration, so these pin it to one instead: greyscale antialiasing, no
// hinting, sRGB. Load-bearing rather than tidy-up - removing them changes every
// one of the 30 captures (measured), so the committed images ARE this
// configuration's output and a host rendering any other way reports all 30 as
// changed.
const DETERMINISTIC_RENDERING = [
  '--disable-lcd-text',
  '--disable-font-subpixel-positioning',
  '--font-render-hinting=none',
  '--force-color-profile=srgb',
];

const KINDS = { still: (r) => r.capture !== 'video', video: (r) => r.capture === 'video' };

function selected() {
  let list = recipes;
  if (args.KIND) {
    const pick = KINDS[args.KIND];
    if (!pick) {
      throw new Error(`unknown KIND "${args.KIND}" - use one of: ${Object.keys(KINDS).join(', ')}`);
    }
    list = list.filter(pick);
  }
  if (args.NAME) list = list.filter((r) => r.name === args.NAME);
  if (args.FAMILY) list = list.filter((r) => r.family === args.FAMILY);
  if (!list.length) {
    const known = recipes.map((r) => r.name).sort().join('\n  ');
    throw new Error(`no recipe matched. Known names:\n  ${known}`);
  }
  // SINCE narrows AFTER the explicit filters, so a misspelt NAME still fails
  // loudly above, while a real NAME outside the scope reports "nothing to do".
  if (args.SINCE) list = scoped(list, args.SINCE);
  return list;
}

// Keep only the recipes whose family some changed file reaches, and say why,
// path by path, BEFORE any seeding starts: a wrong scope discovered after a
// three-minute wait is what teaches an operator to stop using the flag.
function scoped(list, since) {
  const paths = changedPaths(since);
  const { selected, reasons } = scope(paths, families, recipeFiles);
  console.log(`scoping to files changed since ${since}, plus uncommitted changes: ${paths.size}`);
  const width = Math.max(0, ...reasons.map(([p]) => p.length));
  for (const [p, why] of reasons) console.log(`  ${p.padEnd(width)}  -> ${why}`);
  const kept = list.filter((r) => selected.has(r.family));
  const inScope = [...new Set(kept.map((r) => r.family))];
  const all = [...new Set(list.map((r) => r.family))];
  const skipped = all.filter((f) => !selected.has(f));
  console.log(`selected ${inScope.length} of ${all.length} families`
    + (inScope.length ? ` (${inScope.join(', ')}; ${kept.length} captures)` : '')
    + (skipped.length ? `; skipping ${skipped.join(', ')}` : '') + '\n');
  return kept;
}

// What changed about this capture since the committed original.
//
// The pixel compare is the headline, and it is only meaningful because
// docs/screenshots holds this harness's own output: an unchanged surface
// reproduces the committed file to within the tolerance png.mjs sets. So
// "unchanged" means there is nothing to eyeball and nothing to copy, which is
// what makes a 30-capture run reviewable. Dimensions are reported only when the
// pixels DID move, where they distinguish a layout change from a change in the
// pixels inside it.
function reportStill(recipe, file) {
  const committed = path.join(COMMITTED, `${recipe.name}.png`);
  const want = pngSize(committed);
  const got = pngSize(file);
  const dims = (d) => `${d.width}x${d.height}`;
  if (!got) return { changed: true, note: 'no readable PNG was written' };
  if (!want) return { changed: true, note: `NEW ${dims(got)} - no committed file to compare` };
  // Width is decisive; height only for fixed-size modes. A full-page capture's
  // height legitimately moves with content, so comparing it would cry wolf.
  // Decided from the headers BEFORE the pixel compare decodes both files in
  // full: a mismatch is a change whatever the pixels say.
  const heightMatters = recipe.capture !== 'fullPage';
  if (got.width !== want.width || (heightMatters && got.height !== want.height)) {
    return { changed: true, note: `CHANGED, size MISMATCH got ${dims(got)} want ${dims(want)}` };
  }
  const diff = pixelDiff(file, committed);
  if (diff && !diff.changed) {
    return { changed: false, note: `unchanged ${dims(got)}` };
  }
  // Height is not compared for a full-page capture, because content length
  // legitimately moves. A LARGE swing is still worth saying out loud: it is
  // what a page captured before its content rendered looks like, and it is
  // otherwise indistinguishable from a clean pass.
  if (!heightMatters) {
    const drift = Math.abs(got.height - want.height) / want.height;
    if (drift >= 0.2) {
      return {
        changed: true,
        note: `CHANGED ${dims(got)} (height differs from committed ${dims(want)} by ` +
          `${Math.round(drift * 100)}% - check the content by eye)`,
      };
    }
  }
  const scale = diff && diff.differing != null
    ? ` (${diff.differing} px differ, max ${diff.maxDelta} levels)` : '';
  return { changed: true, note: `CHANGED ${dims(got)}${scale}` };
}

// Playwright's own stabilisers, and THESE are what make a run reproduce.
// Measured by turning them off and running the suite twice: three of the thirty
// captures come back different between the two runs, by up to 31 grey levels
// over hundreds or thousands of pixels - far above the comparison's tolerance,
// so each would be reported as changed on every run. A CSS transition caught
// mid-flight and a blinking caret are the variable parts. "disabled" finishes
// each animation and renders its end state, which is the state the docs should
// show anyway.
//
// Note which fix does which job: these stabilise a run against ITSELF, while
// DETERMINISTIC_RENDERING above does not (unpinned, run-to-run drift is one
// capture at one grey level) and instead fixes WHICH rendering you get.
const STABLE = { animations: 'disabled', caret: 'hide' };

async function captureStill(page, recipe, file) {
  // Wait for images to settle. A recipe's waitFor names a piece of TEXT, which
  // is in the DOM before the logo beside it has arrived, so the tournament logo
  // was missing from a capture that otherwise looked complete. The brand logo
  // also has an onError fallback that swaps src and starts a SECOND load, so
  // "the request finished" is not the same as "the image is on screen".
  // Bounded, because a request that never settles must not hang the run.
  await page.evaluate(() => Promise.race([
    Promise.all(Array.from(document.images)
      .filter((img) => !img.complete)
      .map((img) => new Promise((done) => { img.onload = done; img.onerror = done; }))),
    new Promise((done) => setTimeout(done, 5000)),
  ]));
  // Drop the focus ring the driving left behind. Every capture here is reached
  // by clicking, so the last control tapped keeps focus and renders a ring no
  // operator would see at that moment - one shipped a dark ring around a
  // reorder arrow. No recipe has focus as its subject; the states that matter
  // (a ticked box, a typed value) survive blurring.
  await page.evaluate(() => document.activeElement && document.activeElement.blur());
  if (recipe.capture && recipe.capture.selector) {
    await page.locator(recipe.capture.selector).first().screenshot({ path: file, ...STABLE });
    return;
  }
  await page.screenshot({ path: file, fullPage: recipe.capture === 'fullPage', ...STABLE });
}

async function runRecipe(browser, recipe, ctx) {
  const { base, api, dataDir, fixture } = ctx;
  const isVideo = recipe.capture === 'video';
  const context = await browser.newContext({
    viewport: recipe.viewport,
    deviceScaleFactor: recipe.dpr || 1,
    ...(isVideo ? { recordVideo: { dir: OUT, size: recipe.viewport } } : {}),
  });
  // Before the page exists, so there is no navigation order to reason about:
  // an init script on the context applies to every page it opens.
  if (recipe.auth === 'admin') await authAdmin(context);
  const page = await context.newPage();
  // A capture is a real browser session, so the page can tell us it is broken.
  // Two severities, deliberately not treated alike. An UNCAUGHT exception means
  // the surface is broken and the capture would record that as the product
  // working, so it fails the capture. A console error does not: the SPA asks
  // for a team's lineup before one exists and the server answers 404, which the
  // client handles and which is normal on seven captures here. Failing on that
  // would make the gate cry wolf on every team surface, and a gate that always
  // fires is one the operator learns to skip.
  const pageErrors = [];
  const pageFaults = [];
  page.on('pageerror', (err) => pageErrors.push(err.message.split('\n')[0]));
  page.on('console', (msg) => {
    if (msg.type() === 'error') pageFaults.push(`console.error ${msg.text().split('\n')[0]}`);
  });
  // page.video() has to be taken while the page is alive: closing the context
  // is what finalises the file, and by then the page handle is gone.
  const video = isVideo ? page.video() : null;
  try {
    const args = { page, base, api, dataDir, fixture };
    if (recipe.route) {
      await page.goto(base + recipe.route, { waitUntil: 'domcontentloaded' });
    }
    if (recipe.waitFor) {
      await page.locator(recipe.waitFor).first().waitFor({ state: 'visible', timeout: 20000 });
    }
    if (recipe.drive) await recipe.drive(args);
    // Refuse to capture a fixture that is missing what the shot exists to show.
    if (recipe.assert) await recipe.assert(args);
    let file = null;
    if (!isVideo) {
      file = path.join(OUT, `${recipe.name}.png`);
      await captureStill(page, recipe, file);
    }
    // Outside the still branch on purpose: a video of a surface that threw
    // records the same broken product a screenshot would, and staging it as a
    // normal clip is how it reaches the docs.
    if (pageErrors.length) {
      throw new Error(`the page threw while being captured: ${pageErrors[0]}`);
    }
    if (!isVideo) return { ...reportStill(recipe, file), faults: pageFaults };
  } finally {
    await context.close();
  }
  const staged = path.join(OUT, `${recipe.name}.webm`);
  await video.saveAs(staged);
  // Deliberately not byte-compared. webm encoding is timing-dependent - frame
  // boundaries move with how fast the machine drove the UI - so a clip of an
  // unchanged surface does not reproduce its committed bytes, and a CHANGED
  // verdict on every run would train the operator to ignore the column.
  return { changed: null, note: `video -> ${path.relative(REPO, staged)}`, faults: pageFaults };
}

async function main() {
  fs.mkdirSync(OUT, { recursive: true });
  const list = selected();
  // A real outcome of SINCE (a docs-only change reaches no capture), not an
  // error: exit 0 without launching a browser.
  if (!list.length) {
    console.log('no capture depends on any changed file - nothing to do');
    return;
  }

  // One server PER FAMILY, not per server kind. Fixtures are not compatible with
  // each other: a self-run tournament and an officiated one differ in mode and
  // password, and POST /api/tournament is an upsert that keeps whatever the
  // first create on that process set. Sharing a data dir between families made a
  // full run fail on whichever family seeded second. A server boot measures
  // about 170ms against seeding that runs into tens of seconds, so the
  // isolation is close to free.
  const groups = new Map();
  for (const r of list) {
    if (!groups.has(r.family)) groups.set(r.family, []);
    groups.get(r.family).push(r);
  }

  const results = [];
  // Chromium refuses to run its sandbox as root; the recorder this harness
  // replaced passed --no-sandbox unconditionally, which is broader than needed.
  const asRoot = typeof process.getuid === 'function' && process.getuid() === 0;
  const browser = await chromium.launch({
    args: [...DETERMINISTIC_RENDERING, ...(asRoot ? ['--no-sandbox'] : [])],
  });
  try {
    for (const [familyName, group] of groups) {
      const family = families[familyName];
      if (!family) throw new Error(`${group[0].name}: unknown family ${familyName}`);

      // Which server a family seeds against is the family's property, so it
      // is declared once there rather than on each of its recipes.
      const server = await start(family.server);
      // null until the seed returns, so a seed that THROWS never reaches a
      // teardown written for the fixture it would have produced.
      let fixture = null;
      try {
        process.stdout.write(`seeding ${familyName}... `);
        fixture = (await family.seed({
          api: client(server.base), base: server.base, browser, dataDir: server.dataDir,
        })) || {};
        process.stdout.write('done\n');

        for (const recipe of group) {
          const ctx = {
            base: server.base,
            api: client(server.base),
            dataDir: server.dataDir,
            fixture,
          };
          try {
            const verdict = await runRecipe(browser, recipe, ctx);
            results.push([recipe.name, verdict]);
            console.log(`  ${recipe.name}: ${verdict.note}`);
          } catch (err) {
            // Remove any output from an EARLIER run. The console FAILED line
            // scrolls past in a 33-recipe run, and contributing.md tells the
            // operator to copy what is in out/ across to docs/ - so a stale
            // image left behind is one they would copy believing it fresh.
            for (const ext of ['png', 'webm']) {
              const stale = path.join(OUT, `${recipe.name}.${ext}`);
              if (fs.existsSync(stale)) fs.rmSync(stale);
            }
            const why = err.message.split('\n')[0];
            results.push([recipe.name, { failed: true, note: `FAILED ${why}` }]);
            console.log(`  ${recipe.name}: FAILED ${why}`);
          }
        }
      } finally {
        // A seed may hold something open across its captures (a signed-in
        // page whose editor the clip watches); this is where it lets go,
        // before the server it was talking to. Its own try/finally so a
        // teardown that throws neither masks the error that got us here nor
        // leaves the server running.
        try {
          if (fixture && family.teardown) await family.teardown(fixture);
        } finally {
          await server.stop();
        }
      }
    }
  } finally {
    await browser.close();
  }

  // Playwright names each recording after its page and only COPIES it on
  // saveAs, so the originals pile up in the staging dir run after run.
  for (const f of fs.readdirSync(OUT)) {
    if (f.startsWith('page@') && f.endsWith('.webm')) fs.rmSync(path.join(OUT, f));
  }

  const failed = results.filter(([, v]) => v.failed);
  const changed = results.filter(([, v]) => !v.failed && v.changed === true);
  const unchanged = results.filter(([, v]) => v.changed === false);
  const videos = results.filter(([, v]) => !v.failed && v.changed === null);

  console.log(`\n${results.length} captured, ${failed.length} failed`);
  console.log(`${unchanged.length} unchanged, ${changed.length} changed, ${videos.length} video (not compared)`);
  // The point of the content compare: name the short list, so the operator
  // reviews and copies those rather than re-checking all 30 by eye.
  if (changed.length) {
    console.log('\nchanged - eyeball these, then copy them over docs/screenshots/:');
    for (const [name, v] of changed) console.log(`  ${name}: ${v.note}`);
  }
  if (videos.length) {
    // Name each clip's REAL destination: two of the three are published from
    // docs/screenshots/, and a blanket "copy over docs/videos/" left the
    // published clip stale and an orphan beside it.
    console.log('\nvideos are always restaged - copy each over its committed file only if you drove a change:');
    for (const [name] of videos) {
      const twin = ['videos', 'screenshots'].map((d) => path.join('docs', d, `${name}.webm`))
        .find((p) => fs.existsSync(path.join(REPO, p)));
      console.log(`  ${name} -> ${twin || '(new clip: no committed file yet; pick a docs/ home and reference it)'}`);
    }
  }
  // A page that logged an error while being photographed is worth saying out
  // loud: the capture looks like the product working, and records it broken.
  const faulted = results.filter(([, v]) => v.faults && v.faults.length);
  if (faulted.length) {
    console.log('\npage errors during capture - the surface misbehaved while being photographed:');
    for (const [name, v] of faulted) {
      for (const fault of [...new Set(v.faults)]) console.log(`  ${name}: ${fault}`);
    }
  }
  console.log(`\nstaged in ${path.relative(REPO, OUT)}/`);
  if (failed.length) process.exitCode = 1;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
