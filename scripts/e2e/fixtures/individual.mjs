// Helpers for the INDIVIDUAL operator journeys (knockout-mixed-individual
// spec): the rosters, a deterministic winner rule, the audit log, tap-target
// measurement, and the few surfaces the shared fixtures do not own (the
// overlay editor's identity line, the shiaijo page's own confirm dialogs, the
// decision panel). Owned by reviewer A; the shared fixtures stay untouched.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect } from '@playwright/test';
import { EDITOR, INLINE_EDITOR } from './scoring.mjs';

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
// Small rosters for the FINDING tests, which seed their own competitions so
// each can run alone once its defect is fixed.
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

// Audit rows. Every row is printed (so the run log carries the table) and kept
// for afterAll, which writes them to AUDIT_JSON, beside the journey's
// screenshots in the gitignored output folder, for the person writing up the
// review. Nothing here judges a row: judgement is by eye.
export const AUDIT_JSON = path.resolve(fileURLToPath(new URL('../output/knockout-mixed-individual/audit.json', import.meta.url)));
export function auditLog() {
  const rows = [];
  const record = (row) => {
    rows.push(row);
    console.log(`AUDIT ${JSON.stringify(row)}`);
  };
  const flush = () => {
    let prior = [];
    try { prior = JSON.parse(fs.readFileSync(AUDIT_JSON, 'utf8')); } catch { prior = []; }
    // A journey re-run replaces its own earlier rows and keeps the others.
    const journeys = new Set(rows.map((r) => r.journey));
    const kept = prior.filter((r) => !journeys.has(r.journey));
    fs.mkdirSync(path.dirname(AUDIT_JSON), { recursive: true });
    fs.writeFileSync(AUDIT_JSON, JSON.stringify([...kept, ...rows], null, 2));
  };
  return { record, flush, rows };
}

// A control's rendered size, for the 44px coarse-pointer floor.
export async function tapSize(locator) {
  const box = await locator.boundingBox();
  if (!box) return null;
  return { w: Math.round(box.width), h: Math.round(box.height), meets44: box.width >= 44 && box.height >= 44 };
}

// The overlay editor's identity: its eyebrow (competition, pool/round) and its
// title line ("Shiaijo B · 10:20"), which is what says which court it is on.
export async function overlayIdentity(page) {
  const ed = page.locator(EDITOR).first();
  const eyebrow = ((await ed.locator('.editor-modal__eyebrow').allInnerTexts())[0] || '').trim();
  const title = ((await ed.locator('.editor-modal__title').allInnerTexts())[0] || '').trim();
  return { eyebrow, title, court: (title.match(/Shiaijo\s+(\S+)/) || [])[1] || null };
}

// The same for the inline editor (shiaijo page, Bracket tab panel).
export async function inlineIdentity(page) {
  const ed = page.locator(INLINE_EDITOR).first();
  const eyebrow = ((await ed.locator('.editor-modal__eyebrow').allInnerTexts())[0] || '').trim();
  const title = ((await ed.locator('.editor-modal__title').allInnerTexts())[0] || '').trim();
  return { eyebrow, title, court: (title.match(/Shiaijo\s+(\S+)/) || [])[1] || null };
}

// The two names an editor shows, Shiro first.
export async function editorSides(page, root = INLINE_EDITOR) {
  const ed = page.locator(root).first();
  const shiro = (await ed.locator('.sb-side--shiro .numbered-name__text').first().innerText()).trim();
  const aka = (await ed.locator('.sb-side--aka .numbered-name__text').first().innerText()).trim();
  return { shiro, aka };
}

// The slot marks a side holds, in slot order ("M", "Ht", ...).
export async function slotMarks(page, side, root = INLINE_EDITOR) {
  const name = side === 'shiro' ? 'Shiro' : 'Aka';
  const labels = await page.locator(root).first().locator(`[aria-label^="${name} slot "]`).evaluateAll(
    (els) => els.map((e) => e.getAttribute('aria-label')),
  );
  return labels.map((l) => (l.match(/: (?:remove )?(\S+)$/) || [])[1]).filter((v) => v && v !== 'empty');
}

// The shiaijo page's own confirm dialogs (Send back to queue, Move court) are
// not the shared confirmDialog, so clumsy.hastyConfirm cannot see them. Same
// rule here: tap the most prominent button, and say what it was.
export async function hastyShiaijoConfirm(page) {
  const dialog = page.locator('.shiaijo-move-confirm[role="dialog"]').last();
  await dialog.waitFor({ state: 'visible' });
  const buttons = dialog.locator('.shiaijo-move-confirm__actions button');
  let best = null;
  const labels = [];
  for (let i = 0; i < await buttons.count(); i += 1) {
    const b = buttons.nth(i);
    const cls = (await b.getAttribute('class')) || '';
    const label = (await b.innerText()).trim();
    labels.push(label);
    const rank = /\bbtn--danger\b/.test(cls) ? 3 : /\bbtn--primary\b/.test(cls) ? 2 : /\bbtn--ghost\b/.test(cls) ? 0 : 1;
    if (!best || rank > best.rank) best = { b, label, rank };
  }
  const title = ((await dialog.locator('h3').allInnerTexts())[0] || '').trim();
  const message = ((await dialog.locator('p').allInnerTexts())[0] || '').trim();
  await best.b.tap();
  await dialog.waitFor({ state: 'hidden' });
  return { label: best.label, prominence: ['ghost', 'plain', 'primary', 'danger'][best.rank], otherLabels: labels.filter((l) => l !== best.label), title, message };
}

// The decision panel (DecisionPrompt, admin_scoring_shared.jsx) is inline in
// the editor, not a dialog.
export const decisionButton = (page, kind, root = INLINE_EDITOR) => page.locator(root).first()
  .getByTestId({ 'kiken-voluntary': 'scoring-modal-kiken-voluntary-button', 'kiken-injury': 'scoring-modal-kiken-injury-button', fusenpai: 'scoring-modal-fusenpai-button' }[kind]);
export const decisionPrompt = (page, root = INLINE_EDITOR) => page.locator(root).first().locator('form.decision-prompt');

// Open a decision, pick the side (or leave the default, the hasty operator's
// way when `side` is omitted), optionally type a reason, and Record.
export async function recordDecision(page, kind, { side, reason, root = INLINE_EDITOR } = {}) {
  await decisionButton(page, kind, root).tap();
  const form = decisionPrompt(page, root);
  await expect(form).toBeVisible();
  const defaultSide = await form.locator('input[name="decision-side"]:checked').getAttribute('value');
  if (side) await form.locator(`input[name="decision-side"][value="${side}"]`).check();
  if (reason) await form.getByTestId('decision-reason').fill(reason);
  await form.getByRole('button', { name: 'Record' }).tap();
  return { defaultSide, chosen: side || defaultSide };
}

// Is the inline editor holding a RUNNING bout (no PRE-MATCH / CORRECTION pill)?
export async function inlineRunning(page) {
  const ed = page.locator(INLINE_EDITOR).first();
  if (!(await ed.isVisible().catch(() => false))) return false;
  const pill = ed.locator('.editor-head-pill');
  return (await pill.count()) === 0;
}

// Make sure the shiaijo page has a running bout in its editor: when the court
// is idle ("Ready when you are"), start the Up next card's match. Returns the
// pairing, or null when the court has nothing left to start.
export async function ensureRunning(page) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (await inlineRunning(page)) return editorSides(page);
    const card = page.locator('.shiaijo-upnext__card');
    const idle = await page.locator('.shiaijo__placeholder').isVisible().catch(() => false);
    if (idle && await card.isVisible().catch(() => false)) {
      const start = card.getByRole('button', { name: 'Start match' });
      if (await start.isEnabled().catch(() => false)) await start.tap();
    }
    await page.waitForTimeout(250);
  }
  return null;
}

// Score the running bout on the shiaijo page for the deterministic winner (one
// men) and Finish (+ Start Next) through the two-tap guard. Returns the pairing.
export async function playRunningBout(page, { waza = 'M' } = {}) {
  const pair = await ensureRunning(page);
  if (!pair) throw new Error('playRunningBout: nothing running or startable on this court');
  const side = winnerOf(pair);
  const ed = page.locator(INLINE_EDITOR).first();
  await ed.locator(`.sb-side--${side} .ipt-btn`, { hasText: new RegExp(`^${waza}$`) }).first().tap();
  await expect(ed.locator('.sync-pill__label').first()).toHaveText('Synced');
  const finish = ed.locator('button').filter({ hasText: /^Finish( \+ Start Next|$)/ }).first();
  await finish.tap();
  const armed = ed.locator('button').filter({ hasText: /^Tap again to finish/ }).first();
  await armed.waitFor({ state: 'visible', timeout: 3000 });
  await armed.tap();
  await expect(completedRowFor(page, pair)).toBeVisible();
  // Wait for the editor to let go of the finished bout (it moves on to the
  // next one, or the court goes idle), so the caller never reads it as live.
  await expect.poll(async () => {
    if (!(await inlineRunning(page))) return 'moved';
    const now = await editorSides(page).catch(() => null);
    return now && now.shiro === pair.shiro && now.aka === pair.aka ? 'same' : 'moved';
  }, { timeout: 10_000 }).toBe('moved');
  return { ...pair, winner: side };
}

// How many POOL bouts still wait in this court's queue (the Up next card and
// the Upcoming rows; not Later, not Completed). A pool bout reads
// "Match n of m" in its header, a knockout bout "Match n".
export async function queuedPoolBouts(page) {
  const texts = await page.locator('.shiaijo-upnext__card, .shiaijo__queue .shiaijo-group:not(.shiaijo-pending) .shiaijo-qrow:not(.shiaijo-qrow--complete)')
    .allInnerTexts();
  return texts.filter((t) => /Match \d+ of \d+/.test(t)).length;
}

// The newest Completed row on the shiaijo page whose names match a pairing.
export function completedRowFor(page, { shiro, aka }) {
  return page.locator('.shiaijo-completed .shiaijo-qrow').filter({ hasText: shiro }).filter({ hasText: aka }).last();
}
