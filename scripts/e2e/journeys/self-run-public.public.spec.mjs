// The COMPETITOR's journey on a SELF-RUN tournament, on the PUBLIC project (a
// 390x844 phone, no sign-in): someone who has never run a court picks up a
// phone, opens the public page and scores their own match, for every kind of
// competition the walk covers (individual kendo in pools and knockout,
// naginata, engi, a team match, a representative bout, kachinuki), offline and
// against a second phone that finishes first, and registers for a competition.
//
// The test's own `page` is phone A. The tournament is created in Self-run mode
// and every competition is made, rostered and drawn from a SECOND context with
// the operator's iPad (OPERATOR_DEVICE), through the interface
// (fixtures/self-run.mjs seedDrawReady). Every result is entered on a phone,
// never on an admin page; a draw-ready competition starts on its first
// recorded result. Phone B, where a path needs two phones, is another
// PUBLIC_DEVICE context (openSecondPhone).
//
// One test per path of the spec (FR2 to FR11). Not serial: each test signs the
// organiser in (or creates the tournament when this file's server has none),
// seeds a competition of its own on a shiaijo of its own, and keeps its own
// screenshots, so one failing path hides nothing else. What is asserted is
// what a competitor must see and what was recorded; how a screen reads is
// recorded as AUDIT rows and judged from the screenshots (output/
// self-run-public/<path>/), and the rulings drawn from them are in the PR.
//
// FR1's last line, that a result entered here is recorded as self-reported, is
// read from the organiser's signed-in context (recordedSources): the admin
// pages show no such mark, so the organiser's browser reads the competition the
// app serves it. That is a read, never a seed.
//
// FR12 (the armed Finish holds its place) is measured in the browser at 390 px
// on every public editor: the button's box before the first tap and armed, and
// the armed label's line count (armedHolds).
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { flushAudits, journeyRecorder as recorder } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { armTwoTap, dwell, settled as paceSettled, tapOnSide } from '../fixtures/pace.mjs';
import { labelLines } from '../fixtures/league.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { pasteRoster } from '../fixtures/competition.mjs';
import { cardMarks, competitionTab, openViewer } from '../fixtures/public.mjs';
import {
  armedFinishButton, beforeFinish, finishButton, finishLanded, ipponButton, filledSlots,
} from '../fixtures/scoring.mjs';
import * as Team from '../fixtures/team.mjs';
import {
  DOORS, EDITOR, SELF_RUN_MODE, addRepresentativeBout, doorRow, comeBackOnline, contactOrganizerLine, engiEditorOpen, engiFlags,
  engiSubmitButton, goOffline, kachinukiBoutIppon, kachinukiCurrentBout, kachinukiDoneRows, kachinukiEnchoButton,
  kachinukiEndMatchButton, kachinukiRecordBout, kachinukiSheet, matchCard, matchHistory, offeredDecisions,
  openMatchCard, openSecondPhone, openUnsentList, publicEditor, publicIppon, removeRepresentativeBout,
  reportResultButton, resultReportedBadge, resultWaitingLine, seedDrawReady, settled, startPublicMatch, suneButton,
  teamBoutIppon, teamBoutRow, teamSheet, teamTotals, unsentCorner, unsentDiscard, unsentItems, unsentList,
} from '../fixtures/self-run.mjs';

// Enough shiaijo for each test here to have one of its own (one per path, A to
// I). The type counts only when this file's first test creates the tournament.
const CUP = { name: 'Self-run Cup', courts: 10, mode: SELF_RUN_MODE };
// The organiser password fixtures/setup.mjs signs in with, for the one read
// recordedSources makes from the organiser's page.
const ORGANISER_PASSWORD = 'e2e-operator';

// ---------------------------------------------------------------- helpers --

// The organiser's iPad on its own context, signed in (the first test to run
// creates the self-run tournament).
async function organiser(browser, baseURL) {
  const ctx = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
  const op = await ctx.newPage();
  await enterTournament(op, CUP);
  return { ctx, op };
}

// Which of `names` a piece of text names, in roster order (each name is
// distinct within a test, so a substring test is enough).
const namesIn = (text, names) => names.filter((n) => text.includes(n));

// The pairings the Schedule page lists among `names`, in its order: [{ shiro,
// aka }] as each row names them (a row is found by both names, so which is
// which only matters for the record; a side's marks are read by side).
async function schedulePairs(page, names) {
  await page.goto('/schedule');
  const rows = page.locator('button.tw-match');
  await expect(rows.first(), 'the Schedule lists the matches').toBeVisible();
  const out = [];
  for (const t of await rows.allInnerTexts()) {
    const both = namesIn(t.replace(/\s+/g, ' '), names);
    if (both.length === 2) out.push({ shiro: both[0], aka: both[1] });
  }
  return out;
}

// Findings this journey raised that are still OPEN (keyed S1, S2, ...). A step
// that meets one records what it saw (an annotation and an AUDIT row) instead
// of failing; the fix takes the id out of this set, and the assertion holds
// from then on. None is open now. Fixed, and asserted: S1 (a self-run
// draw-ready competition's first match is on the home page's Up next and its
// Overview), S3 (a strike tapped straight after naming a fighter counts), S4
// (an engi result saved from the public flag editor is recorded as
// self-reported), S5 (End match on the public kachinuki sheet finishes the
// encounter), S6 (offline, the list the corner opens shows the one match it
// counts as one row) and S7 (the refused-result message names the match's
// competition).
const OPEN = new Set([]);
async function unlessOpen(id, what, check) {
  if (!OPEN.has(id)) { await check(); return true; }
  try {
    await check();
    test.info().annotations.push({ type: 'finding-holds', description: `${id}: ${what} now holds; take ${id} out of OPEN` });
    return true;
  } catch {
    test.info().annotations.push({ type: 'finding-open', description: `${id}: ${what}` });
    return false;
  }
}

// What a public editor offers that only the organiser may record. The team
// and kachinuki sheets' per-bout Fusensho is NOT one of them: the server takes
// it from a participant on purpose (IsSelfRunReportableSubDecision, "per-bout
// forfeiture is a factual observation"), so it is recorded in the journey's
// report and left out of the check.
async function expectNoDecisions(page, record, what) {
  const found = await offeredDecisions(page);
  const rest = found.filter((f) => !/fusensho/i.test(f));
  record({ step: what, decisionsOffered: found, perBoutFusensho: found.length - rest.length });
  expect(rest, `${what}: no kiken, fusenpai or hantei`).toEqual([]);
}

// Open the match card through a door, then let the bounce window of the tap
// that opened it pass (the card ignores a tap inside it, useOpenedTapGuard),
// so the next tap on the card counts.
// The home page lists three matches under Up next across every shiaijo, and
// the tests here share one tournament, so the home door first picks the
// match's shiaijo in "Filter by shiaijo", as a competitor looking for their
// own court does (`which.court`).
async function openCard(page, door, which) {
  let card;
  if (door === 'home' && which.court) {
    const row = await doorRow(page, door, which);
    await page.getByRole('combobox', { name: 'Filter by shiaijo' }).first().selectOption({ label: `Shiaijo ${which.court}` });
    await expect(row, 'the home door lists the match').toBeVisible();
    await row.tap();
    card = matchCard(page);
    await expect(card).toBeVisible();
  } else {
    card = await openMatchCard(page, door, which);
  }
  await paceSettled(card);
  return card;
}

// Report result on an open card: returns the editor it opens.
async function report(page) {
  const btn = reportResultButton(page);
  await expect(btn, 'the card offers Report result').toBeVisible();
  await btn.tap();
  const ed = publicEditor(page);
  await expect(ed).toBeVisible();
  return ed;
}

// Close the editor with its own Close (nothing unsaved, so it closes at once)
// and then the card it came from.
async function closeEditorAndCard(page) {
  await publicEditor(page).getByRole('button', { name: 'Close', exact: true }).first().tap();
  await expect(publicEditor(page)).toHaveCount(0);
  const card = matchCard(page);
  if (await card.count()) {
    await paceSettled(card);
    await card.getByRole('button', { name: 'Close' }).first().tap();
    await expect(card).toHaveCount(0);
  }
}

async function closeCard(page) {
  const card = matchCard(page);
  await paceSettled(card);
  await card.getByRole('button', { name: 'Close' }).first().tap();
  await expect(card).toHaveCount(0);
}

// FR12: tap a two-tap commit once and measure it: its box before the tap and
// armed, and the armed label's lines. Returns { armed, ...measure }; the caller
// makes the second tap on `armed`.
async function armedHolds(button, armedLoc) {
  await expect(button).toBeVisible();
  const before = await button.boundingBox();
  const label = (await button.innerText()).trim();
  const armed = await armTwoTap(button, armedLoc);
  const after = await armed.boundingBox();
  return {
    armed,
    label,
    armedLabel: (await armed.innerText()).replace(/\s+/g, ' ').trim(),
    widthBefore: Math.round(before.width),
    widthArmed: Math.round(after.width),
    heightBefore: Math.round(before.height),
    heightArmed: Math.round(after.height),
    lines: await labelLines(armed),
  };
}

function expectHeld(m, what) {
  expect(m.lines, `${what}: the armed label "${m.armedLabel}" stays on one line at 390 px`).toBe(1);
  expect(Math.abs(m.widthArmed - m.widthBefore), `${what}: the button keeps its width between its taps (${m.widthBefore} -> ${m.widthArmed} px)`).toBeLessThanOrEqual(1);
  expect(Math.abs(m.heightArmed - m.heightBefore), `${what}: the button keeps its height between its taps`).toBeLessThanOrEqual(1);
}

// The result source of every finished match of a competition, as the server
// keeps it, read from the ORGANISER's signed-in page: [{ id, source }].
async function recordedSources(op, id) {
  return op.evaluate(async ({ id: compId, password }) => {
    const res = await fetch(`/api/viewer/competitions/${encodeURIComponent(compId)}`, {
      headers: { 'X-Tournament-Password': password },
    });
    const data = await res.json();
    const out = [];
    const seen = new Set();
    const walk = (o) => {
      if (Array.isArray(o)) { o.forEach(walk); return; }
      if (!o || typeof o !== 'object') return;
      if (typeof o.id === 'string' && o.status === 'completed' && !seen.has(o.id)) {
        seen.add(o.id);
        out.push({ id: o.id, source: o.resultSource || '' });
      }
      Object.values(o).forEach(walk);
    };
    walk(data);
    return out;
  }, { id, password: ORGANISER_PASSWORD });
}

// `open` names the finding (OPEN) under which a missing mark is recorded
// rather than failed.
async function expectSelfReported(op, id, count, { open } = {}) {
  await expect.poll(async () => (await recordedSources(op, id)).length, { message: 'the finished results reach the organiser' })
    .toBeGreaterThanOrEqual(count);
  const sources = await recordedSources(op, id);
  const check = async () => {
    for (const s of sources) expect(s.source, `${s.id} is recorded as self-reported`).toBe('self-reported');
  };
  if (open) await unlessOpen(open, 'the result is recorded as self-reported', check);
  else await check();
  return sources;
}

// The text of the card, squashed, for the record.
const cardText = async (page) => (await matchCard(page).innerText()).replace(/\s+/g, ' ').trim();

// The page's error toast (the outbox's latched one) and every toast shown.
const errorToast = (page) => page.locator('.toast.toast--error.is-visible');

// Individual rosters, distinct names and dojos per test.
const pairRoster = (a, b) => [[a, 'Kita Dojo'], [b, 'Minami Dojo']];

test.describe('a competitor scores on the self-run public page', () => {
  test.afterAll(() => flushAudits(test.info().file));

  // ------------------------------------------------------------------ FR2 --
  test('FR2: Report result is reached from all four doors and opens the right editor; a match with a side not yet known offers none', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const shot = journeyShots('self-run-public/doors');
    const { record } = recorder('doors', 'self-run-public/doors');
    const names = ['Akira Doors', 'Ben Doors', 'Chie Doors', 'Dan Doors', 'Eri Doors', 'Finn Doors'];
    const { ctx, op } = await organiser(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Doors Open', kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'] },
        roster: names.map((n, i) => [n, ['Kita', 'Minami', 'Higashi'][i % 3] + ' Dojo']),
      });
      const pairs = await schedulePairs(page, names);
      expect(pairs.length, 'two pools of three: six matches').toBe(6);
      const first = { id, tab: 'Pools', court: 'A', ...pairs[0] };
      const which = { id, tab: 'Pools', court: 'A', ...pairs[1] };

      await test.step('draw ready, nothing played: which doors list the first match', async () => {
        const seen = {};
        for (const door of DOORS) {
          const row = await doorRow(page, door, first);
          const check = async () => expect(row, `the ${door} door lists the first match`).toBeVisible({ timeout: 5_000 });
          if (door === 'home' || door === 'overview') seen[door] = await unlessOpen('S1', `the ${door} door lists a draw-ready competition's first match`, check);
          else { await check(); seen[door] = true; }
          if (door === 'overview') await shot(page, 'draw-ready-overview');
          if (door === 'home') await shot(page, 'draw-ready-home', { fullPage: true });
        }
        record({ step: 'draw ready', doorsListingTheFirstMatch: seen });
      });

      await test.step('the first match, played from the Schedule, starts the competition', async () => {
        await openCard(page, 'schedule', first);
        await report(page);
        await startPublicMatch(page);
        await awardOn(page, 'shiro', 'M');
        const ed = publicEditor(page);
        const m = await armedHolds(finishButton(page, EDITOR), armedFinishButton(page, EDITOR));
        record({ step: 'pool finish', fr12: { ...m, armed: undefined } });
        await shot(page, 'pool-finish-armed');
        const before = await beforeFinish(ed);
        await m.armed.tap();
        await finishLanded(ed, before);
        expectHeld(m, 'Finish (pool bout)');
      });

      for (const door of DOORS) {
        await test.step(`the ${door} door opens the card with Report result, and Report result opens the ippon editor`, async () => {
          await openCard(page, door, which);
          await expect(reportResultButton(page)).toBeVisible();
          record({ step: door, action: 'card opened', card: await cardText(page) });
          await shot(page, `door-${door}-card`);
          await report(page);
          await expect(publicIppon(page, 'shiro'), 'the ippon editor: an M button for Shiro').toBeVisible();
          await expect(publicIppon(page, 'aka'), 'and for Aka').toBeVisible();
          await expect(ipponButton(page, 'shiro', 'S', EDITOR), 'no Sune button outside naginata').toHaveCount(0);
          if (door === 'home') await shot(page, 'door-home-editor');
          await closeEditorAndCard(page);
        });
      }

      await test.step('a knockout match whose sides come from the pools offers no Report result', async () => {
        await openViewer(page, id);
        await competitionTab(page, 'Bracket').tap();
        const pending = page.locator('button.bc-match').first();
        await expect(pending, 'the bracket draws its matches').toBeVisible();
        const text = (await pending.innerText()).replace(/\s+/g, ' ').trim();
        expect(namesIn(text, names), 'a knockout match names nobody before the pools are played').toEqual([]);
        await pending.tap();
        await expect(matchCard(page)).toBeVisible();
        record({ step: 'bracket', action: 'card of a match with sides not known', card: await cardText(page) });
        await shot(page, 'bracket-sides-unknown');
        await expect(reportResultButton(page)).toHaveCount(0);
        await closeCard(page);
      });

      await test.step('FR1: the pool result is recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await ctx.close();
    }
  });

  // ------------------------------------------------------------------ FR3 --
  test('FR3: an individual knockout match scored from the public page: Start keeps the editor open, a second phone follows, a level bout points at the organizer, two-tap Finish, Result reported', async ({ page, browser, baseURL }) => {
    test.setTimeout(300_000);
    const shot = journeyShots('self-run-public/individual');
    const { record } = recorder('individual', 'self-run-public/individual');
    const which = { shiro: 'Goro Kendo', aka: 'Hana Kendo' };
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Kendo Final', kind: 'individual', format: 'knockout', courts: ['B'] },
        roster: pairRoster(which.shiro, which.aka),
      });
      Object.assign(which, { id });

      await test.step('phone B opens the card from the schedule and keeps it open', async () => {
        await openCard(b.page, 'schedule', which);
        expect(await cardMarks(matchCard(b.page))).toEqual({ shiro: '', aka: '' });
      });

      await test.step('phone A: Report result, then Start match keeps the editor open with the match running', async () => {
        await openCard(page, 'tab', { ...which, tab: 'Bracket' });
        await report(page);
        await shot(page, 'editor-before-start');
        await startPublicMatch(page);
        await expect(publicEditor(page), 'the editor stays open after Start').toBeVisible();
        await shot(page, 'editor-running');
        expect(await offeredDecisions(page), 'no kiken, fusenpai, fusensho or hantei on the public editor').toEqual([]);
        await expect(matchHistory(page), 'no match history on the public editor').toHaveCount(0);
      });

      await test.step('one ippon each: phone B shows both without a reload; the level knockout bout asks for the organizer', async () => {
        await awardOn(page, 'shiro', 'M');
        await awardOn(page, 'aka', 'M');
        await expect.poll(() => cardMarks(matchCard(b.page)), { message: 'phone B follows the taps' }).toEqual({ shiro: 'M', aka: 'M' });
        await expect(publicEditor(page)).toContainText('ask the organizer for a hantei');
        record({ step: 'level', editor: (await publicEditor(page).innerText()).replace(/\s+/g, ' ').slice(0, 600) });
        await shot(page, 'editor-level-asks-organizer');
        await shot(b.page, 'phone-b-follows');
      });

      await test.step('a second ippon for Shiro, then Finish in two taps: it holds its place, and the editor and card close', async () => {
        await awardOn(page, 'shiro', 'K');
        const ed = publicEditor(page);
        const m = await armedHolds(finishButton(page, EDITOR), armedFinishButton(page, EDITOR));
        record({ step: 'finish', fr12: { ...m, armed: undefined } });
        await shot(page, 'finish-armed');
        const before = await beforeFinish(ed);
        await m.armed.tap();
        await finishLanded(ed, before);
        await expect(matchCard(page), 'the card closes with the editor').toHaveCount(0);
        expectHeld(m, 'Finish');
      });

      await test.step('the card, opened again, shows the score and Result reported, on both phones', async () => {
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        await expect(contactOrganizerLine(page)).toBeVisible();
        await expect(reportResultButton(page)).toHaveCount(0);
        expect(await cardMarks(matchCard(page))).toEqual({ shiro: 'KM', aka: 'M' });
        await shot(page, 'card-result-reported');
        await expect(resultReportedBadge(b.page)).toBeVisible();
        expect(await cardMarks(matchCard(b.page))).toEqual({ shiro: 'KM', aka: 'M' });
      });

      await test.step('FR1: the organiser reads the result as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ------------------------------------------------------------------ FR4 --
  test('FR4: a naginata match from the public page: the Sune button for each side, a Sune tap shown on the card and on a second phone', async ({ page, browser, baseURL }) => {
    test.setTimeout(300_000);
    const shot = journeyShots('self-run-public/naginata');
    const { record } = recorder('naginata', 'self-run-public/naginata');
    const which = { shiro: 'Ivo Naginata', aka: 'Jun Naginata' };
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Naginata Final', kind: 'individual', format: 'knockout', naginata: true, courts: ['C'] },
        roster: pairRoster(which.shiro, which.aka),
      });
      Object.assign(which, { id });
      await openCard(b.page, 'schedule', which);

      await test.step('Report result from the Bracket tab: the editor offers Sune (S) for each side', async () => {
        await openCard(page, 'tab', { ...which, tab: 'Bracket' });
        await report(page);
        await expect(suneButton(page, 'shiro')).toBeVisible();
        await expect(suneButton(page, 'aka')).toBeVisible();
        await shot(page, 'editor-sune');
      });

      await test.step('a Sune for Shiro is on phone B\'s card at once, and on the card after Finish', async () => {
        await startPublicMatch(page);
        await awardOn(page, 'shiro', 'S');
        await expect.poll(() => cardMarks(matchCard(b.page)), { message: 'phone B shows the Sune' }).toEqual({ shiro: 'S', aka: '' });
        await shot(b.page, 'phone-b-sune');
        const ed = publicEditor(page);
        const m = await armedHolds(finishButton(page, EDITOR), armedFinishButton(page, EDITOR));
        record({ step: 'finish', fr12: { ...m, armed: undefined } });
        const before = await beforeFinish(ed);
        await m.armed.tap();
        await finishLanded(ed, before);
        await openCard(page, 'schedule', which);
        expect(await cardMarks(matchCard(page))).toEqual({ shiro: 'S', aka: '' });
        await expect(resultReportedBadge(page)).toBeVisible();
        await shot(page, 'card-sune-reported');
        expectHeld(m, 'Finish (naginata)');
      });

      await test.step('FR1: recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ------------------------------------------------------------------ FR5 --
  test('FR5: an engi match: Report result opens the flag editor from every door; flags saved with the two-tap Save result show on both phones', async ({ page, browser, baseURL }) => {
    test.setTimeout(420_000);
    const shot = journeyShots('self-run-public/engi');
    const { record } = recorder('engi', 'self-run-public/engi');
    // Four pairs, so the second match can be reached from every door once the
    // first has started the competition (S1: before that, home and Overview
    // list nothing).
    const names = ['Aya Engi', 'Kei Engi', 'Mio Engi', 'Ren Engi'];
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Engi Pairs', kind: 'individual', format: 'knockout', engi: true, courts: ['D'] },
        roster: [
          ['Aya Engi - Rin Engi', 'Kita Dojo'], ['Kei Engi - Sho Engi', 'Minami Dojo'],
          ['Mio Engi - Nao Engi', 'Higashi Dojo'], ['Ren Engi - Tai Engi', 'Nishi Dojo'],
        ],
      });
      const pairs = await schedulePairs(page, names);
      expect(pairs.length, 'four pairs: two first-round matches').toBeGreaterThanOrEqual(2);
      const which = { id, tab: 'Bracket', court: 'D', ...pairs[0] };
      const second = { id, tab: 'Bracket', court: 'D', ...pairs[1] };

      await test.step('flags 2 to 1 and Save result in two taps', async () => {
        await openCard(b.page, 'schedule', which);
        await openCard(page, 'schedule', which);
        await report(page);
        await expect(engiEditorOpen(page)).toBeVisible();
        await expect(publicEditor(page).locator('.ipt-btn'), 'no ippon buttons on the flag editor').toHaveCount(0);
        await engiFlags(page, { shiro: 2, aka: 1 });
        await shot(page, 'flags-entered');
        const ed = page.locator(EDITOR).first();
        const btn = engiSubmitButton(page);
        const m = await armedHolds(btn, btn.filter({ hasText: /^Tap again to/ }));
        record({ step: 'save', fr12: { ...m, armed: undefined } });
        await shot(page, 'save-armed');
        const before = await beforeFinish(ed);
        await m.armed.tap();
        await finishLanded(ed, before);
        expectHeld(m, 'Save result (engi)');
      });

      await test.step('the result shows on phone A\'s card and on phone B', async () => {
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        const a = await cardText(page);
        await expect.poll(() => cardText(b.page), { message: 'phone B shows the finished result' }).toContain('Result reported');
        record({ step: 'result', cardA: a, cardB: await cardText(b.page) });
        await shot(page, 'card-flags-reported');
        await shot(b.page, 'phone-b-flags');
        await closeCard(page);
      });

      for (const door of DOORS) {
        await test.step(`the ${door} door: Report result opens the flag editor, never the ippon editor`, async () => {
          await openCard(page, door, second);
          await report(page);
          await expect(engiEditorOpen(page)).toBeVisible();
          await expect(publicEditor(page).locator('.ipt-btn'), 'no ippon buttons on the flag editor').toHaveCount(0);
          if (door === 'home') await shot(page, 'flag-editor-from-home');
          await closeEditorAndCard(page);
        });
      }

      await test.step('FR1: recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ------------------------------------------------------------- FR6, FR7 --
  test('FR6 and FR7: a team encounter completed from the public sheet: fighters named from the member list and by typing, every bout scored, the result and the names on both phones', async ({ page, browser, baseURL }) => {
    test.setTimeout(420_000);
    const shot = journeyShots('self-run-public/team');
    const { record } = recorder('team', 'self-run-public/team');
    const which = { shiro: 'Hayabusa', aka: 'Inoshishi' };
    const typed = { shiro: ['Ito Taro', 'Kato Jiro', 'Sano Ken'], aka: ['Mori Ryo', 'Oda Sei'] };
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Team Final', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['E'] },
        roster: [[which.shiro, 'Kita Dojo'], [which.aka, 'Minami Dojo']],
      });
      Object.assign(which, { id });

      await test.step('Report result from the Bracket tab opens the team score sheet; Start match', async () => {
        await openCard(page, 'tab', { ...which, tab: 'Bracket' });
        await report(page);
        await expect(teamBoutRow(page, 1)).toBeVisible();
        await shot(page, 'sheet-before-start', { fullPage: true });
        await startPublicMatch(page);
        await expect(teamSheet(page)).toBeVisible();
        await expectNoDecisions(page, record, 'the team sheet');
      });

      await test.step('the fighters: names typed on every Shiro bout and two Aka bouts, one Aka fighter picked from the member list', async () => {
        for (let n = 1; n <= 3; n += 1) {
          await Team.typeIntoNameBox(Team.rowNameBox(teamBoutRow(page, n), 'shiro'), typed.shiro[n - 1]);
          await settled(page);
        }
        for (let n = 1; n <= 2; n += 1) {
          await Team.typeIntoNameBox(Team.rowNameBox(teamBoutRow(page, n), 'aka'), typed.aka[n - 1]);
          await settled(page);
        }
        // Bout 3 Aka: the list the box opens, its first member.
        const box = Team.rowNameBox(teamBoutRow(page, 3), 'aka');
        await Team.dwellAfterNameOption(page);
        await box.tap();
        const options = page.locator('.pmf__option');
        await expect(options.first(), 'the name box lists the team\'s members').toBeVisible();
        const offered = (await options.allInnerTexts()).map((t) => t.replace(/\s+/g, ' ').trim());
        record({ step: 'member list', offered });
        await shot(page, 'member-list-open');
        await Team.tapNameOption(options.first());
        await Team.closeNameList(page);
        await settled(page);
        await shot(page, 'fighters-named', { fullPage: true });
      });

      await test.step('every bout scored; the IV/PW band at each step', async () => {
        const totals = [];
        // The first strike, tapped while the name box last used still has
        // the focus, as a competitor taps it straight after naming a fighter.
        const r1 = teamBoutRow(page, 1);
        const focused = await page.evaluate(() => document.activeElement?.className || '');
        await Team.rowIppon(r1, 'shiro', 'M').tap();
        const counted = await unlessOpen('S3', 'the first strike after naming a fighter counts', async () => {
          await expect(Team.rowSlots(r1, 'shiro'), 'the strike shows on bout 1').toHaveCount(1, { timeout: 3_000 });
        });
        record({ step: 'first strike after naming', focusedBefore: focused, counted });
        await settled(page);
        totals.push(await teamTotals(page));
        await teamBoutIppon(page, 2, 'aka', 'K');
        totals.push(await teamTotals(page));
        await teamBoutIppon(page, 3, 'shiro', 'M');
        await teamBoutIppon(page, 3, 'shiro', 'D');
        totals.push(await teamTotals(page));
        record({ step: 'totals', totals, sheet: await Team.sheetState(teamSheet(page), 3) });
        await shot(page, 'sheet-scored', { fullPage: true });
        expect(totals[2].shiro).toMatch(/IV: 2/);
        expect(totals[2].aka).toMatch(/IV: 1/);
      });

      await test.step('phone B shows the names the competitors gave', async () => {
        await openCard(b.page, 'schedule', which);
        for (const name of [...typed.shiro, ...typed.aka]) {
          await expect(matchCard(b.page), `phone B's card names ${name}`).toContainText(name);
        }
        await shot(b.page, 'phone-b-names');
        await closeCard(b.page);
      });

      await test.step('phone B opens the sheet too, so it holds it while phone A finishes', async () => {
        await openCard(b.page, 'schedule', which);
        await report(b.page);
      });

      await test.step('Finish in two taps holds its place; the result shows on the card', async () => {
        const sheet = teamSheet(page);
        const m = await armedHolds(finishButton(page, sheet), armedFinishButton(page, sheet));
        record({ step: 'finish', fr12: { ...m, armed: undefined } });
        await shot(page, 'team-finish-armed', { fullPage: true });
        const before = await beforeFinish(sheet);
        await m.armed.tap();
        await finishLanded(sheet, before);
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        await shot(page, 'card-team-reported', { fullPage: true });
        expectHeld(m, 'Finish (team)');
      });

      await test.step('FR7: phone B, whose sheet is still open, changes a fighter after the finish: what it is told', async () => {
        const sheetB = teamSheet(b.page);
        await expect.poll(async () => (await sheetB.innerText()).replace(/\s+/g, ' '), { message: 'phone B\'s sheet learns the match finished' })
          .not.toMatch(/Tap again to finish/);
        const box = Team.rowNameBox(teamBoutRow(b.page, 1), 'aka');
        const editable = (await box.count()) > 0 && await box.isEditable().catch(() => false);
        let notice = '';
        if (editable) {
          await Team.typeIntoNameBox(box, 'Late Name');
          await expect.poll(async () => (await sheetB.locator('.alert, [role="alert"], .lineup-notice').allInnerTexts()).join(' | ')
            + (await b.page.locator('.toast.is-visible').allInnerTexts()).join(' | '), { message: 'phone B is told something' }).not.toBe('');
          notice = (await sheetB.locator('.alert, [role="alert"], .lineup-notice').allInnerTexts()).join(' | ')
            + ' || ' + (await b.page.locator('.toast.is-visible').allInnerTexts()).join(' | ');
          expect(notice, 'no organiser-only directions').not.toMatch(/Lineups page/);
        }
        record({ step: 'late lineup change', editable, notice, sheet: (await sheetB.innerText()).replace(/\s+/g, ' ').slice(0, 800) });
        await shot(b.page, 'phone-b-late-change', { fullPage: true });
      });

      await test.step('FR1: recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ------------------------------------------------------------------ FR8 --
  test('FR8: a level knockout team match offers Add representative bout; one added by mistake is removed, the next decides the match, and the finished sheet offers neither', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const shot = journeyShots('self-run-public/representative-bout');
    const { record } = recorder('representative-bout', 'self-run-public/representative-bout');
    const which = { shiro: 'Kitsune', aka: 'Tanuki' };
    const { ctx, op } = await organiser(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Rep Bout Final', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['F'] },
        roster: [[which.shiro, 'Kita Dojo'], [which.aka, 'Minami Dojo']],
      });
      Object.assign(which, { id });
      const sheet = () => teamSheet(page);

      await test.step('three drawn bouts leave the match level: Add representative bout, and no hantei', async () => {
        await openCard(page, 'tab', { ...which, tab: 'Bracket' });
        await report(page);
        await startPublicMatch(page);
        for (let n = 1; n <= 3; n += 1) await Team.ensureTie(sheet(), n);
        await expect(addRepresentativeBout(page)).toBeVisible();
        await expectNoDecisions(page, record, 'the level team sheet');
        await shot(page, 'level-add-offered', { fullPage: true });
      });

      await test.step('added by mistake, removed while unscored', async () => {
        await tapOnSide(page, 'rep', addRepresentativeBout(page));
        await expect(teamBoutRow(page, 'DH')).toBeVisible();
        await settled(page);
        await shot(page, 'rep-added', { fullPage: true });
        await expect(removeRepresentativeBout(page)).toBeVisible();
        await tapOnSide(page, 'rep', removeRepresentativeBout(page));
        await expect(teamBoutRow(page, 'DH')).toHaveCount(0);
        await settled(page);
      });

      await test.step('added again, scored like any bout, and the match finished on its winner', async () => {
        await tapOnSide(page, 'rep', addRepresentativeBout(page));
        await expect(teamBoutRow(page, 'DH')).toBeVisible();
        await settled(page);
        await teamBoutIppon(page, 'DH', 'aka', 'M');
        record({ step: 'rep scored', totals: await teamTotals(page) });
        await shot(page, 'rep-scored', { fullPage: true });
        const s = sheet();
        const m = await armedHolds(finishButton(page, s), armedFinishButton(page, s));
        record({ step: 'finish', fr12: { ...m, armed: undefined } });
        const before = await beforeFinish(s);
        await m.armed.tap();
        await finishLanded(s, before);
        expectHeld(m, 'Finish (team, representative bout)');
      });

      await test.step('the finished match: Result reported, so no sheet and neither Add nor Remove', async () => {
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        await expect(reportResultButton(page)).toHaveCount(0);
        await expect(matchCard(page).getByRole('button', { name: /representative bout/i })).toHaveCount(0);
        record({ step: 'finished', card: await cardText(page) });
        await shot(page, 'card-rep-reported', { fullPage: true });
      });

      await test.step('FR1: recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await ctx.close();
    }
  });

  test('FR8: a kachinuki match from the public sheet: the winner stays on, Encho, End match in two taps, and no representative bout', async ({ page, browser, baseURL }) => {
    test.setTimeout(360_000);
    const shot = journeyShots('self-run-public/kachinuki');
    const { record } = recorder('kachinuki', 'self-run-public/kachinuki');
    const which = { shiro: 'Washi', aka: 'Kuma' };
    const { ctx, op } = await organiser(browser, baseURL);
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Kachinuki Final', kind: 'team', format: 'knockout', teamSize: 2, teamMatchType: 'kachinuki', courts: ['G'] },
        roster: [[which.shiro, 'Kita Dojo'], [which.aka, 'Minami Dojo']],
      });
      Object.assign(which, { id });
      let ended = false;

      await test.step('Report result opens the kachinuki sheet; Start match', async () => {
        await openCard(page, 'tab', { ...which, tab: 'Bracket' });
        await report(page);
        await expect(kachinukiSheet(page)).toBeVisible();
        await startPublicMatch(page);
        await expectNoDecisions(page, record, 'the kachinuki sheet');
        await expect(addRepresentativeBout(page), 'no representative bout in kachinuki').toHaveCount(0);
        await shot(page, 'kachinuki-running', { fullPage: true });
      });

      await test.step('bout 1 to Shiro, recorded: the winner stays on for bout 2', async () => {
        await kachinukiBoutIppon(page, 'shiro', 'M');
        await kachinukiRecordBout(page);
        await expect(kachinukiDoneRows(page)).toHaveCount(1);
        await expect(kachinukiCurrentBout(page)).toBeVisible();
        record({ step: 'bout 2', sheet: (await kachinukiSheet(page).innerText()).replace(/\s+/g, ' ').slice(0, 800) });
        await shot(page, 'bout-2-winner-stays', { fullPage: true });
      });

      await test.step('bout 2 is level at one each, goes to Encho and Shiro wins it: recorded, Aka has nobody left', async () => {
        await kachinukiBoutIppon(page, 'shiro', 'M');
        await kachinukiBoutIppon(page, 'aka', 'M');
        await expect(kachinukiEnchoButton(page), 'a level bout offers Encho').toBeVisible();
        await shot(page, 'bout-2-level-encho-offered', { fullPage: true });
        await kachinukiEnchoButton(page).tap();
        // A level pair in overtime still reads X (X beats (E) in the middle
        // until the overtime strike): Undo encho, beside Encho, says it took.
        await expect(kachinukiSheet(page).getByRole('button', { name: /^Undo encho/i }), 'Encho took: Undo encho is offered').toBeEnabled();
        await settled(page);
        await kachinukiBoutIppon(page, 'shiro', 'K');
        await expect(kachinukiCurrentBout(page).locator('.team-sub-match__score'), 'the overtime win reads (E)').toContainText('(E)');
        record({ step: 'encho', middle: (await kachinukiCurrentBout(page).locator('.team-sub-match__score').innerText()).trim() });
        await kachinukiRecordBout(page);
        await shot(page, 'bout-2-recorded', { fullPage: true });
      });

      await test.step('End match in two taps holds its place and finishes the encounter', async () => {
        const btn = kachinukiEndMatchButton(page);
        const m = await armedHolds(btn, btn.filter({ hasText: /^Tap again/ }));
        record({ step: 'end match', fr12: { ...m, armed: undefined } });
        await shot(page, 'end-match-armed', { fullPage: true });
        expectHeld(m, 'End match (kachinuki)');
        const ed = publicEditor(page);
        const before = await beforeFinish(ed);
        await m.armed.tap();
        await finishLanded(ed, before);
        ended = true;
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        await shot(page, 'card-kachinuki-reported', { fullPage: true });
      });

      if (ended) {
        await test.step('FR1: recorded as self-reported', async () => {
          record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
        });
      }
    } finally {
      await ctx.close();
    }
  });

  // ------------------------------------------------------------------ FR9 --
  test('FR9: phone A finishes offline, phone B finishes first; back online A is told once, in plain words naming the match, and B\'s result stands', async ({ page, browser, baseURL }) => {
    test.setTimeout(300_000);
    const shot = journeyShots('self-run-public/refused');
    const { record } = recorder('refused', 'self-run-public/refused');
    const which = { shiro: 'Lea Refused', aka: 'Sora Refused' };
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    const ctxA = page.context();
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Refused Final', kind: 'individual', format: 'knockout', courts: ['H'] },
        roster: pairRoster(which.shiro, which.aka),
      });
      Object.assign(which, { id });

      await test.step('both phones open the editor; phone A starts the match online', async () => {
        await openCard(page, 'schedule', which);
        await report(page);
        await startPublicMatch(page);
        await openCard(b.page, 'schedule', which);
        await report(b.page);
      });

      await test.step('phone A, offline, scores Shiro and finishes: the editor closes at once and the corner counts it', async () => {
        await goOffline(ctxA);
        await awardOn(page, 'shiro', 'M', { offline: true });
        const ed = publicEditor(page);
        const armed = await armTwoTap(finishButton(page, EDITOR), armedFinishButton(page, EDITOR));
        await armed.tap();
        await expect(ed, 'the editor closes on an offline Finish').toHaveCount(0);
        await expect(unsentCorner(page)).toBeVisible();
        record({ step: 'offline finish', corner: (await unsentCorner(page).innerText()).trim() });
        await shot(page, 'offline-finish-corner');
      });

      await test.step('phone B scores Aka and finishes first', async () => {
        await awardOn(b.page, 'aka', 'M');
        const ed = publicEditor(b.page);
        const armed = await armTwoTap(finishButton(b.page, EDITOR), armedFinishButton(b.page, EDITOR));
        const before = await beforeFinish(ed);
        await armed.tap();
        await finishLanded(ed, before);
      });

      await test.step('phone A back online: one message, the server\'s sentence naming the match, kept until dismissed', async () => {
        await comeBackOnline(ctxA);
        const toast = errorToast(page);
        await expect(toast, 'phone A is told its result was refused').toBeVisible({ timeout: 30_000 });
        const text = (await toast.locator('.toast__msg').innerText()).replace(/\s+/g, ' ').trim();
        record({ step: 'refusal', toast: text });
        await shot(page, 'refused-toast');
        expect(text).toContain('This match result has already been reported. Contact the tournament organizer to correct it.');
        expect(text, 'the message names the match').toMatch(/Match|Final/);
        await unlessOpen('S7', 'the refused-result message names the match\'s competition', async () => {
          expect(text, 'the message names the competition').toContain('Refused Final');
        });
        expect(text, 'the tap notice is folded into the refused result').not.toMatch(/taps? (was|were) not applied/);
        await expect(toast.locator('.toast__dismiss'), 'the message can be dismissed').toBeVisible();
        // An error toast is latched: still there after its info-toast life.
        await page.waitForTimeout(6_000);
        await expect(toast, 'the message stays until it is read or dismissed').toBeVisible();
        await toast.locator('.toast__dismiss').tap();
        await expect(toast).toHaveCount(0);
        await page.waitForTimeout(2_000);
        await expect(errorToast(page), 'told once: no second message follows').toHaveCount(0);
      });

      await test.step('the count goes away and phone B\'s result stands on phone A', async () => {
        await expect(unsentCorner(page)).toHaveCount(0, { timeout: 20_000 });
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        expect(await cardMarks(matchCard(page))).toEqual({ shiro: '', aka: 'M' });
        await shot(page, 'phone-a-reads-b-result');
      });

      await test.step('FR1: B\'s result is recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await comeBackOnline(ctxA).catch(() => {});
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ----------------------------------------------------------------- FR10 --
  test('FR10: a result entered offline is kept on the phone: the corner count, the list with no Discard, the card says it is waiting, and it is sent when the connection returns', async ({ page, browser, baseURL }) => {
    test.setTimeout(300_000);
    const shot = journeyShots('self-run-public/offline');
    const { record } = recorder('offline', 'self-run-public/offline');
    const which = { shiro: 'Tomo Offline', aka: 'Yuki Offline' };
    const { ctx, op } = await organiser(browser, baseURL);
    const b = await openSecondPhone(browser, baseURL);
    const ctxA = page.context();
    try {
      const id = await seedDrawReady(op, {
        competition: { name: 'Offline Final', kind: 'individual', format: 'knockout', courts: ['I'] },
        roster: pairRoster(which.shiro, which.aka),
      });
      Object.assign(which, { id });
      await openCard(b.page, 'schedule', which);

      let row;
      await test.step('phone A opens the editor, then loses the connection: Start, a tap and Finish are taken at once', async () => {
        await openCard(page, 'schedule', which);
        row = page.locator('button.tw-match').filter({ hasText: which.shiro }).filter({ hasText: which.aka }).first();
        await report(page);
        await goOffline(ctxA);
        await startPublicMatch(page);
        await awardOn(page, 'shiro', 'M', { offline: true });
        const armed = await armTwoTap(finishButton(page, EDITOR), armedFinishButton(page, EDITOR));
        await armed.tap();
        await expect(publicEditor(page), 'the editor closes on Finish').toHaveCount(0);
        await expect(matchCard(page)).toHaveCount(0);
      });

      await test.step('the corner says "1 result not sent"; its list shows the match waiting, with no Discard', async () => {
        await expect(unsentCorner(page)).toBeVisible();
        // The first count the corner shows as the editor closes, for the
        // record: it can still be counting the score and the tap before the
        // Finish folds them into one result.
        const firstRead = (await unsentCorner(page).innerText()).replace(/\s+/g, ' ').trim();
        await expect(unsentCorner(page)).toHaveText(/1 result not sent/);
        const corner = (await unsentCorner(page).innerText()).replace(/\s+/g, ' ').trim();
        record({ step: 'corner', firstRead, corner });
        await shot(page, 'corner-count');
        const opened = Date.now();
        await openUnsentList(page);
        await expect(unsentItems(page).first()).toBeVisible();
        await expect(unsentDiscard(page), 'nothing to discard while it waits for the connection').toHaveCount(0);
        await unlessOpen('S6', 'the list shows the one result the corner counts as one row', async () => {
          await expect(unsentItems(page), 'one row for the one match waiting').toHaveCount(1, { timeout: 2_000 });
        });
        record({ step: 'list', list: (await unsentList(page).innerText()).replace(/\s+/g, ' ').trim() });
        await shot(page, 'unsent-list');
        await dwell(page, opened);
        await page.getByRole('dialog', { name: 'Held on this device' }).getByRole('button', { name: 'Close' }).first().tap();
        await expect(unsentList(page)).toHaveCount(0);
      });

      await test.step('the match card says the result is waiting and offers no Report result', async () => {
        await row.tap();
        await expect(matchCard(page)).toBeVisible();
        await expect(resultWaitingLine(page)).toBeVisible();
        await expect(reportResultButton(page)).toHaveCount(0);
        await expect(resultReportedBadge(page)).toHaveCount(0);
        record({ step: 'card while unsent', card: await cardText(page), line: (await resultWaitingLine(page).innerText()).trim() });
        await shot(page, 'card-unsent');
        await closeCard(page);
        expect(await cardMarks(matchCard(b.page)), 'phone B has nothing yet').toEqual({ shiro: '', aka: '' });
      });

      await test.step('back online: sent by itself, the count goes, the result shows on both phones', async () => {
        await comeBackOnline(ctxA);
        await expect(unsentCorner(page)).toHaveCount(0, { timeout: 20_000 });
        await settled(page);
        await expect.poll(() => cardMarks(matchCard(b.page)), { message: 'phone B shows the result' }).toEqual({ shiro: 'M', aka: '' });
        await expect(resultReportedBadge(b.page)).toBeVisible();
        await openCard(page, 'schedule', which);
        await expect(resultReportedBadge(page)).toBeVisible();
        expect(await cardMarks(matchCard(page))).toEqual({ shiro: 'M', aka: '' });
        await shot(page, 'card-sent');
      });

      await test.step('FR1: recorded as self-reported', async () => {
        record({ step: 'source', sources: await expectSelfReported(op, id, 1) });
      });
    } finally {
      await comeBackOnline(ctxA).catch(() => {});
      await b.ctx.close();
      await ctx.close();
    }
  });

  // ----------------------------------------------------------------- FR11 --
  test('FR11: Register shows only on an individual competition in setup; a team competition says who enters teams, on its card and its registration link; after the draw the link says registration is closed', async ({ page, browser, baseURL }) => {
    test.setTimeout(300_000);
    const shot = journeyShots('self-run-public/registration');
    const { record } = recorder('registration', 'self-run-public/registration');
    const { ctx, op } = await organiser(browser, baseURL);
    try {
      const open = await createCompetition(op, { name: 'Open Registration', kind: 'individual', format: 'knockout', courts: ['J'] });
      const teams = await createCompetition(op, { name: 'Team Registration', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['J'] });
      const drawn = await seedDrawReady(op, {
        competition: { name: 'Drawn Registration', kind: 'individual', format: 'knockout', courts: ['J'] },
        roster: pairRoster('Uma Drawn', 'Vic Drawn'),
      });
      // A team competition in setup with a roster, so the card is not empty.
      await pasteRoster(op, teams, [['Ookami', 'Kita Dojo'], ['Saru', 'Minami Dojo']]);
      const cardOf = (name) => page.locator('.comp-item').filter({ has: page.locator('.vlist-item__name', { hasText: new RegExp(`^${name}$`) }) });

      await test.step('the home: Register on the individual competition in setup only; the team card says the organiser enters teams', async () => {
        await page.goto('/');
        await expect(cardOf('Open Registration')).toBeVisible();
        await expect(cardOf('Open Registration').getByRole('button', { name: 'Register for this competition' })).toBeVisible();
        await expect(cardOf('Team Registration').getByRole('button', { name: 'Register for this competition' })).toHaveCount(0);
        await expect(cardOf('Team Registration')).toContainText('Teams are entered by the tournament organizer.');
        await expect(cardOf('Team Registration')).toContainText(/\bteams\b/);
        await expect(cardOf('Drawn Registration').getByRole('button', { name: 'Register for this competition' })).toHaveCount(0);
        record({
          step: 'home',
          open: (await cardOf('Open Registration').innerText()).replace(/\s+/g, ' '),
          team: (await cardOf('Team Registration').innerText()).replace(/\s+/g, ' '),
          drawn: (await cardOf('Drawn Registration').innerText()).replace(/\s+/g, ' '),
        });
        await shot(page, 'home-registration', { fullPage: true });
      });

      await test.step('the team competition\'s registration link says who adds teams', async () => {
        await page.goto(`/register/${encodeURIComponent(teams)}`);
        await expect(page.getByRole('heading', { name: 'Registration unavailable' })).toBeVisible();
        await expect(page.getByText('Teams are entered by the tournament organizer. Ask them to add your team.')).toBeVisible();
        await shot(page, 'register-team-link');
      });

      await test.step('after the draw the registration link says registration is closed', async () => {
        await page.goto(`/register/${encodeURIComponent(drawn)}`);
        await expect(page.getByText('Registration is closed for this competition.')).toBeVisible();
        await shot(page, 'register-closed');
      });

      await test.step('the individual competition in setup opens its form', async () => {
        await page.goto(`/register/${encodeURIComponent(open)}`);
        await expect(page.getByRole('heading', { name: 'Register' })).toBeVisible();
        await shot(page, 'register-open');
      });
    } finally {
      await ctx.close();
    }
  });
});

// One ippon on the public editor for `side`, and return once the board shows
// it; online, once the outbox has sent it too. Paced per side (fixtures/pace.mjs).
async function awardOn(page, side, waza, { offline = false } = {}) {
  const before = await filledSlots(page, side, EDITOR).count();
  await tapOnSide(page, `public ${side}`, publicIppon(page, side, waza));
  await expect(filledSlots(page, side, EDITOR)).toHaveCount(before + 1);
  if (!offline) await settled(page);
}
