// bc-evts stage 3, S2: the app reloads itself onto a newer server build when
// nothing would be lost (no score editor open, the outbox idle), and otherwise
// shows the banner until it can. One rule for the app and every display.
import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
    bundleBuild, noteServerBuild, buildsMatch, registerOpenEditor, setOutboxIdleProbe,
    setReloader, setBeforeReload, checkVersion, noteOutboxDrained, subscribeVersionState,
    versionState, resetVersionForTests, VERSION_BANNER_TEXT, VERSION_RELOAD_LABEL,
} from '../app_version.jsx';

function setPageBuild(build) {
    document.head.querySelectorAll('meta[name="app-build"]').forEach((m) => m.remove());
    if (build === null) return;
    const meta = document.createElement('meta');
    meta.setAttribute('name', 'app-build');
    meta.setAttribute('content', build);
    document.head.appendChild(meta);
}

let reload;
let calls;
beforeEach(() => {
    resetVersionForTests();
    setPageBuild('abc');
    calls = [];
    reload = vi.fn(() => calls.push('reload'));
    setReloader(reload);
    setBeforeReload(() => calls.push('beforeReload'));
});

describe('bundleBuild', () => {
    it('reads the page meta once', () => {
        expect(bundleBuild()).toBe('abc');
        setPageBuild('other');
        expect(bundleBuild()).toBe('abc');
    });
    it('is unknown without the meta', () => {
        setPageBuild(null);
        expect(bundleBuild()).toBe('unknown');
    });
});

describe('buildsMatch', () => {
    it('matches equal builds and an unknown on either side', () => {
        expect(buildsMatch('a', 'a')).toBe(true);
        expect(buildsMatch('a', 'b')).toBe(false);
        expect(buildsMatch('unknown', 'b')).toBe(true);
        expect(buildsMatch('a', 'unknown')).toBe(true);
    });
});

describe('checkVersion', () => {
    it('is current before any server build is known', () => {
        expect(checkVersion()).toBe('current');
        expect(reload).not.toHaveBeenCalled();
    });

    it('reloads once on a newer server build with no editor open and an idle outbox, persisting first', () => {
        noteServerBuild('def');
        expect(calls).toEqual(['beforeReload', 'reload']);
        expect(versionState()).toBe('reloading');
        noteServerBuild('def');
        noteServerBuild('ghi');
        noteOutboxDrained();
        checkVersion();
        expect(reload).toHaveBeenCalledTimes(1);
    });

    it('reads an unknown build on either side as a match', () => {
        noteServerBuild('unknown');
        expect(versionState()).toBe('current');
        resetVersionForTests();
        setReloader(reload);
        setPageBuild(null);
        noteServerBuild('def');
        expect(versionState()).toBe('current');
        expect(reload).not.toHaveBeenCalled();
    });

    it('ignores an empty or missing build', () => {
        noteServerBuild('');
        noteServerBuild(null);
        noteServerBuild(undefined);
        expect(versionState()).toBe('current');
        expect(reload).not.toHaveBeenCalled();
    });

    it('is stale while an editor is open and reloads once it closes', () => {
        const unregister = registerOpenEditor();
        noteServerBuild('def');
        expect(versionState()).toBe('stale');
        expect(reload).not.toHaveBeenCalled();
        unregister();
        expect(calls).toEqual(['beforeReload', 'reload']);
    });

    it('counts editors, and a double unregister (StrictMode) counts once', () => {
        const a = registerOpenEditor();
        const b = registerOpenEditor();
        noteServerBuild('def');
        a();
        a();
        expect(versionState()).toBe('stale');
        b();
        expect(reload).toHaveBeenCalledTimes(1);
    });

    it('is stale while the outbox holds writes and reloads when it drains idle', () => {
        let idle = false;
        setOutboxIdleProbe(() => idle);
        noteServerBuild('def');
        expect(versionState()).toBe('stale');
        noteOutboxDrained();
        expect(versionState()).toBe('stale');
        idle = true;
        noteOutboxDrained();
        expect(reload).toHaveBeenCalledTimes(1);
    });

    it('goes back to current when the server is back on the bundle build', () => {
        registerOpenEditor();
        noteServerBuild('def');
        expect(versionState()).toBe('stale');
        noteServerBuild('abc');
        expect(versionState()).toBe('current');
    });

    it('tells subscribers of each change', () => {
        const seen = [];
        const unsub = subscribeVersionState((s) => seen.push(s));
        const close = registerOpenEditor();
        noteServerBuild('def');
        close();
        unsub();
        expect(seen).toEqual(['stale', 'reloading']);
    });
});

it('words the banner', () => {
    expect(VERSION_BANNER_TEXT).toBe('A new version of the app is installed. Reload to keep scoring.');
    expect(VERSION_RELOAD_LABEL).toBe('Reload');
});
