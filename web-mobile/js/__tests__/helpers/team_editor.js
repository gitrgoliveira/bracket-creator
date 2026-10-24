// The team score editor (admin_scoring_team.jsx) in render tests, over the same
// window and faked outbox as the individual editor (individual_editor.js:
// installIndividualEditor in beforeAll, resetOutbox in beforeEach). The editor
// is reached as every host reaches it, through ScoreEditorModal, which hands a
// team match (compKind "team") to TeamScoreEditorModal.
import { NOW } from './individual_editor.js';

export { installIndividualEditor, resetOutbox, API, NOW } from './individual_editor.js';

// A bout row as the server stores it.
export const boutRow = (position, overrides = {}) => ({
  position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '',
  ...overrides,
});

// A running three-bout fixed-order team match: sideA (Kyoto) is Aka, sideB
// (Osaka) is Shiro. A knockout match unless `phase` says otherwise.
export const teamBout = (overrides = {}) => ({
  id: 'tm1', compId: 'c1', status: 'running', phase: 'bracket', court: 'A', compKind: 'team', teamSize: 3,
  sideA: { id: 'ta', name: 'Kyoto' }, sideB: { id: 'tb', name: 'Osaka' },
  subResults: [boutRow(1), boutRow(2), boutRow(3)], modifiedAt: NOW, scoreVersion: 4,
  ...overrides,
});

// A running kachinuki encounter (teamMatchType on the match, as the hosts
// stamp it): K1 (Aka) against O1 (Shiro) on bout 1.
export const kachinukiBout = (overrides = {}) => teamBout({
  id: 'km1', teamMatchType: 'kachinuki', phase: 'pool',
  subResults: [boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1' })],
  ...overrides,
});

// The bout rows the sheet renders, in order; a side of one (color "shiro" or
// "aka"); a ippon button, a mark slot and the per-bout controls.
export const rows = (container) => [...container.querySelectorAll('.team-sub-match')];
export const side = (container, idx, color) => rows(container)[idx].querySelector(`.team-sub-match__side--${color}`);
export const ipponBtn = (container, idx, color, letter) => [...side(container, idx, color).querySelectorAll('button.ipt-btn')].find((b) => b.textContent === letter);
export const markSlots = (container, idx, color) => [...rows(container)[idx].querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)];
export const marks = (container, idx, color) => markSlots(container, idx, color).map((b) => b.textContent).filter((t) => t !== '·');
export const fusenshoBtn = (container, idx, color) => side(container, idx, color).querySelector('[data-testid="scoring-modal-fusensho-button"]');
export const tieBtn = (container, idx) => rows(container)[idx].querySelector('[data-testid="scoring-modal-tie-button"]');
export const foulBtn = (container, idx, color, add) => side(container, idx, color).querySelector(`button[aria-label="${add ? 'Add' : 'Remove'} a ${color === 'shiro' ? 'SHIRO' : 'AKA'} foul"]`);
export const byTestId = (container, testid) => container.querySelector(`[data-testid="${testid}"]`);
