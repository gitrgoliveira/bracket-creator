// bc-evts stage 3 (S2): a new build installed while a score editor is open
// never reloads the page under the operator: the banner shows instead, and
// closing the editor (with the outbox idle) reloads by itself.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout } from '../helpers/individual_editor.js';
import * as V from '../../app_version.jsx';
import { VersionBanner } from '../../version_banner.jsx';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => { restore(); document.head.innerHTML = ''; });
beforeEach(() => {
  resetOutbox();
  document.head.innerHTML = '<meta name="app-build" content="build-a">';
  V.resetVersionForTests();
});

describe('a new build while the score editor is open', () => {
  it('keeps the banner while the editor is open; closing it reloads', async () => {
    const reload = vi.fn();
    V.setReloader(reload);
    V.setOutboxIdleProbe(() => true);
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" />);
    render(<VersionBanner />);
    act(() => { V.noteServerBuild('build-b'); });
    expect(screen.getByTestId('version-banner').textContent).toContain(V.VERSION_BANNER_TEXT);
    expect(reload).not.toHaveBeenCalled();
    act(() => { r.unmount(); });
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('the public self-run sheet is the same editor and holds the reload too', async () => {
    const reload = vi.fn();
    V.setReloader(reload);
    V.setOutboxIdleProbe(() => true);
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={vi.fn()} password="" selfReport />);
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).not.toHaveBeenCalled();
    act(() => { r.unmount(); });
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
