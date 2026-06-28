// Tests for Fighting Spirit awards: AwardsView rendering + API method.
// Covers:
//   - FightingSpiritSection renders awards separate from the podium
//   - FightingSpiritSection renders nothing when the list is empty/absent
//   - API.updateCompetitionAwards sends correct URL, method, headers, body
import { describe, it, expect, vi, afterEach } from 'vitest';
import { FightingSpiritSection, AwardsView } from '../viewer.jsx';
import { API } from '../api_client.jsx';

// --- Tree helpers (mirrors viewer.test.jsx) ---

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

function findByTestId(node, testId) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) {
    for (const k of node) {
      const found = findByTestId(k, testId);
      if (found) return found;
    }
    return null;
  }
  if (node.props?.['data-testid'] === testId) return node;
  const childResults = [
    ...(Array.isArray(node.children) ? node.children : node.children ? [node.children] : []),
    ...(Array.isArray(node.props?.children) ? node.props.children : node.props?.children ? [node.props.children] : []),
  ];
  for (const child of childResults) {
    const found = findByTestId(child, testId);
    if (found) return found;
  }
  return null;
}

// --- FightingSpiritSection ---

describe('FightingSpiritSection', () => {
  it('renders nothing when fsAwards is undefined', () => {
    const result = FightingSpiritSection({ fsAwards: undefined, isFs: false });
    expect(result).toBeNull();
  });

  it('renders nothing when fsAwards is null', () => {
    const result = FightingSpiritSection({ fsAwards: null, isFs: false });
    expect(result).toBeNull();
  });

  it('renders nothing when fsAwards is an empty array', () => {
    const result = FightingSpiritSection({ fsAwards: [], isFs: false });
    expect(result).toBeNull();
  });

  it('renders a section with the correct testId when awards are present', () => {
    const awards = [
      { title: 'Fighting Spirit', recipientName: 'Alice Yamada', recipientDojo: 'Shinjuku' },
    ];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    const section = findByTestId(result, 'fighting-spirit-section');
    expect(section).not.toBeNull();
  });

  it('renders each award row with the correct testId', () => {
    const awards = [
      { title: 'Fighting Spirit', recipientName: 'Alice', recipientDojo: 'Shinjuku' },
      { title: 'Best Technique', recipientName: 'Bob' },
    ];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    expect(findByTestId(result, 'fighting-spirit-award-0')).not.toBeNull();
    expect(findByTestId(result, 'fighting-spirit-award-1')).not.toBeNull();
  });

  it('includes recipient name in rendered text', () => {
    const awards = [{ title: 'Spirit', recipientName: 'Carol Ito', recipientDojo: 'Osaka' }];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    const text = collectText(result);
    expect(text).toContain('Carol Ito');
  });

  it('includes dojo in rendered text when present', () => {
    const awards = [{ title: 'Spirit', recipientName: 'Dan', recipientDojo: 'Kyoto' }];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    const text = collectText(result);
    expect(text).toContain('Kyoto');
  });

  it('does not render dojo element when absent', () => {
    const awards = [{ title: 'Spirit', recipientName: 'Eve' }];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    const text = collectText(result);
    expect(text).not.toContain('undefined');
  });

  it('renders title text in the award row', () => {
    const awards = [{ title: 'Kantōshō', recipientName: 'Fumi' }];
    const result = FightingSpiritSection({ fsAwards: awards, isFs: false });
    const text = collectText(result);
    expect(text).toContain('Kantōshō');
  });
});

// --- AwardsView: Fighting Spirit section is independent of the podium ---
//
// NOTE: With the static React stub, child component vnodes are NOT executed :
// they appear as {type: FightingSpiritSection, props: {...}} in the tree.
// We assert via the vnode type (component reference), which is reliable
// and doesn't require a reactive runtime.

function findFightingSpiritVnode(node) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) {
    for (const k of node) {
      const found = findFightingSpiritVnode(k);
      if (found) return found;
    }
    return null;
  }
  // The vnode emitted by JSX for <FightingSpiritSection ...> has type === FightingSpiritSection.
  if (node.type === FightingSpiritSection) return node;
  const kids = node.children || node.props?.children;
  if (kids) {
    return findFightingSpiritVnode(kids);
  }
  return null;
}

describe('AwardsView + Fighting Spirit awards', () => {
  const fsAwards = [
    { title: 'Fighting Spirit', recipientName: 'Grace Hayashi', recipientDojo: 'Sapporo' },
  ];

  it('includes a FightingSpiritSection vnode when bracket has no final winner (no-podium branch)', () => {
    // A bracket with no winner → awards array empty → early-return branch.
    // The FS section vnode must still be in the tree.
    const comp = { id: 'c1', name: 'Test Comp', format: 'playoffs', fightingSpiritAwards: fsAwards };
    const result = AwardsView({ c: comp, bracket: null, standings: null, pools: null, players: [] });
    const fsVnode = findFightingSpiritVnode(result);
    expect(fsVnode).not.toBeNull();
    // Props must carry the awards.
    expect(fsVnode.props.fsAwards).toEqual(fsAwards);
  });

  it('does NOT include a FightingSpiritSection vnode when comp has no awards (empty-bracket branch)', () => {
    const comp = { id: 'c2', name: 'Test Comp', format: 'playoffs', fightingSpiritAwards: [] };
    const result = AwardsView({ c: comp, bracket: null, standings: null, pools: null, players: [] });
    const fsVnode = findFightingSpiritVnode(result);
    expect(fsVnode).toBeNull();
  });

  it('includes a FightingSpiritSection vnode alongside a completed podium', () => {
    const bracket = {
      rounds: [
        [],
        [
          { sideA: 'Alice', sideB: 'Bob', winner: 'Alice' },
          { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' },
        ],
        [{ sideA: 'Alice', sideB: 'Carol', winner: 'Alice' }],
      ],
    };
    const comp = { id: 'c3', name: 'Comp', format: 'playoffs', fightingSpiritAwards: fsAwards };
    const result = AwardsView({ c: comp, bracket, standings: null, pools: null, players: [] });
    // The main tree text must include the champion name.
    expect(collectText(result)).toContain('Alice');
    // The FS vnode must also be in the tree, separate from podium.
    const fsVnode = findFightingSpiritVnode(result);
    expect(fsVnode).not.toBeNull();
    expect(fsVnode.props.fsAwards).toEqual(fsAwards);
  });

  it('the FightingSpiritSection vnode is NOT nested inside a podium-place element', () => {
    const bracket = {
      rounds: [
        [],
        [{ sideA: 'X', sideB: 'Y', winner: 'X' }],
        [{ sideA: 'X', sideB: 'Z', winner: 'X' }],
      ],
    };
    const comp = { id: 'c4', name: 'Comp', format: 'playoffs', fightingSpiritAwards: fsAwards };
    const result = AwardsView({ c: comp, bracket, standings: null, pools: null, players: [] });
    // The FS vnode must exist.
    const fsVnode = findFightingSpiritVnode(result);
    expect(fsVnode).not.toBeNull();
    // awards-place-1-0 must NOT contain a FightingSpiritSection vnode inside it.
    const place1 = findByTestId(result, 'awards-place-1-0');
    if (place1) {
      expect(findFightingSpiritVnode(place1)).toBeNull();
    }
  });
});

// --- API.updateCompetitionAwards ---

describe('API.updateCompetitionAwards', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('calls PUT /api/competitions/:id/awards with correct headers and body', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'c1', fightingSpiritAwards: [] }),
    });
    const awards = [{ title: 'Fighting Spirit', recipientName: 'Alice', recipientDojo: 'Tokyo' }];
    await API.updateCompetitionAwards('c1', awards, 'mainpw', 'adminpw');

    expect(global.fetch).toHaveBeenCalledWith(
      '/api/competitions/c1/awards',
      expect.objectContaining({
        method: 'PUT',
        headers: expect.objectContaining({
          'Content-Type': 'application/json',
          'X-Tournament-Password': 'mainpw',
          'X-Admin-Password': 'adminpw',
        }),
      })
    );
    const body = JSON.parse(global.fetch.mock.calls[0][1].body);
    expect(body).toEqual({ fightingSpiritAwards: awards });
  });

  it('omits X-Admin-Password when adminPassword is empty', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    await API.updateCompetitionAwards('c1', [], 'mainpw', '');
    const headers = global.fetch.mock.calls[0][1].headers;
    expect(headers).not.toHaveProperty('X-Admin-Password');
  });

  it('throws on non-2xx response with server error message', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: 'title is required' }),
    });
    await expect(API.updateCompetitionAwards('c1', [{}], 'pw', 'adm')).rejects.toThrow('title is required');
  });

  it('returns parsed competition JSON on success', async () => {
    const comp = { id: 'c1', fightingSpiritAwards: [{ title: 'Spirit', recipientName: 'Bob' }] };
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => comp });
    const result = await API.updateCompetitionAwards('c1', comp.fightingSpiritAwards, 'pw', 'adm');
    expect(result.fightingSpiritAwards[0].recipientName).toBe('Bob');
  });
});
