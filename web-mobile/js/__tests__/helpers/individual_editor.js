// The window the individual score editor (admin_scoring_individual.jsx) reads
// in render tests, with the outbox's tap surface faked (fake_outbox.js).
// Call installIndividualEditor() in beforeAll and resetOutbox() in beforeEach.
import { vi } from 'vitest';
import { installWindowStubs } from './stub_globals.js';
import { makeFakeOutbox } from './fake_outbox.js';

export const API = {
  fetchCompetitionDetails: vi.fn(),
  recordScore: vi.fn(),
  recordDaihyosen: vi.fn(),
  removeDaihyosen: vi.fn(),
  putMatchLineup: vi.fn(),
  recordDecision: vi.fn(),
};

const stubs = () => ({
  isHikiwake: (v) => v === 'hikiwake',
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API,
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  // eslint-disable-next-line react/prop-types
  Term: ({ children }) => children,
  GlossaryHint: () => null,
});

// installIndividualEditor installs the window and answers the editor
// component and the restore function.
export async function installIndividualEditor() {
  const restore = installWindowStubs(stubs());
  await import('../../admin_scoring_modal.jsx');
  return { ScoreEditorModal: window.ScoreEditorModal, restore };
}

// resetOutbox gives each test a fresh fake outbox and a recordScore that
// answers at once, as the outbox's door does.
export function resetOutbox(opts) {
  const outbox = makeFakeOutbox(opts);
  Object.assign(API, outbox.api);
  API.recordScore = vi.fn().mockResolvedValue({ applied: true, pending: true });
  API.fetchCompetitionDetails = vi.fn().mockResolvedValue({ config: {} });
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  return outbox;
}

export const NOW = 1_800_000_000_000;

// A running individual pool bout.
export const bout = (overrides = {}) => ({
  id: 'm1', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], modifiedAt: NOW, scoreVersion: 3,
  ...overrides,
});

export const letterBtn = (side, l) => [...side.querySelectorAll('.ipt-btn')].find((b) => b.textContent.trim() === l);
export const shiroSide = (c) => c.querySelector('.sb-side--shiro');
export const akaSide = (c) => c.querySelector('.sb-side--aka');
// The scored marks the board shows, by their slot labels.
export const marks = (c) => [...c.querySelectorAll('[aria-label*=" slot "]')]
  .map((s) => s.getAttribute('aria-label')).filter((l) => /: remove /.test(l));
