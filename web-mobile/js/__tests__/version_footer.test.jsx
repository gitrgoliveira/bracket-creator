import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { VersionFooter } from '../app.jsx';
import { makeReactive } from './helpers/reactive_react.js';

describe('VersionFooter', () => {
  let runtime, realReact;

  beforeEach(() => {
    realReact = global.React;
    runtime = makeReactive();
    global.React = runtime.React;
    vi.useFakeTimers();
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete window.appVersionInfo;
    vi.useRealTimers();
  });

  it('renders a semver version with GitHub link', () => {
    window.appVersionInfo = { version: 'v1.2.3' };
    runtime.mount(VersionFooter, {});
    vi.advanceTimersByTime(200); // trigger setInterval check

    // The tree should be a div containing the versionText and the a tag
    const treeStr = JSON.stringify(runtime.currentTree());
    expect(treeStr).toContain('v1.2.3');
    expect(treeStr).toContain('https://github.com/gitrgoliveira/bracket-creator');
    expect(treeStr).toContain('app-version-link');
  });

  it('renders a non-semver version using gitCommit and buildDate with GitHub link', () => {
    window.appVersionInfo = { version: 'dev', gitCommit: 'fc928e0', buildDate: '2026-06-16' };
    runtime.mount(VersionFooter, {});
    vi.advanceTimersByTime(200);

    const treeStr = JSON.stringify(runtime.currentTree());
    expect(treeStr).toContain('fc928e0');
    expect(treeStr).toContain('2026-06-16');
    expect(treeStr).toContain('https://github.com/gitrgoliveira/bracket-creator');
    expect(treeStr).toContain('app-version-link');
  });

  it('renders nothing if version is missing', () => {
    window.appVersionInfo = { version: '' };
    runtime.mount(VersionFooter, {});
    vi.advanceTimersByTime(200);

    expect(runtime.currentTree()).toBeNull();
  });
});
