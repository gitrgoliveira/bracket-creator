import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { VersionFooter } from '../app.jsx';
import { makeReactive } from './helpers/reactive_react.js';

describe('VersionFooter', () => {
  let runtime, realReact;

  beforeEach(() => {
    realReact = global.React;
    runtime = makeReactive();
    global.React = runtime.React;
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete window.appVersionInfo;
    delete window.versionPromise;
    delete window.API;
  });

  it('renders a semver version with GitHub link', () => {
    window.appVersionInfo = { version: 'v1.2.3' };
    runtime.mount(VersionFooter, {});

    // The tree should be a div containing the versionText and the a tag
    const treeStr = JSON.stringify(runtime.currentTree());
    expect(treeStr).toContain('v1.2.3');
    expect(treeStr).toContain('https://github.com/gitrgoliveira/bracket-creator');
    expect(treeStr).toContain('app-version-link');
  });

  it('renders a non-semver version using gitCommit and buildDate with GitHub link', () => {
    window.appVersionInfo = { version: 'dev', gitCommit: 'fc928e0', buildDate: '2026-06-16' };
    runtime.mount(VersionFooter, {});

    const treeStr = JSON.stringify(runtime.currentTree());
    expect(treeStr).toContain('fc928e0');
    expect(treeStr).toContain('2026-06-16');
    expect(treeStr).toContain('https://github.com/gitrgoliveira/bracket-creator');
    expect(treeStr).toContain('app-version-link');
  });

  it('renders nothing if version is missing', () => {
    window.appVersionInfo = { version: '' };
    runtime.mount(VersionFooter, {});

    expect(runtime.currentTree()).toBeNull();
  });

  it('fetches version info on demand if missing and renders it', async () => {
    // leave window.appVersionInfo undefined
    let resolveFetch;
    window.API = {
      fetchVersion: () => new Promise(res => { resolveFetch = res; })
    };

    runtime.mount(VersionFooter, {});
    // initially null because promise is pending
    expect(runtime.currentTree()).toBeNull();

    // resolve the promise
    resolveFetch({ version: 'v2.0.0' });
    // flush microtask queue
    await Promise.resolve();
    await Promise.resolve();

    const treeStr = JSON.stringify(runtime.currentTree());
    expect(treeStr).toContain('v2.0.0');
    expect(treeStr).toContain('app-version-link');
  });
});
