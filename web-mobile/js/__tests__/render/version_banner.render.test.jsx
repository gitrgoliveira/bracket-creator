// bc-evts S2: the one new-build banner. A page whose server runs another
// build reloads by itself once no score editor is open and the outbox is
// idle; until then it shows the banner, whose Reload persists the outbox and
// reloads. Unknown on either side counts as the same build.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let V;
let VersionBanner;

beforeEach(async () => {
  vi.resetModules();
  globalThis.React = React;
  document.head.innerHTML = '<meta name="app-build" content="build-a">';
  V = await import('../../app_version.jsx');
  ({ VersionBanner } = await import('../../version_banner.jsx'));
  V.resetVersionForTests();
});

afterEach(() => {
  document.head.innerHTML = '';
  vi.resetModules();
});

describe('VersionBanner', () => {
  it('shows nothing while the builds match (or either is unknown)', () => {
    render(<VersionBanner />);
    act(() => { V.noteServerBuild('build-a'); });
    expect(screen.queryByTestId('version-banner')).toBeNull();
    act(() => { V.noteServerBuild('unknown'); });
    expect(screen.queryByTestId('version-banner')).toBeNull();
  });

  it('with an editor open, a new build shows the banner instead of reloading', () => {
    const reload = vi.fn();
    V.setReloader(reload);
    const close = V.registerOpenEditor();
    render(<VersionBanner onReload={reload} />);
    act(() => { V.noteServerBuild('build-b'); });
    expect(screen.getByTestId('version-banner').textContent).toContain(V.VERSION_BANNER_TEXT);
    expect(reload).not.toHaveBeenCalled();
    // Reload on the banner is the operator's choice.
    fireEvent.click(screen.getByRole('button', { name: V.VERSION_RELOAD_LABEL }));
    expect(reload).toHaveBeenCalledTimes(1);
    act(() => { close(); });
  });

  it('reloads by itself once the editor closes and the outbox is idle', () => {
    const reload = vi.fn();
    const saved = vi.fn();
    V.setReloader(reload);
    V.setBeforeReload(saved);
    let idle = false;
    V.setOutboxIdleProbe(() => idle);
    const close = V.registerOpenEditor();
    render(<VersionBanner />);
    act(() => { V.noteServerBuild('build-b'); });
    expect(screen.getByTestId('version-banner')).toBeTruthy();
    act(() => { close(); });
    // The editor is gone but a write is still unsent: still the banner.
    expect(reload).not.toHaveBeenCalled();
    idle = true;
    act(() => { V.noteOutboxDrained(); });
    expect(saved).toHaveBeenCalledTimes(1);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('version-banner')).toBeNull();
  });
});
