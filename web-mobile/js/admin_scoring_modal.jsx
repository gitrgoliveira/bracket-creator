// Thin entry for the score-editor module (mp-zac3).
// Sets window.ScoreEditorModal (window-bridge consumers: admin_shiaijo.jsx,
// admin_schedule.jsx, admin_pools.jsx, viewer.jsx) and re-exports the
// complete named-export surface so existing importers keep resolving.
//
// Implementation lives in the sibling modules:
//   admin_scoring_autosave.jsx   : autosave/sync infra
//   admin_scoring_team.jsx       : team helpers + TeamScoreEditorModal
//   admin_scoring_individual.jsx: ScoreEditorModal (individual editor)

import { ScoreEditorModal } from './admin_scoring_individual.jsx';
import { EngiScoreEditorModal } from './admin_scoring_engi.jsx';
import { registerUnsavedInput } from './app_version.jsx';

import {
  teamResultLabel,
  isKoTieBlocked,
  resolveMatchLineup,
  resolveLineupTeamId,
} from './admin_scoring_team.jsx';

import { SyncStatusPill } from './admin_scoring_autosave.jsx';

import {
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  getIpponButtons,
  getValidPointKeys,
  applyFusenshoToggle,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  resolveDecisionPassword,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  DecisionPrompt,
} from './admin_scoring_shared.jsx';

// bc-evts S2: an open score editor holds a new build's reload back (the
// reload waits, behind the banner, until no editor is open). Every host
// reaches the editors through these window bridges, the self-run public
// sheet included, so the editors register here, on mount.
function withOpenEditorRegistered(Editor) {
  function OpenEditor(props) {
    React.useEffect(() => registerUnsavedInput(), []);
    return <Editor {...props} />;
  }
  return OpenEditor;
}

window.ScoreEditorModal = withOpenEditorRegistered(ScoreEditorModal);
window.EngiScoreEditorModal = withOpenEditorRegistered(EngiScoreEditorModal);

export {
  EngiScoreEditorModal,
  SyncStatusPill,
  resolveDecisionPassword,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  DecisionPrompt,
  MAX_IPPONS_PER_SIDE,
  isBoutDecided,
  applyFoulIncrement,
  reconcileFoulsAtOpen,
  nextFoulOnDecrement,
  applyFusenshoToggle,
  getIpponButtons,
  getValidPointKeys,
  resolveMatchLineup,
  resolveLineupTeamId,
  teamResultLabel,
  isKoTieBlocked,
};
