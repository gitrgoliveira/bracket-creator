// The first-run tournament form, the admin sign-in form, Sign out, and the
// sign-in-or-create step the journeys start from. Every selector for those
// lives here.
import { expect } from '@playwright/test';
import { dwell } from './pace.mjs';

const PASSWORD = 'e2e-operator';

// A form field by its visible label (each label is tied to its input with
// htmlFor, app.jsx). Anchored at the start: "Number of Shiaijo (courts)" and a
// locked server's "Admin Password (from ...)" carry more than the words.
const field = (page, label) => page.getByLabel(new RegExp(`^${label}`));

// The first-run "Welcome to Bracket Creator" form (app.jsx), shown on a
// server with no tournament yet. Creating one signs this page in, so a
// journey that wants the login form next calls signOut() then login().
export async function createTournament(page, {
  name = 'E2E Cup', venue = 'Test Hall', courts = 2, mode = 'Officiated', password = PASSWORD,
} = {}) {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Welcome to Bracket Creator' })).toBeVisible();
  await field(page, 'Tournament Name').fill(name);
  await field(page, 'Venue').fill(venue);
  await field(page, 'Number of Shiaijo').fill(String(courts));
  await page.getByRole('group', { name: 'Tournament type' }).getByRole('button', { name: mode }).tap();
  await field(page, 'Admin Password').fill(password);
  await page.getByRole('button', { name: 'Create Tournament' }).tap();
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

// Sign in, creating the tournament first when this file's server has none yet.
// Every test in a file can call it, whichever runs first or in which worker.
// Returns { created } so a caller can tell the first run from a later one.
export async function enterTournament(page, { name = 'E2E Cup', courts = 2 } = {}) {
  await page.goto('/');
  // The first-run form, or the public home of a tournament that exists:
  // whichever the first paint shows, however long it takes.
  const welcome = page.getByRole('heading', { name: 'Welcome to Bracket Creator' });
  const home = page.locator('.viewer__body');
  await welcome.or(home).first().waitFor({ state: 'visible' });
  if (await welcome.isVisible()) {
    await createTournament(page, { name, courts });
    return { created: true };
  }
  await login(page);
  return { created: false };
}

export async function signOut(page) {
  await page.getByRole('button', { name: 'Sign out' }).tap();
  await expect(page.getByRole('button', { name: 'Sign out' })).toHaveCount(0);
}

// Sign in through the real form: /admin on a signed-out page opens the
// "Admin sign in" dialog (app.jsx). Never the localStorage shortcut
// scripts/screenshots uses: the form is part of what a journey walks.
export async function login(page, password = PASSWORD) {
  await page.goto('/admin');
  const form = page.locator('.modal.auth');
  await expect(form.getByText('Admin sign in')).toBeVisible();
  const shown = Date.now();
  await form.locator('#admin-password').fill(password);
  // The dialog ignores a tap inside the bounce window of its opening.
  await dwell(page, shown);
  await form.getByRole('button', { name: 'Sign in' }).tap();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}
