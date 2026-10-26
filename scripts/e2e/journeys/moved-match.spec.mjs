// A WAITING match moved to another shiaijo mid-competition reaches the other
// court's console without a reload (bc-mvsj, operator request 2026-10-03).
//
// Two competitions in progress on two courts, a bout being fought on each:
// "Individual" (a league) on Shiaijo A and "Teams" (a team knockout) on
// Shiaijo B. Court A and court B are two devices (two browser contexts), and
// a third, the head table, is the "another device" the notes ask for. Court
// B's console is opened once and never reloaded: a mark left on its window is
// read back after every live change to prove it.
//
// Asserted, as shipped in PR #453 (operator decisions 2026-10-03): the move
// confirm names the match (both competitors); A's queue drops the match; B
// gains the competition picker without a reload, keeps showing Teams, keeps
// the moved match out of its own queue and names it in the quiet "also
// waiting" line; picking Individual shows it as Up next; B holds Teams while
// Teams still has work, even when a Teams bout finishes on another device;
// the amber "Switch to Individual" line appears once Teams has nothing left
// and switches only on the tap; a move made from the competition's Scores tab
// reaches both consoles the same way.
//
// Step 5 differs from the bead's text, which predates bc-sbq (PR #447): the
// console's OWN Start match never meets the court gate, because picking another
// bout sends the running one back to the queue with its score (operator ruling
// 2026-09-26). That shape is asserted (one bout running, the score kept). The
// server's refusal naming the running bout is reached through a door that does
// not defer: the competition's Scores tab on the same device.
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament, login } from '../fixtures/setup.mjs';
import { openShiaijo, upNextCard, sides, startUpNext } from '../fixtures/shiaijo.mjs';
import { EDITOR, awardIppon, finishButton, finishMatch } from '../fixtures/scoring.mjs';
import { hastyShiaijoConfirm } from '../fixtures/clumsy.mjs';
import { rendered, settled, untilStable } from '../fixtures/pace.mjs';
import { EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { tapSize } from '../fixtures/pointer.mjs';
import { ROSTER_6, recordDecision, slotMarks } from '../fixtures/individual.mjs';
import * as T from '../fixtures/team.mjs';
import {
  seedCompetitions, readScoresTab, readScoresRows, operatorDevice, markPage, pageMark,
  competitionPicker, officiatingName, pickCompetition, nudges, nudgeLines, courtState,
  upcomingRow, chooseCourt, runningOnCourt, playTeamMatch, liveBout,
} from '../fixtures/shared-court.mjs';

test.describe.configure({ mode: 'serial' });

const JOURNEY = 'moved-match';
const audit = auditLog(JOURNEY);
const row = (r) => audit.record({ journey: 'MV', ...r });
const TEAMS = [['Hayabusa', 'Kita Dojo'], ['Inoshishi', 'Minami Dojo'], ['Kitsune', 'Higashi Dojo'], ['Tanuki', 'Nishi Dojo']];

let A; // court A's iPad
let B; // court B's iPad, opened once and never reloaded
let H; // the head table
let ids;
let shot;
let moved; // the pairing moved from A to B

// Court B's console has not reloaded or navigated since the mark was left.
async function bStillOpen() {
  expect(await pageMark(B), 'court B was not reloaded').toBe('court-B-open');
  expect(new URL(B.url()).pathname).toBe('/admin/shiaijo/B');
}

const queueNames = (s) => [s.upNext, ...s.upcoming].filter(Boolean).map((r) => r.names.join(' vs '));
const pairText = (p) => `${p.shiro} vs ${p.aka}`;
const samePair = (a, b) => !!a && !!b && a.shiro === b.shiro && a.aka === b.aka;
// A pairing's Start, on the Up next card or its Upcoming row, whichever holds it.
const startButton = (page, pair) => upNextCard(page).filter({ hasText: pair.shiro }).filter({ hasText: pair.aka })
  .or(upcomingRow(page, pair)).first().getByRole('button', { name: 'Start match' });

test.describe('moved-match', () => {
  test.beforeAll(async ({ browser, serverForFile }, testInfo) => {
    test.setTimeout(240_000);
    const server = await serverForFile(testInfo.file);
    shot = journeyShots(JOURNEY);
    A = await operatorDevice(browser, server.base);
    await enterTournament(A, { name: 'Moved Match Cup', courts: 2 });
    const seeded = await seedCompetitions(A, [
      { key: 'IND', opts: { name: 'Individual', kind: 'individual', format: 'league', courts: ['A'], numberPrefix: 'I' }, roster: ROSTER_6.slice(0, 5) },
      { key: 'TEAMS', opts: { name: 'Teams', kind: 'team', format: 'knockout', teamSize: 3, teamMatchType: 'fixed', courts: ['B'], numberPrefix: 'T' }, roster: TEAMS },
    ]);
    ids = seeded.ids;
    row({ step: 'setup', action: 'wizard defaults, both competitions created before any roster', variant: 'seed', defaults: seeded.defaults });
    row({ step: 'setup', action: 'draw read back (Scores tabs)', variant: 'seed',
      individual: await readScoresTab(A, ids.IND), teams: await readScoresTab(A, ids.TEAMS) });
    B = await operatorDevice(browser, server.base);
    await login(B);
    H = await operatorDevice(browser, server.base);
    await login(H);
  });

  test.afterAll(async () => {
    audit.flush();
    for (const p of [A, B, H]) if (p) await p.context().close();
  });

  test('a waiting match moved from court A reaches court B without a reload', async () => {
    test.setTimeout(600_000);

    // 1. A bout being fought on each court.
    await openShiaijo(A, 'A');
    const indLive = await startUpNext(A);
    await openShiaijo(B, 'B');
    const teamsSf1 = await sides(upNextCard(B));
    const edB = await T.startUpNextTeam(B);
    await T.boutIppon(edB, 1, 'shiro', 'M');
    row({ step: '1 setup', action: 'a bout running on each court', variant: 'correct path', courtA: indLive, courtB: teamsSf1,
      shotA: await shot(A, 'court-A-bout-running'), shotB: await shot(B, 'court-B-teams-running') });

    // 2. Court B shows Teams alone: a plain title, no picker.
    await expect.poll(() => officiatingName(B)).toBe('Teams');
    await expect(competitionPicker(B)).toHaveCount(0);
    await markPage(B, 'court-B-open');
    row({ step: '2 court B before', action: 'officiating title on court B', variant: 'correct path',
      title: await officiatingName(B), picker: false, state: await courtState(B) });

    // 3. Court A moves a WAITING Individual match to B through the queue row's
    // court control and the confirm, answered the way a hurried operator does.
    // A row whose competitors are not the ones fighting on A now: a match
    // with a competitor still on another court is refused at its start for
    // that (step 8 walks it), which is not what this step is about.
    const before = await courtState(A);
    const busy = new Set([indLive.shiro, indLive.aka]);
    const free = before.upcoming.filter((r) => !r.names.some((n) => busy.has(n)));
    const last = free[free.length - 1];
    moved = { shiro: last.names[0], aka: last.names[1] };
    const qrow = upcomingRow(A, moved);
    row({ step: '3 move', action: 'court control on the queue row', variant: 'size',
      ...(await tapSize(qrow.locator('button[aria-haspopup="listbox"]').first())) });
    await chooseCourt(qrow, 'B');
    const dialog = A.locator('.shiaijo-move-confirm[role="dialog"]');
    await expect(dialog).toBeVisible();
    const confirmShot = await shot(A, 'move-confirm');
    const hasty = await hastyShiaijoConfirm(A);
    row({ step: '3 move', action: 'Move to Shiaijo B confirm', variant: 'V4 hastyShiaijoConfirm', ...hasty, moved, confirmShot });
    expect(hasty.title).toBe('Move to Shiaijo B?');
    // PR #453 (a): the confirm names the match, both competitors.
    expect(hasty.message).toBe(`${pairText(moved)} leaves Shiaijo A and joins the queue on Shiaijo B.`);
    await expect.poll(async () => queueNames(await courtState(A)).includes(pairText(moved)), { timeout: 10_000 }).toBe(false);
    row({ step: '3 move', action: "court A's queue after the move", variant: 'correct path', queue: queueNames(await courtState(A)),
      shot: await shot(A, 'court-A-after-move') });

    // 4. Court B, untouched: the picker appears, Teams stays, the moved match
    // is not in B's queue and is named by the quiet line (PR #453 (b)).
    await expect(competitionPicker(B)).toBeVisible({ timeout: 10_000 });
    await bStillOpen();
    const options = await competitionPicker(B).locator('option').allInnerTexts();
    const bAfter = await courtState(B);
    const lines = await nudgeLines(B);
    row({ step: '4 court B live', action: 'court B after the move (no reload)', variant: 'correct path', options, officiating: bAfter.officiating,
      queue: queueNames(bAfter), nudges: lines, shot: await shot(B, 'court-B-picker-appeared') });
    expect(options.map((o) => o.trim()).sort()).toEqual(['Individual', 'Teams']);
    expect(bAfter.officiating).toBe('Teams');
    expect(queueNames(bAfter)).not.toContain(pairText(moved));
    expect(lines).toEqual([{ text: '1 Individual match also waiting on this court.', quiet: true, amber: false }]);

    // 5. B picks Individual: the moved match is Up next. A waiting match moved
    // to a court now goes LAST in that court's queue (one minute after its
    // latest timed waiting match, bc-mvrq); it is still Up next here because it
    // is the only Individual match waiting on B, so nothing below depends on its
    // time, and the console's confirm words (step 3) are unchanged.
    await pickCompetition(B, 'Individual');
    await bStillOpen();
    expect(await sides(upNextCard(B))).toEqual(moved);
    row({ step: '5 pick Individual', action: 'competition picker -> Individual', variant: 'correct path',
      upNext: await sides(upNextCard(B)), editorStill: (await courtState(B)).editor, shot: await shot(B, 'court-B-individual-picked') });

    // 5a. The server's refusal: the same device's other tab, the competition's
    // Scores tab, starts the moved match while the Teams bout is fought.
    const tab = await B.context().newPage();
    await tab.goto(`/admin/competition/${ids.IND}/scores`);
    const movedScoreRow = tab.locator('.score-edit-row').filter({ hasText: moved.shiro }).filter({ hasText: moved.aka }).first();
    await movedScoreRow.getByRole('button', { name: /^Score$/ }).tap();
    await expect(tab.locator(EDITOR)).toBeVisible();
    await settled(tab.locator(EDITOR).getByRole('button', { name: 'Start match' }));
    await tab.locator(EDITOR).getByRole('button', { name: 'Start match' }).tap();
    const refusal = tab.locator('.toast, .alert--error, [data-testid="start-refusal-notice"]').filter({ hasText: /Shiaijo B is running/ }).first();
    await expect(refusal).toBeVisible({ timeout: 10_000 });
    const refusalText = (await refusal.innerText()).replace(/\s+/g, ' ').trim();
    // The running bout as its own editor names it ("Teams · Semifinals ·
    // Match 1"); the refusal names it inside its competition ("Match 1
    // (Semifinals)", engine.OperatorMatchLabel), without the competition or the
    // teams: recorded for the copy review (bc-shcr P2), not asserted.
    const liveEyebrow = (await courtState(B)).editor.eyebrow;
    const [, round, num] = liveEyebrow.match(/· (\w+) · Match (\d+)$/) || [];
    row({ step: '5a start refused', action: 'Start match on the moved match from the Scores tab while Teams runs on B', variant: 'correct path (court gate)',
      refusal: refusalText, runningBout: liveEyebrow, namesCompetition: refusalText.includes('Teams'),
      namesTeams: refusalText.includes(teamsSf1.shiro) && refusalText.includes(teamsSf1.aka),
      shot: await shot(tab, 'scores-tab-start-refused') });
    expect(refusalText).toContain(`Shiaijo B is running Match ${num} (${round}). Finish it or send it back to the queue first.`);
    const runningAfterRefusal = await runningOnCourt(H, 'B');
    expect(runningAfterRefusal.map((r) => r.comp)).toEqual(['Teams']);
    await tab.close();

    // 5b. The console's own Start match: the Teams bout goes back to the queue
    // with its score (bc-sbq) and the moved match starts. One bout runs on B.
    await upNextCard(B).getByRole('button', { name: 'Start match' }).tap();
    await expect.poll(async () => ((await courtState(B)).editor || {}).names, { timeout: EXPECT_TIMEOUT_MS }).toEqual([moved.shiro, moved.aka]);
    const oneRunning = await runningOnCourt(H, 'B');
    const bLive = await courtState(B);
    row({ step: '5b console start', action: 'Start match (Up next) while the Teams bout is fought', variant: 'correct path (bc-sbq: the running bout is sent back)',
      runningOnB: oneRunning, alsoRunning: bLive.alsoRunning, nudges: bLive.nudges, shot: await shot(B, 'court-B-moved-match-started') });
    expect(oneRunning).toHaveLength(1);
    expect(oneRunning[0]).toMatchObject({ comp: 'Individual', shiro: moved.shiro, aka: moved.aka });
    expect(bLive.alsoRunning).toBeNull();
    await bStillOpen();
    // Back as it was: the moved match goes back to the queue, Teams is picked
    // and its semi-final starts again with the point it had.
    await B.getByRole('button', { name: 'Send back to queue' }).tap();
    await hastyShiaijoConfirm(B);
    await pickCompetition(B, 'Teams');
    expect(await sides(upNextCard(B))).toEqual(teamsSf1);
    await T.startUpNextTeam(B);
    await expect(T.rowSlots(T.boutRow(edB, 1), 'shiro')).toHaveText(['M']);
    row({ step: '5b console start', action: 'Teams semi-final started again', variant: 'recovery', pointKept: true,
      shot: await shot(B, 'court-B-teams-restarted-with-point') });

    // 6. Teams' own bout finishes on ANOTHER device (the head table): court B
    // stays on Teams, which still has work here.
    await H.goto(`/admin/competition/${ids.TEAMS}/scores`);
    const runningRow = H.locator('.score-edit-row.score-edit-row--running').first();
    await runningRow.getByRole('button', { name: /^Score$/ }).tap();
    const hed = H.locator(EDITOR);
    await expect(T.boutRow(hed, 1)).toBeVisible();
    for (const n of [2, 3]) await T.boutIppon(hed, n, 'shiro', 'M');
    const headFinishLabel = (await T.finishBtn(hed).innerText()).trim();
    const completedOnB = (await courtState(B)).completed;
    await T.finishTeam(hed);
    // Court B has seen the finish (its Completed list holds it) before it is
    // asked whether it stayed on Teams: read earlier, it would prove nothing.
    await expect.poll(async () => (await courtState(B)).completed, { timeout: EXPECT_TIMEOUT_MS }).toBeGreaterThan(completedOnB);
    const bHeld = await courtState(B);
    row({ step: '6 finish elsewhere', action: 'head table finishes the Teams semi-final', variant: 'two devices', headFinishLabel,
      courtB: bHeld, shot: await shot(B, 'court-B-after-head-table-finish') });
    expect(bHeld.officiating).toBe('Teams');
    await bStillOpen();
    // Close the head table's editor if Finish + Start Next left it open.
    if (await hed.isVisible().catch(() => false)) await hed.getByRole('button', { name: /✕ Close/ }).tap().catch(() => {});

    // Court B plays the other semi-final itself (started by the head table's
    // Finish + Start Next, or from Up next).
    await expect.poll(async () => {
      const s = await courtState(B);
      return !!(s.upNext || (s.editor && !s.editor.pill));
    }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const sf2 = await playTeamMatch(B);
    row({ step: '6 finish elsewhere', action: 'court B plays the other semi-final', variant: 'correct path', pair: sf2 });

    // The final finishes on the head table too.
    await expect.poll(async () => {
      await H.goto(`/admin/competition/${ids.TEAMS}/scores`);
      return H.locator('.score-edit-row').filter({ has: H.getByRole('button', { name: /^Score$/ }) }).count();
    }, { timeout: 20_000 }).toBeGreaterThan(0);
    await H.locator('.score-edit-row').filter({ has: H.getByRole('button', { name: /^Score$/ }) }).first()
      .getByRole('button', { name: /^Score$/ }).tap();
    await expect(hed).toBeVisible();
    const startFinal = hed.getByRole('button', { name: 'Start match' });
    if (await startFinal.isVisible().catch(() => false)) {
      await settled(startFinal);
      await startFinal.tap();
    }
    await expect(T.syncPill(hed)).toBeVisible();
    for (const n of [1, 2, 3]) await T.boutIppon(hed, n, 'shiro', 'M');
    await T.finishTeam(hed);

    // Teams has nothing left on B: B still shows Teams, now with the amber
    // line, and switches only on the tap.
    await expect.poll(async () => (await nudgeLines(B)).map((l) => l.amber), { timeout: EXPECT_TIMEOUT_MS }).toEqual([true]);
    // Deliberate: the court must NOT switch by itself, so the time is the evidence.
    await B.waitForTimeout(3000);
    const bDone = await courtState(B);
    const amber = await nudgeLines(B);
    row({ step: '6 nudge', action: 'court B once Teams has nothing left, 3s later', variant: 'correct path', courtB: bDone, nudges: amber,
      shot: await shot(B, 'court-B-switch-to-individual') });
    expect(bDone.officiating).toBe('Teams');
    expect(amber).toEqual([{ text: 'Switch to Individual: 1 match waiting on this court.', quiet: false, amber: true }]);
    await bStillOpen();
    await nudges(B).first().tap();
    await expect.poll(() => officiatingName(B)).toBe('Individual');
    expect(await sides(upNextCard(B))).toEqual(moved);
    row({ step: '6 nudge', action: 'tap the Switch line', variant: 'correct path', officiating: await officiatingName(B),
      upNext: await sides(upNextCard(B)), shot: await shot(B, 'court-B-switched-by-tap') });
    await bStillOpen();

    // 7. The other door: the head table moves another waiting Individual match
    // from A to B on the competition's Scores tab (admin.jsx moveMatchCourt).
    // Both consoles follow, neither reloaded.
    await markPage(A, 'court-A-open');
    const aBefore = await courtState(A);
    // One that involves a competitor fighting on A now, for step 8.
    const second = [...aBefore.upcoming].reverse().find((r) => r.names.some((n) => busy.has(n))) || aBefore.upcoming[aBefore.upcoming.length - 1];
    const secondPair = { shiro: second.names[0], aka: second.names[1] };
    await H.goto(`/admin/competition/${ids.IND}/scores`);
    const hRow = H.locator('.score-edit-row').filter({ hasText: secondPair.shiro }).filter({ hasText: secondPair.aka }).first();
    await chooseCourt(hRow, 'B');
    await rendered(H);
    const confirmAsked = await H.locator('.modal[role="dialog"], .shiaijo-move-confirm').isVisible().catch(() => false);
    await expect.poll(async () => queueNames(await courtState(A)).includes(pairText(secondPair)), { timeout: 10_000 }).toBe(false);
    await expect.poll(async () => queueNames(await courtState(B)).includes(pairText(secondPair)), { timeout: 10_000 }).toBe(true);
    expect(await pageMark(A)).toBe('court-A-open');
    await bStillOpen();
    row({ step: '7 other door', action: 'head table moves a waiting match A -> B from the Scores tab', variant: 'correct path',
      moved: secondPair, confirmAsked, courtA: queueNames(await courtState(A)), courtB: queueNames(await courtState(B)),
      shotA: await shot(A, 'court-A-after-scores-tab-move'), shotB: await shot(B, 'court-B-after-scores-tab-move') });

    // 8. Recorded, not asserted: on B the operator starts the first moved match,
    // then taps Start on the second, whose competitor is still fighting on A.
    // The console sends the running bout back to the queue BEFORE the server
    // answers the start (pickMatch's defer), and the start is refused for the
    // competitor, not the court (bc-stpf; its fixme below asserts the fix).
    // The pairing's Start, on the Up next card or its Upcoming row, whichever
    // holds it when the tap lands: a start just made moves the next pairing
    // from its row onto the card while a hurried tap is on its way.
    const startOf = async (pair) => upNextCard(B).filter({ hasText: pair.shiro }).filter({ hasText: pair.aka })
      .or(upcomingRow(B, pair)).first().getByRole('button', { name: 'Start match' });
    await (await startOf(moved)).tap();
    await expect.poll(async () => ((await courtState(B)).editor || {}).names, { timeout: EXPECT_TIMEOUT_MS }).toEqual([moved.shiro, moved.aka]);
    // 8a. At once, before the court's list has caught up with the start just made.
    const notice = async () => (await B.locator('.shiaijo-upnext__error[role="alert"]').allInnerTexts()).join(' | ');
    const courtB = async () => [await notice(), await runningOnCourt(H, 'B')];
    // One plain read, not a settled one: the tap must still come at once.
    const beforeSecondStart = await courtB();
    await (await startOf(secondPair)).tap();
    // A start the server refuses shows its notice once answered; one it accepts
    // shows the bout running: the row records whichever, so wait for the court
    // to leave the state before the tap, and then to settle.
    await untilStable(courtB, { from: beforeSecondStart });
    row({ step: '8 second start', action: `Start match on ${pairText(secondPair)} at once after starting ${pairText(moved)}`,
      variant: 'V2 hurried second start (inside the list refresh)', notice: await notice(), courtB: await courtState(B),
      runningOnB: await runningOnCourt(H, 'B'), shot: await shot(B, 'court-B-second-start-at-once') });
    // 8b. Again once the list shows the first one running (Up next moved on),
    // when 8a left it running: a quicker list makes 8a defer it already, and
    // then 8a's row is this shape.
    const stillRunning = (await runningOnCourt(H, 'B')).some((m) => m.shiro === moved.shiro && m.aka === moved.aka);
    if (!stillRunning) {
      row({ step: '8 competitor busy', action: '8a already sent the first moved match back', variant: 'note' });
      return;
    }
    await expect.poll(async () => ((await courtState(B)).upNext || {}).names, { timeout: EXPECT_TIMEOUT_MS }).toEqual([secondPair.shiro, secondPair.aka]);
    const beforeBusyStart = await untilStable(courtB);
    await (await startOf(secondPair)).tap();
    await untilStable(courtB, { from: beforeBusyStart });
    row({ step: '8 competitor busy', action: `Start match on ${pairText(secondPair)} (one of them fights on A) while ${pairText(moved)} runs on B`,
      variant: 'V5 lost place (the moved match\'s competitor is elsewhere)', notice: await notice(), courtB: await courtState(B),
      runningOnB: await runningOnCourt(H, 'B'), shot: await shot(B, 'court-B-start-refused-competitor-busy') });
  });

  // bc-stpf (operator ruling 2026-10-12, option A: check that the start would
  // be accepted before sending the running bout back; if it would be refused,
  // nothing moves and the reason shows). Step 8's shape: the moved match runs
  // on court B with a point, and the operator taps Start on a match one of
  // whose competitors is still fighting on A. Fixed: the refusal shows and the
  // moved match keeps running with its point. Then, with that competitor free
  // (A's bout sent back), the same Start is accepted and sends the moved match
  // back first (step 5b asserts a bout sent back this way keeps its score).
  // Relies on what the main test leaves: A's first Individual bout still
  // running, and step 7's match, which names one of its competitors, waiting
  // on B.
  test.fixme('bc-stpf: a Start the server would refuse leaves the running bout running and shows why', async () => {
    const onA = await runningOnCourt(H, 'A');
    expect(onA, "court A's first Individual bout is still running").toHaveLength(1);
    const busy = [onA[0].shiro, onA[0].aka];
    await openShiaijo(B, 'B');
    if ((await competitionPicker(B).count()) && (await officiatingName(B)) !== 'Individual') await pickCompetition(B, 'Individual');
    const runs = async (pair) => samePair(await liveBout(B), pair);
    if (!(await runs(moved))) {
      await startButton(B, moved).tap();
      await expect.poll(() => runs(moved), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    }
    await awardIppon(B, 'shiro', 'M');
    const marks = await slotMarks(B, 'shiro');
    // The waiting match whose competitor fights on A.
    let blocked = null;
    await expect.poll(async () => {
      const s = await courtState(B);
      const r = [s.upNext, ...s.upcoming].filter(Boolean).find((x) => x.names.some((n) => busy.includes(n)));
      blocked = r ? { shiro: r.names[0], aka: r.names[1] } : null;
      return !!blocked;
    }, { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    await startButton(B, blocked).tap();
    const refusal = B.locator('[role="alert"], .toast').filter({ hasText: /is fighting now in .+ on Shiaijo A\. Finish that match first\./ }).first();
    await expect(refusal).toBeVisible({ timeout: 10_000 });
    // Deliberate: the running bout must STAY running, and a late send back would
    // pass a single read, so the time is the evidence.
    await B.waitForTimeout(1500);
    expect(await runs(moved), 'the moved match is still running on B').toBe(true);
    expect(await slotMarks(B, 'shiro')).toEqual(marks);
    const onB = await runningOnCourt(H, 'B');
    expect(onB).toHaveLength(1);
    expect(onB[0]).toMatchObject({ shiro: moved.shiro, aka: moved.aka });
    // Accepted: A's bout goes back to the queue, so its competitors are free.
    await openShiaijo(A, 'A');
    await A.getByRole('button', { name: 'Send back to queue' }).tap();
    await hastyShiaijoConfirm(A);
    await expect.poll(() => liveBout(A), { timeout: 10_000 }).toBeNull();
    await startButton(B, blocked).tap();
    await expect.poll(() => runs(blocked), { timeout: EXPECT_TIMEOUT_MS }).toBe(true);
    const onBafter = await runningOnCourt(H, 'B');
    expect(onBafter).toHaveLength(1);
    expect(onBafter[0]).toMatchObject({ shiro: blocked.shiro, aka: blocked.aka });
  });

  // bc-adna (operator ruling 2026-10-12: "The operator's view is where scoring
  // is done and has a queue. In Admin view, changes are not tied/blocked by
  // the court status, there's no queue or advancing"). On the competition's
  // Scores tab, with other Individual matches waiting on A, the bead's fixed
  // behaviour is three things, each asserted on its own (soft, so one failing
  // does not hide the others, and the network ones sit in the finally so they
  // are read whatever happened before): Finish reads plain Finish; the tab
  // sends no /score start by itself (a start is a /score write whose request
  // carries startOnly), after Finish or after a fusenpai, so nothing starts on
  // A; and no start it sends is refused for the court's status (court_busy),
  // whoever asked for it. The refusal half is read from
  // the network log, because the console policy would otherwise let the toast
  // of the bead's report pass. The operator's own Start match in the editor
  // sends no startOnly (it writes the editor's own board), so it never counts
  // as a start by itself. The same fix retires shared-shiaijo's
  // S9 assertions ("Finish + Start Next →" and the start after a fusenpai on
  // the Scores page) and the Scores tab refusal in step 5a above. Runs after
  // bc-stpf, but takes court A as it finds it: the running Individual bout
  // there, or else one it starts from the Scores tab whose competitors are not
  // fighting on B.
  test.fixme('bc-adna: the Scores tab never starts a match by itself', async () => {
    const scoreWrites = [];
    const pending = [];
    const onResponse = async (res) => {
      const req = res.request();
      if (req.method() !== 'PUT' || !/\/matches\/[^/]+\/score$/.test(new URL(res.url()).pathname)) return;
      const sent = req.postData() || '';
      if (!/"status"\s*:\s*"running"/.test(sent)) return;
      // A start is the write whose request carries startOnly (startPatch).
      let isStart = false;
      try { isStart = JSON.parse(sent).startOnly === true; } catch { /* not JSON: not a start */ }
      const body = res.status() === 409 ? await res.text().catch(() => '') : '';
      scoreWrites.push({
        url: res.url(),
        status: res.status(),
        courtBusy: /"error"\s*:\s*"court_busy"/.test(body),
        isStart,
      });
    };
    const listener = (res) => { pending.push(onResponse(res)); };
    H.on('response', listener);
    try {
      const onB = (await runningOnCourt(H, 'B')).flatMap((m) => [m.shiro, m.aka]);
      const rows = await readScoresTab(H, ids.IND);
      const waitingOnA = rows.filter((m) => m.court === 'A' && m.status === 'scheduled' && !onB.includes(m.shiro) && !onB.includes(m.aka));
      let first = rows.find((m) => m.court === 'A' && m.status === 'running');
      const startsHere = !first;
      if (startsHere) first = waitingOnA.shift();
      expect(first, 'an Individual match on A to score').toBeTruthy();
      expect(waitingOnA.length, 'two more Individual matches wait on A after it').toBeGreaterThan(1);
      const ed = H.locator(EDITOR);
      const open = async (m) => {
        await H.goto(`/admin/competition/${ids.IND}/scores`);
        await H.locator('.score-edit-row').filter({ hasText: m.shiro }).filter({ hasText: m.aka }).first().getByRole('button', { name: /^Score$/ }).tap();
        await expect(ed).toBeVisible();
      };
      // The operator's own Start match: the editor's write carries no startOnly.
      const start = async () => {
        const btn = ed.getByRole('button', { name: 'Start match' });
        await settled(btn);
        await btn.tap();
        await expect(finishButton(H, EDITOR)).toBeVisible();
      };
      await open(first);
      if (startsHere) await start();
      await expect.soft(finishButton(H, EDITOR), 'the Scores tab editor offers plain Finish').toHaveText('Finish');
      await awardIppon(H, 'shiro', 'M', EDITOR);
      await finishMatch(H, EDITOR);
      // Deliberate: nothing may start by itself, so the time is the evidence.
      await H.waitForTimeout(1500);
      expect.soft(await runningOnCourt(H, 'A'), 'nothing started on A after Finish').toHaveLength(0);
      // A fusenpai on the next one, started by the operator.
      const second = waitingOnA.find((m) => !samePair(m, first));
      await open(second);
      await start();
      await recordDecision(H, 'fusenpai', { side: 'aka', root: EDITOR });
      // Deliberate: no start may follow (no toast, nothing running), so the time is the evidence.
      await H.waitForTimeout(2000);
      await expect.soft(H.locator('.toast').filter({ hasText: /is running/ })).toHaveCount(0);
      // One navigation; the poll reads the tab as the app follows the fusenpai.
      await H.goto(`/admin/competition/${encodeURIComponent(ids.IND)}/scores`);
      await expect.poll(async () => ((await readScoresRows(H)).find((m) => samePair(m, second)) || {}).status, { timeout: 10_000 }).toBe('completed');
      expect.soft(await runningOnCourt(H, 'A'), 'nothing started on A after the fusenpai').toHaveLength(0);
    } finally {
      await Promise.all(pending);
      H.off('response', listener);
      expect.soft(scoreWrites.filter((w) => w.courtBusy), 'no start sent from the Scores tab was refused for the court').toEqual([]);
      expect.soft(scoreWrites.filter((w) => w.isStart), 'the Scores tab sent no start by itself').toEqual([]);
    }
  });
});
