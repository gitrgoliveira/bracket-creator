import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// Two surfaces in admin_setup.jsx, mounted with REAL React.
//
// 1. The tournament's "Number of Shiaijo (courts)" field: the FIRST place a
//    shiaijo count is typed, and the only one that said nothing about the
//    per-competition count rule. An organiser typed 3 here and met the refusal
//    two screens later, where it reads as a verdict on their venue.
//
// 2. The bulk-import page: each row error rendered as a .tag-badge--warn,
//    which is 10px uppercase letter-spaced styling meant for a one- or
//    two-word tag, carrying a 220-character sentence with the raw `courts:`
//    API prefix still on it. The preview table also showed no allocation at
//    all, so an operator importing ten competitions had every row refused with
//    nothing shown beforehand.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  SponsorsManager: Stub('SponsorsManager'),
  BrandingManager: Stub('BrandingManager'),
  isNonPublicOrigin: () => false,
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptAdminPassword: vi.fn().mockResolvedValue(null),
  API: {
    importCompetitions: vi.fn().mockResolvedValue({ results: [] }),
    updateTournament: vi.fn().mockResolvedValue(null),
  },
};

const originals = {};
let AdminEditTournament;
let AdminImportPage;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_setup.jsx');
  AdminEditTournament = window.AdminEditTournament;
  AdminImportPage = window.AdminImportPage;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

const makeTournament = (over = {}) => ({
  name: 'Spring Taikai',
  date: '01-07-2026',
  venue: 'Budokan',
  courts: ['A', 'B', 'C'],
  durationDays: 1,
  mode: 'officiated',
  competitions: [],
  ...over,
});

async function mountEdit(tournament = makeTournament()) {
  let result;
  await act(async () => {
    result = render(
      <AdminEditTournament
        tournament={tournament}
        onSave={noop}
        onCancel={noop}
        onLogout={noop}
        onViewerMode={noop}
        authConfig={{ mode: 'file' }}
        password=""
        showToast={noop}
      />
    );
  });
  return result;
}

const venueHint = (container) => {
  const el = container.querySelector('[data-testid="venue-shiaijo-hint"]');
  return el && el.textContent;
};

describe('Tournament venue field teaches the rule up front (U3)', () => {
  it('says any number is fine and scopes the rule to each competition', async () => {
    const { container } = await mountEdit();
    const hint = venueHint(container);
    expect(hint).not.toBeNull();
    expect(hint).toContain('Pick the number your venue actually has: any number is fine.');
    expect(hint).toContain('This is a rule about each competition, never about your venue.');
  });

  it('answers "does my third shiaijo just sit idle?" at the field', async () => {
    const { container } = await mountEdit();
    expect(venueHint(container)).toContain(
      'With 3 shiaijo you can run one competition on 2 and another on the remaining 1 at the same time, so all 3 stay busy.'
    );
  });

  it('follows the number being typed', async () => {
    const { container } = await mountEdit();
    // Selector derived from the constant, not a literal: the input's max IS
    // MAX_COURTS, so a change to the cap must not quietly stop matching here.
    const input = container.querySelector(`input[type="number"][max="${window.MAX_COURTS}"]`);
    expect(input).not.toBeNull();
    await act(async () => { fireEvent.change(input, { target: { value: '5' } }); });
    const hint = venueHint(container);
    expect(hint).toContain('Each competition then runs on 1, 2 or 4 of them.');
    expect(hint).toContain('on 4 and another on the remaining 1');
  });

  it('drops the split for a venue that is itself a legal count', async () => {
    const { container } = await mountEdit(makeTournament({ courts: ['A', 'B', 'C', 'D'] }));
    const hint = venueHint(container);
    expect(hint).toContain('This is a rule about each competition, never about your venue.');
    expect(hint).not.toContain('at the same time');
  });
});

async function mountImport(tournament = makeTournament()) {
  let result;
  await act(async () => {
    result = render(
      <AdminImportPage
        tournament={tournament}
        onBack={noop}
        onImported={noop}
        onLogout={noop}
        onViewerMode={noop}
        password=""
      />
    );
  });
  return result;
}

// The preview and results panels are driven by component state that only a
// file selection / upload fills. Selecting a real folder is not reachable from
// jsdom, so the panels are exercised by driving the same inputs the handlers
// set: a manifest.json File through the hidden <input>, and a stubbed
// importCompetitions response through the Import button.
// Both panels appear only after an async hop the component owns: FileReader's
// onload for the preview, the awaited POST for the results. How many macrotask
// ticks that takes depends on machine load, so a fixed `setTimeout(0)` passed
// in isolation and failed under the full suite. Spin inside act() until the
// node the component MUST produce exists (RTL's waitFor cannot be used here:
// nested inside act() its polling never advances).
async function flushUntil(container, selector, tries = 50) {
  for (let i = 0; i < tries; i++) {
    if (container.querySelector(selector)) return;
    await act(async () => { await new Promise((r) => setTimeout(r, 5)); });
  }
  expect(container.querySelector(selector), `never rendered: ${selector}`).not.toBeNull();
}

function manifestFile(body) {
  return new File([JSON.stringify(body)], 'manifest.json', { type: 'application/json' });
}

async function selectManifest(container, body) {
  const input = Array.from(container.querySelectorAll('input[type="file"]'))
    .find((el) => (el.getAttribute('accept') || '').includes('.json'));
  expect(input).not.toBeUndefined();
  const file = manifestFile(body);
  Object.defineProperty(input, 'files', { value: [file], configurable: true });
  await act(async () => { fireEvent.change(input); });
  await flushUntil(container, '.parse-preview tbody tr');
}

describe('Import preview shows the shiaijo allocation (U4)', () => {
  it('adds a Shiaijo column, resolving an omitted courts key to the venue', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'mudansha', name: 'Mudansha', format: 'mixed', participants: 'm.csv' }],
    });
    const headers = Array.from(container.querySelectorAll('.parse-preview th')).map((th) => th.textContent);
    expect(headers).toContain('Shiaijo');
    const cell = container.querySelector('[data-testid="preview-shiaijo-cell"]');
    expect(cell.textContent).toContain('A, B, C');
    expect(cell.textContent).toContain('(venue)');
  });

  it('flags the rows that would be refused and explains it once', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [
        { id: 'a', name: 'Mudansha', format: 'mixed', participants: 'a.csv' },
        { id: 'b', name: 'Yudansha', format: 'mixed', courts: ['A', 'B'], participants: 'b.csv' },
      ],
    });
    const cells = Array.from(container.querySelectorAll('[data-testid="preview-shiaijo-cell"]'));
    expect(cells[0].className).toContain('cell--missing');
    expect(cells[1].className).toBe('');
    const warning = container.querySelector('[data-testid="preview-shiaijo-warning"]');
    expect(warning).not.toBeNull();
    expect(warning.textContent).toContain('1 competition would be refused');
    expect(warning.textContent).toContain('This competition can use 1 or 2 shiaijo (this tournament has 3)');
    expect(warning.textContent).toContain('never about your venue');
  });

  it('says nothing when every previewed allocation is legal', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'a', name: 'Mudansha', format: 'mixed', courts: ['A', 'B'], participants: 'a.csv' }],
    });
    expect(container.querySelector('[data-testid="preview-shiaijo-warning"]')).toBeNull();
    expect(container.querySelector('[data-testid="preview-shiaijo-cell"]').className).toBe('');
  });

  it('leaves a league row unflagged: the count rule does not govern it', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'a', name: 'League', format: 'league', participants: 'a.csv' }],
    });
    expect(container.querySelector('[data-testid="preview-shiaijo-warning"]')).toBeNull();
  });
});

describe('Import row errors read as prose, not as a tag (U4)', () => {
  const runImport = async (container, results) => {
    window.API.importCompetitions.mockResolvedValueOnce({ results });
    window.confirmDialog.mockResolvedValueOnce(true);
    window.promptAdminPassword.mockResolvedValueOnce('');
    const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === 'Import');
    await act(async () => { fireEvent.click(btn); });
    await flushUntil(container, '[data-testid="import-result-row"]');
  };

  const REFUSAL = 'courts: shiaijo count must be a power of two (1, 2, 4, 8 or 16), got 3: use 2 or 4, or 1; the knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly';

  it('renders the reason as body text with the raw courts: prefix stripped', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'a', name: 'Mudansha', format: 'mixed', participants: 'a.csv' }],
    });
    await runImport(container, [{ id: 'a', name: 'Mudansha', error: REFUSAL }]);

    const err = container.querySelector('[data-testid="import-result-error"]');
    expect(err).not.toBeNull();
    expect(err.className).toContain('import-result__error');
    expect(err.className).not.toContain('tag-badge');
    expect(err.textContent.startsWith('Shiaijo count must be a power of two')).toBe(true);
    expect(err.textContent).not.toContain('courts:');
  });

  it('keeps a short status tag beside the name', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'a', name: 'Mudansha', format: 'mixed', participants: 'a.csv' }],
    });
    await runImport(container, [{ id: 'a', name: 'Mudansha', error: REFUSAL }]);
    const tag = container.querySelector('[data-testid="import-result-row"] .tag-badge--warn');
    expect(tag.textContent).toBe('✕ not imported');
    // The 220-character sentence is never inside the uppercase tag.
    expect(tag.textContent.length).toBeLessThan(30);
  });

  it('shows no error line for a row that imported', async () => {
    const { container } = await mountImport();
    await selectManifest(container, {
      competitions: [{ id: 'a', name: 'Mudansha', format: 'mixed', courts: ['A', 'B'], participants: 'a.csv' }],
    });
    await runImport(container, [{ id: 'a', name: 'Mudansha', participantCount: 8, seedCount: 2 }]);
    expect(container.querySelector('[data-testid="import-result-error"]')).toBeNull();
    const row = container.querySelector('[data-testid="import-result-row"]');
    expect(row.querySelector('.tag-badge').textContent).toBe('✓ imported');
  });
});
