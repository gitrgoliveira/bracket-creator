// Operator decision F1 (2026-10-19): on the team score sheet the Fusensho
// button HOLDS ITS PLACE when pressed, as Encho and Undo encho do. Pressing it
// changed its label to "✓ Fusensho", the button grew, wrapped onto a line of
// its own and moved, and a quick second tap landed on H beside it. The button
// now keeps room for its longer label in both states (.holds-label with the
// longer label in data-reserve, laid hidden in the same grid cell by CSS), and
// the kachinuki footer hint that went with the bout's first result keeps its
// line (the overlay is centred, so a line leaving the footer moved the sheet).
//
// jsdom lays nothing out, so this pins the structure; the geometry is measured
// in a real browser by the e2e row that double-taps Fusensho
// (knockout-mixed-kachinuki.spec.mjs, bout 8).
import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { FUSENSHO_PRESSED_LABEL } from '../../admin_scoring_team.jsx';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue({ applied: true }),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});
afterAll(() => restoreGlobals());
beforeEach(() => { Object.assign(window.API, makeFakeOutbox().api); vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

const BOUT_ROWS = [1, 2, 3].map((position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' }));
const match = (over = {}) => ({
  id: 'tm-hold', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3, sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
  subResults: BOUT_ROWS, ...over,
});

const fusenshoButton = (color) => document.querySelectorAll('.team-sub-match')[0]
  .querySelector(`.team-sub-match__side--${color} [data-testid="scoring-modal-fusensho-button"]`);

describe('the Fusensho button holds its place when pressed', () => {
  it('keeps room for its longer label in both states, and shows one label', async () => {
    await act(async () => {
      render(<ScoreEditorModal match={match()} onClose={vi.fn()} onSubmit={vi.fn()} password="" />);
    });
    const before = fusenshoButton('shiro');
    expect(before.classList.contains('holds-label')).toBe(true);
    expect(before.getAttribute('data-reserve')).toBe(FUSENSHO_PRESSED_LABEL);
    expect(before.textContent).toBe('Fusensho');

    await act(async () => { fireEvent.click(before); });
    const after = fusenshoButton('shiro');
    expect(after.textContent).toBe(FUSENSHO_PRESSED_LABEL);
    expect(after.classList.contains('holds-label'), 'the same box in the pressed state').toBe(true);
    expect(after.getAttribute('data-reserve')).toBe(FUSENSHO_PRESSED_LABEL);
    expect(FUSENSHO_PRESSED_LABEL.length).toBeGreaterThan('Fusensho'.length);
  });

  it('the stylesheet lays the reserved label hidden in the label\'s own cell', () => {
    const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../../css/styles.css'), 'utf8');
    expect(css).toMatch(/\.holds-label\s*\{[^}]*display:\s*inline-grid/);
    expect(css).toMatch(/\.holds-label::after\s*\{[^}]*content:\s*attr\(data-reserve\);[^}]*visibility:\s*hidden/);
    expect(css).toMatch(/\.holds-label > \.holds-label__text,\s*\.holds-label::after\s*\{[^}]*grid-area:\s*1 \/ 1/);
  });

  it('a kachinuki sheet keeps the footer hint\'s line once the bout has a result', async () => {
    const kachinuki = match({ teamMatchType: 'kachinuki', subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' }] });
    await act(async () => {
      render(<ScoreEditorModal match={kachinuki} onClose={vi.fn()} onSubmit={vi.fn()} password="" />);
    });
    const shown = () => document.querySelector('[data-testid="kachinuki-end-hint"]');
    const space = () => document.querySelector('[data-testid="kachinuki-end-hint-space"]');
    // One or the other always takes the line.
    expect(!!shown() !== !!space()).toBe(true);
    await act(async () => { fireEvent.click(fusenshoButton('shiro')); });
    expect(!!shown() !== !!space(), 'the line stays taken after the bout has a result').toBe(true);
    if (space()) expect(space().classList.contains('holds-space')).toBe(true);
  });
});
