import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';

// Tests for the "Share registration link" feature added in mp-a1jz:
// CompCard share-button visibility (canShare predicate); asserts which
// tournament/competition configurations show or hide the share button.

let CompCard;

const STUBBED_GLOBALS = {
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
  formatLabelShort: (f) => f,
  formatDate: (d) => d,
  competitionKindLabel: () => 'Individual',
  StatusBadge: function StatusBadge() { return null; },
  useEscapeToClose: vi.fn(),
};
const originalGlobals = {};

beforeAll(async () => {
  for (const [key, stub] of Object.entries(STUBBED_GLOBALS)) {
    originalGlobals[key] = { had: key in window, value: window[key] };
    window[key] = stub;
  }
  await import('../admin_shell.jsx');
  CompCard = window.CompCard;
});

afterAll(() => {
  for (const [key, orig] of Object.entries(originalGlobals)) {
    if (orig.had) window[key] = orig.value;
    else delete window[key];
  }
});

// Recursively gather string/number leaves from the React-stub vnode tree.
function collectText(node) {
  if (node == null || node === false || node === true) return '';
  if (typeof node === 'string') return node;
  if (typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children !== undefined) return collectText(node.children);
  return '';
}

function hasShareButton(vnode) {
  const text = collectText(vnode);
  return text.includes('Share registration link');
}

describe('CompCard share-button visibility', () => {
  const noop = () => {};

  const selfRunTournament = { mode: 'self-run' };
  const officiatedTournament = { mode: 'officiated' };

  it('shows share button for self-run individual comp in setup status', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'setup', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(true);
  });

  it('shows share button when status is empty/null (pre-setup)', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: '', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(true);
  });

  it('hides share button for officiated tournament', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'setup', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: officiatedTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });

  it('hides share button for team competition in self-run mode', () => {
    const c = { id: 'team', name: 'Team', kind: 'team', format: 'KO', status: 'setup', courts: [], players: [], teamSize: 5 };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });

  it('hides share button for started competition (pools)', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'pools', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });

  it('hides share button for started competition (playoffs)', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'playoffs', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });

  it('hides share button for completed competition', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'completed', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, tournament: selfRunTournament, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });

  it('hides share button when tournament prop is missing', () => {
    const c = { id: 'men', name: 'Men', kind: 'individual', format: 'KO', status: 'setup', courts: [], players: [] };
    const vnode = CompCard({ c, onOpen: noop, onStart: noop, showToast: noop });
    expect(hasShareButton(vnode)).toBe(false);
  });
});
