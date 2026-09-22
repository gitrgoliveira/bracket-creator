// Driving the real interface.
//
// Every selector here is one a human clicks. They are gathered in this file so
// a UI change breaks one place rather than thirty recipes.
// One owner: lib/api.mjs declares the password the seeder and the UI share.
// Imported AND re-exported, not `export ... from`: that form re-exports
// without binding the name locally, so authAdmin below could not see it.
import { PASSWORD } from './api.mjs';

export { PASSWORD };

// Sign a context in as the operator, without the login form. The SPA renders
// its admin surfaces when these two localStorage keys are present, so setting
// them before the first navigation is all a capture needs. Driving the form
// instead put a login screen in frame 0 of every video and a form round trip
// in front of every admin screenshot, and left the public captures clearing
// keys that a fresh context never had: a public capture is simply one that
// never calls this. The runner calls it for a recipe declaring `auth: 'admin'`;
// a seed calls it on a context of its own, usually through withAdminPage.
export async function authAdmin(context) {
  await context.addInitScript((pw) => {
    localStorage.setItem('bc_authed', 'true');
    localStorage.setItem('bc_password', pw);
  }, PASSWORD);
}

// A signed-in page for a seed to drive, on a context of its own that is closed
// when fn returns or throws. Pass a viewport when the driven surface lays out
// by width; the editors do.
export async function withAdminPage(browser, viewport, fn) {
  const context = await browser.newContext(viewport ? { viewport } : {});
  await authAdmin(context);
  try {
    return await fn(await context.newPage());
  } finally {
    await context.close();
  }
}

// NOTE: this file deliberately stops at auth. Driving a score editor lives in
// the recipe that needs it, because the individual, fixed-order team and
// kachinuki editors differ in how they score, finish and name a fighter, and
// the shared helpers that once lived here were wrong for two of the three: they
// scoped ippons to the individual board's side wrappers, and located the
// correction-reason box as the last text field, which on a kachinuki row is a
// typeable fighter name. They were exported and imported by nothing. If those
// five local implementations are ever unified, unify them on the team editor's
// behaviour, not the individual one's - and settle first what the editor IS:
// scored.mjs finds it by `[data-testid="scoring-modal-root"], .editor-modal`,
// the other four by `.editor-modal` alone.

export const settle = (page, ms = 350) => page.waitForTimeout(ms);
