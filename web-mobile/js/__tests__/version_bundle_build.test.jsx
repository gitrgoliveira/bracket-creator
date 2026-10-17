// bc-evts stage 3, review round 1, M2: the version check compares the build the
// RUNNING CODE was compiled for with the server's. /dist/ is cached for five
// minutes, so after a reload a fresh page can run modules from the build
// before: the page (its HTML) says nothing about the code, the bundle's own
// constant (__APP_BUILD__, esbuild --define from the commit the server embeds)
// does. A page that reloaded for a build and still runs the old modules does not
// reload again in a loop: it shows the banner.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

let mod;
let reload;

async function freshPage(bundle) {
    vi.resetModules();
    if (bundle === null) delete globalThis.__APP_BUILD__;
    else globalThis.__APP_BUILD__ = bundle;
    mod = await import('../app_version.jsx');
    reload = vi.fn();
    mod.setReloader(reload);
}

beforeEach(() => {
    sessionStorage.clear();
    document.head.querySelectorAll('meta[name="app-build"]').forEach((m) => m.remove());
});

afterEach(() => {
    delete globalThis.__APP_BUILD__;
    document.head.querySelectorAll('meta[name="app-build"]').forEach((m) => m.remove());
});

describe('the build the code runs is the bundle\'s own', () => {
    it('a fresh page over cached old modules is not current: the page\'s HTML does not speak for the code', async () => {
        // The HTML the server wrote names the new build; the modules are the old one.
        const meta = document.createElement('meta');
        meta.setAttribute('name', 'app-build');
        meta.setAttribute('content', 'new-build');
        document.head.appendChild(meta);
        await freshPage('old-build');
        expect(mod.bundleBuild()).toBe('old-build');
        mod.noteServerBuild('new-build');
        expect(mod.versionState()).not.toBe('current');
    });

    it('a bundle compiled without the constant cannot say, so it never forces a reload', async () => {
        await freshPage(null);
        expect(mod.bundleBuild()).toBe('unknown');
        mod.noteServerBuild('new-build');
        expect(mod.versionState()).toBe('current');
        expect(reload).not.toHaveBeenCalled();
    });

    it('the same build is current', async () => {
        await freshPage('b1');
        mod.noteServerBuild('b1');
        expect(mod.versionState()).toBe('current');
    });
});

describe('a reload that brings back the same old modules does not loop', () => {
    it('reloads once for a build; the next page, still on the old bundle, shows the banner instead', async () => {
        await freshPage('old-build');
        mod.noteServerBuild('new-build');
        expect(reload).toHaveBeenCalledTimes(1);
        // The reload served the cached modules again.
        await freshPage('old-build');
        mod.noteServerBuild('new-build');
        expect(reload).not.toHaveBeenCalled();
        expect(mod.versionState()).toBe('stale');
    });

    it('a later build reloads again', async () => {
        await freshPage('old-build');
        mod.noteServerBuild('new-build');
        await freshPage('old-build');
        mod.noteServerBuild('newer-build');
        expect(reload).toHaveBeenCalledTimes(1);
    });
});
