// Scoring an INDIVIDUAL bout on the kendo board (admin_scoring_individual.jsx).
// The board is the same whether it is mounted inline on the shiaijo page
// (INLINE_EDITOR) or as the overlay dialog the Scores tab opens (EDITOR), so
// every helper takes the editor's selector, the inline one by default. The
// team and kachinuki editors score differently and get their own helpers
// (fixtures/team.mjs, fixtures/kachinuki.mjs); they share the Finish check
// below.
//
// The editor selectors come from scripts/screenshots/lib/editor.mjs,
// re-exported here so a journey imports one scoring module. Start and Finish
// are this file's own, not that library's: a journey TAPS, as the operator does
// on the iPad, and must know the action LANDED (the library's startMatch clicks
// and returns silently when the button has not rendered yet; its finishMatch
// clicks, on the overlay only, and returns once the button stops saving).
// Finish also takes the inline editor as well as the overlay.
import { expect } from '@playwright/test';
import { EDITOR, INLINE_EDITOR, TAP_DWELL_MS } from '../../screenshots/lib/editor.mjs';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { armTwoTap, tapOnSide } from './pace.mjs';
import { courtIdle, editorIdentity } from './shiaijo.mjs';

export { EDITOR, INLINE_EDITOR };

// Start the match the OVERLAY editor holds and return once the start has
// landed: "Start match" is offered only while the match is scheduled, so the
// button leaving is the server having it running. Fails when the button never
// appears (the editor did not open on a scheduled match) or never leaves.
export async function startMatch(page) {
  const btn = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
  await expect(btn, 'the overlay editor offers Start match').toBeVisible();
  await btn.tap();
  await expect(btn, 'Start match left the editor once the start landed').toBeHidden();
}

// The most taps clearMarks makes on one side before it gives up. A side holds a
// couple of marks at most, so six leaves room for a tap or two that did not count.
export const MAX_CLEAR_TAPS = 6;

// Tap filled mark slots off until `slots` (a Locator of the FILLED slots, e.g.
// filledSlots or fixtures/team.mjs rowSlots) holds `keep` of them. Bounded: a
// mark that does not go away fails with a message instead of looping until the
// test timeout. A tap the app does not count (one inside the bounce window of
// the layer it just opened, such as a correction row, tap_guard.jsx) removes
// nothing, so after each tap the count is given TAP_DWELL_MS to drop, which
// outlasts that window, before the next tap is made; a tap that lands is not
// waited on. A count that goes UP between taps is a failure, never tapped
// away: a removed mark coming back is the bc-ltap class of defect. `from` is
// the end of the list the next tap takes ('last' or 'first'); `pauseMs` is the
// operator's beat between two removals. Returns the taps made.
export async function clearMarks(slots, { keep = 0, from = 'last', pauseMs = 0 } = {}) {
  let taps = 0;
  let last = null; // the count after the previous tap's wait
  const cameBack = (was, now) => new Error(`clearMarks: ${slots} went UP from ${was} to ${now} mark(s) after ${taps} tap(s) (wanted ${keep}): a removed mark came back`);
  for (let before = await slots.count(); before > keep; before = await slots.count()) {
    if (last !== null && before > last) throw cameBack(last, before);
    if (taps >= MAX_CLEAR_TAPS) {
      throw new Error(`clearMarks: ${slots} still holds ${before} mark(s) (wanted ${keep}) after ${MAX_CLEAR_TAPS} taps`);
    }
    await (from === 'first' ? slots.first() : slots.last()).tap();
    taps += 1;
    const tappedAt = Date.now();
    let now = await slots.count();
    while (now === before && Date.now() - tappedAt < TAP_DWELL_MS) {
      await slots.page().waitForTimeout(50);
      now = await slots.count();
    }
    if (now > before) throw cameBack(before, now);
    last = now;
    if (pauseMs) await slots.page().waitForTimeout(pauseMs);
  }
  return taps;
}

const SIDE_NAME = { shiro: 'Shiro', aka: 'Aka' };

// An ippon button: one waza letter (M, K, D, T, H, and S on naginata) on a
// side of the board. Matched whole, since a substring test would be ambiguous
// the day a two-letter waza appears.
export const ipponButton = (page, side, waza, root = INLINE_EDITOR) => page.locator(root)
  .locator(`.sb-side--${side} .ipt-btn`, { hasText: new RegExp(`^${waza}$`) }).first();

// The autosave indicator on the open editor ("Synced", "Syncing…",
// "Offline", ...; admin_scoring_autosave.jsx).
export const syncState = (page, root = INLINE_EDITOR) => page.locator(root).locator('.sync-pill__label').first();

// The slots a side's points fill, outside to inside, that hold a mark. A slot
// is labelled "<Side> slot <n>: remove <waza>" for a point (a tap clears it),
// "<Side> slot <n>: Ht" for the judges' decision, and "<Side> slot <n>:
// <waza>, <decision>" while a decision such as a kiken locks the scoreline;
// neither of the last two clears on a tap. An unfilled slot ends ": empty".
export const filledSlots = (page, side, root = INLINE_EDITOR) => page.locator(root)
  .locator(`[aria-label^="${SIDE_NAME[side]} slot "]:not([aria-label$=": empty"])`);

// Award one ippon and return once the board shows it and the autosave has
// landed. `side` is 'shiro' | 'aka'. A second strike for the same side waits
// out the bounce window of the first (fixtures/pace.mjs).
export async function awardIppon(page, side, waza = 'M', root = INLINE_EDITOR) {
  const before = await filledSlots(page, side, root).count();
  await tapOnSide(page, `${root} ${side}`, ipponButton(page, side, waza, root));
  await expect(filledSlots(page, side, root)).toHaveCount(before + 1);
  await expect(syncState(page, root)).toHaveText('Synced');
}

// The hikiwake toggle of the individual board ("Mark draw", "Cancel draw" while
// on). It is offered only while neither side holds a point. Returns once the
// board shows the draw.
export async function markDraw(page, root = INLINE_EDITOR) {
  const toggle = within(page, root).first().getByTestId('scoring-modal-mark-draw');
  await toggle.tap();
  await expect(toggle).toHaveText('Cancel draw');
}

// `root` is an editor's selector or its Locator (the team sheet's callers hold one).
const within = (page, root) => (typeof root === 'string' ? page.locator(root) : root);

// The Finish button in its first state ("Finish" or "Finish + Start Next →"),
// and the armed state the first tap turns it into.
export const finishButton = (page, root = INLINE_EDITOR) => within(page, root)
  .locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first();
export const armedFinishButton = (page, root = INLINE_EDITOR) => within(page, root)
  .locator('button').filter({ hasText: /^Tap again to finish/ }).first();

// The server's answer to the next write a match gets, for a journey that must
// not move on (reload, read the court) before an edit has been saved. The sync
// pill is process-wide, not per match: it says whether anything is still being
// written, not that THIS match's edit was, so it cannot stand in for the
// answer to the write itself. Call this BEFORE the tap that
// causes the write, then `await writeLanded(...)` after it. `tail` is the
// write's route below /matches/:id: 'score' (PUT, the default: marks, fusensho,
// Tie, Start, Finish) or 'daihyosen' (POST, the representative bout's add).
// A timeout the caller never reached is not an unhandled rejection.
export function nextMatchWrite(page, { method = 'PUT', tail = 'score' } = {}) {
  const answered = page.waitForResponse((r) => r.request().method() === method
    && new RegExp(`/matches/[^/]+/${tail}$`).test(new URL(r.url()).pathname), { timeout: EXPECT_TIMEOUT_MS });
  answered.catch(() => {});
  return answered;
}

// Return once that write has been answered with a success status.
export async function writeLanded(answered, what = 'the write') {
  const res = await answered;
  expect(res.ok(), `${what} was answered ${res.status()} ${res.url()}`).toBe(true);
  return res;
}

// Tap Finish once: the two-tap guard arms and nothing is submitted yet. Returns
// the armed button once the arm is old enough for a second tap to count.
export async function armFinish(page, root = INLINE_EDITOR) {
  return armTwoTap(finishButton(page, root), armedFinishButton(page, root));
}

// What an editor shows when a write did not save: its not-saved banner
// (notSavedText, the refused write's danger state) and an error alert such as
// the team sheet's "a bout has no result".
const NOT_SAVED = '.pending-write-banner--failed, .alert--error';
const FINISH_LABEL = /^(Finish( \+ Start Next|$)|Tap again to finish|Saving…)/;

const visibleTexts = (loc) => loc.evaluateAll((els) => els
  .filter((e) => e.getClientRects().length > 0)
  .map((e) => e.textContent.replace(/\s+/g, ' ').trim()));

// What an editor reads before a Finish, for finishLanded to compare against:
// the match it holds and any not-saved line already on screen. Then the
// Finish write's answer is listened for: call this BEFORE the tap that
// submits. The write is the PUT /score that completes the match.
export async function beforeFinish(ed) {
  const page = ed.page();
  const answer = page.waitForResponse((r) => r.request().method() === 'PUT' && /\/matches\/[^/]+\/score$/.test(r.url())
    && /"status":"completed"/.test(r.request().postData() || ''), { timeout: EXPECT_TIMEOUT_MS }).then((r) => r, () => null);
  return { ...(await editorIdentity(page, ed)), notSaved: await visibleTexts(ed.locator(NOT_SAVED)), answer };
}

// One reading of whether the editor has let the finished match go.
async function editorState(ed, before) {
  const page = ed.page();
  const fresh = (await visibleTexts(ed.locator(NOT_SAVED)).catch(() => [])).filter((t) => !before.notSaved.includes(t));
  if (fresh.length) return { kind: 'not saved', detail: `the editor says: ${fresh.join(' | ')}` };
  if (!(await ed.isVisible().catch(() => false))) return { kind: 'landed', detail: 'the editor closed' };
  if (await courtIdle(page).isVisible().catch(() => false)) return { kind: 'landed', detail: 'the court is idle' };
  const now = await editorIdentity(page, ed).catch(() => null);
  if (!now) return { kind: 'pending', detail: 'the editor is between two matches' };
  if (now.key !== before.key) return { kind: 'landed', detail: `the editor moved on to ${now.key}` };
  const pill = (await ed.locator('.editor-head-pill').allInnerTexts().catch(() => [])).join(' ').trim();
  if (pill) return { kind: 'landed', detail: `the match is no longer running (${pill})` };
  const labels = (await ed.locator('button').allInnerTexts()).map((t) => t.trim()).filter((t) => FINISH_LABEL.test(t));
  if (!labels.length) return { kind: 'landed', detail: 'Finish is no longer offered for the match' };
  return { kind: 'pending', detail: `the editor still holds the match and offers ${labels.join(', ')}` };
}

// Return once a Finish's second tap has LANDED, and fail if it did not. Two
// things are read, because neither is enough alone. The server's answer to
// the completing write: none within the expect timeout (queued, or never
// sent), an error status, or `applied: false` (the refusal shape:
// superseded, clock_skew) fails at once with what the server said; a refused
// write is also what makes an editor disarm Finish (writeWasRefused). Then
// the editor: it must let the match go (the overlay closes on a plain
// Finish; the inline editor holds another match or the court goes idle;
// Finish + Start Next holds the next match; or the editor keeps the match
// shown as finished), and it fails if a not-saved banner or a new error
// alert appears. The editor's own label is not taken as the answer: right
// after a landed Finish it can read plain "Finish" on the same match for a
// moment, until the list it is fed from catches up. `ed` is the editor
// Locator, `before` what beforeFinish read ahead of the submitting tap.
export async function finishLanded(ed, before) {
  const res = await before.answer;
  expect(res, `the Finish write on ${before.key} was answered by the server`).not.toBeNull();
  const body = await res.json().catch(() => null);
  const refused = res.status() >= 400 || (body && body.applied === false);
  expect(refused, `the server refused the Finish on ${before.key}: ${res.status()} ${JSON.stringify(body)}`).toBe(false);
  let state = { kind: 'pending' };
  await expect.poll(async () => {
    state = await editorState(ed, before);
    return state.kind;
  }, { message: `the editor let ${before.key} go after its Finish landed` }).not.toBe('pending');
  expect(state.kind, `after the Finish on ${before.key}: ${state.detail}`).toBe('landed');
  return state;
}

// Finish in two taps and return once the write has landed (finishLanded). The
// label is "Finish + Start Next →" instead whenever another match waits on the
// same shiaijo, and that form leaves the editor on the next match. Not for the
// engi editor ("Save result" arms too, but is its own button) or a correction
// ("Save correction" does not arm).
export async function finishMatch(page, root = INLINE_EDITOR) {
  const ed = within(page, root).first();
  const armed = await armFinish(page, root);
  const before = await beforeFinish(ed);
  await armed.tap();
  return finishLanded(ed, before);
}
