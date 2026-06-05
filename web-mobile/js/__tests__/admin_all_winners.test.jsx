// Tests for the "All winners" admin summary: buildAllWinners helper and
// AllWinnersModal component (both exported to window by admin_shell.jsx).
//
// The component is tested via the static React-stub (non-reactive), which
// means useState returns the initial value; we therefore test the loading
// state from the vnode and the async aggregation logic separately via
// buildAllWinners.
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { bracketHasDecidedFinal, resolveCompetitionAwards, deriveAwards } from '../viewer.jsx';

// ── tree helpers ─────────────────────────────────────────────────────────────

function collectText(node) {
  if (node == null || node === false || node === true) return '';
  if (typeof node === 'string') return node;
  if (typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children !== undefined) return collectText(node.children);
  return '';
}

function findAll(node, pred) {
  if (!node || typeof node !== 'object') return [];
  const acc = Array.isArray(node) ? [] : (pred(node) ? [node] : []);
  const kids = Array.isArray(node) ? node : (node.children ?? []);
  for (const k of kids) acc.push(...findAll(k, pred));
  return acc;
}

// ── module setup / teardown ───────────────────────────────────────────────────

// Every global this suite stubs lives here so beforeAll can snapshot the prior
// value and afterAll can restore (or delete) it — otherwise stubs like
// window.API / window.deriveAwards leak into later suites and cause
// order-dependent flakes.
const STUBBED_GLOBALS = {
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
  formatLabelShort: (f) => f,
  formatDate: (d) => d,
  competitionKindLabel: (c) => c.kind === 'team' ? 'Teams' : 'Individual',
  StatusBadge: function StatusBadge() { return null; },
  formatAdminHeaderSub: () => '',
  useEscapeToClose: vi.fn(),
  API: {
    fetchCompetitionDetails: vi.fn(),
    swissStandings: vi.fn(),
  },
  // Real viewer helpers so resolveCompetitionAwards works correctly.
  deriveAwards,
  bracketHasDecidedFinal,
  resolveCompetitionAwards,
};
const originalGlobals = {};

// buildAllWinners and AllWinnersModal are loaded with admin_shell.jsx import.
// They are also exposed on window so we can test them without remounting
// the full AdminDashboard.

beforeAll(async () => {
  for (const [key, stub] of Object.entries(STUBBED_GLOBALS)) {
    originalGlobals[key] = { had: key in window, value: window[key] };
    window[key] = stub;
  }

  await import('../admin_shell.jsx');
});

afterAll(() => {
  for (const [key, orig] of Object.entries(originalGlobals)) {
    if (orig.had) window[key] = orig.value;
    else delete window[key];
  }
});

// ── buildAllWinners ───────────────────────────────────────────────────────────

describe('buildAllWinners', () => {
  it('is exported to window', () => {
    expect(typeof window.buildAllWinners).toBe('function');
  });

  it('returns podium for a standalone knockout competition with 4 placings (two 3rds)', async () => {
    const bracket = {
      rounds: [
        [
          { sideA: 'Alice', sideB: 'Bob', winner: 'Alice' },
          { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' },
        ],
        [{ sideA: 'Alice', sideB: 'Carol', winner: 'Alice' }],
      ],
    };
    const comp = { id: 'ko-1', name: 'Knockout', format: 'playoffs', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket, standings: null, pools: null, config: comp, players: [] });

    const results = await window.buildAllWinners([comp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].comp.id).toBe('ko-1');
    expect(results[0].state).toBe('final');
    // deriveAwards returns champion + runner-up + two thirds
    expect(results[0].podium[0].place).toBe(1);
    expect(results[0].podium[1].place).toBe(2);
    expect(results[0].podium[2].place).toBe(3);
    expect(results[0].podium[3].place).toBe(3);
    expect(results[0].podium).toHaveLength(4);
  });

  it('mixed comp whose OWN knockout final is undecided → state "in-progress", podium []', async () => {
    const mixedComp = { id: 'mixed-2', name: 'Pools+KO', format: 'mixed', status: 'playoffs' };
    // undecided final
    const bracket = {
      rounds: [
        [{ sideA: 'Alice', sideB: 'Bob', winner: 'Alice' }, { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' }],
        [{ sideA: 'Alice', sideB: 'Carol', winner: null }],
      ],
    };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket, standings: null, pools: null, players: [] });

    const results = await window.buildAllWinners([mixedComp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].state).toBe('in-progress');
    expect(results[0].podium).toEqual([]);
  });

  it('returns podium for a pool/standings competition (league format)', async () => {
    const standings = [
      { player: { name: 'Alice', dojo: 'Aoyama' } },
      { player: { name: 'Bob', dojo: 'Bunkyo' } },
      { player: { name: 'Carol', dojo: 'Chiba' } },
      { player: { name: 'Dan', dojo: 'Denenchofu' } },
    ];
    const comp = { id: 'league-1', name: 'League', format: 'league', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings, pools: [{ poolName: 'Pool A' }], config: comp, players: [] });

    const results = await window.buildAllWinners([comp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    const podium = results[0].podium;
    expect(podium[0].name).toBe('Alice');
    expect(podium[1].name).toBe('Bob');
    expect(podium[2].place).toBe(3);
    expect(podium[3].place).toBe(3);
  });

  it('excludes non-completed competitions (caller is responsible for pre-filtering)', async () => {
    const comp = { id: 'running-1', name: 'In Progress', format: 'playoffs', status: 'pools', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings: null, pools: null, config: comp, players: [] });

    // Caller passes only completed comps — if we pass none the result is empty.
    const results = await window.buildAllWinners([], {
      fetchCompetitionDetails,
      swissStandings: null,
    });
    expect(results).toHaveLength(0);
    expect(fetchCompetitionDetails).not.toHaveBeenCalled();
  });

  it('fetches swissStandings for swiss-format competitions and passes them to deriveAwards', async () => {
    const swissStandingsData = [
      { player: { name: 'Kenji', dojo: 'Kendo Club' } },
      { player: { name: 'Hiro', dojo: 'Musashi Dojo' } },
    ];
    const comp = { id: 'swiss-1', name: 'Swiss', format: 'swiss', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings: null, pools: null, config: { format: 'swiss' }, players: [] });
    const mockSwissStandings = vi.fn().mockResolvedValue(swissStandingsData);

    const results = await window.buildAllWinners([comp], {
      fetchCompetitionDetails,
      swissStandings: mockSwissStandings,
    });
    expect(mockSwissStandings).toHaveBeenCalledWith('swiss-1');
    expect(results[0].state).toBe('final');
    // standings-based: Kenji is 1st
    expect(results[0].podium[0].name).toBe('Kenji');
  });

  it('returns error field when fetchCompetitionDetails throws, without rejecting the whole Promise', async () => {
    const comp = { id: 'err-1', name: 'Broken', format: 'playoffs', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockRejectedValue(new Error('Network error'));

    const results = await window.buildAllWinners([comp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].podium).toEqual([]);
    expect(results[0].error).toBe('Network error');
  });
});

// ── AllWinnersModal component ─────────────────────────────────────────────────

describe('AllWinnersModal', () => {
  it('is exported to window', () => {
    expect(typeof window.AllWinnersModal).toBe('function');
  });

  it('renders without throwing', () => {
    expect(() => window.AllWinnersModal({
      comps: [],
      onClose: vi.fn(),
    })).not.toThrow();
  });

  it('renders the modal title', () => {
    const vnode = window.AllWinnersModal({ comps: [], onClose: vi.fn() });
    const text = collectText(vnode);
    expect(text).toContain('All winners');
  });

  it('shows loading state initially (useState returns initial value in static stub)', () => {
    const comp = { id: 'c1', name: 'Open', status: 'completed', format: 'playoffs', players: [] };
    const vnode = window.AllWinnersModal({ comps: [comp], onClose: vi.fn() });
    const text = collectText(vnode);
    // Initial state is loading:true — should render loading text
    expect(text).toContain('Loading results');
  });

  it('renders a Close button', () => {
    const vnode = window.AllWinnersModal({ comps: [], onClose: vi.fn() });
    const btns = findAll(vnode, (n) => n.type === 'button');
    const closeBtn = btns.find((b) => collectText(b).includes('Close'));
    expect(closeBtn).toBeDefined();
  });

  it('calls onClose when the close button is clicked', () => {
    const onClose = vi.fn();
    const vnode = window.AllWinnersModal({ comps: [], onClose });
    const btns = findAll(vnode, (n) => n.type === 'button');
    const closeBtn = btns.find((b) => collectText(b).includes('Close'));
    closeBtn.props.onClick();
    expect(onClose).toHaveBeenCalled();
  });
});
