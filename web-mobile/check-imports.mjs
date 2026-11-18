#!/usr/bin/env node
// Static cross-module import/export checker for the web-mobile js/ modules.
//
// Validates that every named import (and every named re-export) from a sibling
// module is actually exported by that module. Catches the ESM blind spot:
// esbuild transpile-only and vitest do NOT fail on a missing named export,
// only native browser ESM does. This script gives the same check
// deterministically in CI.
//
// What it checks, and what it prints apart:
//   1. Import errors: EVERY top-level js/*.jsx module (found by reading the
//      folder, listed or not) has each of its named imports and re-exports
//      resolved against the module it names.
//   2. List drift: CHECK_MODULES below is the declared set of modules, the
//      folder is the observed set, and any difference fails the run, naming
//      the module. A new module must be added to the list by hand, so it
//      cannot slip in unnoticed; a listed module that vanished is a bad
//      rename or move.
//   3. Script tags: index.html's <script type="module"> tags against the
//      imports (see checkScriptTagUrls), including the rule that no module
//      imports an entry whose tag is its compiled ".js" URL.
//
// Import and export statements are read only where one begins a line, from the
// source with its comments blanked out, so a header comment that quotes
// "import { X } from './y.jsx'" is never mistaken for an import.
//
// No npm dependencies, uses Node.js built-ins only.
//
// Usage:   node web-mobile/check-imports.mjs
// Exit 0   all imports resolved, the list matches the folder
// Exit 1   a named import is missing from its source module, the list and the
//          folder differ, or a script tag would load a module twice

import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { resolve, dirname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { stripComments } from './check-helpers.mjs';

const JS_DIR = resolve(dirname(fileURLToPath(import.meta.url)), 'js');

// The modules this checker verifies: every top-level js/*.jsx, kept as a
// hand-written list so that adding or removing a module is a visible edit.
// The first entries carry the reason each was put on the list; the block after
// them is the rest of the folder, sorted. A new module goes into that block,
// and the drift check tells you when it is missing.
//
// admin_setup.jsx and admin_competition_settings.jsx were added by bc-symm:
// both now import a shared set of LABEL_*/*_OPTIONS constants and coupling
// functions from competition_shape.jsx (and qualifier_preview.jsx), and a
// typo'd or renamed export in that shared module would otherwise pass both
// esbuild transpile-only and vitest silently (see the file header above) --
// exactly the drift class this checker exists to catch, and exactly the two
// surfaces the bc-symm operator ruling requires to stay in lockstep.
//
// admin_scoring_shared.jsx and bracket.jsx were added by bc-fsnp/bc-dtfn: the
// scoring editors' shared helpers now import struckIppons from result_slot.jsx
// and defaultWinMaru from bracket.jsx, and the editors import the tap_guard.jsx leaf.
// match_scoreboard.jsx likewise: enchoOn and defaultWinMaru moved into the
// result_slot.jsx leaf, and the scoreboard imports defaultWinMaru from there.
const CHECK_MODULES = [
  'admin_scoring_modal.jsx',
  'admin_scoring_individual.jsx',
  'admin_scoring_team.jsx',
  'admin_scoring_shared.jsx',
  'admin_scoring_autosave.jsx',
  // The one owner of how a match start is judged (both the court console and
  // the Scores tab import it).
  'start_match.jsx',
  'bracket.jsx',
  'match_scoreboard.jsx',
  'admin_setup.jsx',
  'admin_competition_settings.jsx',
  // bc-offl: the topbar's held-writes indicator imports its copy from
  // write_result.jsx, the shell's one ES import.
  'admin_shell.jsx',
  // bc-mrgc: the engi editor imports the change-group hook, and the History
  // disclosure reads its words from four owners (bracket.jsx, result_slot.jsx,
  // admin_scoring_shared.jsx, match_groups.jsx).
  'admin_scoring_engi.jsx',
  'match_history_view.jsx',
  // bc-lpfb: both lineup editors import the helpers they share from
  // lineup_resolver.jsx (the source wording moved there from the panel) and
  // pool_ids.jsx.
  'admin_lineup.jsx',
  'admin_schedule_lineup.jsx',
  // bc-lnul: both lineup editors import the form hook, the source line, the read
  // problem and the draft notice from lineup_draft.jsx, which imports the lineup
  // read, the position diff and the wording from lineup_resolver.jsx.
  'lineup_draft.jsx',
  // lineup_resolver.jsx words a lineup read nobody answered through
  // noAnswerSentence, the one owner of that sentence in write_result.jsx.
  'lineup_resolver.jsx',
  // bc-cfbd: the layers a tap opens keep the bounce of that tap off their
  // backdrop through the tap_guard.jsx leaf: the participant Edit dialog, the
  // public match card, the admin sign-in dialog, the shiaijo console's confirm
  // and ui.jsx's confirm and Modal each import useOpenedTapGuard.
  'admin_participants.jsx',
  'viewer_match.jsx',
  'app.jsx',
  // bc-evts: the outbox imports the reload rule (app_version.jsx), the tap
  // outcome codes (score_events.jsx) and its words (write_result.jsx); the
  // one new-build banner imports app_version.jsx.
  'api_client.jsx',
  'version_banner.jsx',
  'ui.jsx',
  'admin_shiaijo.jsx',
  // The result-mark placement leaf (sideMarks, placeMarks, winnerSideLR,
  // teamMatchMarks): bracket.jsx, admin_shiaijo_queue.jsx and the Scores tab import it
  // by name, so a renamed export would otherwise pass transpile-only.
  'side_marks.jsx',
  // The Scores tab imports from start_match.jsx, write_result.jsx, pool_ids.jsx
  // and side_marks.jsx; a typo'd or renamed export in any of them would fail
  // only in a native browser.
  'admin_schedule_score_editor.jsx',
  // bc-lbla: the shared competition-config controls import useStableId from
  // the stable_id.jsx leaf, which the glossary's <Term> imports too.
  'competition_fields.jsx',
  'glossary.jsx',
  // PR #467: the announcement form imports useStableId too; the bracket
  // helpers import BRONZE_MATCH_ID from write_result.jsx and bronzeFeederSemis
  // from the bronze_feeders.jsx leaf; the kachinuki advisory line imports the
  // squad-member label and the lineup resolver (and the draw test from
  // api_serializers.jsx).
  'admin_announcement.jsx',
  'admin_helpers.jsx',
  'kachinuki_advisory.jsx',
  // The rest of the folder, sorted.
  'admin.jsx',
  'admin_backup.jsx',
  'admin_branding.jsx',
  'admin_calendar.jsx',
  'admin_competition.jsx',
  'admin_competition_bracket.jsx',
  'admin_competition_overview.jsx',
  'admin_competition_swiss.jsx',
  'admin_pools.jsx',
  'admin_registration_desk.jsx',
  'admin_schedule.jsx',
  'admin_schedule_export.jsx',
  'admin_schedule_pacing.jsx',
  'admin_schedule_page.jsx',
  'admin_schedule_utils.jsx',
  'admin_scoring_team_bouts.jsx',
  'admin_scoring_team_kachinuki.jsx',
  'admin_shiaijo_calls.jsx',
  'admin_shiaijo_context.jsx',
  'admin_shiaijo_feed.jsx',
  'admin_shiaijo_moves.jsx',
  'admin_shiaijo_queue.jsx',
  'admin_shiaijo_start.jsx',
  'admin_sponsors.jsx',
  'api_client_clock.jsx',
  'api_client_competitions.jsx',
  'api_client_enqueue.jsx',
  'api_client_events.jsx',
  'api_client_flush.jsx',
  'api_client_ids.jsx',
  'api_client_matches.jsx',
  'api_client_members.jsx',
  'api_client_outbox.jsx',
  'api_client_overlays.jsx',
  'api_client_queue.jsx',
  'api_client_requests.jsx',
  'api_client_status.jsx',
  'api_client_store.jsx',
  'api_client_taps.jsx',
  'api_client_teams.jsx',
  'api_client_tournament.jsx',
  'api_serializers.jsx',
  'app_version.jsx',
  'barred_chip.jsx',
  'barred_match_notice.jsx',
  'base_sender_lease.jsx',
  'bronze_feeders.jsx',
  'calendar_move.jsx',
  'calendar_rules.jsx',
  'competition_put_body.jsx',
  'competition_shape.jsx',
  'competitor_identity.jsx',
  'competitor_search.jsx',
  'court_bridge.jsx',
  'court_move.jsx',
  'court_pace.jsx',
  'court_queue.jsx',
  'data.jsx',
  'data_integrity.jsx',
  'date_order.jsx',
  'dialog_focus.jsx',
  'display.jsx',
  'display_helpers.jsx',
  'display_lobby.jsx',
  'display_scoreboard.jsx',
  'document_versions.jsx',
  'draft_estimate.jsx',
  'draft_taps.jsx',
  'duration.jsx',
  'held_writes_panel.jsx',
  'icon.jsx',
  'ineligible_match.jsx',
  'landed_taps.jsx',
  'latest_ref.jsx',
  'lineup_rename.jsx',
  'lineup_save.jsx',
  'match_groups.jsx',
  'match_shows_score.jsx',
  'minted_members.jsx',
  'notification_keys.jsx',
  'numbered_name.jsx',
  'own_status.jsx',
  'patch.jsx',
  'pause_list.jsx',
  'pool_ids.jsx',
  'published_height.jsx',
  'qr.jsx',
  'qualifier_preview.jsx',
  'queued_writes_wait.jsx',
  'registration.jsx',
  'reset.jsx',
  'result_recency.jsx',
  'result_slot.jsx',
  'retime_warning.jsx',
  'router.jsx',
  'score_events.jsx',
  'server_clock.jsx',
  'side_cell.jsx',
  'sponsor_strip.jsx',
  'squad_member_label.jsx',
  'stable_id.jsx',
  'streaming_overlay.jsx',
  'tap_guard.jsx',
  'team_default_credit.jsx',
  'timer_pool.jsx',
  'toast_queue.jsx',
  'told_taps.jsx',
  'unsaved_entries.jsx',
  'viewer.jsx',
  'viewer_alerts.jsx',
  'viewer_awards.jsx',
  'viewer_competition.jsx',
  'viewer_home.jsx',
  'viewer_notifications.jsx',
  'viewer_schedule.jsx',
  'viewer_standings.jsx',
  'viewer_utils.jsx',
  'viewer_watchlist.jsx',
  'viewer_watchlist_core.jsx',
  'watchlist_link.jsx',
  'write_result.jsx',
];

// ---------------------------------------------------------------------------
// Export and import extraction
// ---------------------------------------------------------------------------
// Both read the comment-free source and take a statement only where it begins
// a line. Handles:
//   export { a, b, localC as exportedD }            (also `... } from './x.jsx'`)
//   export function Foo / export async function Foo
//   export const / export let / export var / export class
//   import { a, b as c } from './x.jsx'
// Does NOT handle `export default` or `export * from` (not used in these modules).

// 'local as exported' -> exported is what importers see; for an import the
// first name is what the source module must export.
function braceNames(body, takeExported) {
  const names = [];
  for (const item of body.split(',')) {
    const parts = item.trim().match(/^(\w+)(?:\s+as\s+(\w+))?$/);
    if (parts) names.push(takeExported ? (parts[2] ?? parts[1]) : parts[1]);
  }
  return names;
}

function extractExports(code) {
  const names = new Set();
  for (const m of code.matchAll(/^export\s*\{([^}]*)\}/gm)) {
    for (const name of braceNames(m[1], true)) names.add(name);
  }
  for (const m of code.matchAll(/^export\s+(?:async\s+)?(?:function|const|let|var|class)\s+(\w+)/gm)) {
    names.add(m[1]);
  }
  return names;
}

// Returns an array of { importedNames: string[], fromPath: string } for every
// `import { … } from './…'` and `export { … } from './…'` (relative paths only).
function extractSiblingImports(code) {
  const results = [];
  for (const m of code.matchAll(/^(?:import|export)\s*\{([^}]*)\}\s*from\s*['"](\.[^'"]+)['"]/gm)) {
    const names = braceNames(m[1], false);
    if (names.length > 0) results.push({ importedNames: names, fromPath: m[2] });
  }
  return results;
}

// ---------------------------------------------------------------------------
// Main check loop
// ---------------------------------------------------------------------------

const exportCache = new Map();

function getCachedExports(absPath) {
  if (!exportCache.has(absPath)) {
    exportCache.set(
      absPath,
      existsSync(absPath) ? extractExports(stripComments(readFileSync(absPath, 'utf8'))) : null,
    );
  }
  return exportCache.get(absPath);
}

const folderModules = readdirSync(JS_DIR).filter((f) => f.endsWith('.jsx')).sort();
const listed = new Set(CHECK_MODULES);

// Import errors: every module in the folder, listed or not.
const importErrors = [];
for (const modName of folderModules) {
  const code = stripComments(readFileSync(resolve(JS_DIR, modName), 'utf8'));
  for (const { importedNames, fromPath } of extractSiblingImports(code)) {
    const sibPath = resolve(JS_DIR, fromPath);
    const sibName = fromPath.replace(/^\.\//, '');
    // Containment guard: a `from` path that resolves outside js/ (e.g. a
    // committed `../../etc/...` traversal) must fail the check, not silently
    // read an arbitrary file. JS_DIR is computed from import.meta.url, so the
    // separator is platform-correct.
    if (sibPath !== JS_DIR && !sibPath.startsWith(JS_DIR + sep)) {
      importErrors.push(`${modName}: import path escapes js/ directory: ${fromPath}`);
      continue;
    }
    const exported = getCachedExports(sibPath);
    if (exported === null) {
      importErrors.push(`${modName}: imports from "${sibName}", which does not exist`);
      continue;
    }
    for (const name of importedNames) {
      if (!exported.has(name)) {
        importErrors.push(`${modName}: imports "${name}" from "${sibName}", not exported by "${sibName}"`);
      }
    }
  }
}

console.log(`Checking named imports of ${folderModules.length} modules...`);
if (importErrors.length === 0) {
  console.log(`  ✓ every named import and re-export resolves`);
} else {
  for (const e of importErrors) console.error(`  ✗ ${e}`);
}

// List drift: the declared set (CHECK_MODULES) against the observed set (the
// folder). Printed apart from the import errors so a task can tell its own
// broken imports from list entries it still has to add.
console.log('\nChecking CHECK_MODULES against the folder...');
const driftErrors = [];
for (const modName of folderModules) {
  if (!listed.has(modName)) {
    driftErrors.push(`${modName}: not on CHECK_MODULES, add it to the rest-of-the-folder block in web-mobile/check-imports.mjs`);
  }
}
for (const modName of CHECK_MODULES) {
  if (!existsSync(resolve(JS_DIR, modName))) {
    // A listed module that has vanished almost always means a bad rename/move:
    // failing the check is correct, not a soft skip, otherwise js/validate
    // would go green on a broken refactor.
    driftErrors.push(`${modName}: listed in CHECK_MODULES but file not found, bad rename/move?`);
  }
}
const listedTwice = CHECK_MODULES.filter((m, i) => CHECK_MODULES.indexOf(m) !== i);
for (const modName of listedTwice) driftErrors.push(`${modName}: listed twice in CHECK_MODULES`);
if (driftErrors.length === 0) {
  console.log(`  ✓ the list names exactly the ${folderModules.length} modules in the folder`);
} else {
  for (const e of driftErrors) console.error(`  ✗ ${e}`);
}

// ---------------------------------------------------------------------------
// Phase 2: script-tag / import URL agreement (the double-evaluation guard)
// ---------------------------------------------------------------------------
//
// A browser keys an ES module by its EXACT URL. A module that is both
// <script type="module" src="...">-tagged in index.html AND imported by another
// module therefore loads TWICE unless the two URLs match character for
// character — once per key. The file evaluates twice, doubling its download and
// parse, and any module-level state (a cache, a counter, a singleton) exists in
// two independent copies while `window.*` assignments race.
//
// It is easy to reintroduce because the two halves live apart and each looks
// right on its own: a "?v=N" cache-bust added to a tag, or a new import of a
// module that happens to be tagged. index.html already carried a note about
// this for the viewer modules; bracket, ui and qr had drifted into it anyway,
// which is why the rule is checked here rather than described again.
//
// The server rewrites "/dist/X.jsx" to the compiled "dist/X.js"
// (internal/mobileapp/server.go), so tagging the ".jsx" form is what makes a
// tag agree with an import of "./X.jsx".
function checkScriptTagUrls() {
  const htmlPath = resolve(dirname(fileURLToPath(import.meta.url)), 'index.html');
  const html = readFileSync(htmlPath, 'utf8');
  // Ignore commented-out tags: the file documents this very rule in comments
  // that contain example src="..." strings.
  // Only HTML comments are stripped. Running the JS comment-stripper over HTML
  // would delete from any "//" (e.g. inside an absolute src) to end of line,
  // which could swallow a live <script> tag and hide a real divergence.
  // A single pass can leave a "<!--" behind when markers are nested or
  // overlapping, so repeat the strip until it stops changing the string.
  let htmlLive = html;
  for (let prev = null; prev !== htmlLive; ) {
    prev = htmlLive;
    htmlLive = htmlLive.replace(/<!--[\s\S]*?-->/g, '');
  }

  // stem -> [full src]. A LIST, not a single value: a module tagged twice is
  // the worst case this check exists for (two tags = two evaluations even
  // without any import), and keeping only the last would hide it.
  const tagged = new Map();
  for (const m of htmlLive.matchAll(/<script[^>]+src="(\/dist\/([^"?]+)(?:\?[^"]*)?)"/g)) {
    const [, fullSrc, file] = m;
    const stem = file.replace(/\.[^.]+$/, '');
    if (!tagged.has(stem)) tagged.set(stem, []);
    tagged.get(stem).push(fullSrc);
  }

  // url -> Set(importers). The stem is derivable from the url, so one level.
  const importedBy = new Map();
  for (const file of readdirSync(JS_DIR)) {
    if (!/\.(jsx|js)$/.test(file)) continue;
    const src = stripComments(readFileSync(resolve(JS_DIR, file), 'utf8'));
    // `from './x'` covers static named/default/namespace imports and re-exports;
    // `import './x'` covers side-effect-only imports, which load the module just
    // as surely and so must agree with any tag too. `import('./x')` is the same
    // again for a DYNAMIC one: it was invisible here because the separator was
    // written as \s+, which a "(" does not satisfy, so a module lazily importing
    // a script-tagged sibling would evaluate twice with the build still green.
    // \b so the now-optional whitespace cannot let an identifier ENDING in
    // "import" (myImport('./x')) read as the keyword.
    for (const m of src.matchAll(/\b(?:from|import)\s*\(?\s*['"]\.\/([^'"]+)['"]/g)) {
      const url = `/dist/${m[1]}`;
      if (!importedBy.has(url)) importedBy.set(url, new Set());
      importedBy.get(url).add(file);
    }
  }
  const stemOf = (url) => url.replace(/^\/dist\//, '').replace(/\?.*$/, '').replace(/\.[^.]+$/, '');

  let errors = 0;
  // Rule 2 of the module split: no module imports an entry whose tag is its
  // compiled ".js" URL (api_client.js, admin_shiaijo.js, ...). Whatever
  // spelling the import uses, the browser would hold a second module URL for
  // the file and run it a second time (two outboxes, or two pages). What an
  // importer needs from an entry belongs in a module of its own. A module
  // tagged with its ".jsx" URL (a leaf that index.html loads first) is not an
  // entry: importing it by that same URL is one module, which the URL check
  // below confirms.
  const isEntryTag = (tagSrc) => /\.js(\?|$)/.test(tagSrc);
  for (const [stem, srcs] of tagged) {
    const entryTag = srcs.find(isEntryTag);
    if (!entryTag) continue;
    for (const [url, importers] of importedBy) {
      if (stemOf(url) !== stem) continue;
      for (const importer of [...importers].sort()) {
        errors++;
        console.error(
          `  ✗ ${importer}: imports "${url.replace(/^\/dist\//, '')}", an entry that index.html loads as ` +
          `<script src="${entryTag}">, so the browser holds two module URLs and runs it twice. ` +
          `Move what ${importer} needs out of the entry into a module of its own.`);
      }
    }
  }
  for (const [stem, srcs] of tagged) {
    if (srcs.length > 1) {
      errors++;
      console.error(`  ✗ ${stem}: tagged ${srcs.length} times (${srcs.join(', ')}) — each tag is its own module URL.`);
    }
  }
  for (const [url, importers] of importedBy) {
    const srcs = tagged.get(stemOf(url));
    if (!srcs) continue; // imported only: one URL, nothing to agree with
    for (const tagSrc of srcs) {
      if (url === tagSrc || isEntryTag(tagSrc)) continue; // an entry tag: rule 2 above
      errors++;
      console.error(
        `  ✗ ${stemOf(url)}: <script src="${tagSrc}"> but imported as "${url}" ` +
        `by ${[...importers].sort().join(', ')} — two module URLs, so it ` +
        `evaluates twice. Make the tag match the import specifier.`);
    }
  }
  if (errors === 0) console.log('  ✓ every script tag agrees with its import URL');
  return errors;
}

console.log('\nChecking script-tag / import URL agreement...');
const tagErrors = checkScriptTagUrls();

const failed = importErrors.length + driftErrors.length + tagErrors;
if (importErrors.length > 0) {
  console.error(`\n${importErrors.length} import mismatch(es), fix exports or imports before merging.`);
}
if (driftErrors.length > 0) {
  console.error(`\n${driftErrors.length} difference(s) between CHECK_MODULES and the folder, fix the list before merging.`);
}
if (tagErrors > 0) {
  console.error(`\n${tagErrors} module(s) would load twice, fix index.html or the import before merging.`);
}
if (failed > 0) process.exit(1);

console.log('\nAll sibling imports OK.');
