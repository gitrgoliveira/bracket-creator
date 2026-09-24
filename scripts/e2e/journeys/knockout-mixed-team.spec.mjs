// TEAM (non-kachinuki) journeys (mp-yqxn.2, reviewer B): lineup entry from all
// three entry points (J3), scoring a team encounter at the court (J4), and one
// result on every surface (J7). Everything is seeded through the interface.
//
//   F3  team, 3 per team, pools + knockout, six teams, shiaijo A+B
//   F4  team, 5 per team, knockout only (daihyosen is knockout-only)
//
// The clumsy operator's variants (fixtures/clumsy.mjs) are PERFORMED here and
// recorded as AUDIT lines (and audit.json per test); whether an outcome is
// acceptable is judged from the screenshots, never asserted. Only what the docs
// promise is asserted (docs/user-guide/court-operators/scoring-a-match.md,
// recording-decisions.md, organisers/team-tournaments.md). A step that
// exposes a functional defect is kept as its own self-contained
// test.fixme('<bead-id>: ...') below the journeys, so this file stays green
// and the step guards the fix once the fixme is removed.
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { login } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { inlineEditor, openShiaijo, sides, upNextCard, completedRows } from '../fixtures/shiaijo.mjs';
import { EDITOR } from '../fixtures/scoring.mjs';
import { openScoreEditor } from '../fixtures/scores.mjs';
import { openTvBoard, openViewer, tvBoard } from '../fixtures/public.mjs';
import { doubleTap, hastyConfirm, interrupt, retapWhileSaving, tapNeighbour } from '../fixtures/clumsy.mjs';
import * as T from '../fixtures/team.mjs';

const SIX_TEAMS = [
  ['Team Ryu', 'Kita Dojo'], ['Team Tora', 'Minami Dojo'], ['Team Kame', 'Higashi Dojo'],
  ['Team Tsuru', 'Nishi Dojo'], ['Team Taka', 'Kita Dojo'], ['Team Kuma', 'Minami Dojo'],
];
const FOUR_TEAMS = (tag) => [
  [`${tag} Hayabusa`, 'Kita Dojo'], [`${tag} Inoshishi`, 'Minami Dojo'],
  [`${tag} Kitsune`, 'Higashi Dojo'], [`${tag} Tanuki`, 'Nishi Dojo'],
];

// A recorder per test: every audit observation is printed as an AUDIT line and
// attached as audit.json, the raw material for the audit table.
function recorder(journey) {
  const rows = [];
  const record = (row) => {
    const r = { journey, ...row };
    rows.push(r);
    console.log(`AUDIT ${JSON.stringify(r)}`);
  };
  return { rows, record };
}

// F3: teams of three, pools then knockout, on shiaijo A and B.
async function seedF3(page, name = 'Team Mixed') {
  const id = await createCompetition(page, {
    name, kind: 'team', format: 'mixed', teamSize: 3, teamMatchType: 'fixed', courts: ['A', 'B'], numberPrefix: 'M',
  });
  await pasteRoster(page, id, SIX_TEAMS);
  await generateDraw(page, id);
  await startCompetition(page, id);
  return id;
}

// F4: teams of five, knockout only, on one shiaijo.
async function seedF4(page, { name, court, prefix, teams }) {
  const id = await createCompetition(page, {
    name, kind: 'team', format: 'knockout', teamSize: 5, teamMatchType: 'fixed', courts: [court], numberPrefix: prefix,
  });
  await pasteRoster(page, id, teams);
  await generateDraw(page, id);
  await startCompetition(page, id);
  return id;
}

// Name every position of `team`'s round-1 lineup on the Lineups page.
async function nameRound1(page, id, team, names, teamSize) {
  await T.openLineups(page, id, { team });
  const labels = T.positionLabels(teamSize);
  for (let i = 0; i < names.length; i += 1) await T.lineupsNameSlot(page, labels[i], names[i]);
  await T.lineupsSave(page);
}

test.describe.configure({ mode: 'serial' });

test.describe('knockout-mixed-team', () => {
  // ------------------------------------------------------------------- J3
  test('J3 lineup entry from the three entry points, on F3 and F4', async ({ page }, testInfo) => {
    test.setTimeout(600_000);
    const shot = journeyShots('J3-lineups');
    const { rows, record } = recorder('J3');
    await T.enterAdmin(page);

    let f3;
    let pair3;
    await test.step('F3: seed a three-person pools + knockout competition', async () => {
      f3 = await seedF3(page);
      await openShiaijo(page, 'A');
      pair3 = await sides(upNextCard(page));
    });

    // ---- Entry point 1: the Lineups page (round default)
    await test.step('F3 EP1 Lineups page: positions are bare numbers, name three slots', async () => {
      await T.openLineups(page, f3, { team: pair3.shiro });
      // teamSize 3 shows bare numbers (positionsForSize).
      for (const l of ['1', '2', '3']) await expect(T.lineupsSelect(page, l)).toBeVisible();
      await expect(T.lineupsSelect(page, 'Senpo')).toHaveCount(0);
      await shot(page, 'f3-lineups-fresh', { fullPage: true });

      // V2: double tap on "Add" (the confirm could open twice).
      await T.lineupsSelect(page, '1').selectOption('__add__');
      await T.lineupsForm(page).getByRole('textbox', { name: 'New member name for 1' }).fill('Aoki');
      const addBtn = T.lineupsForm(page).getByRole('button', { name: /^Add$/ });
      const addBox = await T.tapBox(addBtn);
      const dbl = await doubleTap(addBtn);
      await page.waitForTimeout(400);
      const dialogs = await page.locator('.modal[role="dialog"]').count();
      await shot(page, 'f3-lineups-add-doubletap-dialog');
      // The second tap may have landed on the dialog itself; if it closed it,
      // the operator taps Add again.
      if (!dialogs) await addBtn.tap();
      // V4: the confirm answered by its loudest button.
      const hasty = await hastyConfirm(page);
      const extraDialog = await page.locator('.modal[role="dialog"]').count();
      if (extraDialog) await hastyConfirm(page);
      await expect(T.lineupsSelect(page, '1').locator('option:checked')).toContainText('Aoki');
      record({ step: 'EP1 name slot 1', action: 'Add (name a blank slot)', variant: 'V2 doubleTap', ...dbl, dialogsOpen: dialogs, addTapBox: addBox });
      record({ step: 'EP1 name slot 1', action: '"Name slot" confirm', variant: 'V4 hastyConfirm', ...hasty, secondDialogAfter: extraDialog });
      const members = T.lineupsForm(page).locator('[data-testid^="squad-member-"]');
      record({ step: 'EP1 name slot 1', action: 'Add', variant: 'V2 result', membersListed: await members.count(),
        namedAoki: await members.filter({ hasText: 'Aoki' }).count() });

      await T.lineupsNameSlot(page, '2', 'Baba');
      await T.lineupsNameSlot(page, '3', 'Chiba');
      await shot(page, 'f3-lineups-named', { fullPage: true });
    });

    await test.step('F3 EP1 wrong fighter picked, then repaired; one member at two positions', async () => {
      // V1: the operator picks the reserve (the option under the intended one).
      await T.lineupsPick(page, '3', '.4');
      const wrong = await T.lineupsSelected(page, '3');
      // The list must not offer a member placed at another position.
      const offered3 = await T.lineupsSelect(page, '3').locator('option').allInnerTexts();
      await T.lineupsPick(page, '3', 'Chiba');
      record({ step: 'EP1 wrong fighter', action: 'pick position 3', variant: 'V1 wrong option (reserve)', picked: wrong,
        repairedTo: await T.lineupsSelected(page, '3'), tapsToRecover: 2, offeredAt3: offered3 });
      expect(offered3.some((o) => /Aoki|Baba/.test(o))).toBe(false);

      // Typing a name already placed elsewhere is refused with the position.
      const dup = await T.lineupsStartAdd(page, '2', 'Aoki');
      expect(dup.dialog).toBe(false);
      expect(dup.alert).toMatch(/Aoki is already at/);
      await shot(page, 'f3-lineups-duplicate-refused', { fullPage: true });
      record({ step: 'EP1 duplicate', action: 'type Aoki at 2 (Aoki at 1)', variant: 'refusal copy', copy: dup.alert });
      await T.lineupsForm(page).getByRole('button', { name: 'Cancel' }).tap();
    });

    await test.step('F3 EP1 interruption before Save, then Save re-tapped while saving', async () => {
      // V3: an unsaved change, then the page reloads.
      await T.lineupsPick(page, '3', '.5');
      const r = await interrupt(page, 'reload');
      await T.openLineups(page, f3, { team: pair3.shiro });
      const after = [await T.lineupsSelected(page, '1'), await T.lineupsSelected(page, '2'), await T.lineupsSelected(page, '3')];
      await shot(page, 'f3-lineups-after-reload-unsaved', { fullPage: true });
      record({ step: 'EP1 unsaved lineup', action: 'reload before Save', variant: 'V3 interrupt reload', ...r, positionsAfter: after,
        note: 'names were minted by the confirms; the position picks were never saved' });
      // Re-place the three and save, re-tapping Save while it reads "Saving…".
      await T.lineupsPick(page, '1', 'Aoki');
      await T.lineupsPick(page, '2', 'Baba');
      await T.lineupsPick(page, '3', 'Chiba');
      const saveBtn = T.lineupsForm(page).getByRole('button', { name: /Save lineup|Saving…/ });
      const re = await retapWhileSaving(saveBtn);
      await expect(page.getByText('Lineup saved').first()).toBeVisible();
      record({ step: 'EP1 save', action: 'Save lineup', variant: 'V2 retapWhileSaving', ...re, saveTapBox: await T.tapBox(saveBtn) });
      await page.reload();
      await T.openLineups(page, f3, { team: pair3.shiro });
      expect(await T.lineupsSelected(page, '1')).toContain('Aoki');
      expect(await T.lineupsSelected(page, '3')).toContain('Chiba');
      await shot(page, 'f3-lineups-saved', { fullPage: true });
    });

    // ---- Entry point 2: the at-court match panel (match override)
    await test.step('F3 EP2 match panel from Up next: what it shows of the round default', async () => {
      await openShiaijo(page, 'A');
      await T.openPanelFromUpNext(page);
      const shiroBoxes = await Promise.all(['1', '2', '3'].map((l) => T.panelInput(page, 0, l).inputValue()));
      const label = (await T.panelSide(page, 0).innerText()).includes('Inheriting round default');
      await shot(page, 'f3-panel-round-default');
      record({ step: 'EP2 open panel', action: 'Enter lineup (Up next)', variant: 'correct', inheritingLabel: label, shiroBoxes,
        roundDefaultOnLineupsPage: ['Aoki', 'Baba', 'Chiba'], note: 'bc-lpfb when the boxes are empty' });
    });

    await test.step('F3 EP2 Aka lineup typed in the panel; V1 neighbour of Aka Save; duplicate refusal', async () => {
      await T.typeIntoNameBox(T.panelInput(page, 1, '1'), 'Dai');
      await T.typeIntoNameBox(T.panelInput(page, 1, '2'), 'Eto');
      await T.typeIntoNameBox(T.panelInput(page, 1, '3'), 'Fuji');
      // Is a placed member offered again at another position? Open box 2.
      await T.panelInput(page, 1, '2').tap();
      const box2Options = await T.panelSide(page, 1).locator('.pmf__option').allInnerTexts();
      await T.dismissNameBox(page);
      await shot(page, 'f3-panel-aka-typed');
      // V1: the thumb lands left of Aka's Save lineup.
      const akaSave = T.panelSide(page, 1).getByRole('button', { name: 'Save lineup' });
      const nb = await tapNeighbour(akaSave, 'left');
      await page.waitForTimeout(800);
      const shiroState = (await T.panelSide(page, 0).innerText()).replace(/\s+/g, ' ');
      await shot(page, 'f3-panel-v1-neighbour-of-aka-save');
      record({ step: 'EP2 save Aka', action: 'Save lineup (Aka)', variant: 'V1 tapNeighbour left', ...nb, shiroSideAfter: shiroState.slice(0, 200),
        akaSaveTapBox: await T.tapBox(akaSave), box2OfferedWhileDaiAt1: box2Options });
      // Then the intended tap.
      await T.panelSave(page, 1);
      await expect(T.panelSide(page, 1)).toContainText('Override for this match');
      await shot(page, 'f3-panel-aka-saved');
      const rename = T.panelSide(page, 1).getByRole('button', { name: 'Rename 1 player' });
      record({ step: 'EP2 Rename', action: 'Rename control on the panel', variant: 'tap target', renameTapBox: await T.tapBox(rename) });

      // One member at two positions: type Dai (at 1) into 2 and save.
      await T.typeIntoNameBox(T.panelInput(page, 1, '2'), 'Dai');
      await T.panelSave(page, 1);
      const err = T.panelSide(page, 1).locator('.alert--error');
      await expect(err).toBeVisible();
      const copy = (await err.innerText()).trim();
      await shot(page, 'f3-panel-duplicate-refused');
      record({ step: 'EP2 duplicate', action: 'type Dai at 2 (Dai at 1), Save', variant: 'refusal copy', copy, note: 'bc-lprf when it names 2, the position being typed, not 1' });
      expect(copy).toMatch(/Dai is already at/);
      // Repair: put Eto back.
      await T.pickFromNameBox(T.panelInput(page, 1, '2'), 'Eto');
      await T.panelSave(page, 1);
      await expect(T.panelSide(page, 1).locator('.alert--error')).toHaveCount(0);
      record({ step: 'EP2 duplicate', action: 'repair', variant: 'recover', tapsToRecover: 3 });
    });

    await test.step('F3 EP2 V1: Save lineup tapped on the SHIRO side, whose boxes the panel showed empty', async () => {
      // The thumb meant Aka's Save and landed on Shiro's, whose boxes read
      // empty even though the Lineups page holds a round default.
      const shiroSave = T.panelSide(page, 0).getByRole('button', { name: 'Save lineup' });
      await shiroSave.tap();
      await page.waitForTimeout(800);
      const shiroLabel = (await T.panelSide(page, 0).innerText()).includes('Override for this match');
      await shot(page, 'f3-panel-shiro-empty-saved');
      await T.closePanel(page);
      // What the score sheet now shows for Shiro's first position.
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      const ed = inlineEditor(page);
      await expect(T.boutRow(ed, 1)).toBeVisible();
      const r1 = await T.rowNameBox(T.boutRow(ed, 1), 'shiro').inputValue();
      const r1aka = await T.rowNameBox(T.boutRow(ed, 1), 'aka').inputValue();
      await shot(page, 'f3-sheet-after-shiro-empty-save');
      record({ step: 'EP2 wrong Save', action: 'Save lineup (Shiro, boxes shown empty)', variant: 'V1 wrong side', shiroOverride: shiroLabel,
        sheetRow1Shiro: r1, sheetRow1Aka: r1aka, roundDefaultRow1: 'Aoki' });
    });

    // ---- Entry point 3: the score sheet row
    await test.step('F3 EP3 score sheet: pick the wrong fighter, repair, one member at two rows', async () => {
      const ed = inlineEditor(page);
      const row1 = T.boutRow(ed, 1);
      const box = T.rowNameBox(row1, 'shiro');
      const boxTap = await T.tapBox(row1.locator('.team-sub-match__side--shiro .lineup-name__bar'));
      // Whatever row 1 shows now, name it Aoki first (the intended fighter).
      await box.tap();
      await row1.locator('.pmf__option').first().waitFor({ state: 'visible', timeout: 3000 }).catch(() => {});
      const offered = await row1.locator('.pmf__option').allInnerTexts();
      await shot(page, 'f3-sheet-row1-list');
      await T.dismissNameBox(page);
      record({ step: 'EP3 open row 1 list', action: 'tap row 1 Shiro name box', variant: 'correct', offered });
      // Name row 1 Aoki: from the list when it offers her, else by typing.
      if (offered.some((o) => o.includes('Aoki'))) await T.pickFromNameBox(box, 'Aoki');
      else await T.typeIntoNameBox(box, 'Aoki');
      await expect(box).toHaveValue('Aoki');
      // V1: the thumb picks the option under Aoki's.
      await box.tap();
      const opts = row1.locator('.pmf__option');
      const texts = await opts.allInnerTexts();
      const aokiIdx = texts.findIndex((t) => t.includes('Aoki'));
      const wrongOpt = opts.nth(Math.min(aokiIdx + 1, texts.length - 1));
      const wrongText = (await wrongOpt.innerText()).trim();
      await wrongOpt.tap();
      await page.waitForTimeout(600);
      const afterWrong = await box.inputValue();
      const labelAfterWrong = (await T.rowMemberLabel(row1, 'shiro').innerText().catch(() => '')).trim();
      await shot(page, 'f3-sheet-wrong-fighter');
      // Repair by picking Aoki again.
      await T.pickFromNameBox(box, 'Aoki');
      await expect(box).toHaveValue('Aoki');
      record({ step: 'EP3 wrong fighter', action: 'pick row 1 Shiro', variant: 'V1 option below', offered, picked: wrongText,
        boxAfterWrong: afterWrong, labelAfterWrong, repaired: await box.inputValue(), tapsToRecover: 2, boxTapBox: boxTap });

      // Re-enter rows 2 and 3 (the empty Shiro override left them blank):
      // the operator's cost of the wrong-side Save above.
      await T.pickFromNameBox(T.rowNameBox(T.boutRow(ed, 2), 'shiro'), 'Baba');
      await expect(T.rowNameBox(T.boutRow(ed, 2), 'shiro')).toHaveValue('Baba');
      await T.pickFromNameBox(T.rowNameBox(T.boutRow(ed, 3), 'shiro'), 'Chiba');
      await expect(T.rowNameBox(T.boutRow(ed, 3), 'shiro')).toHaveValue('Chiba');
      // One member at two rows: type Baba (row 2) into row 1.
      await T.typeIntoNameBox(box, 'Baba');
      await page.waitForTimeout(800);
      const err = ed.getByTestId('team-editor-lineup-warning');
      const errText = (await err.count()) ? (await err.innerText()).trim() : '';
      await shot(page, 'f3-sheet-duplicate');
      record({ step: 'EP3 duplicate', action: 'type Baba into row 1 (Baba at row 2)', variant: 'refusal copy', copy: errText,
        row1After: await box.inputValue(), row2After: await T.rowNameBox(T.boutRow(ed, 2), 'shiro').inputValue() });
      expect(errText).toMatch(/Baba is already at/);
      if ((await box.inputValue()) !== 'Aoki') await T.pickFromNameBox(box, 'Aoki');
    });

    await test.step('F3 the three entry points side by side after the edits', async () => {
      const ed = inlineEditor(page);
      const sheet = [];
      for (const n of [1, 2, 3]) sheet.push(await T.rowNameBox(T.boutRow(ed, n), 'shiro').inputValue());
      await shot(page, 'f3-sheet-final');
      await T.openLineups(page, f3, { team: pair3.shiro });
      const lineupsPage = [await T.lineupsSelected(page, '1'), await T.lineupsSelected(page, '2'), await T.lineupsSelected(page, '3')];
      record({ step: 'three entry points', action: 'compare Shiro positions', variant: 'consistency', sheet, lineupsPage });
    });

    await test.step('F3 V4: a sixth member through "+ Add new member…" while two reserve slots are blank', async () => {
      // Position 3 of the Aka team holds Fuji (slot .3); its reserves .4 and
      // .5 have no name yet. The operator adds a substitute at position 3.
      await T.openLineups(page, f3, { team: pair3.aka });
      const before = await T.lineupsForm(page).locator('[data-testid^="squad-member-"]').count();
      const r = await T.lineupsStartAdd(page, '3', 'Goto');
      let hasty = null;
      if (r.dialog) {
        await shot(page, 'f3-lineups-add-member-confirm');
        hasty = await hastyConfirm(page);
      }
      await page.waitForTimeout(800);
      const after = await T.lineupsForm(page).locator('[data-testid^="squad-member-"]').count();
      await shot(page, 'f3-lineups-after-add-member', { fullPage: true });
      record({ step: 'EP1 add a substitute', action: '+ Add new member… at 3 (reserves .4/.5 blank)', variant: 'V4 hastyConfirm',
        ...(hasty || {}), membersBefore: before, membersAfter: after, alert: r.alert });
    });

    await test.step('F3: finish the pool encounter with bout 3 left unfought; what the standings count', async () => {
      await openShiaijo(page, 'A');
      const ed = inlineEditor(page);
      await expect(T.boutRow(ed, 1)).toBeVisible();
      const pairNow = await sides(ed);
      for (const n of [1, 2, 3]) {
        for (const side of ['shiro', 'aka']) {
          while (await T.rowSlots(T.boutRow(ed, n), side).count()) await T.rowSlots(T.boutRow(ed, n), side).first().tap();
        }
      }
      await T.boutIppon(ed, 1, 'shiro', 'M');
      await T.ensureTie(ed, 2);
      // Bout 3 is not fought (no fighter on either side). Whatever the sheet
      // shows for it is left alone.
      await page.waitForTimeout(1200);
      const sheet = await T.sheetState(ed, 3);
      await shot(page, 'f3-pool-encounter-bout3-unfought');
      await T.finishTeam(ed);
      await page.goto(`/admin/competition/${f3}/pools`);
      await page.waitForTimeout(1500);
      await shot(page, 'f3-pools-standings', { fullPage: true });
      const standings = await page.locator('tr').filter({ hasText: pairNow.shiro }).first().innerText().catch(() => '');
      const standingsAka = await page.locator('tr').filter({ hasText: pairNow.aka }).first().innerText().catch(() => '');
      const header = await page.locator('tr').filter({ hasText: /\bIT\b/ }).first().innerText().catch(() => '');
      record({ step: 'pool standings', action: 'Finish with bout 1 won, bout 2 drawn, bout 3 unfought', variant: 'consistency', pair: pairNow, sheet,
        header: header.replace(/\s+/g, ' '), shiroRow: standings.replace(/\s+/g, ' '), akaRow: standingsAka.replace(/\s+/g, ' '),
        note: 'B3: IT counts the unfought bout 3 as a draw when it reads 2' });
    });

    await test.step('F3: a team kiken in the pool and the default-win chain for its remaining matches', async () => {
      await openShiaijo(page, 'A');
      const ed = inlineEditor(page);
      if (!(await T.boutRow(ed, 1).isVisible().catch(() => false))) await T.startUpNextTeam(page);
      await expect(T.boutRow(ed, 1)).toBeVisible();
      const pair = await sides(ed);
      const summary = ed.locator('.decision-disclosure__summary');
      await summary.scrollIntoViewIfNeeded();
      await summary.tap();
      await ed.getByTestId('scoring-modal-kiken-voluntary-button').tap();
      const prompt = ed.locator('form.decision-prompt');
      await prompt.locator('input[value="shiro"]').check();
      await prompt.locator('button.btn--primary').tap();
      const panel = ed.locator('.remaining-matches');
      const shown = await panel.waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
      const panelText = shown ? (await panel.innerText({ timeout: 2000 }).catch(() => '')).replace(/\s+/g, ' ') : '';
      await shot(page, 'f3-kiken-remaining-matches');
      // Does it stay long enough to act on? The operator reads it first.
      await page.waitForTimeout(1500);
      const stillThere = await panel.isVisible().catch(() => false);
      const editorNow = await sides(ed).catch(() => null);
      await shot(page, 'f3-kiken-1500ms-later');
      record({ step: 'pool kiken chain', action: 'the remaining-matches panel, 1.5 s later', variant: 'V5 lost place', stillThere, editorNow,
        upNext: await sides(upNextCard(page)).catch(() => null) });
      const award = panel.getByRole('button', { name: 'Award default win to opponent' });
      const awards = await award.count();
      let nb = null;
      if (awards) {
        nb = await tapNeighbour(award.first(), 'up').catch((e) => ({ error: String(e.message).slice(0, 120) }));
        await page.waitForTimeout(800);
      }
      const panelAfterSlip = await panel.isVisible().catch(() => false);
      await shot(page, 'f3-kiken-after-neighbour-tap');
      record({ step: 'pool kiken chain', action: `Kiken – Voluntary, Shiro (${pair.shiro}) withdraws`, variant: 'correct', panelShown: shown,
        panelText: panelText.slice(0, 300), awardButtons: awards, awardTapBox: awards ? await T.tapBox(award.first()).catch(() => null) : null });
      record({ step: 'pool kiken chain', action: 'Award default win to opponent', variant: 'V1 tapNeighbour up', ...(nb || {}), panelStillOpen: panelAfterSlip });
      let awarded = 0;
      if (panelAfterSlip) {
        while (await award.count()) {
          const dbl = awarded === 0 ? await doubleTap(award.first()) : (await award.first().tap(), null);
          if (dbl) record({ step: 'pool kiken chain', action: 'Award default win to opponent', variant: 'V2 doubleTap', ...dbl });
          awarded += 1;
          await page.waitForTimeout(1200);
          if (awarded > 4) break;
        }
      }
      await shot(page, 'f3-kiken-after-awards');
      await page.goto(`/admin/competition/${f3}/pools`);
      await page.waitForTimeout(1500);
      await shot(page, 'f3-pools-after-kiken-chain', { fullPage: true });
      const matchesText = (await page.locator('body').innerText()).replace(/\s+/g, ' ');
      const i = matchesText.indexOf('Pool A');
      record({ step: 'pool kiken chain', action: 'Pools tab after the chain', variant: 'consistency', awardsTapped: awarded,
        poolA: matchesText.slice(i, i + 700), note: 'bc-kpnl when the panel vanished and the match is still scheduled' });
      // The operator carries on at the court: Up next now holds the
      // withdrawn team's remaining match.
      await openShiaijo(page, 'A');
      const upNext = await sides(upNextCard(page)).catch(() => null);
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap().catch(() => {});
      await page.waitForTimeout(1500);
      const msgs = (await page.locator('.alert, [role="alert"], .toast, [class*="toast"]').allInnerTexts().catch(() => [])).join(' | ').replace(/\s+/g, ' ');
      await shot(page, 'f3-start-withdrawn-team-match');
      record({ step: 'pool kiken chain', action: 'Start match on the withdrawn team\'s remaining match', variant: 'V5 lost place', upNext,
        messages: msgs.slice(0, 300), editorOpen: await T.boutRow(inlineEditor(page), 1).isVisible().catch(() => false) });
    });

    // ---- F4: five-person teams, FIK position names
    let f4;
    let pair4;
    await test.step('F4: seed a five-person knockout on shiaijo C and name Shiro on the Lineups page', async () => {
      f4 = await seedF4(page, { name: 'Team KO J3', court: 'C', prefix: 'K', teams: FOUR_TEAMS('J3') });
      await openShiaijo(page, 'C');
      pair4 = await sides(upNextCard(page));
      await T.openLineups(page, f4, { team: pair4.shiro });
      // teamSize 5 shows the FIK position names.
      for (const l of T.FIK5) await expect(T.lineupsSelect(page, l)).toBeVisible();
      await shot(page, 'f4-lineups-fik-names', { fullPage: true });
      await nameRound1(page, f4, pair4.shiro, ['Ito', 'Kato', 'Sato', 'Mori', 'Ueda'], 5);
      const dup = await T.lineupsStartAdd(page, 'Taisho', 'Ito');
      expect(dup.alert).toMatch(/Ito is already at Senpo/);
      await shot(page, 'f4-lineups-duplicate-refused', { fullPage: true });
      record({ step: 'F4 EP1 duplicate', action: 'type Ito at Taisho (Ito at Senpo)', variant: 'refusal copy', copy: dup.alert });
      await T.lineupsForm(page).getByRole('button', { name: 'Cancel' }).tap();
    });

    await test.step('F4 EP2 panel on a first-round (semifinal) match: FIK names and the round default', async () => {
      await openShiaijo(page, 'C');
      await T.openPanelFromUpNext(page);
      for (const l of T.FIK5) await expect(T.panelInput(page, 0, l)).toBeVisible();
      const shiroBoxes = await Promise.all(T.FIK5.map((l) => T.panelInput(page, 0, l).inputValue()));
      await shot(page, 'f4-panel-fik-names');
      record({ step: 'F4 EP2 open panel', action: 'Enter lineup (semifinal)', variant: 'correct', shiroBoxes });
      // Aka: pick numbered blank slots by number, name them later on the sheet.
      await T.pickFromNameBox(T.panelInput(page, 1, 'Senpo'), '.1');
      const pickedLabel = (await T.panelSide(page, 1).locator('.pmf__opt-label').first().innerText().catch(() => '')).trim();
      await T.panelSave(page, 1);
      await shot(page, 'f4-panel-aka-senpo-by-number');
      record({ step: 'F4 EP2 pick by number', action: 'pick Aka Senpo slot .1 (no name)', variant: 'correct', pickedLabel });
      await T.closePanel(page);
    });

    await test.step('F4 EP3 score sheet: FIK row aria-labels, name the picked slot by typing', async () => {
      await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      const ed = inlineEditor(page);
      const row1 = T.boutRow(ed, 1);
      await expect(row1.getByRole('textbox', { name: 'Senpo SHIRO player' })).toBeVisible();
      await expect(row1.getByRole('textbox', { name: 'Senpo AKA player' })).toBeVisible();
      const shiroRow1 = await T.rowNameBox(row1, 'shiro').inputValue();
      await T.typeIntoNameBox(T.rowNameBox(row1, 'aka'), 'Nakamura');
      await page.waitForTimeout(800);
      const akaLabel = (await T.rowMemberLabel(row1, 'aka').innerText()).trim();
      await shot(page, 'f4-sheet-row1');
      record({ step: 'F4 EP3 sheet', action: 'type Nakamura into Aka Senpo (picked slot .1)', variant: 'correct', shiroRow1, akaLabel,
        akaName: await T.rowNameBox(row1, 'aka').inputValue() });
      expect(akaLabel).toMatch(/\.1$/);
      // The name lands on the Lineups page's member list (the same member).
      await T.openLineups(page, f4, { team: pair4.aka });
      await expect(T.lineupsForm(page).locator('[data-testid^="squad-member-"]').filter({ hasText: 'Nakamura' })).toHaveCount(1);
      await shot(page, 'f4-lineups-aka-member-named-from-sheet', { fullPage: true });
    });

    await testInfo.attach('audit.json', { body: JSON.stringify(rows, null, 2), contentType: 'application/json' });
  });
  // ------------------------------------------------------------------- J4
  test('J4 team scoring at the court (F4)', async ({ page }, testInfo) => {
    test.setTimeout(600_000);
    const shot = journeyShots('J4-team-scoring');
    const { rows, record } = recorder('J4');
    await T.enterAdmin(page);
    const id = await seedF4(page, { name: 'Team KO J4', court: 'D', prefix: 'S', teams: FOUR_TEAMS('J4') });
    await openShiaijo(page, 'D');
    const ed = inlineEditor(page);
    let sf1;

    await test.step('start the first semifinal from Up next (double tap on Start match)', async () => {
      sf1 = await sides(upNextCard(page));
      const start = upNextCard(page).getByRole('button', { name: 'Start match' });
      const startBox = await T.tapBox(start);
      const dbl = await doubleTap(start);
      await expect(T.boutRow(ed, 1)).toBeVisible();
      await page.waitForTimeout(800);
      const identity = (await ed.locator('.editor-modal__eyebrow').first().innerText()).trim();
      const upNextNow = await sides(upNextCard(page)).catch(() => null);
      await shot(page, 'sf1-started');
      record({ step: 'start SF1', action: 'Start match (Up next)', variant: 'V2 doubleTap', ...dbl, startTapBox: startBox, editorHolds: identity,
        upNextAfter: upNextNow, errorsShown: await page.locator('.alert--error, [role="alert"]').allInnerTexts() });
      expect(await sides(ed)).toEqual(sf1);
    });

    await test.step('bout 1: Shiro men; a double tap on kote; clear the extra mark', async () => {
      const row = T.boutRow(ed, 1);
      await T.boutIppon(ed, 1, 'shiro', 'M');
      const k = T.rowIppon(row, 'shiro', 'K');
      const ipponBox = await T.tapBox(k);
      const dbl = await doubleTap(k);
      await expect(T.syncPill(ed)).toHaveText('Synced');
      const slotsAfter = await T.rowSlots(row, 'shiro').allInnerTexts();
      await shot(page, 'bout1-double-tap-kote');
      // Recover: tap the extra mark in the centre (the sheet's own hint).
      const hint = (await ed.getByTestId('team-scoring-clear-hint').innerText().catch(() => '')).trim();
      const extra = T.rowSlots(row, 'shiro').filter({ hasText: /^K$/ });
      const extraCount = await extra.count();
      for (let i = 0; i < extraCount; i += 1) await T.rowSlots(row, 'shiro').filter({ hasText: /^K$/ }).first().tap();
      await expect(T.rowSlots(row, 'shiro')).toHaveCount(1);
      await expect(T.syncPill(ed)).toHaveText('Synced');
      record({ step: 'bout 1', action: 'Shiro kote', variant: 'V2 doubleTap', ...dbl, slotsAfter, hint, tapsToRecover: extraCount, ipponTapBox: ipponBox,
        slotTapBox: await T.tapBox(T.rowSlots(row, 'shiro').first()) });
      expect(await T.rowSlots(row, 'shiro').allInnerTexts()).toEqual(['M']);
    });

    await test.step('bout 2: Aka men, the thumb lands on the neighbouring control first', async () => {
      const row = T.boutRow(ed, 2);
      const intended = T.rowIppon(row, 'aka', 'M');
      const nb = await tapNeighbour(intended, 'left');
      await page.waitForTimeout(600);
      const tieOn = /✓/.test((await T.rowTie(row).innerText().catch(() => '')) || '');
      const middle = (await T.rowMiddle(row).innerText()).trim();
      await shot(page, 'bout2-neighbour-tap');
      // Undo: tap the same control again when it toggled something.
      let undoTaps = 0;
      if (tieOn) { await T.rowTie(row).tap(); undoTaps += 1; }
      const shiroSlots = await T.rowSlots(row, 'shiro').count();
      for (let i = 0; i < shiroSlots; i += 1) { await T.rowSlots(row, 'shiro').first().tap(); undoTaps += 1; }
      await expect(T.syncPill(ed)).toHaveText('Synced');
      record({ step: 'bout 2', action: 'Aka men', variant: 'V1 tapNeighbour left', ...nb, tieToggled: tieOn, middleAfter: middle, tapsToRecover: undoTaps });
      await T.boutIppon(ed, 2, 'aka', 'M');
      const totals = await T.teamTotals(ed);
      record({ step: 'running total', action: 'after bouts 1-2', variant: 'correct', ...totals, sheet: await T.sheetState(ed) });
      // B12: how many bout rows fit the operator's screen.
      const coarse = await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
      expect(coarse).toBe(true);
      await page.evaluate(() => window.scrollTo(0, 0));
      const layout = await page.evaluate(() => {
        const rows = [...document.querySelectorAll('.scoring-panel .team-sub-match')].map((r) => r.getBoundingClientRect());
        const vh = innerHeight;
        const visible = rows.reduce((n, r) => n + Math.max(0, Math.min(r.bottom, vh) - Math.max(r.top, 0)) / r.height, 0);
        const band = document.querySelector('.scoring-panel .team-summary')?.getBoundingClientRect();
        return { rowHeights: rows.map((r) => Math.round(r.height)), boutsVisibleAtTop: Math.round(visible * 10) / 10,
          bandTop: band ? Math.round(band.top) : null, viewport: vh };
      });
      record({ step: 'layout', action: 'bouts per screen at 1180x820', variant: 'R6 glance', coarse, ...layout });
      expect(totals.shiro).toBe('IV: 1 · PW: 1');
      expect(totals.aka).toBe('IV: 1 · PW: 1');
      await shot(page, 'after-bout2-totals');
    });

    await test.step('bout 3: fusensho toggled, untoggled (prior score back), then across a reload', async () => {
      const row = T.boutRow(ed, 3);
      await T.boutIppon(ed, 3, 'shiro', 'K');
      await T.rowFusensho(row, 'aka').tap();
      await expect(T.rowSlots(row, 'aka')).toHaveCount(2);
      await expect(T.syncPill(ed)).toHaveText('Synced');
      const onAka = await T.rowSlots(row, 'aka').allInnerTexts();
      await shot(page, 'bout3-fusensho-aka');
      await T.rowFusensho(row, 'aka').tap();
      await expect(T.syncPill(ed)).toHaveText('Synced');
      const restored = await T.rowSlots(row, 'shiro').allInnerTexts();
      record({ step: 'bout 3', action: 'Fusensho Aka, then untoggle', variant: 'correct', fusenshoMarks: onAka, shiroAfterUntoggle: restored,
        fusenshoTapBox: await T.tapBox(T.rowFusensho(row, 'aka')) });
      expect(restored).toEqual(['K']);
      await expect(T.rowSlots(row, 'aka')).toHaveCount(0);

      // V3: fusensho, the operator looks away (the write lands), the page
      // reloads, then the operator untoggles "from memory".
      await T.rowFusensho(row, 'aka').tap();
      await page.waitForTimeout(1500);
      await expect(T.syncPill(ed)).toHaveText('Synced');
      const r = await interrupt(page, 'reload');
      await expect(T.boutRow(ed, 3)).toBeVisible();
      await page.waitForTimeout(1500);
      const afterReload = {
        shiro: await T.rowSlots(T.boutRow(ed, 3), 'shiro').allInnerTexts(),
        aka: await T.rowSlots(T.boutRow(ed, 3), 'aka').allInnerTexts(),
        fusenshoLabel: (await T.rowFusensho(T.boutRow(ed, 3), 'aka').innerText()).trim(),
        middle: (await T.rowMiddle(T.boutRow(ed, 3)).innerText()).trim(),
      };
      await T.boutRow(ed, 3).scrollIntoViewIfNeeded();
      await shot(page, 'bout3-after-reload-before-untoggle');
      await T.rowFusensho(T.boutRow(ed, 3), 'aka').tap();
      await page.waitForTimeout(1200);
      const afterUntoggle = { shiro: await T.rowSlots(T.boutRow(ed, 3), 'shiro').allInnerTexts(), aka: await T.rowSlots(T.boutRow(ed, 3), 'aka').allInnerTexts() };
      await shot(page, 'bout3-untoggle-after-reload');
      record({ step: 'bout 3', action: 'Fusensho Aka, reload, untoggle', variant: 'V3 interrupt reload', ...r, afterReload, afterUntoggle, sheet: await T.sheetState(ed),
        expected: 'Shiro K restored (the control title promises the previous score)', note: 'bc-fsnp when Shiro is empty' });
      // Make bout 3 a clean hikiwake for the tie that follows.
      for (const side of ['shiro', 'aka']) {
        while (await T.rowSlots(T.boutRow(ed, 3), side).count()) await T.rowSlots(T.boutRow(ed, 3), side).first().tap();
      }
      if (/✓/.test(await T.rowFusensho(T.boutRow(ed, 3), 'aka').innerText())) await T.rowFusensho(T.boutRow(ed, 3), 'aka').tap();
      await T.ensureTie(ed, 3);
    });

    await test.step('bout 4: a point entered offline, a point then an instant reload, the tab hidden, Back pressed', async () => {
      const identity = (await ed.locator('.editor-modal__eyebrow').first().innerText()).trim();
      // What the unfought bout 4 shows before anyone has touched it.
      const untouched4 = { middle: (await T.rowMiddle(T.boutRow(ed, 4)).innerText()).trim(), tieLabel: (await T.rowTie(T.boutRow(ed, 4)).innerText()).trim() };
      await T.boutRow(ed, 4).scrollIntoViewIfNeeded();
      await shot(page, 'bout4-before-it-is-fought');
      record({ step: 'bout 4', action: 'look at a bout not yet fought', variant: 'V5 which bout is live', untouched4, sheet: await T.sheetState(ed),
        note: 'bc-unfx when an unfought bout reads X / ✓ Tie' });
      let offlinePill;
      const off = await interrupt(page, 'offline', {
        during: async (p) => {
          await T.rowIppon(T.boutRow(inlineEditor(p), 4), 'aka', 'D').tap();
          await expect(T.syncPill(inlineEditor(p))).not.toHaveText('Synced', { timeout: 3000 }).catch(() => {});
          offlinePill = (await T.syncPill(inlineEditor(p)).innerText()).trim();
          await shot(p, 'bout4-offline');
        },
      });
      const resynced = await expect(T.syncPill(ed)).toHaveText('Synced').then(() => true, () => false);
      record({ step: 'bout 4', action: 'Aka do while offline', variant: 'V3 interrupt offline', ...off, pillWhileOffline: offlinePill, resynced, sheet: await T.sheetState(ed) });
      // A second strike, then the page reloads at once (inside the autosave's
      // debounce): what survives?
      await T.rowIppon(T.boutRow(ed, 4), 'aka', 'K').tap();
      const pillAtReload = (await T.syncPill(ed).innerText()).trim();
      const quick = await interrupt(page, 'reload');
      await expect(T.boutRow(ed, 4)).toBeVisible();
      await page.waitForTimeout(1500);
      record({ step: 'bout 4', action: 'Aka kote, reload within 300ms', variant: 'V3 interrupt reload (instant)', ...quick, pillAtReload,
        akaAfter: await T.rowSlots(T.boutRow(ed, 4), 'aka').allInnerTexts(), expected: ['D', 'K'], note: 'bc-sync when K is gone' });
      const hidden = await interrupt(page, 'hidden');
      record({ step: 'bout 4', action: 'tab hidden and back', variant: 'V3 interrupt hidden', ...hidden,
        sameMatch: (await ed.locator('.editor-modal__eyebrow').first().innerText()).trim() === identity,
        akaKept: await T.rowSlots(T.boutRow(ed, 4), 'aka').allInnerTexts() });
      const back = await interrupt(page, 'back');
      const edBack = await T.boutRow(ed, 4).waitFor({ state: 'visible', timeout: 15000 }).then(() => true, () => false);
      await shot(page, 'bout4-after-back');
      record({ step: 'bout 4', action: 'Back then Forward', variant: 'V3 interrupt back + V5 lost place', ...back, editorBack: edBack,
        sameMatch: edBack && (await ed.locator('.editor-modal__eyebrow').first().innerText()).trim() === identity,
        akaKept: edBack ? await T.rowSlots(T.boutRow(ed, 4), 'aka').allInnerTexts() : null, sheet: edBack ? await T.sheetState(ed) : null });
      await expect(T.boutRow(ed, 4)).toBeVisible();
      while (await T.rowSlots(T.boutRow(ed, 4), 'aka').count()) await T.rowSlots(T.boutRow(ed, 4), 'aka').first().tap();
      await T.ensureTie(ed, 4);
      // Bout 5, not yet fought: the operator taps its Tie for a real draw.
      const row5 = T.boutRow(ed, 5);
      await row5.scrollIntoViewIfNeeded();
      const before5 = { middle: (await T.rowMiddle(row5).innerText()).trim(), tieLabel: (await T.rowTie(row5).innerText()).trim() };
      await T.rowTie(row5).tap();
      await page.waitForTimeout(1500);
      const after5 = { middle: (await T.rowMiddle(row5).innerText()).trim(), tieLabel: (await T.rowTie(row5).innerText()).trim() };
      await shot(page, 'bout5-tie-tapped');
      record({ step: 'bout 5', action: 'Tie (hikiwake) on the unfought bout 5', variant: 'correct', before5, after5,
        note: 'bc-unfx when the tap turns the draw OFF because the unfought bout already read X' });
    });

    await test.step('bout 5 tied: the encounter is level, Finish is held back', async () => {
      const row5 = T.boutRow(ed, 5);
      await row5.scrollIntoViewIfNeeded();
      await expect(T.syncPill(ed)).toHaveText('Synced');
      await T.ensureTie(ed, 5);
      await expect(T.syncPill(ed)).toHaveText('Synced');
      await shot(page, 'bout5-scoring-view');
      const totals = await T.teamTotals(ed);
      const finish = T.finishBtn(ed);
      record({ step: 'tied encounter', action: 'all five bouts scored, level', variant: 'correct', ...totals,
        finishLabel: (await finish.innerText()).trim(), finishDisabled: await finish.isDisabled(),
        bandVisibleWhileScoringBout5: await ed.locator('.team-summary').isVisible(), sheet: await T.sheetState(ed) });
      expect(totals.shiro).toBe(totals.aka);
      await expect(finish).toBeDisabled();
      await ed.getByTestId('scoring-modal-daihyosen-button').scrollIntoViewIfNeeded();
      await shot(page, 'tied-daihyosen-offer');
    });

    await test.step('add the representative bout (daihyosen)', async () => {
      const add = ed.getByTestId('scoring-modal-daihyosen-button');
      const addBox = await T.tapBox(add);
      await add.tap();
      const appeared = await T.boutRow(ed, 'DH').waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
      await shot(page, 'daihyosen-added');
      let recovery = 'none needed';
      if (!appeared) {
        await page.getByRole('button', { name: 'Refresh' }).tap();
        const afterRefresh = await T.boutRow(ed, 'DH').waitFor({ state: 'visible', timeout: 8000 }).then(() => true, () => false);
        recovery = afterRefresh ? 'Refresh' : 'Refresh did not show it';
        if (!afterRefresh) { await page.reload(); recovery += ', reload'; }
      }
      await expect(T.boutRow(ed, 'DH')).toBeVisible();
      const dh = T.boutRow(ed, 'DH');
      const middle = (await T.rowMiddle(dh).innerText()).trim();
      const pickers = await dh.locator('.lineup-name input').count();
      const names = await dh.locator('.tsm-name').allInnerTexts();
      await dh.scrollIntoViewIfNeeded();
      await shot(page, 'daihyosen-row');
      record({ step: 'daihyosen', action: 'Add representative bout', variant: 'correct', rowAppearedInPlace: appeared, recovery, addTapBox: addBox,
        dhMiddle: middle, representativePickers: pickers, dhNames: names,
        editorError: (await ed.getByTestId('team-editor-error').allInnerTexts()).join(' ') });
      // The centre of a daihyosen row carries (DH) (recording-decisions.md).
      expect(middle).toBe('(DH)');
    });

    await test.step('daihyosen hantei: the thumb lands on AKA wins first', async () => {
      await ed.getByTestId('team-daihyosen-hantei-arm').tap();
      const shiroWins = ed.getByTestId('team-daihyosen-hantei-shiro');
      const nb = await tapNeighbour(shiroWins, 'right');
      await page.waitForTimeout(300);
      const akaHt = await ed.getByTestId('team-daihyosen-ht-aka').count();
      await shot(page, 'hantei-wrong-side');
      await shiroWins.tap();
      await expect(ed.getByTestId('team-daihyosen-ht-shiro')).toBeVisible();
      await expect(ed.getByTestId('team-daihyosen-ht-aka')).toHaveCount(0);
      await shot(page, 'hantei-shiro');
      record({ step: 'hantei', action: 'SHIRO wins', variant: 'V1 tapNeighbour right', ...nb, akaHtShown: akaHt, tapsToRecover: 1,
        hanteiTapBox: await T.tapBox(shiroWins),
        verdict: (await ed.locator('.team-summary__verdict').first().innerText()).trim(),
        dhMiddle: (await T.rowMiddle(T.boutRow(ed, 'DH')).innerText()).trim() });
      // The centre never carries Ht: it stays (DH).
      await expect(T.rowMiddle(T.boutRow(ed, 'DH'))).toHaveText('(DH)');
    });

    let sf2;
    await test.step('Finish the semifinal with a double tap (Finish + Start Next)', async () => {
      const finish = T.finishBtn(ed);
      await finish.scrollIntoViewIfNeeded();
      const labelBefore = (await finish.innerText()).trim();
      const dbl = await doubleTap(finish);
      await page.waitForTimeout(1500);
      const done = await completedRows(page).count();
      let extraTaps = 0;
      if (!done) {
        // The double tap only armed it: the operator taps again.
        await T.finishBtn(ed).tap();
        extraTaps = 1;
        await expect(completedRows(page)).toHaveCount(1);
      }
      const row = completedRows(page).last();
      const result = (await row.locator('.shiaijo-qrow__result').innerText()).trim();
      sf2 = await sides(ed).catch(() => null);
      await shot(page, 'sf1-finished-sf2-open');
      record({ step: 'finish SF1', action: 'Finish + Start Next', variant: 'V2 doubleTap', ...dbl, labelBefore, finishedByDoubleTap: !!done,
        extraTaps, completedResult: result, editorNowHolds: sf2,
        eyebrow: (await ed.locator('.editor-modal__eyebrow').first().innerText().catch(() => '')).trim() });
      expect(await sides(row)).toEqual(sf1);
    });

    await test.step('SF2: Kiken beside Kiken, then a hasty Record on the withdrawal prompt', async () => {
      if (!(await T.boutRow(ed, 1).isVisible())) {
        await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
      }
      sf2 = await sides(ed);
      const summary = ed.locator('.decision-disclosure__summary');
      await summary.scrollIntoViewIfNeeded();
      await summary.tap();
      const vol = ed.getByTestId('scoring-modal-kiken-voluntary-button');
      const nb = await tapNeighbour(vol, 'right');
      const prompt = ed.locator('form.decision-prompt');
      await expect(prompt).toBeVisible();
      const openedTitle = (await prompt.locator('div').first().innerText()).trim();
      await shot(page, 'sf2-kiken-neighbour');
      await prompt.getByRole('button', { name: 'Cancel' }).tap();
      record({ step: 'SF2 kiken', action: 'Kiken - Voluntary', variant: 'V1 tapNeighbour right', ...nb, promptOpened: openedTitle, tapsToRecover: 1,
        kikenTapBox: await T.tapBox(vol) });
      // The intended withdrawal is AKA's. The operator does not read the
      // prompt and taps its loudest button.
      if (!(await vol.isVisible())) await summary.tap();
      await vol.tap();
      await expect(prompt).toBeVisible();
      const preselected = await prompt.locator('input[type="radio"]:checked').getAttribute('value');
      await shot(page, 'sf2-kiken-prompt');
      const radioBox = await T.tapBox(prompt.locator('label', { has: page.locator('input[type="radio"]') }).first());
      const loud = prompt.locator('button.btn--primary');
      const loudLabel = (await loud.innerText()).trim();
      await loud.tap();
      await page.waitForTimeout(1500);
      const remaining = (await ed.locator('.remaining-matches').allInnerTexts()).join(' ').replace(/\s+/g, ' ');
      await shot(page, 'sf2-kiken-recorded');
      // The withdrawal is marked Kiken beside the team on the bracket.
      await expect(page.getByText('Kiken', { exact: true }).first()).toBeVisible();
      record({ step: 'SF2 kiken', action: 'Record withdrawal (intended: Aka)', variant: 'V4 hasty (loudest button)', label: loudLabel,
        promptRadioTapBox: radioBox,
        completedRowResult: (await completedRows(page).last().locator('.shiaijo-qrow__result').innerText().catch(() => '')).trim(),
        preselectedSide: preselected, recordedAgainst: preselected, remainingMatchesPanel: remaining.slice(0, 300),
        note: 'the side picker preselects Shiro, so a hasty Record withdraws the wrong team' });
    });

    await test.step('SF2 after the hasty kiken: the final starts, then the operator notices and corrects', async () => {
      const close = ed.locator('.remaining-matches button', { hasText: '✕' });
      if (await close.count()) await close.first().tap();
      // Not noticing, the operator starts the next match: the final.
      await openShiaijo(page, 'D');
      const finalBefore = await sides(upNextCard(page));
      await T.startUpNextTeam(page);
      await shot(page, 'final-started-with-wrong-team');
      record({ step: 'SF2 kiken recovery', action: 'final started before the mistake is noticed', variant: 'V5 lost place', finalPair: finalBefore });
      // Correct on the finished semifinal.
      const row = completedRows(page).filter({ hasText: sf2.shiro }).filter({ hasText: sf2.aka }).last();
      const result = (await row.locator('.shiaijo-qrow__result').innerText().catch(() => '')).trim();
      await row.getByRole('button', { name: /Correct/ }).tap();
      await expect(ed.getByText('CORRECTION', { exact: true }).or(ed.locator('.editor-modal__eyebrow', { hasText: 'MATCH 2' })).first()).toBeVisible();
      const full = (await ed.innerText()).replace(/\s+/g, ' ');
      await shot(page, 'sf2-correct-after-kiken');
      record({ step: 'SF2 kiken recovery', action: 'Correct on the semifinal', variant: 'V4 recovery', completedResult: result,
        correctionShowsKiken: /Kiken\b(?! – )/.test(full.replace('Withdrawal or no-show (kiken · fusenpai)', '')),
        offersReinstate: /reinstate/i.test(full), primary: await ed.locator('.score-nav__actions button.btn--primary').allInnerTexts() });
      // The documented kiken-undo: record the withdrawal again, on the right side.
      const summary = ed.locator('.decision-disclosure__summary');
      await summary.scrollIntoViewIfNeeded();
      await summary.tap();
      await ed.getByTestId('scoring-modal-kiken-voluntary-button').tap();
      const prompt = ed.locator('form.decision-prompt');
      await prompt.locator('input[value="aka"]').check();
      await shot(page, 'sf2-rekiken-aka-prompt');
      await prompt.locator('button.btn--primary').tap();
      const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') });
      const locked = await dialog.waitFor({ state: 'visible', timeout: 6000 }).then(() => true, () => false);
      let hasty = null;
      if (locked) {
        await shot(page, 'sf2-decision-locked-confirm');
        hasty = await hastyConfirm(page);
      }
      await page.waitForTimeout(1500);
      const errs = (await ed.locator('[style*="danger"], .alert--error').allInnerTexts()).join(' | ');
      await shot(page, 'sf2-after-rekiken');
      record({ step: 'SF2 kiken recovery', action: 'Kiken – Voluntary again, Aka, on the corrected semifinal', variant: 'V4 hastyConfirm (decision_locked)',
        confirmShown: locked, ...(hasty || {}), errorsAfter: errs });
      await openShiaijo(page, 'D');
      await page.waitForTimeout(1000);
      await shot(page, 'court-after-kiken-recovery');
      const court = (await page.locator('body').innerText()).replace(/\s+/g, ' ');
      record({ step: 'SF2 kiken recovery', action: 'court after the recovery', variant: 'V5 lost place',
        editorHolds: await sides(ed).catch(() => null), upNext: await sides(upNextCard(page)).catch(() => null),
        court: court.slice(court.indexOf('QUEUE'), court.indexOf('QUEUE') + 500) });
    });

    await test.step('Final: fusenpai (a team did not show up)', async () => {
      await openShiaijo(page, 'D');
      if (!(await T.boutRow(ed, 1).isVisible().catch(() => false)) && (await upNextCard(page).count())) await T.startUpNextTeam(page);
      await expect(T.boutRow(ed, 1)).toBeVisible();
      const pair = await sides(ed);
      const summary = ed.locator('.decision-disclosure__summary');
      await summary.scrollIntoViewIfNeeded();
      await summary.tap();
      await ed.getByTestId('scoring-modal-fusenpai-button').tap();
      const prompt = ed.locator('form.decision-prompt');
      await prompt.locator('input[value="aka"]').check();
      await shot(page, 'final-fusenpai-prompt');
      await prompt.locator('button.btn--primary').tap();
      await page.waitForTimeout(1500);
      await shot(page, 'final-fusenpai-recorded');
      record({ step: 'final fusenpai', action: 'Fusenpai (Aka did not show)', variant: 'correct', pair,
        errors: (await ed.locator('[style*="danger"], .alert--error').allInnerTexts().catch(() => [])).join(' | '),
        pageAfter: (await page.locator('body').innerText()).replace(/\s+/g, ' ').slice(0, 600) });
      // The no-show carries Fus. beside it; for a team encounter the marks
      // appear on the bracket (recording-decisions.md), which the console's
      // Just played panel shows.
      await expect(page.getByText('Fus.', { exact: true }).first()).toBeVisible();
    });

    await testInfo.attach('audit.json', { body: JSON.stringify(rows, null, 2), contentType: 'application/json' });
  });
  // ------------------------------------------------------------------- J7
  test('J7 one result on every surface: operator, head table, viewer, TV board', async ({ page, browser, baseURL }, testInfo) => {
    test.setTimeout(600_000);
    const shot = journeyShots('J7-one-result');
    const { rows, record } = recorder('J7');
    await T.enterAdmin(page);
    const id = await seedF4(page, { name: 'Team KO J7', court: 'E', prefix: 'F', teams: [['J7 Asahi', 'Kita Dojo'], ['J7 Yuhi', 'Minami Dojo']] });
    await openShiaijo(page, 'E');
    const pair = await sides(upNextCard(page));
    await upNextCard(page).getByRole('button', { name: 'Start match' }).tap();
    const ed = inlineEditor(page);
    await expect(T.boutRow(ed, 1)).toBeVisible();

    // The head table: a second operator device on the Scores tab.
    const head = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
    // The public: a spectator phone and the venue's TV board.
    const phoneCtx = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    const tvCtx = await browser.newContext({ viewport: { width: 1280, height: 720 }, baseURL });
    try {
      const hp = await head.newPage();
      await login(hp);
      const phone = await phoneCtx.newPage();
      const tv = await tvCtx.newPage();
      await openTvBoard(tv, 'E');
      await openViewer(phone, id);

      await test.step('bout 1 scored at the court shows on the TV board', async () => {
        await T.boutIppon(ed, 1, 'shiro', 'M');
        await expect(tvBoard(tv)).toContainText(pair.shiro);
        await expect(tvBoard(tv)).toContainText(/IV\s*1/);
        await shot(tv, 'tv-after-bout1');
        await shot(phone, 'viewer-after-bout1', { fullPage: true });
        record({ step: 'bout 1 live', action: 'Shiro men at the court', variant: 'correct',
          tv: (await tvBoard(tv).innerText()).replace(/\s+/g, ' ').slice(0, 300),
          viewer: (await phone.locator('.viewer__body').innerText()).replace(/\s+/g, ' ').slice(0, 300) });
      });

      await test.step('level the encounter and add the daihyosen at the court', async () => {
        await T.boutIppon(ed, 2, 'aka', 'M');
        for (const n of [3, 4, 5]) await T.ensureTie(ed, n);
        await ed.getByTestId('scoring-modal-daihyosen-button').tap();
        await expect(T.boutRow(ed, 'DH')).toBeVisible();
        await expect(tvBoard(tv)).toContainText('(DH)');
        await shot(tv, 'tv-with-daihyosen');
        record({ step: 'daihyosen live', action: 'Add representative bout at the court', variant: 'correct',
          tv: (await tvBoard(tv).innerText()).replace(/\s+/g, ' ').slice(0, 300) });
      });

      await test.step('the head table decides the daihyosen by hantei and finishes; the court sees it', async () => {
        await openScoreEditor(hp, id, { court: 'E' });
        const hed = hp.locator(EDITOR);
        await expect(T.boutRow(hed, 'DH')).toBeVisible();
        await hed.getByTestId('team-daihyosen-hantei-arm').tap();
        await hed.getByTestId('team-daihyosen-hantei-shiro').tap();
        await expect(hed.getByTestId('team-daihyosen-ht-shiro')).toBeVisible();
        await shot(hp, 'head-table-hantei-shiro');
        await T.finishTeam(hed);
        // The court console, left open, follows the result.
        await expect(completedRows(page)).toHaveCount(1, { timeout: 15000 });
        await shot(page, 'court-after-head-table-finish');
        const courtRow = (await completedRows(page).first().innerText()).replace(/\s+/g, ' ');
        record({ step: 'finish elsewhere', action: 'head table Finish with hantei Shiro', variant: 'two devices', courtCompletedRow: courtRow,
          courtShows: (await page.locator('main, body').first().innerText()).replace(/\s+/g, ' ').slice(0, 400) });
      });

      await test.step('both operators open the correction; the head table moves the verdict; the court adopts it', async () => {
        // The court operator opens Correct on the finished final and leaves it.
        await completedRows(page).first().getByRole('button', { name: /Correct/ }).tap();
        await expect(T.boutRow(ed, 'DH')).toBeVisible();
        await expect(ed.getByTestId('team-daihyosen-ht-shiro')).toBeVisible();
        await shot(page, 'court-correction-open-shiro');
        // The head table corrects the hantei to Aka.
        await hp.goto(`/admin/competition/${id}/scores`);
        const row = hp.locator('.score-edit-row').filter({ has: hp.getByRole('button', { name: /^Correct$/ }) }).first();
        await row.getByRole('button', { name: /^Correct$/ }).tap();
        const hed = hp.locator(EDITOR);
        await expect(T.boutRow(hed, 'DH')).toBeVisible();
        const htChip = hed.getByTestId('team-daihyosen-ht-shiro');
        if (await htChip.count()) await htChip.tap();
        if (await hed.getByTestId('team-daihyosen-hantei-arm').count()) await hed.getByTestId('team-daihyosen-hantei-arm').tap();
        await hed.getByTestId('team-daihyosen-hantei-aka').tap();
        await expect(hed.getByTestId('team-daihyosen-ht-aka')).toBeVisible();
        await hed.locator('.score-nav__actions button.btn--primary').tap();
        const reason = hp.locator('.reason-prompt, form').filter({ hasText: /Reason for correction/ }).last();
        await expect(reason).toBeVisible();
        await shot(hp, 'head-table-correction-reason');
        await reason.locator('input, textarea').first().fill('Hantei was for Aka');
        await reason.getByRole('button', { name: /Save|Confirm|Record/ }).last().tap();
        await expect(hp.locator(EDITOR)).toHaveCount(0, { timeout: 15000 }).catch(() => {});
        await shot(hp, 'head-table-after-correction');
        // The court's open correction editor adopts the new verdict, untouched.
        const adopted = await ed.getByTestId('team-daihyosen-ht-aka').waitFor({ state: 'visible', timeout: 10000 }).then(() => true, () => false);
        await shot(page, 'court-correction-after-remote-change');
        record({ step: 'adopt verdict', action: 'court correction editor left open while the head table corrects hantei to Aka',
          variant: 'two devices', adopted, courtShiroHtStill: await ed.getByTestId('team-daihyosen-ht-shiro').count(),
          courtVerdict: (await ed.locator('.team-summary__verdict').first().innerText().catch(() => '')).trim() });
        expect(adopted).toBe(true);
        await expect(ed.getByTestId('team-daihyosen-ht-shiro')).toHaveCount(0);
      });

      await test.step('the viewer and the TV board show the same final result', async () => {
        await phone.reload();
        await expect(phone.locator('.viewer__body')).toBeVisible();
        await shot(phone, 'viewer-final', { fullPage: true });
        const viewerText = (await phone.locator('.viewer__body').innerText()).replace(/\s+/g, ' ');
        await tv.reload();
        await tv.waitForTimeout(1500);
        await shot(tv, 'tv-final');
        const tvText = (await tvBoard(tv).innerText()).replace(/\s+/g, ' ');
        record({ step: 'every surface', action: 'viewer and TV after the correction', variant: 'correct',
          viewer: viewerText.slice(0, 600), tv: tvText.slice(0, 400), expectedWinner: pair.aka });
        await expect(phone.locator('.viewer__body')).toContainText(pair.aka);
        // For a team encounter the marks appear on the bracket
        // (recording-decisions.md): the viewer's Bracket tab.
        await phone.getByRole('tab', { name: 'Bracket' }).or(phone.getByRole('button', { name: 'Bracket' })).first().tap();
        await phone.waitForTimeout(800);
        await shot(phone, 'viewer-bracket-final', { fullPage: true });
        const bracketText = (await phone.locator('.viewer__body').innerText()).replace(/\s+/g, ' ');
        record({ step: 'every surface', action: 'viewer Bracket tab after the correction', variant: 'correct', bracket: bracketText.slice(0, 400),
          htOnBracket: /\bHt\b/.test(bracketText) });
        // The winner's check sits beside the corrected winner, Aka.
        expect(bracketText).toMatch(new RegExp(`✓\\s*\\S*\\s*${pair.aka}`));
      });
    } finally {
      await head.close();
      await phoneCtx.close();
      await tvCtx.close();
    }
    await testInfo.attach('audit.json', { body: JSON.stringify(rows, null, 2), contentType: 'application/json' });
  });
  // ------------------------------------------------------------ findings
  // Each <bead-id> below is a functional defect a journey step exposed,
  // kept as a self-contained test so the fix can remove the fixme and keep
  // the step as its guard. Each seeds its own competition on its own shiaijo.

  test.fixme('bc-lpfb: the match lineup panel shows a pool match\'s round-default lineup as empty', async ({ page }) => {
    await T.enterAdmin(page);
    const id = await createCompetition(page, {
      name: 'B1 Pools', kind: 'team', format: 'mixed', teamSize: 3, teamMatchType: 'fixed', courts: ['G', 'H'], numberPrefix: 'G',
    });
    await pasteRoster(page, id, SIX_TEAMS);
    await generateDraw(page, id);
    await startCompetition(page, id);
    await openShiaijo(page, 'G');
    const pair = await sides(upNextCard(page));
    await nameRound1(page, id, pair.shiro, ['Aoki', 'Baba', 'Chiba'], 3);
    await openShiaijo(page, 'G');
    await T.openPanelFromUpNext(page);
    // The panel says "Inheriting round default" and must show that default,
    // as the score sheet does.
    await expect(T.panelSide(page, 0)).toContainText('Inheriting round default');
    await expect(T.panelInput(page, 0, '1')).toHaveValue('Aoki');
    await expect(T.panelInput(page, 0, '3')).toHaveValue('Chiba');
  });

  test.fixme('bc-lprf: the match lineup panel\'s "already at" refusal names the wrong position', async ({ page }) => {
    await T.enterAdmin(page);
    await seedF4(page, { name: 'B2 KO', court: 'I', prefix: 'I', teams: FOUR_TEAMS('B2') });
    await openShiaijo(page, 'I');
    await T.openPanelFromUpNext(page);
    await T.typeIntoNameBox(T.panelInput(page, 1, 'Senpo'), 'Dai');
    await T.typeIntoNameBox(T.panelInput(page, 1, 'Jiho'), 'Eto');
    await T.panelSave(page, 1);
    await expect(T.panelSide(page, 1)).toContainText('Override for this match');
    // Dai holds Senpo; typing Dai at Jiho is refused naming where Dai IS
    // (team-tournaments.md: "refused with the position they hold").
    await T.typeIntoNameBox(T.panelInput(page, 1, 'Jiho'), 'Dai');
    await T.panelSave(page, 1);
    await expect(T.panelSide(page, 1).locator('.alert--error')).toHaveText('Dai is already at Senpo.');
  });

  test.fixme('bc-unfx: a bout not yet fought reads as a draw (X, "✓ Tie") on the sheet and the TV board', async ({ page, browser, baseURL }) => {
    await T.enterAdmin(page);
    await seedF4(page, { name: 'B3 KO', court: 'J', prefix: 'J', teams: [['B3 Ume', 'Kita Dojo'], ['B3 Sakura', 'Minami Dojo']] });
    await openShiaijo(page, 'J');
    const ed = await T.startUpNextTeam(page);
    await T.boutIppon(ed, 1, 'shiro', 'M');
    await page.waitForTimeout(1500);
    await page.reload();
    await expect(T.boutRow(ed, 2)).toBeVisible();
    // Bouts 2-5 have not been fought: no draw mark, and Tie is not pressed.
    for (const n of [2, 3, 4, 5]) {
      await expect(T.rowMiddle(T.boutRow(ed, n))).toHaveText('vs');
      await expect(T.rowTie(T.boutRow(ed, n))).toHaveText('Tie (hikiwake)');
    }
    const tvCtx = await browser.newContext({ viewport: { width: 1280, height: 720 }, baseURL });
    try {
      const tv = await tvCtx.newPage();
      await openTvBoard(tv, 'J');
      await expect(tvBoard(tv)).toContainText(/#1\s*M\s*vs\s*#1/);
      await expect(tvBoard(tv)).not.toContainText(/#2\s*X\s*#2/);
    } finally {
      await tvCtx.close();
    }
  });

  test.fixme('bc-fsnp: a fusensho wipes the points the other side already scored, so its undo cannot bring them back after a reload', async ({ page }) => {
    await T.enterAdmin(page);
    await seedF4(page, { name: 'B4 KO', court: 'K', prefix: 'L', teams: [['B4 Ume', 'Kita Dojo'], ['B4 Sakura', 'Minami Dojo']] });
    await openShiaijo(page, 'K');
    const ed = await T.startUpNextTeam(page);
    await T.boutIppon(ed, 1, 'shiro', 'K');
    // The kote is saved before the fusensho (the pill cannot say so: it
    // reads Synced through the autosave's debounce).
    await page.waitForTimeout(1500);
    // Shiro's fighter cannot continue; the bout goes to Aka by default.
    await T.rowFusensho(T.boutRow(ed, 1), 'aka').tap();
    await expect(T.rowSlots(T.boutRow(ed, 1), 'aka')).toHaveText(['○', '○']);
    // recording-decisions.md: "Any point the withdrawing side had already
    // scored stays valid and is kept on the sheet."
    await expect(T.rowSlots(T.boutRow(ed, 1), 'shiro')).toHaveText(['K']);
    await page.waitForTimeout(1500);
    await page.reload();
    await expect(T.rowFusensho(T.boutRow(ed, 1), 'aka')).toHaveText('✓ Fusensho');
    // "Click to undo fusensho: restores the previous score".
    await T.rowFusensho(T.boutRow(ed, 1), 'aka').tap();
    await expect(T.rowSlots(T.boutRow(ed, 1), 'shiro')).toHaveText(['K']);
    await expect(T.rowSlots(T.boutRow(ed, 1), 'aka')).toHaveCount(0);
  });

  // Operator ruling 2026-09-24: every bout of a team match is fought; there
  // are no unfinished team matches. So Finish must refuse while a numbered
  // bout has no result, and an unfought bout never reaches the standings.
  test.fixme('bc-tmfn: a team encounter cannot be finished while a bout has no result', async ({ page }) => {
    await T.enterAdmin(page);
    const id = await createCompetition(page, {
      name: 'B3 Pools', kind: 'team', format: 'mixed', teamSize: 3, teamMatchType: 'fixed', courts: ['M', 'N'], numberPrefix: 'Q',
    });
    await pasteRoster(page, id, SIX_TEAMS);
    await generateDraw(page, id);
    await startCompetition(page, id);
    await openShiaijo(page, 'M');
    const ed = await T.startUpNextTeam(page);
    const pair = await sides(ed);
    await T.boutIppon(ed, 1, 'shiro', 'M');
    await T.ensureTie(ed, 2);
    // Bout 3 is never scored: Finish must not commit the encounter.
    await page.waitForTimeout(1200);
    const finish = T.finishBtn(ed);
    await finish.tap().catch(() => {});
    await finish.tap().catch(() => {});
    await page.goto(`/admin/competition/${id}/pools`);
    // The encounter is still unfinished, so no team has played a match yet.
    await expect(page.locator('tr').filter({ hasText: pair.shiro }).first()).toContainText(/0\s*0\s*0\s*0\s*0\s*0\s*0\s*0\s*$/);
  });

  test.fixme('bc-dhrp: the daihyosen row offers no way to pick each team\'s representative', async ({ page }) => {
    await T.enterAdmin(page);
    await seedF4(page, { name: 'B6 KO', court: 'F', prefix: 'R', teams: [['B6 Ume', 'Kita Dojo'], ['B6 Sakura', 'Minami Dojo']] });
    await openShiaijo(page, 'F');
    const ed = await T.startUpNextTeam(page);
    for (const n of [1, 2, 3, 4, 5]) await T.ensureTie(ed, n);
    await ed.getByTestId('scoring-modal-daihyosen-button').tap();
    const dh = T.boutRow(ed, 'DH');
    await expect(dh).toBeVisible();
    // recording-decisions.md: "The score editor lets you pick each team's
    // representative from its roster."
    await expect(T.rowNameBox(dh, 'shiro')).toBeVisible();
    await expect(T.rowNameBox(dh, 'aka')).toBeVisible();
  });
  test.fixme('bc-kpnl: on the court console the withdrawn team\'s remaining-matches panel vanishes before a default win can be awarded', async ({ page }) => {
    await T.enterAdmin(page);
    const id = await createCompetition(page, {
      name: 'B8 Pools', kind: 'team', format: 'mixed', teamSize: 3, teamMatchType: 'fixed', courts: ['O', 'P'], numberPrefix: 'V',
    });
    await pasteRoster(page, id, SIX_TEAMS);
    await generateDraw(page, id);
    await startCompetition(page, id);
    await openShiaijo(page, 'O');
    const ed = await T.startUpNextTeam(page);
    const summary = ed.locator('.decision-disclosure__summary');
    await summary.scrollIntoViewIfNeeded();
    await summary.tap();
    await ed.getByTestId('scoring-modal-kiken-voluntary-button').tap();
    const prompt = ed.locator('form.decision-prompt');
    await prompt.locator('input[value="shiro"]').check();
    await prompt.locator('button.btn--primary').tap();
    // recording-decisions.md / the kiken default-win chain: the withdrawn
    // team's remaining pool matches are offered for a default win, and the
    // offer stays until the operator acts on it.
    const award = page.getByRole('button', { name: 'Award default win to opponent' });
    await expect(award.first()).toBeVisible();
    await page.waitForTimeout(2000);
    await expect(award.first()).toBeVisible();
  });
});
