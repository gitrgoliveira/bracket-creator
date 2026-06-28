// Thin entry for admin_schedule : imports submodules, re-exports ES surface,
// and assigns window.* for non-module consumers. (mp-d7tl)
// Double-eval guard (mp-zd1v): this file IS loaded via its own
// <script type="module" src="/dist/admin_schedule.js?v=5"> tag in index.html,
// and is NOT ES-imported by admin.js or any other module : consumers like
// admin.js read the window.* globals assigned below. The SUBMODULES
// (admin_schedule_utils, _pacing, _lineup, _export, _score_editor, _page) have
// NO <script> tags of their own; they evaluate only via the import chain from
// this file. So everything loads exactly once per tab.
// Do NOT add a <script> tag for a submodule, and do NOT add an ES `import` of
// this file anywhere : either would register a second module-URL identity and
// double-eval, re-running all window.* assignments.

import {
  timeEdited, timeToMinutes, clampMatchDuration, allMatchesCompleted,
} from './admin_schedule_utils.jsx';
import {
  filterMatchesByCourt, computeCourtPaceStats, CourtPacePanel,
  suggestRebalances, PerCourtBreakdown,
} from './admin_schedule_pacing.jsx';
import { pickCopySource, MatchLineupPanel } from './admin_schedule_lineup.jsx';
import { AdminExport } from './admin_schedule_export.jsx';
import { startPatch, AdminScoreEditor, AdminScoreEditorPage } from './admin_schedule_score_editor.jsx';
import { AdminSchedulePage } from './admin_schedule_page.jsx';

// ES surface for the vitest suite : every symbol the unit tests import is
// re-exported here so admin_schedule.jsx stays the single canonical import
// target even though each lives in its own submodule.
export {
  timeEdited, timeToMinutes, clampMatchDuration, allMatchesCompleted,
  filterMatchesByCourt, computeCourtPaceStats, CourtPacePanel,
  suggestRebalances, pickCopySource, MatchLineupPanel,
};

window.AdminSchedulePage = AdminSchedulePage;
window.PerCourtBreakdown = PerCourtBreakdown;
window.AdminScoreEditorPage = AdminScoreEditorPage;
window.AdminScoreEditor = AdminScoreEditor;
window.AdminExport = AdminExport;
window.filterMatchesByCourt = filterMatchesByCourt;
window.startPatch = startPatch;
// Reused by the shiaijo operator console so team-match lineups can be set
// without leaving that page (mp-c2yr).
window.MatchLineupPanel = MatchLineupPanel;
