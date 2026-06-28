// Thin entry for the score-editor module (mp-zac3).
// Sets window.ScoreEditorModal (window-bridge consumers: admin_shiaijo.jsx,
// admin_schedule.jsx, admin_pools.jsx, viewer.jsx) and re-exports the
// complete named-export surface so existing importers keep resolving.
//
// Implementation lives in the sibling modules:
//   admin_scoring_autosave.jsx   : autosave/sync infra
//   admin_scoring_team.jsx       : team helpers + TeamScoreEditorModal
//   admin_scoring_individual.jsx : ScoreEditorModal (individual editor)

import { ScoreEditorModal } from './admin_scoring_individual.jsx';

import {
  teamResultLabel,
  isKoTieBlocked,
  resolveMatchLineup,
  resolveLineupTeamId,
} from './admin_scoring_team.jsx';

import {
  AUTOSAVE_DEBOUNCE_MS,
  SyncStatusPill,
  useDebouncedRunningWrite,
} from './admin_scoring_autosave.jsx';

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
  assertRunningWritePersisted,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
  nextEnchoPeriod,
  prevEnchoPeriod,
  initialEnchoPeriodsForMatch,
  daihyosenEnchoFields,
  decideDrawToggle,
  shouldBlockScoringKeys,
  DecisionPrompt,
} from './admin_scoring_shared.jsx';

window.ScoreEditorModal = ScoreEditorModal;

export {
  useDebouncedRunningWrite,
  SyncStatusPill,
  AUTOSAVE_DEBOUNCE_MS,
  resolveDecisionPassword,
  assertRunningWritePersisted,
  buildDecisionBody,
  submitDecisionRequest,
  makeSubmitDecision,
  shouldShowEnchoMaxBanner,
  canIncrementEncho,
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
