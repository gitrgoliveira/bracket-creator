// Recipe registry.
//
// Each group of captures lives in its own file and exports { families, recipes }
// so the files stay independently editable. A recipe is mostly declarative -
// where to look, how big, what to wait for - with a drive() for the captures
// whose subject IS a procedure (opening an editor, scoring a bout). Pure data
// does not survive those, and forcing it to would grow a step DSL for no gain.
//
// Families exist because several captures share one seeded tournament; the
// runner seeds each family once per run.
import * as admin from './admin.mjs';
import * as adminsetup from './adminsetup.mjs';
import * as webui from './webui.mjs';
import * as publicViews from './public.mjs';
import * as videos from './videos.mjs';
import * as editors from './editors.mjs';
import * as scored from './scored.mjs';

const groups = [admin, adminsetup, webui, publicViews, videos, editors, scored];

// Merge by hand rather than with Object.assign: two files declaring the same
// family key would otherwise silently override one another, and the loser's
// recipes would quietly seed against the winner's fixture.
export const families = {};
for (const group of groups) {
  for (const [key, family] of Object.entries(group.families || {})) {
    // Re-exporting another file's family is how two groups share one seeded
    // tournament, so the same object under the same key is fine. Two DIFFERENT
    // families under one key is the bug: the loser's recipes would silently
    // capture against the winner's fixture.
    if (families[key] && families[key] !== family) {
      throw new Error(`two different fixture families are both called "${key}" - ` +
        'rename one; family keys are global across recipe files');
    }
    families[key] = family;
  }
}
export const recipes = groups.flatMap((g) => g.recipes || []);
