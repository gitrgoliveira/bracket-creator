// Tests for the public "All winners / Results" view:
//   - buildAllWinnersPublic (aggregation logic, mirroring admin_all_winners.test.jsx)
//   - AllWinnersView (component rendering states)
//
// Both are exported to window by viewer.jsx.
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { bracketHasDecidedFinal, resolveCompetitionAwards, deriveAwards } from '../viewer.jsx';

// ── tree helpers ──────────────────────────────────────────────────────────────

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

function findByTestId(node, testId) {
  const all = findAll(node, (n) => n.props && n.props['data-testid'] === testId);
  return all[0] || null;
}

// ── module setup / teardown ───────────────────────────────────────────────────

const STUBBED_GLOBALS = {
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
  formatLabelShort: (f) => f,
  formatDate: (d) => d,
  competitionKindLabel: (c) => c.kind === 'team' ? 'Teams' : 'Individual',
  StatusBadge: function StatusBadge() { return null; },
  formatViewerHeaderEyebrow: () => '',
  formatLabel: (f) => f,
  hasBothSides: () => true,
  compareDmy: () => 0,
  useEscapeToClose: vi.fn(),
  AppRouter: null,
  API: {
    fetchCompetitionDetails: vi.fn(),
    swissStandings: vi.fn(),
    branding: vi.fn(),
    bind: vi.fn(),
  },
  // Real viewer helpers so resolveCompetitionAwards works correctly.
  deriveAwards,
  bracketHasDecidedFinal,
  resolveCompetitionAwards,
};
const originalGlobals = {};

beforeAll(async () => {
  for (const [key, stub] of Object.entries(STUBBED_GLOBALS)) {
    originalGlobals[key] = { had: key in window, value: window[key] };
    window[key] = stub;
  }
  // viewer.jsx is already imported (it's the source of the exported functions at
  // the top of this file). All window.* exports are set by the module load,
  // including buildAllWinnersPublic and AllWinnersView.
});

afterAll(() => {
  for (const [key, orig] of Object.entries(originalGlobals)) {
    if (orig.had) window[key] = orig.value;
    else delete window[key];
  }
});

// ── buildAllWinnersPublic ─────────────────────────────────────────────────────

describe('buildAllWinnersPublic', () => {
  it('is exported to window', () => {
    expect(typeof window.buildAllWinnersPublic).toBe('function');
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

    const results = await window.buildAllWinnersPublic([comp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].comp.id).toBe('ko-1');
    expect(results[0].state).toBe('final');
    expect(results[0].podium[0].place).toBe(1);
    expect(results[0].podium[1].place).toBe(2);
    expect(results[0].podium[2].place).toBe(3);
    expect(results[0].podium[3].place).toBe(3);
    expect(results[0].podium).toHaveLength(4);
  });

  it('filters out linked playoffs comp (sourceCompID set) — not fetched or returned', async () => {
    const mixedComp = { id: 'mixed-1', name: 'Pools+KO', format: 'mixed', status: 'completed' };
    const playoffComp = { id: 'po-1', name: 'Playoffs', format: 'playoffs', sourceCompID: 'mixed-1', status: 'completed' };
    const bracket = {
      rounds: [
        [{ sideA: 'Alice', sideB: 'Bob', winner: 'Alice' }, { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' }],
        [{ sideA: 'Alice', sideB: 'Carol', winner: 'Alice' }],
      ],
    };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket, standings: null, pools: null, players: [] });

    const results = await window.buildAllWinnersPublic([mixedComp, playoffComp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    // shell comp must be filtered out entirely — never fetched, never returned
    expect(fetchCompetitionDetails).not.toHaveBeenCalledWith('po-1');
    expect(results.find(r => r.comp.id === 'po-1')).toBeUndefined();
    // parent mixed comp is resolved normally
    const mixedResult = results.find(r => r.comp.id === 'mixed-1');
    expect(mixedResult).toBeDefined();
    expect(mixedResult.state).toBe('final');
    expect(mixedResult.podium).toHaveLength(4);
  });

  it('mixed comp whose OWN knockout final is undecided → state "in-progress", podium []', async () => {
    const mixedComp = { id: 'mixed-2', name: 'Pools+KO', format: 'mixed', status: 'completed' };
    const allComps = [mixedComp];
    // undecided final
    const bracket = {
      rounds: [
        [{ sideA: 'Alice', sideB: 'Bob', winner: 'Alice' }, { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' }],
        [{ sideA: 'Alice', sideB: 'Carol', winner: null }],
      ],
    };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket, standings: null, pools: null, players: [] });

    const results = await window.buildAllWinnersPublic(allComps, {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].state).toBe('in-progress');
    expect(results[0].podium).toEqual([]);
  });

  it('returns podium for a standings-based competition (league format)', async () => {
    const standings = [
      { player: { name: 'Alice', dojo: 'Aoyama' } },
      { player: { name: 'Bob', dojo: 'Bunkyo' } },
      { player: { name: 'Carol', dojo: 'Chiba' } },
      { player: { name: 'Dan', dojo: 'Denenchofu' } },
    ];
    const comp = { id: 'league-1', name: 'League', format: 'league', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings, pools: [{ poolName: 'Pool A' }], config: comp, players: [] });

    const results = await window.buildAllWinnersPublic([comp], {
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

  it('only processes completed competitions (non-completed are skipped)', async () => {
    const running = { id: 'r-1', name: 'Running', format: 'playoffs', status: 'pools', players: [] };
    const setup = { id: 's-1', name: 'Setup', format: 'playoffs', status: 'setup', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings: null, pools: null, players: [] });

    const results = await window.buildAllWinnersPublic([running, setup], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(0);
    expect(fetchCompetitionDetails).not.toHaveBeenCalled();
  });

  it('fetches swissStandings for swiss-format competitions', async () => {
    const swissData = [
      { player: { name: 'Kenji', dojo: 'Kendo Club' } },
      { player: { name: 'Hiro', dojo: 'Musashi Dojo' } },
    ];
    const comp = { id: 'swiss-1', name: 'Swiss', format: 'swiss', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockResolvedValue({ bracket: null, standings: null, pools: null, config: { format: 'swiss' }, players: [] });
    const mockSwissStandings = vi.fn().mockResolvedValue(swissData);

    const results = await window.buildAllWinnersPublic([comp], {
      fetchCompetitionDetails,
      swissStandings: mockSwissStandings,
    });

    expect(mockSwissStandings).toHaveBeenCalledWith('swiss-1');
    expect(results[0].state).toBe('final');
    expect(results[0].podium[0].name).toBe('Kenji');
  });

  it('returns error field when fetchCompetitionDetails throws, without rejecting the whole Promise', async () => {
    const comp = { id: 'err-1', name: 'Broken', format: 'playoffs', status: 'completed', players: [] };
    const fetchCompetitionDetails = vi.fn().mockRejectedValue(new Error('Network error'));

    const results = await window.buildAllWinnersPublic([comp], {
      fetchCompetitionDetails,
      swissStandings: null,
    });

    expect(results).toHaveLength(1);
    expect(results[0].podium).toEqual([]);
    expect(results[0].error).toBe('Network error');
  });
});

// ── AllWinnersView component ──────────────────────────────────────────────────

describe('AllWinnersView', () => {
  it('is exported to window', () => {
    expect(typeof window.AllWinnersView).toBe('function');
  });

  it('renders without throwing with empty tournament', () => {
    expect(() => window.AllWinnersView({
      tournament: { name: 'Test Tournament', competitions: [] },
      onBack: vi.fn(),
    })).not.toThrow();
  });

  it('shows loading state initially (useState returns initial value in static stub)', () => {
    const vnode = window.AllWinnersView({
      tournament: { name: 'Test', competitions: [] },
      onBack: vi.fn(),
    });
    const text = collectText(vnode);
    // Initial state is loading:true — should render loading text
    expect(text).toContain('Loading results');
  });

  it('renders the page title "Results"', () => {
    const vnode = window.AllWinnersView({
      tournament: { name: 'Test Tournament', competitions: [] },
      onBack: vi.fn(),
    });
    const text = collectText(vnode);
    expect(text).toContain('Results');
  });

  it('renders tournament name in the eyebrow', () => {
    const vnode = window.AllWinnersView({
      tournament: { name: 'My Championship', competitions: [] },
      onBack: vi.fn(),
    });
    const text = collectText(vnode);
    expect(text).toContain('My Championship');
  });

  it('renders a back button', () => {
    const vnode = window.AllWinnersView({
      tournament: { name: 'Test', competitions: [] },
      onBack: vi.fn(),
    });
    const btns = findAll(vnode, (n) => n.type === 'button' && n.props && n.props['aria-label'] === 'Back');
    expect(btns.length).toBeGreaterThan(0);
  });

  it('calls onBack when the back button is clicked', () => {
    const onBack = vi.fn();
    const vnode = window.AllWinnersView({
      tournament: { name: 'Test', competitions: [] },
      onBack,
    });
    const btns = findAll(vnode, (n) => n.type === 'button' && n.props && n.props['aria-label'] === 'Back');
    btns[0].props.onClick();
    expect(onBack).toHaveBeenCalled();
  });

  it('loading state has the expected data-testid', () => {
    const vnode = window.AllWinnersView({
      tournament: { name: 'Test', competitions: [] },
      onBack: vi.fn(),
    });
    const loadingNode = findByTestId(vnode, 'all-winners-loading');
    expect(loadingNode).not.toBeNull();
  });
});
