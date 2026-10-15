// Locked mode: the server is started with --lock-password and a bcrypt hash in
// TOURNAMENT_PASSWORD_HASH (operating-modes.md, "Locked mode"), so this is its
// own spec file: a file's server is started once, and the file-mode journeys
// (organiser-admin.spec.mjs) need a different one.
//
// The hash is made by the app's own `hash-password` command, which reads the
// password from standard input, exactly as the docs tell an organiser to.
// The server is started with these options through serverOptionsFor
// (fixtures/test.mjs).
//
// What is asserted: the first-run form says the password is the one behind the
// hash, the sign-in dialog accepts it, offers no "Forgot password?", and the
// /reset page shows the operator-disabled message instead of a form. A wrong
// password is not tried: the refusal is a 401 the browser logs as a console
// error, and the console policy lets none through.
import { execFileSync } from 'node:child_process';
import { BIN } from '../../screenshots/lib/server.mjs';
import { fileURLToPath } from 'node:url';
import { test, expect, serverOptionsFor } from '../fixtures/test.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { auditLog } from '../fixtures/audit.mjs';
import { createTournament, login } from '../fixtures/setup.mjs';
import { dwell } from '../fixtures/pace.mjs';

const PASSWORD = 'e2e-locked-secret';

// Started with --lock-password and the hash of PASSWORD, made when the file's
// server starts (see serverOptionsFor in fixtures/test.mjs).
serverOptionsFor(fileURLToPath(import.meta.url), () => {
  const hash = execFileSync(BIN, ['hash-password'], { input: PASSWORD, encoding: 'utf8' }).trim();
  if (!/^\$2[aby]\$/.test(hash)) throw new Error('hash-password did not print a bcrypt hash');
  return { args: ['--lock-password'], env: { TOURNAMENT_PASSWORD_HASH: hash } };
});

test.describe.configure({ mode: 'serial' });

test.describe('organiser locked mode', () => {
  let shot;
  const audit = auditLog('organiser-locked');
  const { record } = audit;

  test.beforeAll(() => {
    shot = journeyShots('organiser-locked');
  });
  test.afterAll(() => audit.flush());

  test('sign-in works with the hashed password and offers no Forgot password', async ({ page }) => {
    await test.step('the first-run form names the password behind the hash', async () => {
      await page.goto('/');
      await expect(page.getByRole('heading', { name: 'Welcome to Bracket Creator' })).toBeVisible();
      await expect(page.getByLabel('Admin Password (from TOURNAMENT_PASSWORD_HASH)')).toBeVisible();
      await expect(page.getByText('This server is running in locked mode.')).toBeVisible();
      await shot(page, 'first-run-locked');
    });

    await test.step('create the tournament with that password', async () => {
      await createTournament(page, { name: 'Locked Cup', courts: 1, password: PASSWORD });
      await shot(page, 'created');
    });

    await test.step('the sign-in dialog has no Forgot password?, and the password signs in', async () => {
      await page.getByRole('button', { name: 'Sign out' }).tap();
      // The dialog decides on Forgot password? from the auth config: wait for
      // that answer, so its absence is not just the config not having come yet.
      const authConfig = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/auth-config' && r.ok());
      await page.goto('/admin');
      const dialog = page.locator('.modal.auth');
      await expect(dialog.getByText('Admin sign in')).toBeVisible();
      await authConfig;
      await dwell(page, Date.now());
      await expect(dialog.getByRole('button', { name: 'Forgot password?' })).toHaveCount(0);
      await shot(page, 'sign-in-no-forgot');
      await login(page, PASSWORD);
      await expect(page.getByRole('heading', { level: 1, name: 'Locked Cup' })).toBeVisible();
      record({ journey: 'organiser-locked', step: 'sign in', signedIn: true });
    });
  });

  test('the /reset page says the operator disabled it', async ({ page }) => {
    await page.goto('/reset');
    await expect(page.getByRole('heading', { name: 'Password reset disabled' })).toBeVisible();
    await expect(page.getByText('password reset disabled by the operator', { exact: false })).toBeVisible();
    await expect(page.locator('#reset-password')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Reset password' })).toHaveCount(0);
    await shot(page, 'reset-disabled');
  });
});
