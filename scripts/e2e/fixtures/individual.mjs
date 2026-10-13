// Helpers for the INDIVIDUAL operator journeys (knockout-mixed-individual
// spec): the rosters, a deterministic winner rule, and the few surfaces the
// shared fixtures do not own (the decision panel, playing and correcting a
// bout on the shiaijo page). Which match an editor holds is read by
// fixtures/shiaijo.mjs editorIdentity. The audit log (fixtures/audit.mjs),
// tap-target measurement (fixtures/pointer.mjs) and the hasty confirms
// (fixtures/clumsy.mjs) are shared with the other journeys.
import { expect } from '@playwright/test';
import { INLINE_EDITOR, awardIppon, finishMatch } from './scoring.mjs';
import {
  UP_NEXT, WAITING_ROWS, completedRowFor, courtDone, courtIdle, inlineEditor, upNextCard,
} from './shiaijo.mjs';
import { hastyShiaijoConfirm } from './clumsy.mjs';

// Twelve entrants for F1 (pools of 3, two through): six dojos, two members
// each, (name, dojo) unique. Names start with distinct letters so the
// "first name alphabetically wins" rule below gives every pool a strict
// order and never a tie-break bout.
export const ROSTER_12 = [
  ['Aoi Sato', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Mumeishi'], ['Finn Hale', 'Hizen'],
  ['Goro Abe', 'Gyokusen'], ['Hana Ito', 'Thames'], ['Ivo Nunes', 'Musashi'],
  ['Jun Oda', 'Seishinkan'], ['Kai Ueda', 'Mumeishi'], ['Lea Roth', 'Hizen'],
];
// Six for F1b (two pools of 3): the decision and Pools-tab journeys.
export const ROSTER_6 = [
  ['Mika Endo', 'Gyokusen'], ['Nils Berg', 'Thames'], ['Oto Kudo', 'Musashi'],
  ['Pia Lund', 'Seishinkan'], ['Rin Saito', 'Mumeishi'], ['Sam Doyle', 'Hizen'],
];
// Six more for F1c (two pools of 3): scored from the competition's Pools tab.
export const ROSTER_6B = [
  ['Bo Grant', 'Gyokusen'], ['Cy Novak', 'Thames'], ['Di Weller', 'Musashi'],
  ['Ed Frost', 'Seishinkan'], ['Flo Adler', 'Mumeishi'], ['Gil Varga', 'Hizen'],
];
// Small rosters for the '<bead-id>: ...' tests, which seed their own
// competitions so each runs alone.
export const rosterOf = (prefix, n) => Array.from({ length: n }, (_, i) => [
  `${prefix} ${['Aki', 'Bea', 'Cal', 'Dee', 'Eli', 'Fay'][i]}`, ['Gyokusen', 'Thames', 'Musashi', 'Seishinkan', 'Mumeishi', 'Hizen'][i],
]);
// Four for each knockout-only fixture: two semi-finals, then the final (and
// the bronze match when joint 3rd places are off).
export const ROSTER_4_BRONZE = [
  ['Taro Ono', 'Gyokusen'], ['Uma Patel', 'Thames'], ['Vic Lange', 'Musashi'], ['Wen Zhao', 'Seishinkan'],
];
export const ROSTER_4_JOINT = [
  ['Xan Moss', 'Gyokusen'], ['Yui Hara', 'Thames'], ['Zoe Quinn', 'Musashi'], ['Ada Pike', 'Seishinkan'],
];

// The deterministic winner of a pairing: the name that sorts first. Transitive,
// so a pool's standings come out strict and no tie-break bout is added.
export const winnerOf = ({ shiro, aka }) => (shiro.localeCompare(aka) <= 0 ? 'shiro' : 'aka');

// The two names an editor shows, Shiro first.
export async function editorSides(page, root = INLINE_EDITOR) {
  const ed = page.locator(root).first();
  const shiro = (await ed.locator('.sb-side--shiro .numbered-name__text').first().innerText()).trim();
  const aka = (await ed.locator('.sb-side--aka .numbered-name__text').first().innerText()).trim();
  return { shiro, aka };
}

// The slot marks a side holds, in slot order ("M", "Ht", ...). A slot reads
// "<Side> slot <n>: remove M" (a point), ": Ht" (the judges' decision),
// ": M, kiken" (a point a decision has locked) or ": empty"
// (fixtures/scoring.mjs filledSlots).
export async function slotMarks(page, side, root = INLINE_EDITOR) {
  const name = side === 'shiro' ? 'Shiro' : 'Aka';
  const labels = await page.locator(root).first().locator(`[aria-label^="${name} slot "]`).evaluateAll(
    (els) => els.map((e) => e.getAttribute('aria-label')),
  );
  return labels.map((l) => (l.match(/: (?:remove )?([^,\s]+)(?:, .+)?$/) || [])[1]).filter((v) => v && v !== 'empty');
}

// The decision panel (DecisionPrompt, admin_scoring_shared.jsx) is inline in
// the editor, not a dialog.
export const decisionButton = (page, kind, root = INLINE_EDITOR) => page.locator(root).first()
  .getByTestId({ 'kiken-voluntary': 'scoring-modal-kiken-voluntary-button', 'kiken-injury': 'scoring-modal-kiken-injury-button', fusenpai: 'scoring-modal-fusenpai-button' }[kind]);
export const decisionPrompt = (page, root = INLINE_EDITOR) => page.locator(root).first().locator('form.decision-prompt');

// Open a decision, pick the side, optionally type a reason, and Record. The
// prompt picks no side for the operator and Record stays off until one is
// picked (bc-dsid), so `side` is required. Returns what the prompt showed: the
// side preselected (none), whether Record was off before the pick, and the
// label it read once the side was picked ("Record: AKA withdrew").
export async function recordDecision(page, kind, { side, reason, root = INLINE_EDITOR } = {}) {
  if (!side) throw new Error('recordDecision: pick a side; the prompt preselects none (bc-dsid)');
  await decisionButton(page, kind, root).tap();
  const form = decisionPrompt(page, root);
  await expect(form).toBeVisible();
  const checked = form.locator('input[name="decision-side"]:checked');
  const preselected = (await checked.count()) ? await checked.getAttribute('value') : null;
  const record = form.locator('button[type="submit"]');
  const recordDisabledBeforePick = await record.isDisabled();
  // The radio covers its pill, so the tap lands where the thumb does.
  const pick = form.locator(`input[name="decision-side"][value="${side}"]`);
  await pick.tap();
  await expect(pick).toBeChecked();
  if (reason) await form.getByTestId('decision-reason').fill(reason);
  const recordLabel = (await record.innerText()).trim();
  await record.tap();
  return { preselected, recordDisabledBeforePick, recordLabel, chosen: side };
}

// Is the inline editor holding a RUNNING bout (no PRE-MATCH / CORRECTION pill)?
export async function inlineRunning(page) {
  const ed = inlineEditor(page).first();
  if (!(await ed.isVisible().catch(() => false))) return false;
  const pill = ed.locator('.editor-head-pill');
  return (await pill.count()) === 0;
}

// Make sure the shiaijo page has a running bout in its editor, and return its
// pairing. When the court is idle ("Ready when you are") the Up next card's
// Start match is tapped, once. Returns null only when the court has nothing to
// start: idle with no startable Up next card (none, or one whose Start is off,
// e.g. a barred match), or done, held for a moment so a court between two
// renders is not taken for one with nothing left. Throws when Start was tapped
// and no bout runs by the deadline, or the console never settles.
const NOTHING_HELD_MS = 1_500;
export async function ensureRunning(page, { timeout = 15_000 } = {}) {
  const start = upNextCard(page).getByRole('button', { name: 'Start match' });
  let tappedAt = null;
  let nothingSince = null;
  let outcome = null;
  await expect(async () => {
    if (await inlineRunning(page)) { outcome = 'running'; return; }
    const idle = await courtIdle(page).isVisible().catch(() => false);
    const done = !idle && await courtDone(page).isVisible().catch(() => false);
    const startable = idle && (await start.count()) > 0 && await start.isEnabled({ timeout: 1_000 }).catch(() => false);
    if (tappedAt === null && startable) {
      // The card can leave under the tap (Finish + Start Next has just started
      // the match it shows): a tap that finds it gone did not happen, and the
      // next look reads the editor.
      if (await start.tap({ timeout: 2_000 }).then(() => true, () => false)) tappedAt = Date.now();
      throw new Error('Start match tapped on the Up next card; waiting for the bout to run');
    }
    if (tappedAt !== null) throw new Error(`Start match was tapped ${Date.now() - tappedAt} ms ago and no bout is running`);
    if (done || idle) {
      nothingSince ??= Date.now();
      if (Date.now() - nothingSince >= NOTHING_HELD_MS) { outcome = 'nothing'; return; }
      throw new Error('the court shows nothing to start; checking it stays so');
    }
    nothingSince = null;
    throw new Error('the court console shows neither a running bout, the idle placeholder nor the done state');
  }).toPass({ timeout, intervals: [250] });
  return outcome === 'running' ? editorSides(page) : null;
}

// Score the running bout on the shiaijo page for the deterministic winner (one
// men) and Finish (+ Start Next) through the two-tap guard, which returns once
// the write has landed and the editor has let the bout go. Returns the
// pairing.
export async function playRunningBout(page, { waza = 'M' } = {}) {
  const pair = await ensureRunning(page);
  if (!pair) throw new Error('playRunningBout: nothing running or startable on this court');
  const side = winnerOf(pair);
  await awardIppon(page, side, waza);
  await finishMatch(page);
  await expect(completedRowFor(page, pair)).toBeVisible();
  return { ...pair, winner: side };
}

// Open the correction of a completed bout on the shiaijo page, the way the
// operator now must. The console never holds a live bout and an open
// correction at once (bc-crpn): while a bout runs, Correct is refused with a
// notice at the row and a toast, and the notice says what to do: send the live
// bout back to the queue (its score is kept), then correct. Returns the bout
// that was live (null when the court was idle) and what the refusal said.
export async function correctCompleted(page, pair) {
  const done = completedRowFor(page, pair);
  const live = (await inlineRunning(page)) ? await editorSides(page) : null;
  await done.getByRole('button', { name: 'Correct' }).tap();
  let refusal = null;
  if (live) {
    const notice = done.locator('.shiaijo-upnext__error[role="alert"]');
    await expect(notice).toBeVisible();
    const toast = (await page.locator('.toast').allInnerTexts().catch(() => [])).join(' | ');
    refusal = {
      notice: (await notice.innerText()).trim(), toast,
      editorStillOnLiveBout: (await inlineRunning(page)) && JSON.stringify(await editorSides(page)) === JSON.stringify(live),
    };
    expect(refusal.notice).toMatch(/^Shiaijo \S+ is running .+\. Finish it or send it back to the queue first\. Then correct this match\.$/);
    await page.getByRole('button', { name: 'Send back to queue' }).tap();
    refusal.sendBack = await hastyShiaijoConfirm(page);
    await expect.poll(() => inlineRunning(page), { timeout: 10_000 }).toBe(false);
    await done.getByRole('button', { name: 'Correct' }).tap();
  }
  await expect(inlineEditor(page).first().locator('.editor-head-pill')).toHaveText('CORRECTION');
  return { live, refusal };
}

// How many POOL bouts still wait in this court's queue (the Up next card and
// the Upcoming rows; not Later, not Completed). A pool bout reads
// "Match n of m" in its header, a knockout bout "Match n".
export async function queuedPoolBouts(page) {
  const texts = await page.locator(`${UP_NEXT}, ${WAITING_ROWS}`).allInnerTexts();
  return texts.filter((t) => /Match \d+ of \d+/.test(t)).length;
}
