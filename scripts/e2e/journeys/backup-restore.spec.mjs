// The organiser's backup and restore, from the Backup and restore page: back up
// the whole tournament, change the tournament afterwards (rename a
// competition, add one), then restore the backup over it, answering the plan
// row by row, and undo the restore.
//
// Everything is seeded through the interface. The backup the journey restores
// is the one it downloaded, kept in a throwaway folder for the run.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test, expect } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { pasteRoster } from '../fixtures/competition.mjs';
import {
  backUpTournament, backupListNames, chooseBackup, confirmWithPassword, openBackupPage, planRow, restoreButton,
} from '../fixtures/organiser.mjs';

const PASSWORD = 'e2e-operator';
const MEN = [['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi']];
const WOMEN = [['Emi Hoshino', 'Kenshikan'], ['Fumi Yamada', 'Shudokan'], ['Gen Ota', 'Kenshikan']];

test.describe.configure({ mode: 'serial' });

test.describe('backup and restore', () => {
  let shot;
  let work; // throwaway folder for the downloaded backup

  test.beforeAll(() => {
    shot = journeyShots('backup-restore');
    work = fs.mkdtempSync(path.join(os.tmpdir(), 'bc-e2e-backup-'));
  });
  test.afterAll(() => {
    if (work) fs.rmSync(work, { recursive: true, force: true });
  });

  test('a whole-tournament backup restores over a changed tournament, and the restore undoes', async ({ page }) => {
    let backup;
    let menId;

    await test.step('seed two competitions and back up the tournament', async () => {
      await enterTournament(page, { name: 'Backup Cup', courts: 2 });
      menId = await createCompetition(page, { name: 'Men', kind: 'individual', format: 'knockout', courts: ['A'] });
      await pasteRoster(page, menId, MEN);
      const womenId = await createCompetition(page, { name: 'Women', kind: 'individual', format: 'knockout', courts: ['B'] });
      await pasteRoster(page, womenId, WOMEN);

      await openBackupPage(page);
      backup = await backUpTournament(page, work);
      expect(backup).toMatch(/\.zip$/);
    });

    await test.step('change the tournament: rename Men, add Juniors', async () => {
      await page.goto(`/admin/competition/${menId}/settings`);
      await page.locator('#settings-display-name').fill('Men Live');
      // The page has a Save changes button above the form and one below it.
      await page.getByRole('button', { name: 'Save changes' }).first().tap();
      // A clean save goes back to the dashboard, which lists the new name.
      await expect(page).toHaveURL(/\/admin$/);
      await expect(page.getByText('Men Live', { exact: true })).toBeVisible();
      await createCompetition(page, { name: 'Juniors', kind: 'individual', format: 'knockout', courts: ['A'] });
      await openBackupPage(page);
      expect((await backupListNames(page)).sort()).toEqual(['Juniors', 'Men Live', 'Women']);
    });

    await test.step('the plan names each competition, and Cancel puts it away', async () => {
      await chooseBackup(page, backup);
      // Men is listed under the backup's name, with the name it has now.
      await expect(planRow(page, 'Men')).toContainText('Called “Men Live” in this tournament now.');
      await expect(planRow(page, 'Men')).toContainText('This competition already exists:');
      await expect(planRow(page, 'Women')).toContainText('This competition already exists:');
      await expect(planRow(page, 'Juniors')).toContainText('This competition is not in the backup archive:');
      // Nothing answered yet: the Restore button names the next open choice.
      const restore = restoreButton(page);
      await expect(restore).toHaveAttribute('aria-disabled', 'true');
      await expect(restore).toHaveText(/^Choose what to do with "/);
      await shot(page, 'plan-unanswered');

      await page.getByRole('button', { name: 'Cancel', exact: true }).tap();
      await expect(page.getByRole('heading', { name: 'Restore plan: whole tournament' })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Choose a backup file' })).toBeFocused();
    });

    await test.step('answer the plan: Men as a copy, replace Women, keep Juniors', async () => {
      await chooseBackup(page, backup);
      const men = planRow(page, 'Men');
      await men.getByRole('radio', { name: /Restore as a copy named/ }).check();
      const copyName = men.getByRole('textbox', { name: 'Name for the copy of "Men"' });
      // A name the tournament will hold after the restore is refused before
      // the password is asked for.
      await copyName.fill('Women');
      await expect(men.getByText('That name is already used.')).toBeVisible();
      await expect(copyName).toHaveAttribute('aria-invalid', 'true');
      await copyName.fill('Men Archive');
      await expect(men.getByText('That name is already used.')).toHaveCount(0);

      await planRow(page, 'Women').getByRole('radio', { name: 'Replace it' }).check();
      await planRow(page, 'Juniors').getByRole('radio', { name: 'Keep it' }).check();
      const restore = restoreButton(page);
      await expect(restore).toHaveText('Restore');
      await expect(restore).not.toHaveAttribute('aria-disabled', 'true');
      await shot(page, 'plan-answered');
    });

    await test.step('restore with the password', async () => {
      await restoreButton(page).tap();
      await confirmWithPassword(page, 'Confirm restore', 'Restore', PASSWORD);
      await expect(page.getByRole('heading', { level: 2, name: 'Restore completed' })).toBeVisible();
      await expect(page.getByText(/Safety copy created:/)).toBeVisible();
      await expect.poll(async () => (await backupListNames(page)).sort())
        .toEqual(['Juniors', 'Men Archive', 'Men Live', 'Women']);
      await shot(page, 'restore-done');
    });

    await test.step('undo the restore', async () => {
      await page.getByRole('button', { name: 'Undo this restore' }).tap();
      await confirmWithPassword(page, 'Confirm undo', 'Undo restore', PASSWORD);
      await expect(page.getByRole('heading', { level: 2, name: 'Restore undone' })).toBeVisible();
      await expect.poll(async () => (await backupListNames(page)).sort())
        .toEqual(['Juniors', 'Men Live', 'Women']);
      // The safety copy the restore took is kept on this computer.
      await expect(page.locator('.badge--archive', { hasText: 'Before restore' }).first()).toBeVisible();
      await shot(page, 'undo-done');
    });
  });
});
