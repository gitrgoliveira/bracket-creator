// The engi score editor (admin_scoring_engi.jsx) in render tests, over the
// same window and faked outbox as the individual editor (individual_editor.js:
// installIndividualEditor in beforeAll, resetOutbox in beforeEach). The
// editor is reached as every host reaches it, through ScoreEditorModal, which
// hands an engi match (compEngi) to EngiScoreEditorModal.
import { NOW } from './individual_editor.js';

export { installIndividualEditor, resetOutbox, API, NOW } from './individual_editor.js';

// A running engi pool match: sideA is Aka, sideB is Shiro.
export const engiBout = (overrides = {}) => ({
  id: 'm1', compId: 'c1', compEngi: true, status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Aka One - Aka Two' }, sideB: { id: 'p2', name: 'Shiro One - Shiro Two' },
  flagsA: 0, flagsB: 0, modifiedAt: NOW, scoreVersion: 3,
  ...overrides,
});

// The counts the board shows, Shiro first as on the board.
export const counts = (container) => [
  Number(container.querySelector('[data-testid="engi-shiro-count"]').textContent),
  Number(container.querySelector('[data-testid="engi-aka-count"]').textContent),
];
export const btn = (container, testid) => container.querySelector(`[data-testid="${testid}"]`);
