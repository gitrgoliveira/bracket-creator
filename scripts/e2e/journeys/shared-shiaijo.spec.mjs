// A shiaijo shared by two competitions (bc-shcr), walked as the distracted,
// clumsy court operator on the iPad (fixtures/clumsy.mjs, V1-V5).
//
// Seeded through the interface (fixtures/shared-court.mjs): both competitions
// are created before any roster is pasted, then each gets its roster, draw and
// Start.
//   SI  "Shared Individual": pools of 3, two through, 12 entrants, Shiaijo A+B
//   ST  "Shared Team": team knockout, teams of 3 (Regular), 4 teams, Shiaijo A
// So Shiaijo A carries SI's pools A and B, SI's quarter-finals M1/M2, semi-final
// M5 and final M7, and all of ST. Shiaijo B carries SI's pools C and D, M3/M4
// and M6. ST is then re-timed with Auto-schedule so its matches sit between
// SI's pool bouts on the clock: the automatic advance is then tested against an
// ST match that is EARLIER in clock time than the next SI bout.
//
// Asserted, because the operator rulings make it the correct behaviour
// (2026-09-26/27): only one bout runs on a shiaijo; the console never switches
// competition by itself; the automatic advance (Finish + Start Next, the start
// after a withdrawal or no-show) stays in the scored match's competition, on
// the console and on the Scores page; picking another bout sends the running
// one back to the queue with its score (bc-sbq); Correct is refused while a
// bout is live (bc-crpn). Everything else is an audit row with a screenshot,
// judged by a person: the picker's default, the nudge lines, the done state,
// the alert, the court_busy copy, the public queue order, the slot times.
//
// What changed since bc-shcr's plan (2026-09-27) and is walked as main has it:
// the console HOLDS the competition it shows (PR #453), so P7 is asserted as
// "held"; the receiving court names the other competition's waiting matches in
// a quiet line (#453); Correct is refused while a bout is live and Start while
// a correction is open (#462, bc-crpn), so S4 opens its correction by sending
// the live bout back first; the withdrawal prompt picks no side (bc-dsid).
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament, login } from '../fixtures/setup.mjs';
import {
  openShiaijo, upNextCard, sides, startUpNext, completedRowFor, inlineEditor, editorIdentity,
} from '../fixtures/shiaijo.mjs';
import {
  EDITOR, INLINE_EDITOR, ipponButton, filledSlots, finishButton, armedFinishButton, armFinish, finishMatch, running, settled, unsentCount,
  held, notHeld,
} from '../fixtures/scoring.mjs';
import {
  doubleTap, hastyShiaijoConfirm, interrupt, loudestButton, retapWhileSaving, tapNeighbour,
} from '../fixtures/clumsy.mjs';
import { dwell, rendered, settled as paceSettled, untilStable } from '../fixtures/pace.mjs';
import { EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { tapSize } from '../fixtures/pointer.mjs';
import { openTvBoard, tvBoard } from '../fixtures/public.mjs';
import {
  ROSTER_12, winnerOf, editorSides, slotMarks, inlineRunning, ensureRunning,
  playRunningBout, recordDecision, correctCompleted, queuedPoolBouts,
} from '../fixtures/individual.mjs';
import * as T from '../fixtures/team.mjs';
import {
  seedCompetitions, readScoresTab, readScoresRows, bracketSlots, operatorDevice, publicDevice,
  competitionPicker, officiatingName, pickCompetition, nudges, nudgeLines, courtState, runningOnCourt,
  playTeamMatch, liveBout, correctAcross, chooseCourt,
} from '../fixtures/shared-court.mjs';

test.describe.configure({ mode: 'serial' });

const JOURNEY = 'shared-shiaijo';
const audit = auditLog(JOURNEY);
const SI = 'Shared Individual';
const ST = 'Shared Team';
const TEAMS = [['Hayabusa', 'Kita Dojo'], ['Inoshishi', 'Minami Dojo'], ['Kitsune', 'Higashi Dojo'], ['Tanuki', 'Nishi Dojo']];
// Auto-schedule start for ST: between SI's pool A and pool B bouts on A.
const ST_RESTART = '09:12';

let C; // the court A console: the clumsy operator's iPad, kept across tests
let H; // the head table (Scores pages, schedule): another device
let Bc; // the court B console: another device
let P; // a spectator's phone
let TV; // the venue TV board for Shiaijo A
let ids;
let shot;
const draw = {}; // what the draw produced, read back from the UI
const bouts = {}; // pairings met on the way, by name

const row = (journey, r) => audit.record({ journey, ...r });
const same = (a, b) => !!a && !!b && a.shiro === b.shiro && a.aka === b.aka;
const pairOf = (r) => ({ shiro: r.shiro, aka: r.aka });
// Return once this device's outbox is empty (all queued writes landed).
const settleOn = (page) => settled(page);
// The eyebrow of the bout an editor holds names its competition first.
const compOfEyebrow = (eyebrow) => {
  const e = (eyebrow || '').toUpperCase();
  if (e.startsWith(SI.toUpperCase())) return SI;
  if (e.startsWith(ST.toUpperCase())) return ST;
  return `unknown: ${eyebrow}`;
};
// A court's column on the public schedule (/schedule) and on the Tournament
// schedule page (/admin/schedule): both title it "SHIAIJO <court>".
const scheduleColumn = (page, court) => page.locator('.tw-court')
  .filter({ has: page.locator('.tw-court__title', { hasText: new RegExp(`^SHIAIJO ${court}$`) }) });
// The clock times a text shows (HH:MM) other than `planned`.
const timesOtherThan = (text, planned) => (text.match(/\b\d{1,2}:\d{2}\b/g) || []).filter((t) => t !== planned);
// The labels the editors' autosave pill takes (admin_scoring_autosave.jsx).
const SYNC_PILL = /^(Synced|Syncing…|Offline|Not saving)$/;

// The bout running in the console's editor (individual or team), with its
// competition and court.
async function liveOnConsole() {
  const l = await liveBout(C);
  return l ? { comp: compOfEyebrow(l.eyebrow), ...l } : null;
}

// Finish the running SI bout (starting Up next when the court is idle) for the
// deterministic winner, adding a men only when the winner is not already ahead
// (a reopened bout keeps the marks it had). Finish (+ Start Next) through the
// two-tap guard. Returns the pairing.
async function finishSiBout(page) {
  const pair = await ensureRunning(page);
  if (!pair) throw new Error('finishSiBout: nothing running or startable');
  const w = winnerOf(pair);
  const o = w === 'shiro' ? 'aka' : 'shiro';
  if ((await slotMarks(page, w)).length <= (await slotMarks(page, o)).length) {
    await ipponButton(page, w, 'M').tap();
    await settleOn(page);
  }
  await finishMatch(page);
  await expect(completedRowFor(page, pair)).toBeVisible();
  await expect.poll(async () => !same(await liveBout(page), pair), { timeout: 10_000 }).toBe(true);
  return pair;
}

// Wait until the console's editor holds a running bout other than `prev`.
async function nextLive(prev) {
  let live = null;
  await expect.poll(async () => {
    live = await liveOnConsole();
    return !!live && !same(live, prev);
  }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
  return live;
}

// Move a court on by one match: a barred match in its queue (a competitor
// this journey withdrew or recorded as a no-show earlier, who still qualified)
// is closed from its own notice with the fusensho it offers, the way the
// operator does; otherwise the running or Up next bout is played. Returns what
// was done.
async function advanceCourt(page) {
  const fusensho = page.locator('.shiaijo__queue').getByTestId('barred-match-record-fusensho').first();
  if (!(await inlineRunning(page)) && await fusensho.isVisible().catch(() => false)) {
    const label = (await fusensho.innerText()).trim();
    await fusensho.tap();
    await expect(fusensho).toBeHidden({ timeout: 10_000 });
    return { resolved: label };
  }
  return { played: await playRunningBout(page) };
}

test.describe('shared-shiaijo', () => {
  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    test.setTimeout(300_000);
    const server = await serverForFile(testInfo.file);
    shot = journeyShots(JOURNEY);
    C = await operatorDevice(browser, server.base);
    await enterTournament(C, { name: 'Shared Court Cup', courts: 2 });
    const seeded = await seedCompetitions(C, [
      { key: 'SI', opts: { name: SI, kind: 'individual', format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A', 'B'], numberPrefix: 'SI' }, roster: ROSTER_12 },
      { key: 'ST', opts: { name: ST, kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['A'], numberPrefix: 'ST' }, roster: TEAMS },
    ]);
    ids = seeded.ids;
    draw.defaults = seeded.defaults;
    draw.si = await readScoresTab(C, ids.SI);
    draw.st = await readScoresTab(C, ids.ST);
    draw.siKnockout = await bracketSlots(C, ids.SI);
    draw.stKnockout = await bracketSlots(C, ids.ST);
    H = await operatorDevice(browser, server.base);
    await login(H);
    Bc = await operatorDevice(browser, server.base);
    await login(Bc);
    P = await publicDevice(browser, server.base);
    const tvCtx = await browser.newContext({ viewport: { width: 1280, height: 720 }, baseURL: server.base });
    TV = await tvCtx.newPage();
  });

  test.afterAll(async () => {
    audit.flush();
    for (const p of [C, H, Bc, P, TV]) if (p) await p.context().close();
  });

  // bc-seqc (operator ruling 2026-10-12, bc-shcr D3: "By default, competitions
  // should be one after the other and the start-time will need to be adjusted
  // depending on how the day is going"). ST was created after SI and shares
  // Shiaijo A with it, so by default every ST match on A comes after SI's last
  // match there, and ends. First in the file because S1 re-times ST with
  // Auto-schedule, after which the defaults can no longer be read; it only reads
  // the Scores and Bracket tabs and the wizard's form (nothing is created), so
  // S1 starts from the same state. The wizard still offers its start time to the
  // operator, who can change it as the day goes. The hint's new wording is the
  // fix's to write: it is recorded here, and only its presence is asserted.
  test.fixme('bc-seqc: a competition sharing a shiaijo with an earlier one is scheduled after it by default', async () => {
    const clock = (t) => /^\d\d:\d\d$/.test(t);
    const siOnA = [
      ...(await readScoresTab(H, ids.SI)).filter((m) => m.court === 'A').map((m) => m.time),
      ...(await bracketSlots(H, ids.SI)).filter((m) => m.court === 'A').map((m) => m.time),
    ].filter(clock).sort();
    const stOnA = (await bracketSlots(H, ids.ST)).filter((m) => m.court === 'A').map((m) => m.time).filter(clock).sort();
    row('bc-seqc', { step: 'S8 slots', action: 'default slots on Shiaijo A', variant: 'fixme', siOnA, stOnA,
      hint: draw.defaults.ST.sharedCourtHint, startTimes: { si: draw.defaults.SI.startTime, st: draw.defaults.ST.startTime } });
    expect(siOnA.length).toBeGreaterThan(0);
    expect(stOnA.length).toBeGreaterThan(0);
    // After SI's last match there ENDS, not merely starts: its slot is as long as
    // the gap SI's own slots keep (an SI match of a single slot has no gap to read).
    const minutes = (t) => Number(t.slice(0, 2)) * 60 + Number(t.slice(3));
    const siStarts = [...new Set(siOnA.map(minutes))];
    const slot = Math.min(...siStarts.slice(1).map((m, i) => m - siStarts[i]).filter((g) => g > 0), Infinity);
    const siEnds = siStarts[siStarts.length - 1] + (Number.isFinite(slot) ? slot : 1);
    expect(minutes(stOnA[0]), `${ST}'s first match on A (${stOnA[0]}) comes after ${SI}'s last there ends (${siEnds} min)`).toBeGreaterThanOrEqual(siEnds);
    // The wizard still tells the operator something about sharing a shiaijo, and
    // still lets them set the start time themselves.
    expect(draw.defaults.ST.sharedCourtHint, 'the wizard hint about sharing a shiaijo').toBeTruthy();
    // H is shared with the tests after this one: the wizard it opens (a typed
    // start time in it) is left again whatever the checks find.
    try {
      await H.goto('/admin/create-competition');
      const startInput = H.locator('#create-start-time');
      await expect(startInput).toBeEditable();
      await startInput.fill('13:45');
      await expect(startInput).toHaveValue('13:45');
    } finally {
      await H.goto('/admin');
    }
  });

  // S1 the picker, S8 the slot times (P6), S7 the public queue before play.
  test('S1 S8 S7: the shared court before the first bout', async () => {
    test.setTimeout(300_000);
    const r = (x) => row('S1-S8-S7', x);

    // S8 / P6: the defaults both competitions got, created before any roster.
    const onA = [
      ...draw.si.filter((m) => m.court === 'A').map((m) => ({ comp: SI, match: m.label, time: m.time })),
      ...draw.siKnockout.filter((m) => m.court === 'A').map((m) => ({ comp: SI, match: m.match, time: m.time })),
      ...draw.stKnockout.filter((m) => m.court === 'A').map((m) => ({ comp: ST, match: m.match, time: m.time })),
    ].sort((a, b) => a.time.localeCompare(b.time));
    const clash = onA.filter((x) => onA.some((y) => y.comp !== x.comp && y.time === x.time));
    r({ step: 'S8 slots', action: 'wizard default Start times (both created before any roster)', variant: 'P6 record',
      si: draw.defaults.SI, st: draw.defaults.ST });
    r({ step: 'S8 slots', action: 'every slot on Shiaijo A after Start', variant: 'P6 record (D3)', slotsOnA: onA, sameTimeOnA: clash,
      siKnockout: draw.siKnockout, stKnockout: draw.stKnockout });
    await H.goto(`/admin/competition/${ids.SI}/scores`);
    await shot(H, 's8-si-scores-tab', { fullPage: true });
    await H.goto(`/admin/competition/${ids.ST}/scores`);
    await shot(H, 's8-st-scores-tab', { fullPage: true });
    await H.goto('/admin/schedule');
    await expect(H.getByRole('heading', { level: 1, name: 'Tournament schedule' })).toBeVisible();
    await shot(H, 's8-schedule-page-defaults', { fullPage: true });

    // S1: the court console on A offers both competitions.
    await openShiaijo(C, 'A');
    await expect(competitionPicker(C)).toBeVisible();
    const s1 = await courtState(C);
    r({ step: 'S1 picker', action: 'open /admin/shiaijo/A', variant: 'correct path', officiating: s1.officiating, options: s1.pickerOptions,
      why: 'no bout running: the default is the first competition with a scheduled match on the court (effectiveCompId)',
      upNext: s1.upNext, nudges: s1.nudges, shot: await shot(C, 's1-court-A-picker') });
    expect([...s1.pickerOptions].sort()).toEqual([SI, ST]);
    // V1: the thumb meant the picker and landed beside it.
    let nb = { hit: null, note: [] };
    for (const dir of ['left', 'down']) {
      try {
        nb = { ...(await tapNeighbour(competitionPicker(C), dir)), direction: dir };
        break;
      } catch (e) {
        nb.note.push(String(e.message).slice(0, 120));
      }
    }
    await rendered(C);
    r({ step: 'S1 picker', action: 'competition picker', variant: 'V1 tapNeighbour (left, else down)', direction: nb.direction, hit: nb.hit, gapPx: nb.gapPx, note: nb.note,
      url: new URL(C.url()).pathname, officiating: await officiatingName(C).catch(() => null), shot: await shot(C, 's1-picker-neighbour-tapped') });
    r({ step: 'S1 picker', action: 'competition picker', variant: 'size', ...(await tapSize(competitionPicker(C))) });
    if (new URL(C.url()).pathname !== '/admin/shiaijo/A') await openShiaijo(C, 'A');

    // Court B hosts SI alone: a plain heading, no picker.
    await openShiaijo(Bc, 'B');
    await expect.poll(() => officiatingName(Bc)).toBe(SI);
    await expect(competitionPicker(Bc)).toHaveCount(0);
    r({ step: 'S1 picker', action: 'open /admin/shiaijo/B (SI alone)', variant: 'correct path', officiating: await officiatingName(Bc), picker: false });

    // S8: Auto-schedule competition re-times ST alone, from 09:12.
    await H.goto('/admin/schedule');
    await H.locator('#auto-schedule-competition').selectOption({ label: ST });
    await H.locator('#auto-schedule-start').fill(ST_RESTART);
    const spacing = await H.locator('#auto-match-duration').inputValue().catch(() => null);
    await H.getByRole('button', { name: 'Auto-schedule competition' }).tap();
    await expect(H.getByRole('button', { name: 'Auto-schedule competition' })).toBeEnabled({ timeout: EXPECT_TIMEOUT_MS });
    // The button is enabled again before it has visibly been busy, so the saved
    // times are what says the re-timing is done: ST's earliest slot is the restart.
    // One navigation; the poll reads the tab as the app follows the saved times.
    await H.goto(`/admin/competition/${encodeURIComponent(ids.ST)}/scores`);
    await expect.poll(async () => (await readScoresRows(H)).map((m) => m.time).filter(Boolean).sort()[0]).toBe(ST_RESTART);
    const siAfter = await readScoresTab(H, ids.SI);
    draw.st = await readScoresTab(H, ids.ST);
    draw.stKnockout = await bracketSlots(H, ids.ST);
    r({ step: 'S8 slots', action: `Auto-schedule competition: ${ST} from ${ST_RESTART}`, variant: 'P6 record (D3)', spacing,
      stAfter: draw.stKnockout, siUnchanged: JSON.stringify(siAfter.map((m) => m.time)) === JSON.stringify(draw.si.map((m) => m.time)) });
    await H.goto('/admin/schedule');
    await shot(H, 's8-schedule-page-after-auto-schedule', { fullPage: true });

    // S7 / P8 / D2: the public surfaces for A before play.
    await openTvBoard(TV, 'A');
    await P.goto('/schedule');
    // Both screens fill from the server after they load: the text they settle on.
    await untilStable(async () => [await tvBoard(TV).innerText(), await P.locator('body').innerText()]);
    r({ step: 'S7 public', action: 'TV board A and the public schedule before play', variant: 'P8 record (D2)',
      tv: (await tvBoard(TV).innerText()).replace(/\s+/g, ' ').slice(0, 500),
      schedule: (await P.locator('body').innerText()).replace(/\s+/g, ' ').slice(0, 900),
      tvShot: await shot(TV, 's7-tv-A-before-play'), scheduleShot: await shot(P, 's7-public-schedule-before-play', { fullPage: true }) });
  });

  // bc-pubq (operator ruling 2026-10-12, bc-shcr D2: "The view should follow
  // the queue the scoring operator has"). Before play, with ST re-timed between
  // SI's pool bouts, the TV board for A names the match the court A console
  // will start next and nothing of the competition the console is not
  // running, and the public schedule shows ONE "Next up" on Shiaijo A, on that
  // match. Relies on the state S1 leaves (nothing played, ST re-timed). It
  // only navigates C, TV and P, and leaves P on /schedule and TV on A's board,
  // as S2 expects. Today the schedule's second "Next up" (ST's semi-final) is
  // what fails here; the TV naming ST's semi-final as NEXT was seen while SI
  // ran (S7 inside S2), so in this state its check guards a regression.
  test.fixme('bc-pubq: the TV board and the public schedule follow the court console\'s queue on a shared shiaijo', async () => {
    await openShiaijo(C, 'A');
    const s = await courtState(C);
    expect(s.upNext, 'the court A console has an Up next').not.toBeNull();
    const [shiro, aka] = s.upNext.names;
    const otherNames = s.officiating === SI ? TEAMS.map((t) => t[0]) : ROSTER_12.map((p) => p[0]);
    await openTvBoard(TV, 'A');
    // The board's next-match slots (display_scoreboard.jsx), each on its own. The
    // promoted slot is the match the board puts first before play (its NOW row on an
    // individual board, the headline on a team board) and must be the console's Up
    // next; the NEXT line, drawn when a queued match is not already on the board,
    // must not be the other competition's. The UP NEXT strip lists the next POOL and
    // is not the next match, so it is not read here.
    const promoted = s.officiating === SI ? TV.getByTestId('tvd-indiv-row-now').first() : tvBoard(TV);
    await expect(promoted, 'the TV board promotes a match').toBeVisible();
    await expect(promoted).toContainText(shiro);
    await expect(promoted).toContainText(aka);
    const nextLine = TV.locator('div:has(> span:text-is("NEXT"))');
    // The NEXT line is drawn only when a queued match is not already on the
    // board, so its absence is allowed; the board has to have stopped drawing
    // before it is taken to be absent.
    if (await untilStable(() => nextLine.count())) for (const name of otherNames) await expect(nextLine).not.toContainText(name);
    // And the other competition appears nowhere else on the board either.
    for (const name of otherNames) await expect(tvBoard(TV)).not.toContainText(name);
    await P.goto('/schedule');
    const nextUp = scheduleColumn(P, 'A').locator('.tw-match')
      .filter({ has: P.locator('.tw-match__queue', { hasText: /^Next up$/ }) });
    await expect(nextUp).toHaveCount(1);
    await expect(nextUp).toContainText(shiro);
    await expect(nextUp).toContainText(aka);
  });

  // S2 / S2b the automatic advance; S4a Correct while SI's own bout runs; S9
  // the Scores page; S3 switching competition mid-bout; S7 again (D2).
  test('S2 S4a S2b S9 S3: running SI on the shared court, then switching to ST', async () => {
    test.setTimeout(600_000);
    const r = (x) => row('S2-S9-S3', x);
    const siA = draw.si.filter((m) => m.court === 'A');
    const stSf = draw.st.filter((m) => m.court === 'A');
    await openShiaijo(C, 'A');
    await pickCompetition(C, SI);

    // S2: the first SI bout on A, then Finish + Start Next, double-tapped.
    bouts.b1 = await startUpNext(C);
    let live = await liveOnConsole();
    expect(live).toMatchObject({ comp: SI, court: 'A' });
    await ipponButton(C, winnerOf(bouts.b1), 'M').tap();
    await settleOn(C);
    const fin = finishButton(C);
    const finLabel = (await fin.innerText()).trim();
    const dbl = await doubleTap(fin);
    await expect(completedRowFor(C, bouts.b1).or(armedFinishButton(C))).toBeVisible();
    // Out the arm's bounce window before reading, so a second tap that was
    // wrongly counted would have committed by now, and the tap below counts.
    await dwell(C, Date.now());
    const committedByDouble = await completedRowFor(C, bouts.b1).isVisible().catch(() => false);
    const armedAfter = await armedFinishButton(C).isVisible().catch(() => false);
    r({ step: 'S2 advance', action: finLabel, variant: 'V2 doubleTap', ...dbl, committedByDouble, stillArmed: armedAfter,
      shot: await shot(C, 's2-finish-double-tapped') });
    expect(dbl.withinWindow, `harness too slow: the two taps were ${dbl.gapMs} ms apart, not a double tap, so this row did not test the bounce guard`).toBe(true);
    expect(finLabel).toBe('Finish + Start Next →');
    expect(committedByDouble).toBe(false);
    expect(armedAfter).toBe(true);
    await armedFinishButton(C).tap();
    await expect(completedRowFor(C, bouts.b1)).toBeVisible();
    live = await nextLive(bouts.b1);
    bouts.b2 = pairOf(live);
    r({ step: 'S2 advance', action: 'Finish + Start Next (confirmed)', variant: 'correct path', next: live,
      stUpNextAt: stSf[0] && stSf[0].time, shot: await shot(C, 's2-next-si-bout') });
    expect(live).toMatchObject({ comp: SI, court: 'A' });
    expect(same(bouts.b2, pairOf(siA[1]))).toBe(true);

    // S4a: Correct the first bout while SI's next bout runs: refused (bc-crpn),
    // the live bout sent back, the correction opened, then Back to court. The
    // live bout has a point first, so the send-back is seen to keep a score.
    await ipponButton(C, winnerOf(bouts.b2), 'M').tap();
    await settleOn(C);
    await expect.poll(() => slotMarks(C, winnerOf(bouts.b2)), { message: 'the live bout shows its point' }).toEqual(['M']);
    const marksB2 = await slotMarks(C, winnerOf(bouts.b2));
    const corr = await correctCompleted(C, bouts.b1);
    r({ step: 'S4a correct', action: "Correct an SI bout while SI's next bout runs", variant: 'correct path (refused, bc-crpn)',
      liveBout: corr.live, ...corr.refusal, shot: await shot(C, 's4a-correction-open') });
    expect(same(corr.live, bouts.b2)).toBe(true);
    await C.getByRole('button', { name: /Back to court/ }).tap();
    await expect(C.getByRole('button', { name: /Back to court/ })).toBeHidden();
    const landed = await courtState(C);
    r({ step: 'S4a correct', action: 'Back to court', variant: 'V5 lost place', officiating: landed.officiating, upNext: landed.upNext,
      editor: landed.editor, idle: landed.idle, shot: await shot(C, 's4a-back-to-court') });
    const again = await ensureRunning(C);
    expect(again, 'the bout sent back for the correction starts again').not.toBeNull();
    expect(same(again, bouts.b2)).toBe(true);
    expect(await slotMarks(C, winnerOf(bouts.b2))).toEqual(marksB2);

    // S2b: Kiken - Voluntary on the running SI bout. Shiro withdraws: their
    // other pool bout on A becomes barred, and the advance must skip it and
    // start the next SI bout on A, never ST's match although ST's is earlier on
    // the clock.
    const withdrawn = bouts.b2.shiro;
    const kiken = await recordDecision(C, 'kiken-voluntary', { side: 'shiro' });
    await expect(completedRowFor(C, bouts.b2)).toBeVisible();
    live = await nextLive(bouts.b2);
    bouts.b3 = pairOf(live);
    const barredRow = C.locator('.shiaijo__queue .shiaijo-qrow').filter({ hasText: withdrawn }).filter({ has: C.getByTestId('barred-match-notice') }).first();
    const barredText = await barredRow.getByTestId('barred-match-notice').innerText().then((t) => t.replace(/\s+/g, ' ').trim(), () => null);
    const nextSiTime = (siA.find((m) => same(pairOf(m), bouts.b3)) || {}).time;
    r({ step: 'S2b advance', action: 'Kiken - Voluntary (Shiro), side picked by the operator', variant: 'V4 hasty (inline prompt, bc-dsid)',
      ...kiken, withdrawn, next: live, nextSiAt: nextSiTime, stEarliestAt: draw.st[0] && draw.st[0].time, barredRow: barredText,
      shot: await shot(C, 's2b-advance-after-kiken') });
    expect(kiken.preselected).toBeNull();
    expect(live).toMatchObject({ comp: SI, court: 'A' });
    expect([live.shiro, live.aka]).not.toContain(withdrawn);
    // The condition the step is about: ST's waiting match is earlier on the clock.
    r({ step: 'S2b advance', action: 'clock check', variant: 'note', stEarlierThanNextSi: !!nextSiTime && draw.st[0].time < nextSiTime });

    // S7 again (D2): what the public sees while the console runs SI.
    await P.reload();
    await untilStable(async () => [await tvBoard(TV).innerText(), await P.locator('body').innerText()]);
    r({ step: 'S7 public', action: 'TV board A and the public schedule while SI runs on A', variant: 'P8 record (D2)',
      consoleRunsNext: 'the next SI bout on A (the console stays on SI)',
      tv: (await tvBoard(TV).innerText()).replace(/\s+/g, ' ').slice(0, 500),
      schedule: (await P.locator('body').innerText()).replace(/\s+/g, ' ').slice(0, 900),
      tvShot: await shot(TV, 's7-tv-A-while-si-runs'), scheduleShot: await shot(P, 's7-public-schedule-while-si-runs', { fullPage: true }) });

    // S9: the tournament-wide Scores page, All competitions. From the running
    // SI bout, Finish + Start Next starts the next SI bout on A, past the
    // barred one and past ST's rows.
    await C.goto('/admin/score-editor');
    await expect(C.getByRole('heading', { level: 1, name: 'Score editor' })).toBeVisible();
    const listOnA = (await C.locator('.score-edit-row').evaluateAll((rows) => rows.map((x) => x.innerText.replace(/\s+/g, ' ').trim())))
      .filter((t) => / A ▾ /.test(` ${t} `));
    await C.locator('.score-edit-row.score-edit-row--running').filter({ hasText: bouts.b3.shiro }).getByRole('button', { name: /^Score$/ }).tap();
    await expect(C.locator(EDITOR)).toBeVisible();
    await ipponButton(C, winnerOf(bouts.b3), 'M', EDITOR).tap();
    await settleOn(C);
    const s9Label = (await finishButton(C, EDITOR).innerText()).trim();
    await finishMatch(C, EDITOR);
    let ov;
    await expect.poll(async () => {
      ov = { ...(await editorIdentity(C, EDITOR)), ...(await editorSides(C, EDITOR)) };
      return !same(ov, bouts.b3) && (await C.locator(EDITOR).locator('.editor-head-pill').count()) === 0;
    }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    bouts.b4 = pairOf(ov);
    const expectedB4 = siA.find((m) => m.time > nextSiTime && ![m.shiro, m.aka].includes(withdrawn));
    r({ step: 'S9 scores page', action: s9Label, variant: 'correct path', listOnA, next: ov, expected: expectedB4,
      shot: await shot(C, 's9-scores-page-finish-next') });
    expect(s9Label).toBe('Finish + Start Next →');
    expect(compOfEyebrow(ov.eyebrow)).toBe(SI);
    expect(ov.court).toBe('A');
    expect(same(bouts.b4, pairOf(expectedB4))).toBe(true);
    // Prev / Next walk the court, both competitions (the operator taps them).
    const walked = [];
    const nav = (cls) => C.locator(EDITOR).locator(cls);
    for (const cls of ['.score-nav__next', '.score-nav__next', '.score-nav__prev', '.score-nav__prev']) {
      if (!(await nav(cls).isEnabled().catch(() => false))) continue;
      const heldBefore = await C.locator(EDITOR).innerText();
      await nav(cls).tap();
      await expect.poll(() => C.locator(EDITOR).innerText()).not.toBe(heldBefore);
      walked.push((await editorIdentity(C, EDITOR)).eyebrow);
    }
    r({ step: 'S9 scores page', action: 'Next x2, Prev x2', variant: 'correct path (allowed to walk the court)', walked,
      comps: walked.map(compOfEyebrow), shot: await shot(C, 's9-prev-next-walked') });
    // Back to the running SI bout, then a no-show: the next SI bout starts.
    await C.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap();
    await C.locator('.score-edit-row.score-edit-row--running').filter({ hasText: bouts.b4.shiro }).getByRole('button', { name: /^Score$/ }).tap();
    await expect(C.locator(EDITOR)).toBeVisible();
    const fus = await recordDecision(C, 'fusenpai', { side: 'aka', root: EDITOR });
    await expect.poll(async () => {
      ov = { ...(await editorIdentity(C, EDITOR)), ...(await editorSides(C, EDITOR)) };
      return !same(ov, bouts.b4) && (await C.locator(EDITOR).locator('.editor-head-pill').count()) === 0;
    }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    bouts.b5 = pairOf(ov);
    r({ step: 'S9 scores page', action: 'Fusenpai (Aka) in the overlay', variant: 'correct path (after-decision advance)', ...fus, next: ov,
      shot: await shot(C, 's9-after-fusenpai') });
    expect(compOfEyebrow(ov.eyebrow)).toBe(SI);
    expect(ov.court).toBe('A');
    // SI's last startable bout on A: no Finish + Start Next (the barred bout
    // and ST's rows are not next).
    const lastLabel = (await finishButton(C, EDITOR).innerText()).trim();
    r({ step: 'S9 scores page', action: "SI's last startable bout on A", variant: 'correct path', finishLabel: lastLabel });
    expect(lastLabel).toBe('Finish');
    // Leave it running with a point, for S3.
    await ipponButton(C, winnerOf(bouts.b5), 'M', EDITOR).tap();
    await settleOn(C);
    await C.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap();

    // S3: back on the console, SI's bout running with a point. The operator
    // switches the picker to ST and starts ST's Up next.
    await openShiaijo(C, 'A');
    await expect.poll(async () => (await liveOnConsole()) && same(await liveOnConsole(), bouts.b5), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const marksB5 = await slotMarks(C, winnerOf(bouts.b5));
    await pickCompetition(C, ST);
    const s3 = await courtState(C);
    r({ step: 'S3 switch', action: `picker -> ${ST} while an SI bout runs with a point`, variant: 'correct path', editor: s3.editor,
      upNext: s3.upNext, nudges: s3.nudges, shot: await shot(C, 's3-picked-st-si-running') });
    expect(same({ shiro: s3.editor.names[0], aka: s3.editor.names[1] }, bouts.b5)).toBe(true);
    // P1: a reload drops the pick.
    await interrupt(C, 'reload');
    await expect(C.getByRole('heading', { level: 1, name: 'Shiaijo A' })).toBeVisible();
    await expect(C.locator('.shiaijo-officiating__name').first()).toBeVisible();
    r({ step: 'S3 switch', action: `reload with ${ST} picked and an SI bout running`, variant: 'V3 interrupt reload (P1)',
      officiatingAfter: await officiatingName(C), shot: await shot(C, 's3-reload-after-pick') });
    if ((await officiatingName(C)) !== ST) await pickCompetition(C, ST);
    // V1: the thumb lands beside ST's Start match.
    const stCard = upNextCard(C);
    const stPair = await sides(stCard);
    let nbStart;
    try {
      nbStart = await tapNeighbour(stCard.getByRole('button', { name: 'Start match' }), 'right');
    } catch (e) {
      nbStart = { hit: null, note: String(e.message).slice(0, 120) };
    }
    const lineupPanel = C.locator('[data-testid^="match-lineup-side-"]').first();
    // A neighbour that is the lineup button opens the panel; any other control
    // opens nothing, which the read below takes as it finds it.
    if (/lineup/i.test((nbStart.hit && nbStart.hit.label) || '')) await expect(lineupPanel).toBeVisible();
    const panelOpen = await lineupPanel.isVisible().catch(() => false);
    r({ step: 'S3 switch', action: "Start match (ST's Up next)", variant: 'V1 tapNeighbour right', hit: nbStart.hit && nbStart.hit.label,
      note: nbStart.note, lineupPanelOpened: panelOpen, stillRunning: await liveOnConsole(), shot: await shot(C, 's3-start-neighbour-tapped') });
    if (panelOpen) await T.closePanel(C);
    // V2: Start match double-tapped. Count the match writes it sends.
    // Each match write with when it left (ms after the first tap) and the
    // status it was answered with.
    const writes = [];
    const t0 = Date.now();
    const onRes = (res) => {
      const req = res.request();
      const u = new URL(req.url());
      if (req.method() !== 'GET' && /\/api\/competitions\/[^/]+\/matches\//.test(u.pathname)) {
        writes.push(`${req.method()} ${decodeURIComponent(u.pathname.split('/matches/')[1])} -> ${res.status()} (+${Date.now() - t0}ms)`);
      }
    };
    C.on('response', onRes);
    let dblStart;
    try {
      dblStart = await doubleTap(stCard.getByRole('button', { name: 'Start match' }));
      await expect.poll(async () => {
        const l = await liveOnConsole();
        return !!l && l.comp === ST;
      }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
      await expect.poll(() => writes.length).toBeGreaterThan(0);
    } finally {
      // A failed poll must not leave the listener on the shared page.
      C.off('response', onRes);
    }
    const onAafterSwitch = await runningOnCourt(H, 'A');
    const s3b = await courtState(C);
    r({ step: 'S3 switch', action: "Start match (ST's Up next) while the SI bout runs", variant: 'V2 doubleTap', ...dblStart, writes,
      runningOnA: onAafterSwitch, alsoRunning: s3b.alsoRunning, nudges: s3b.nudges, shot: await shot(C, 's3-st-started') });
    expect(onAafterSwitch).toHaveLength(1);
    expect(onAafterSwitch[0]).toMatchObject({ comp: ST, shiro: stPair.shiro, aka: stPair.aka });
    expect(s3b.alsoRunning).toBeNull();

    // ST's semi-final played; Finish + Start Next stays in ST.
    const sf1 = await playTeamMatch(C);
    live = await nextLive(sf1);
    r({ step: 'S3 switch', action: `Finish + Start Next on ${ST}'s semi-final`, variant: 'correct path', played: sf1, next: live,
      shot: await shot(C, 's3-st-finish-next') });
    expect(live).toMatchObject({ comp: ST, court: 'A' });
    bouts.sf2 = pairOf(live);
    await T.boutIppon(inlineEditor(C), 1, 'shiro', 'M');

    // Back to SI: its sent-back bout is Up next with its point. Starting it
    // sends ST's semi-final back; one bout runs, and the point is still there.
    await pickCompetition(C, SI);
    expect(same(await sides(upNextCard(C)), bouts.b5)).toBe(true);
    await upNextCard(C).getByRole('button', { name: 'Start match' }).tap();
    await expect.poll(async () => same(await liveOnConsole(), bouts.b5), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const onAback = await runningOnCourt(H, 'A');
    const marksBack = await slotMarks(C, winnerOf(bouts.b5));
    r({ step: 'S3 switch', action: 'Start the sent-back SI bout (ST semi-final running)', variant: 'correct path (bc-sbq)',
      runningOnA: onAback, marksBefore: marksB5, marksAfter: marksBack, shot: await shot(C, 's3-si-bout-back-with-point') });
    expect(onAback).toHaveLength(1);
    expect(onAback[0]).toMatchObject({ comp: SI, shiro: bouts.b5.shiro, aka: bouts.b5.aka });
    expect(marksBack).toEqual(marksB5);

    // S2b tail: a no-show on SI's last startable bout on A. Nothing is left to
    // start in SI here, so nothing starts: ST's waiting semi-final stays queued.
    const fus2 = await recordDecision(C, 'fusenpai', { side: 'aka' });
    await expect(completedRowFor(C, bouts.b5)).toBeVisible();
    // Nothing starts across the whole window, not only at its end: a start
    // landing late would pass a single read. The editor may still hold b5,
    // just closed, for a moment.
    const quietUntil = Date.now() + 3000;
    while (Date.now() < quietUntil) {
      const l = await liveOnConsole();
      expect(l === null || same(l, bouts.b5), `a start landed after the fusenpai: ${JSON.stringify(l)}`).toBe(true);
      await C.waitForTimeout(250);
    }
    const after = await courtState(C);
    const onAnone = await runningOnCourt(H, 'A');
    r({ step: 'S2b advance', action: "Fusenpai on SI's last startable bout on A", variant: 'correct path (nothing to start)', ...fus2,
      runningOnA: onAnone, court: after, shot: await shot(C, 's2b-nothing-left-to-start') });
    expect(onAnone).toHaveLength(0);
    expect(after.officiating).toBe(SI);
  });

  // S4b Correct on SI while ST runs (P4); S6d/P2 the reopen court_busy panel;
  // S5 the done state (P3) and the nudge (P7), with B's pools played meanwhile.
  test('S4b S6d S5: correcting across competitions, then SI done on A', async () => {
    test.setTimeout(600_000);
    const r = (x) => row('S4b-S5', x);
    if (!/\/admin\/shiaijo\/A$/.test(C.url())) await openShiaijo(C, 'A');

    // ST's semi-final starts again from its Up next, with the point it had.
    await pickCompetition(C, ST);
    expect(same(await sides(upNextCard(C)), bouts.sf2)).toBe(true);
    await T.startUpNextTeam(C);
    const sf2Marks = await T.rowSlots(T.boutRow(inlineEditor(C), 1), 'shiro').allInnerTexts();
    r({ step: 'S4b correct', action: `${ST} semi-final started again`, variant: 'correct path (bc-sbq)', bout1Shiro: sf2Marks });
    expect(sf2Marks).toEqual(['M']);
    // P4: SI's completed bouts are behind the picker while ST is shown.
    const completedST = (await courtState(C)).completed;
    await pickCompetition(C, SI);
    const completedSI = (await courtState(C)).completed;
    r({ step: 'S4b correct', action: `picker -> ${SI} to reach its completed bouts`, variant: 'P4 record', completedWhileST: completedST,
      completedWhileSI: completedSI, shot: await shot(C, 's4b-si-completed-while-st-runs') });
    // Correct the kiken bout while ST's semi-final runs: refused, naming it.
    const corr = await correctAcross(C, bouts.b2);
    r({ step: 'S4b correct', action: `Correct the SI kiken while ${ST} runs`, variant: 'correct path (refused, bc-crpn; P2 copy)',
      liveBout: corr.live, ...corr.refusal, namesCompetition: !!corr.refusal && corr.refusal.notice.includes(ST),
      shot: await shot(C, 's4b-correction-open') });
    expect(same(corr.live, bouts.sf2)).toBe(true);

    // S6d: the head table starts ST's semi-final from ST's Scores tab while the
    // console holds the correction open.
    await H.goto(`/admin/competition/${ids.ST}/scores`);
    await H.locator('.score-edit-row').filter({ hasText: bouts.sf2.shiro }).getByRole('button', { name: /^Score$/ }).tap();
    const hed = H.locator(EDITOR);
    await expect(hed).toBeVisible();
    const startBtn = hed.getByRole('button', { name: 'Start match' });
    await startBtn.waitFor({ state: 'visible' });
    await dwell(H, Date.now());
    await startBtn.tap();
    await running(H, hed);
    const alert = C.locator('.shiaijo-also-running');
    await expect(alert).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
    r({ step: 'S6d reopen', action: `head table starts ${ST}'s semi-final while the console holds an SI correction`, variant: 'two devices',
      alert: (await alert.innerText()).replace(/\s+/g, ' ').trim(), shot: await shot(C, 's6d-also-running-during-correction') });
    await hed.getByRole('button', { name: /✕ Close/ }).tap().catch(() => {});
    // Clear kiken and reopen: the court is busy, so the panel offers the remedy.
    await inlineEditor(C).getByTestId('clear-withdrawal-reopen').tap();
    const conflict = inlineEditor(C).getByTestId('withdrawal-reopen-conflict');
    await expect(conflict).toBeVisible({ timeout: 10_000 });
    const conflictHead = (await conflict.locator('.reopen-conflict__head').innerText()).trim();
    await conflict.scrollIntoViewIfNeeded();
    const conflictShot = await shot(C, 's6d-reopen-court-busy-panel');
    // V4: the hurried operator taps the loudest button.
    const loud = await loudestButton(conflict);
    await loud.button.tap();
    await expect.poll(async () => same(await liveOnConsole(), bouts.b2), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const onA = await runningOnCourt(H, 'A');
    const stRows = await readScoresTab(H, ids.ST);
    r({ step: 'S6d reopen', action: 'Clear kiken and reopen while ST runs', variant: 'V4 loudest button in the court_busy panel', conflictHead,
      namesCompetition: conflictHead.includes(ST), loud: loud.label, others: loud.otherLabels, runningOnA: onA,
      stSemi: stRows.find((m) => same(pairOf(m), bouts.sf2)), conflictShot, shot: await shot(C, 's6d-after-queue-it-and-reopen') });
    expect(onA).toHaveLength(1);
    expect(onA[0]).toMatchObject({ comp: SI, shiro: bouts.b2.shiro, aka: bouts.b2.aka });
    // V5 / P1 / P7: the iPad reloads the tab now.
    await interrupt(C, 'reload');
    await expect(C.getByRole('heading', { level: 1, name: 'Shiaijo A' })).toBeVisible();
    await expect(C.locator('.shiaijo-officiating__name').first()).toBeVisible();
    r({ step: 'S6d reopen', action: 'reload right after the reopen', variant: 'V3/V5 interrupt reload (P1, P7)',
      court: await courtState(C), shot: await shot(C, 's6d-reload') });
    if ((await officiatingName(C)) !== SI) await pickCompetition(C, SI);

    // Play SI's remaining bouts on A (the reopened one first).
    const played = [];
    for (let i = 0; i < 6; i += 1) {
      const running = await liveOnConsole();
      if (!running && (await queuedPoolBouts(C)) === 0) break;
      if (running && running.comp !== SI) break;
      played.push(await finishSiBout(C));
    }
    r({ step: 'S5 SI done on A', action: 'play the rest of SI on A', variant: 'correct path', played });

    // S5 (ii): SI's pools on A are done; its quarter-finals here still wait on
    // pool placeholders from B (hidden rows), and nothing runs on A.
    // Deliberate: nothing may start, so the time is the evidence.
    await C.waitForTimeout(2500);
    const ii = await courtState(C);
    r({ step: 'S5 SI done on A', action: 'the console with SI pools done on A, B pools still to play', variant: 'P3 / P7 record', court: ii,
      nudges: await nudgeLines(C), shot: await shot(C, 's5-ii-si-complete-on-A') });
    expect(ii.officiating).toBe(SI);
    // P1 / P7: a reload with no pick now.
    await interrupt(C, 'reload');
    await expect(C.getByRole('heading', { level: 1, name: 'Shiaijo A' })).toBeVisible();
    await expect(C.locator('.shiaijo-officiating__name').first()).toBeVisible();
    r({ step: 'S5 SI done on A', action: 'reload (the explicit pick is dropped)', variant: 'V3 interrupt reload (P1)',
      officiatingAfter: await officiatingName(C), shot: await shot(C, 's5-ii-after-reload') });
    if ((await officiatingName(C)) !== SI) await pickCompetition(C, SI);

    // Court B plays its pools on its own device; A's console follows unprompted.
    const bPlayed = [];
    for (let i = 0; i < 8 && (await queuedPoolBouts(Bc)) + ((await inlineRunning(Bc)) ? 1 : 0) > 0; i += 1) {
      const id = (await inlineRunning(Bc)) ? (await editorIdentity(Bc)).eyebrow : '';
      if (id && !/POOL/.test(id)) break;
      bPlayed.push(await playRunningBout(Bc));
    }
    await expect.poll(async () => ((await courtState(C)).upNext || {}).head || '', { timeout: 20_000 }).toMatch(/Shared Individual · Match \d+$/);
    const koOnA = await courtState(C);
    r({ step: 'S5 SI knockout on A', action: 'court B finishes its pools; court A, untouched', variant: 'correct path (P7: held, no reload)',
      bPlayed: bPlayed.length, court: koOnA, shot: await shot(C, 's5-quarter-finals-arrive-on-A') });
    expect(koOnA.officiating).toBe(SI);

    // Both courts play their quarter-finals; then B starts its semi-final (M6)
    // and leaves it running with a point, and A plays its semi-final (M5), the
    // Finish that gets there re-tapped while it saves (V2).
    const quarters = [];
    for (let i = 0; i < 2; i += 1) quarters.push({ court: 'B', ...(await advanceCourt(Bc)) });
    for (let i = 0; i < 2; i += 1) quarters.push({ court: 'A', ...(await advanceCourt(C)) });
    r({ step: 'S5 SI knockout on A', action: 'quarter-finals on both courts', variant: 'correct path', quarters });
    const m6 = await ensureRunning(Bc);
    expect(m6).not.toBeNull();
    await ipponButton(Bc, winnerOf(m6), 'M').tap();
    await settleOn(Bc);
    bouts.m6 = m6;
    const m5 = await ensureRunning(C);
    expect(m5).not.toBeNull();
    await ipponButton(C, winnerOf(m5), 'M').tap();
    await settleOn(C);
    await armFinish(C);
    const retap = await retapWhileSaving(armedFinishButton(C));
    await expect(completedRowFor(C, m5)).toBeVisible();
    r({ step: 'S5 SI knockout on A', action: 'Finish on SI semi-final M5', variant: 'V2 retapWhileSaving', pair: m5, ...retap });

    // S5 (i): SI has nothing running or scheduled on A, its final waits on
    // M6 (running on B): a Later row. 3 s later the console still shows SI.
    // Deliberate: the console must NOT switch by itself, so the time is the evidence.
    await C.waitForTimeout(3000);
    const i5 = await courtState(C);
    const lines = await nudgeLines(C);
    r({ step: 'S5 SI final waits', action: 'the console with SI waiting on a feeder from B', variant: 'P3 / P7 record', court: i5, nudges: lines,
      shot: await shot(C, 's5-i-final-waits-on-B') });
    expect(i5.officiating).toBe(SI);
    expect(i5.later.length).toBe(1);
    // The switch happens on the tap, not before.
    const amber = nudges(C).filter({ hasText: ST }).first();
    await expect(amber).toBeVisible();
    await amber.tap();
    await expect.poll(() => officiatingName(C)).toBe(ST);
    r({ step: 'S5 SI final waits', action: `tap the nudge line (${ST})`, variant: 'correct path', officiating: await officiatingName(C),
      upNext: (await courtState(C)).upNext, shot: await shot(C, 's5-switched-by-tap') });
  });

  // bc-rnrm: the Run now button on court console rows was removed (operator
  // ruling 2026-10-11 on PR #442 finding 5). Relies on the state S4b-S5 leaves:
  // SI's final waits on M6, running on B, so it is a Later row on A, the row
  // that formerly offered Run now; ST's final waits on its second semi-final
  // the same way. Only switches the picker, and leaves the console on ST, as
  // S6a expects.
  test('bc-rnrm: no court console row offers Run now', async () => {
    if (!/\/admin\/shiaijo\/A$/.test(C.url())) await openShiaijo(C, 'A');
    await pickCompetition(C, SI);
    await expect(C.locator('.shiaijo-pending .shiaijo-qrow').first()).toBeVisible();
    await expect(C.getByRole('button', { name: 'Run now' })).toHaveCount(0);
    await pickCompetition(C, ST);
    await expect(C.getByRole('button', { name: 'Run now' })).toHaveCount(0);
  });

  // bc-pace (operator ruling 2026-10-12, bc-shcr D2: "The schedule also needs
  // to adapt if the courts are running late or fast"). How the projection is
  // computed and shown is planned on the bead before code, so this pins only
  // the observable its acceptance names: once court A has played matches
  // (SI's pools, quarter-finals and semi-final, all far off the plan's clock
  // in a test run),
  // its next match, ST's second semi-final, shows a time other than its
  // stored one on the court console, the public schedule and the TV board
  // (which shows no time at all today), while the Scores tab still shows the
  // plan. Rewrite the time reads to the planned presentation when the bead is
  // planned. Relies on the state S4b-S5 leaves (nothing running on A, ST's
  // semi-final waiting there); it only reads, and leaves the console on ST.
  test.fixme('bc-pace: court A\'s next match shows a time projected from the court\'s pace, the plan unchanged', async () => {
    const planned = (draw.st.find((m) => same(pairOf(m), bouts.sf2)) || {}).time;
    expect(planned, `${ST}'s second semi-final has a planned time`).toMatch(/^\d\d:\d\d$/);
    const stored = ((await readScoresTab(H, ids.ST)).find((m) => same(pairOf(m), bouts.sf2)) || {}).time;
    expect(stored, 'the Scores tab still shows the plan').toBe(planned);
    if (!/\/admin\/shiaijo\/A$/.test(C.url())) await openShiaijo(C, 'A');
    if ((await officiatingName(C)) !== ST) await pickCompetition(C, ST);
    expect(same(await sides(upNextCard(C)), bouts.sf2)).toBe(true);
    expect(timesOtherThan(await upNextCard(C).innerText(), planned), 'a projected time on the console Up next card').not.toEqual([]);
    await P.goto('/schedule');
    const sfRow = scheduleColumn(P, 'A').locator('.tw-match').filter({ hasText: bouts.sf2.shiro }).filter({ hasText: bouts.sf2.aka }).first();
    await expect(sfRow).toBeVisible();
    expect(timesOtherThan(await sfRow.innerText(), planned), 'a projected time on the public schedule row').not.toEqual([]);
    await openTvBoard(TV, 'A');
    await expect(tvBoard(TV)).toContainText(bouts.sf2.shiro);
    expect(timesOtherThan(await tvBoard(TV).innerText(), planned), 'a projected time on the TV board').not.toEqual([]);
  });

  // bc-sypl (operator ruling 2026-10-12: "You do not need the word/pill saying
  // Synced because you have the Connected pill and it shows the queue of
  // events"). No score editor shows the autosave pill, inline or overlay; a
  // change not sent yet shows in the header alone, as the held-writes count
  // beside the Connected pill, and goes once it is sent. Relies on the state
  // S4b-S5 leaves: court B's console runs SI's semi-final M6, Shiro or Aka
  // ahead by a men. It adds a kote for the other side while offline, which S6a
  // and bc-mvrq do not depend on.
  test('bc-sypl: score editors show no sync pill; the header shows what is not sent yet', async () => {
    if (!/\/admin\/shiaijo\/B$/.test(Bc.url())) await openShiaijo(Bc, 'B');
    await expect.poll(async () => same(await liveBout(Bc), bouts.m6), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const ed = inlineEditor(Bc);
    await expect(ed.getByText(SYNC_PILL)).toHaveCount(0);
    const conn = Bc.locator('.topbar').getByRole('status').filter({ hasText: 'Connected' });
    await expect(conn).toBeVisible();
    await notHeld(Bc);
    const other = winnerOf(bouts.m6) === 'shiro' ? 'aka' : 'shiro';
    const before = await filledSlots(Bc, other).count();
    await Bc.context().setOffline(true);
    try {
      await ipponButton(Bc, other, 'K').tap();
      await expect(filledSlots(Bc, other)).toHaveCount(before + 1);
      await held(Bc);
      await expect(ed.getByText(SYNC_PILL)).toHaveCount(0);
    } finally {
      await Bc.context().setOffline(false);
    }
    await notHeld(Bc);
    await expect(conn).toBeVisible();
    // The overlay editor, on the head table, holds the same match.
    await H.goto(`/admin/competition/${ids.SI}/scores`);
    await H.locator('.score-edit-row.score-edit-row--running').filter({ hasText: bouts.m6.shiro }).getByRole('button', { name: /^Score$/ }).tap();
    await expect(H.locator(EDITOR)).toBeVisible();
    await expect(H.locator(EDITOR).getByText(SYNC_PILL)).toHaveCount(0);
    await H.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap();
  });

  // S6a / P5: a RUNNING match moved onto a shiaijo where another bout runs.
  test('S6a: a running match moved onto the busy shared court (P5)', async () => {
    test.setTimeout(300_000);
    const r = (x) => row('S6a', x);
    if (!/\/admin\/shiaijo\/A$/.test(C.url())) await openShiaijo(C, 'A');
    if ((await officiatingName(C)) !== ST) await pickCompetition(C, ST);
    // ST's semi-final runs on A; SI's semi-final M6 runs on B.
    await T.startUpNextTeam(C);
    expect((await liveOnConsole()).comp).toBe(ST);
    // The head table moves M6 from B to A on the Tournament schedule page.
    await H.goto('/admin/schedule');
    const bCol = H.locator('.tw-court').filter({ has: H.locator('.tw-court__title', { hasText: /^SHIAIJO B$/ }) });
    const m6Row = bCol.locator('.tw-match.tw-match--running').filter({ hasText: bouts.m6.shiro }).first();
    await expect(m6Row).toBeVisible();
    await m6Row.locator('button[aria-haspopup="listbox"]').tap();
    await H.getByRole('listbox', { name: 'Choose shiaijo' }).getByRole('option', { name: 'A', exact: true }).tap();
    await rendered(H);
    const confirm = H.getByRole('dialog').filter({ hasText: 'Shiaijo A' });
    await paceSettled(confirm);
    const askedFirst = true;
    await confirm.getByRole('button', { name: /Move/ }).tap();
    await expect(confirm).toBeHidden();
    const alert = C.locator('.shiaijo-also-running');
    await expect(alert).toHaveCount(0);
    const alertShown = false;
    let onA;
    await expect.poll(async () => {
      onA = await runningOnCourt(H, 'A');
      return onA.length;
    }).toBe(1);
    r({ step: 'S6a move', action: 'schedule page: move running SI M6 from B onto A while ST runs on A', variant: 'P5 record', askedFirst,
      runningOnA: onA, alert: alertShown ? (await alert.innerText()).replace(/\s+/g, ' ').trim() : null,
      consoleShot: await shot(C, 's6a-one-running-on-A'), scheduleShot: await shot(H, 's6a-schedule-after-move', { fullPage: true }) });
    // Can either bout still take a point? The console's editor holds one of them.
    const held = await liveOnConsole();
    const ed = inlineEditor(C);
    let pointTried = null;
    // The waits below are the time a refusal (or none) takes to show: whether
    // the point is refused is what the row records, so there is no state to wait for.
    if (held && held.comp === ST) {
      const before = await T.rowSlots(T.boutRow(ed, 2), 'shiro').count();
      await T.rowIppon(T.boutRow(ed, 2), 'shiro', 'M').tap();
      await C.waitForTimeout(2500);
      pointTried = { comp: ST, before, after: await T.rowSlots(T.boutRow(ed, 2), 'shiro').count() };
    } else if (held) {
      await ipponButton(C, winnerOf(held), 'K').tap();
      await C.waitForTimeout(2500);
      pointTried = { comp: SI, marks: await slotMarks(C, winnerOf(held)) };
    }
    const unsent = await unsentCount(C).catch(() => null);
    const toasts = (await C.locator('.toast, .pending-write-banner, .alert--error').allInnerTexts().catch(() => [])).join(' | ').replace(/\s+/g, ' ');
    r({ step: 'S6a move', action: 'a point on the bout the console shows, ST running with M6 queued on A', variant: 'P5 record', held, pointTried, unsent,
      messages: toasts.slice(0, 400), shot: await shot(C, 's6a-point-st-running') });
    // Recovery: send the shown bout back to the queue.
    await C.getByRole('button', { name: 'Send back to queue' }).tap();
    const sent = await hastyShiaijoConfirm(C);
    await expect.poll(async () => !same(await liveOnConsole(), held)).toBe(true);
    let onAafter;
    await expect.poll(async () => {
      onAafter = await runningOnCourt(H, 'A');
      return onAafter.length;
    }).toBe(0);
    r({ step: 'S6a move', action: 'Send back to queue (the shown bout)', variant: 'V4 hastyShiaijoConfirm (recovery)', ...sent,
      runningOnA: onAafter, court: await courtState(C), shot: await shot(C, 's6a-after-send-back') });
    expect(onAafter).toHaveLength(0);
    // Was the point the board showed (with the outbox settled) stored? The
    // match is queued now; an editor shows a queued match's kept score.
    if (pointTried && pointTried.comp === ST) {
      await H.goto(`/admin/competition/${ids.ST}/scores`);
      await H.locator('.score-edit-row').filter({ hasText: held.shiro }).getByRole('button', { name: /^Score$/ }).tap();
      const hed = H.locator(EDITOR);
      await expect(T.boutRow(hed, 2)).toBeVisible();
      const stored = await T.rowSlots(T.boutRow(hed, 2), 'shiro').count();
      r({ step: 'S6a move', action: 'the scored point, read back on the head table after the send back', variant: 'P5 record',
        bout2ShiroOnBoardBefore: pointTried.after, bout2ShiroStored: stored, shot: await shot(H, 's6a-scored-point-read-back') });
      await hed.getByRole('button', { name: /✕ Close/ }).tap().catch(() => {});
    }
    // Leave ST running on A and M6 queued on A for subsequent steps.
    await openShiaijo(C, 'A');
    await pickCompetition(C, ST);
    await T.startUpNextTeam(C);
    await expect.poll(async () => ((await liveOnConsole()) || {}).comp, { timeout: EXPECT_TIMEOUT_MS }).toBe(ST);
  });

  // bc-mvrq (operator ruling 2026-10-12: "warn about the move; the match then
  // goes into the other shiaijo's queue as its NEXT match (it does not keep
  // running there)"). S6a's shape again: ST's semi-final running on A, SI's
  // M6 running on B, and the head table moves M6 onto A from the Tournament
  // schedule page. Fixed: a confirm names the match and the court first; once
  // confirmed M6 stops running, keeps its score (the Send back to queue rule)
  // and is next in A's queue, and only ST's bout runs on A. The other doors
  // (the schedule page's drag, the Scores row picker, the Bracket tab's panel
  // picker) are pinned by the fix's own tests. Last in the file: S6a leaves
  // ST running on A and M6 queued on A, so this first puts the courts back into
  // S6a's starting shape through the UI (moving M6 to B and starting it there).
  test('bc-mvrq: moving a running match to another shiaijo asks first, then queues it next there', async () => {
    test.setTimeout(300_000);
    if (!/\/admin\/shiaijo\/A$/.test(C.url())) await openShiaijo(C, 'A');
    // ST's semi-final runs on A.
    await pickCompetition(C, ST);
    if (((await liveOnConsole()) || {}).comp !== ST) await T.startUpNextTeam(C);
    await expect.poll(async () => ((await liveOnConsole()) || {}).comp, { timeout: EXPECT_TIMEOUT_MS }).toBe(ST);
    const stLive = await liveOnConsole();
    // M6, waiting on A, moves to B (a waiting move asks nothing) and starts there.
    const m6ScoresRow = () => H.locator('.score-edit-row').filter({ hasText: bouts.m6.shiro }).filter({ hasText: bouts.m6.aka }).first();
    await H.goto(`/admin/competition/${ids.SI}/scores`);
    await chooseCourt(m6ScoresRow(), 'B');
    await openShiaijo(Bc, 'B');
    await expect.poll(async () => same(await sides(upNextCard(Bc)).catch(() => null), bouts.m6), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    expect(same(await ensureRunning(Bc), bouts.m6)).toBe(true);
    const marks = { shiro: await slotMarks(Bc, 'shiro'), aka: await slotMarks(Bc, 'aka') };
    // The move: the running M6, from B's column of the schedule page onto A.
    await H.goto('/admin/schedule');
    const m6Row = scheduleColumn(H, 'B').locator('.tw-match.tw-match--running').filter({ hasText: bouts.m6.shiro }).first();
    await expect(m6Row).toBeVisible();
    await m6Row.locator('button[aria-haspopup="listbox"]').tap();
    await H.getByRole('listbox', { name: 'Choose shiaijo' }).getByRole('option', { name: 'A', exact: true }).tap();
    const confirm = H.getByRole('dialog').filter({ hasText: 'Move to Shiaijo A?' });
    await paceSettled(confirm);
    await expect(confirm.locator('.modal__title')).toHaveText('Move to Shiaijo A?');
    const expectedMsg = `${bouts.m6.shiro} vs ${bouts.m6.aka} is running on Shiaijo B. It stops there and goes first in the queue on Shiaijo A. The score already saved is kept: starting it again carries on from there.`;
    await expect(confirm.locator('.dialog-msg')).toHaveText(expectedMsg);
    await expect(confirm.locator('.btn--ghost', { hasText: 'Cancel' })).toBeVisible();
    await expect(confirm.getByRole('button', { name: 'Move to Shiaijo A' })).toBeVisible();

    // Cancel sends nothing.
    await confirm.locator('.btn--ghost', { hasText: 'Cancel' }).tap();
    await expect(confirm).toBeHidden();
    await expect(scheduleColumn(H, 'B').locator('.tw-match.tw-match--running').filter({ hasText: bouts.m6.shiro })).toBeVisible();

    // Repeat and accept.
    await m6Row.locator('button[aria-haspopup="listbox"]').tap();
    await H.getByRole('listbox', { name: 'Choose shiaijo' }).getByRole('option', { name: 'A', exact: true }).tap();
    await paceSettled(confirm);
    await confirm.getByRole('button', { name: /Move/ }).tap();
    await expect(confirm).toBeHidden();

    // One bout runs on A, ST's; none on B; the console shows no second bout.
    await expect.poll(async () => runningOnCourt(H, 'A')).toMatchObject([{ comp: ST, shiro: stLive.shiro, aka: stLive.aka }]);
    await expect.poll(async () => runningOnCourt(H, 'B')).toHaveLength(0);
    await expect(C.locator('.shiaijo-also-running')).toHaveCount(0);

    // M6 is scheduled on A, first in A's queue, with the score it had.
    await pickCompetition(C, SI);
    await expect.poll(async () => same(await sides(upNextCard(C)).catch(() => null), bouts.m6), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    await H.goto(`/admin/competition/${ids.SI}/scores`);
    await expect.poll(async () => {
      const row = m6ScoresRow();
      const court = (await row.locator('.score-edit-row__court').innerText()).replace(/\s*▾$/, '').trim();
      const isRunning = await row.evaluate((el) => el.classList.contains('score-edit-row--running'));
      return { court, isRunning };
    }).toEqual({ court: 'A', isRunning: false });

    await m6ScoresRow().getByRole('button', { name: /^Score$/ }).tap();
    await expect(H.locator(EDITOR)).toBeVisible();
    expect({ shiro: await slotMarks(H, 'shiro', EDITOR), aka: await slotMarks(H, 'aka', EDITOR) }).toEqual(marks);

    // Open History and assert the line "Moved to Shiaijo A and sent back to the queue".
    const hist = H.locator(EDITOR).getByTestId('match-history');
    await hist.getByTestId('match-history-toggle').tap();
    await expect.poll(async () => hist.innerText()).toContain('Moved to Shiaijo A and sent back to the queue');
    await H.locator(EDITOR).getByRole('button', { name: /✕ Close/ }).tap();
  });
});
