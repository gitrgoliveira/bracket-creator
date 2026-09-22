// Driving the real interface.
//
// Every selector here is one a human clicks. They are gathered in this file so
// a UI change breaks one place rather than thirty recipes.
// One owner: lib/api.mjs declares the password the seeder and the UI share.
// Imported AND re-exported, not `export ... from`: that form re-exports
// without binding the name locally, so loginAdmin below could not see it.
import { PASSWORD } from './api.mjs';

export { PASSWORD };

// The SPA renders / as an operator when these keys are present, so a public
// capture needs a context that has never logged in.
export async function loginAdmin(page, base) {
  await page.goto(base + '/admin', { waitUntil: 'domcontentloaded' });
  const pw = page.locator('input[type=password]').first();
  await pw.waitFor({ state: 'visible', timeout: 15000 });
  await pw.fill(PASSWORD);
  await pw.press('Enter');
  await page.waitForFunction(
    () => !document.querySelector('input[type=password]'),
    { timeout: 15000 },
  );
}

export async function clearAuth(page, base) {
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => {
    localStorage.removeItem('bc_authed');
    localStorage.removeItem('bc_password');
  });
}

// NOTE: this file deliberately stops at auth. Driving a score editor lives in
// the recipe that needs it, because the individual, fixed-order team and
// kachinuki editors differ in how they score, finish and name a fighter, and
// the shared helpers that once lived here were wrong for two of the three: they
// scoped ippons to the individual board's side wrappers, and located the
// correction-reason box as the last text field, which on a kachinuki row is a
// typeable fighter name. They were exported and imported by nothing. If those
// five local implementations are ever unified, unify them on the team editor's
// behaviour, not the individual one's.

export const settle = (page, ms = 350) => page.waitForTimeout(ms);
