// Thin entry for admin_schedule — imports submodules, re-exports ES surface,
// and assigns window.* for non-module consumers. (mp-d7tl)
// Double-eval trap (mp-zd1v): this file has no <script type=module> tag of
// its own in index.html. Loaded as a static import from admin.js — one eval
// per tab, so window.* assignments happen exactly once.

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

// ES surface for the vitest suite. computeCourtPaceStats, filterMatchesByCourt,
// and CourtPacePanel use `export function` at declaration in their submodules —
// re-export them here so admin_schedule.jsx stays the canonical import target.
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
