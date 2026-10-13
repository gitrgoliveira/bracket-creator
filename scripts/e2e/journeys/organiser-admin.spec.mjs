// The organiser's side jobs, in file mode: the Excel download of a competition
// with a result, importing competitions from a folder, the tournament logo and
// a sponsor, and the forgotten-password reset. Locked mode is its own spec
// file (organiser-locked.spec.mjs), because it is a different server.
//
// One server for the file; the tests run in order and each leaves the server
// for the next. The reset goes LAST: it changes the admin password every
// earlier test signs in with.
//
// Everything is seeded through the interface. The files the journey feeds to
// the app's file inputs (an import bundle, two PNGs) are written to a
// throwaway folder at run time; nothing binary is committed.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { test, expect } from '../fixtures/test.mjs';
import { OPERATOR_DEVICE, PUBLIC_DEVICE, EXPECT_TIMEOUT_MS } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { enterTournament, login, signOut } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { openShiaijo } from '../fixtures/shiaijo.mjs';
import { playRunningBout } from '../fixtures/individual.mjs';
import { openViewer, openTvBoard, tvBoard } from '../fixtures/public.mjs';
import { dwell } from '../fixtures/pace.mjs';
import {
  addSponsor, downloadResultsWorkbook, importFiles, importResultRows, loadedImage, openEditTournament,
  pngBytes, readWorkbook, resetPassword, todayDMY, uploadLogo, writeImportBundle,
} from '../fixtures/organiser.mjs';

const ROSTER = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'], ['Dan Evans', 'Seishinkan'],
];
const IMPORTED = [
  ['Emi Hoshino', 'Kenshikan'], ['Fumi Yamada', 'Shudokan'], ['Gen Ota', 'Kenshikan'],
];
const IMPORTED_B = [
  ['Hiro Sasaki', 'Seiwakan'], ['Ino Mizuno', 'Tenshin'],
];
const NEW_PASSWORD = 'e2e-after-reset';

test.describe.configure({ mode: 'serial' });

test.describe('organiser admin (file mode)', () => {
  let shot;
  let work; // throwaway folder for the files fed to file inputs
  let compId; // the competition with a result
  const audit = auditLog('organiser-admin');
  const { record } = audit;

  test.beforeAll(() => {
    shot = journeyShots('organiser-admin');
    work = fs.mkdtempSync(path.join(os.tmpdir(), 'bc-e2e-organiser-'));
  });
  test.afterAll(() => {
    audit.flush();
    if (work) fs.rmSync(work, { recursive: true, force: true });
  });

  test('Excel export: a competition with a result downloads as a workbook', async ({ page }) => {
    let pair;
    await test.step('seed a knockout of four and play one bout', async () => {
      await enterTournament(page, { name: 'Organiser Cup', courts: 2 });
      compId = await createCompetition(page, {
        name: 'Export Men', kind: 'individual', format: 'knockout', courts: ['A'], numberPrefix: 'X',
      });
      await pasteRoster(page, compId, ROSTER);
      await generateDraw(page, compId);
      await startCompetition(page, compId);
      await openShiaijo(page, 'A');
      pair = await playRunningBout(page);
      await shot(page, 'bout-played');
    });

    await test.step('download the results workbook from the Export section', async () => {
      const dl = await downloadResultsWorkbook(page, compId);
      await shot(page, 'export-section');
      const book = readWorkbook(dl.bytes);
      record({ journey: 'organiser-admin', step: 'excel export', file: dl.name, bytes: dl.bytes.length, sheets: book.sheets });
      expect(dl.name).toBe(`results-${compId}.xlsx`);
      expect(book.sheets.length).toBeGreaterThan(0);
      expect(book.sheets).toContain('Elimination Matches');
      for (const [name] of ROSTER) expect(book.text, `${name} is in the workbook`).toContain(name);
      // The played bout's two names sit in the bracket, and its winner is told
      // apart from the others by a mark: the workbook is not the blank draw.
      expect(book.text).toContain(pair.shiro);
      expect(book.text).toContain(pair.aka);
    });
  });

  test('Import competitions: a folder of CSVs lands on the dashboard and the public page', async ({ page, browser, baseURL }) => {
    // Two rows: one with a date, and one without (the manifest's date is optional
    // and a row without one takes the tournament's).
    const files = writeImportBundle(path.join(work, 'bundle'), [
      { id: 'imp-kyu', name: 'Imported Kyu', kind: 'individual', format: 'knockout', date: todayDMY(), roster: IMPORTED },
      { id: 'imp-nodate', name: 'Imported No Date', kind: 'individual', format: 'mixed', roster: IMPORTED_B },
    ]);
    await login(page, 'e2e-operator');
    await test.step('import through the page', async () => {
      await importFiles(page, files);
      await shot(page, 'import-results');
      const rows = importResultRows(page);
      await expect(rows).toHaveCount(2);
      for (const name of ['Imported Kyu', 'Imported No Date']) {
        const row = rows.filter({ hasText: name });
        await expect(row).toContainText('imported');
        await expect(row).not.toContainText('not imported');
      }
    });

    await test.step('the dashboard lists it and its roster is there', async () => {
      await expect(page).toHaveURL(/\/admin$/, { timeout: EXPECT_TIMEOUT_MS });
      await expect(page.getByText('Imported Kyu').first()).toBeVisible();
      await expect(page.getByText('Export Men').first()).toBeVisible();
      await page.goto('/admin/competition/imp-kyu/participants');
      for (const [name] of IMPORTED) await expect(page.getByText(name).first()).toBeVisible();
      await shot(page, 'imported-roster');
    });

    await test.step('the row with no date took the tournament date and can be drawn (bc-imdt)', async () => {
      await page.goto('/admin/competition/imp-nodate');
      await expect(page.getByRole('button', { name: 'Generate draw' })).toBeEnabled();
      await expect(page.getByText(/Cannot start: invalid date/)).toHaveCount(0);
      // The tournament's date, as the page head writes it ("13 Oct 2026").
      const day = new Date().toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
      await expect(page.locator('.page-head__sub')).toContainText(day);
      await shot(page, 'imported-no-date');
    });

    await test.step('a spectator sees it listed, and its roster once the imported roster is drawn and started', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await phone.goto('/');
        await expect(phone.locator('.viewer__body')).toBeVisible();
        await expect(phone.getByText('Imported Kyu').first()).toBeVisible();
        await openViewer(phone, 'imp-kyu');
        await expect(phone.locator('.viewer__body')).toContainText('Not started yet');

        // The imported roster is a real roster: it draws and starts.
        await page.goto('/admin/competition/imp-kyu');
        await generateDraw(page, 'imp-kyu');
        await startCompetition(page, 'imp-kyu');
        await shot(page, 'imported-started');
        await openViewer(phone, 'imp-kyu');
        await phone.getByRole('tab', { name: 'Bracket' }).or(phone.getByRole('button', { name: 'Bracket', exact: true })).first().tap();
        for (const [name] of IMPORTED) await expect(phone.locator('.viewer__body')).toContainText(name);
        await shot(phone, 'viewer-imported', { fullPage: true });
      } finally {
        await spectator.close();
      }
    });
  });

  test('Branding and sponsors: the logo and a sponsor show on the public page', async ({ page, browser, baseURL }) => {
    const logo = path.join(work, 'logo.png');
    const sponsor = path.join(work, 'sponsor.png');
    fs.writeFileSync(logo, pngBytes(3, 2));
    fs.writeFileSync(sponsor, pngBytes(5, 4, [20, 90, 200]));
    await login(page, 'e2e-operator');

    await test.step('upload a logo and add a sponsor on Edit details', async () => {
      await openEditTournament(page);
      await uploadLogo(page, logo);
      await addSponsor(page, { name: 'Acme Dojo Supplies', file: sponsor });
      await shot(page, 'edit-details-uploaded', { fullPage: true });
    });

    await test.step('the admin top bar shows the uploaded logo', async () => {
      await page.goto('/admin');
      const img = page.locator('.topbar__logo').first();
      const size = await loadedImage(img);
      expect(size).toMatchObject({ w: 3, h: 2 });
    });

    await test.step('a spectator phone shows the logo and the sponsor strip', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await phone.goto('/');
        await expect(phone.locator('.viewer__body')).toBeVisible();
        expect(await loadedImage(phone.locator('.viewer__logo').first())).toMatchObject({ w: 3, h: 2 });
        const strip = phone.getByRole('complementary', { name: 'Sponsors' });
        await strip.scrollIntoViewIfNeeded();
        expect(await loadedImage(strip.getByRole('img', { name: 'Acme Dojo Supplies' }))).toMatchObject({ w: 5, h: 4 });
        await shot(phone, 'viewer-logo-and-sponsor', { fullPage: true });

        // The docs: sponsor images appear on the public viewer only, never on
        // the TV board.
        await openTvBoard(phone, 'A');
        await expect(tvBoard(phone).getByRole('img', { name: 'Acme Dojo Supplies' })).toHaveCount(0);
        await expect(phone.getByRole('complementary', { name: 'Sponsors' })).toHaveCount(0);
        await shot(phone, 'tv-board-no-sponsor');

        // The docs also list the lobby display for the logo. Recorded, and held
        // by the fixme below.
        await phone.goto('/display?court=all');
        await expect(phone.getByTestId('lobby-root')).toBeVisible();
        record({ journey: 'organiser-admin', step: 'logo on the lobby display', logoImages: await phone.locator('img[alt="Tournament logo"]').count() });
        await shot(phone, 'lobby-display');
      } finally {
        await spectator.close();
      }
    });
  });

  test.fixme('bc-lblg: the docs list the lobby display among the surfaces that show the tournament logo, and it shows none', async ({ browser, baseURL }) => {
    // run-tournament.md (Branding and sponsors): "Logo: ... shown on the viewer,
    // the lobby displays, and the admin screens." The viewer and the admin top
    // bar show it (asserted above); /display?court=all, the lobby grid, has no
    // logo image at all (display_lobby.jsx). Runs after the upload test.
    const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
    try {
      const phone = await spectator.newPage();
      await phone.goto('/display?court=all');
      await expect(phone.getByTestId('lobby-root')).toBeVisible();
      await expect(phone.getByRole('img', { name: 'Tournament logo' })).toBeVisible();
    } finally {
      await spectator.close();
    }
  });

  test('Reset in file mode: Forgot password sets a new one from the loopback browser', async ({ page, browser, baseURL }) => {
    // A second organiser signed in on another device: the reset signs it out.
    const other = await browser.newContext({ ...OPERATOR_DEVICE, baseURL });
    try {
      const otherPage = await other.newPage();
      await login(otherPage, 'e2e-operator');

      await test.step('from the sign-in dialog, Forgot password? leads to the reset page', async () => {
        await page.goto('/admin');
        const dialog = page.locator('.modal.auth');
        await expect(dialog.getByText('Admin sign in')).toBeVisible();
        // The sign-in dialog ignores a tap inside the bounce window of its opening.
        await dwell(page, Date.now());
        const forgot = dialog.getByRole('button', { name: 'Forgot password?' });
        await expect(forgot).toBeVisible();
        await forgot.tap();
        const confirm = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
        await confirm.waitFor({ state: 'visible' });
        const confirmShown = Date.now();
        await expect(confirm).toContainText('sign out all other admins');
        await dwell(page, confirmShown);
        await confirm.getByRole('button', { name: 'Reset password' }).tap();
        await expect(page).toHaveURL(/\/reset$/);
        await expect(page.getByRole('heading', { name: 'Reset admin password' })).toBeVisible();
        await shot(page, 'reset-page');
      });

      await test.step('choose a new password: no old one is asked for, and the browser is signed in', async () => {
        await resetPassword(page, NEW_PASSWORD);
        await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
        await shot(page, 'after-reset');
        // The tournament and its competitions are untouched: only the password changed.
        await page.goto('/admin');
        await expect(page.getByText('Export Men').first()).toBeVisible();
        await expect(page.getByText('Imported Kyu').first()).toBeVisible();
      });

      await test.step('the new password signs in; the other organiser is asked to sign in again', async () => {
        await signOut(page);
        await login(page, NEW_PASSWORD);
        await expect(otherPage.locator('.modal.auth')).toBeVisible({ timeout: EXPECT_TIMEOUT_MS });
        await shot(otherPage, 'other-admin-signed-out');
      });
    } finally {
      await other.close();
    }
  });
});
