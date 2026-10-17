// bc-evts stage 3, review round 2, R7/R8 (operator decision S2 a: "the server
// writes its build into the served page"). The build a page runs is the
// <meta name="app-build"> the server wrote into it; /dist/ is served no-cache
// with its ETag, so a reload brings the modules of that same build. A page that
// reloaded for a build and still reads another does not reload again in a loop:
// it shows the banner. A tab that cannot note the reload in sessionStorage
// notes it on the address it reloads to (stage 3 review 8, L2), so the page it
// loads stops the loop all the same.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

let mod;
let reload;

function servePage(build) {
    document.head.querySelectorAll('meta[name="app-build"]').forEach((m) => m.remove());
    if (build === null) return;
    const meta = document.createElement('meta');
    meta.setAttribute('name', 'app-build');
    meta.setAttribute('content', build);
    document.head.appendChild(meta);
}

async function freshPage(build) {
    vi.resetModules();
    servePage(build);
    mod = await import('../app_version.jsx');
    reload = vi.fn();
    mod.setReloader(reload);
}

beforeEach(() => { sessionStorage.clear(); });

afterEach(() => { servePage(null); });

describe('the build a page runs is the one the server wrote into it', () => {
    it('reads the page meta once', async () => {
        await freshPage('b1');
        expect(mod.bundleBuild()).toBe('b1');
        servePage('other');
        expect(mod.bundleBuild()).toBe('b1');
    });

    it('a page without the meta cannot say, so it never forces a reload', async () => {
        await freshPage(null);
        expect(mod.bundleBuild()).toBe('unknown');
        mod.noteServerBuild('new-build');
        expect(mod.versionState()).toBe('current');
        expect(reload).not.toHaveBeenCalled();
    });

    it('the same build is current; another reloads', async () => {
        await freshPage('b1');
        mod.noteServerBuild('b1');
        expect(mod.versionState()).toBe('current');
        mod.noteServerBuild('b2');
        expect(reload).toHaveBeenCalledTimes(1);
        expect(mod.versionState()).toBe('reloading');
    });
});

describe('a reload never loops', () => {
    it('reloads once for a build; a next page still naming another build shows the banner', async () => {
        await freshPage('old');
        mod.noteServerBuild('new');
        expect(reload).toHaveBeenCalledTimes(1);
        await freshPage('old');
        mod.noteServerBuild('new');
        expect(reload).not.toHaveBeenCalled();
        expect(mod.versionState()).toBe('stale');
    });

    it('a later build reloads again', async () => {
        await freshPage('old');
        mod.noteServerBuild('new');
        await freshPage('new');
        mod.noteServerBuild('newer');
        expect(reload).toHaveBeenCalledTimes(1);
    });

    // Stage 3 review 8, L2: with sessionStorage refused, the reload is noted
    // on the address it goes to, so the page it loads knows it reloaded for
    // that build and shows the banner rather than reloading again.
    it('a tab that cannot note the reload in sessionStorage reloads once, by an address that notes it', async () => {
        const refuse = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceededError'); });
        const said = vi.spyOn(console, 'warn').mockImplementation(() => {});
        try {
            history.replaceState(null, '', '/court/A?view=queue#top');
            await freshPage('old');
            mod.noteServerBuild('new');
            expect(reload).toHaveBeenCalledTimes(1);
            const to = new URL(reload.mock.calls[0][0], location.href);
            expect(to.searchParams.get('bc_reloaded_for'), 'the address names the build').toBe('new');
            expect(to.pathname + '?view=' + to.searchParams.get('view') + to.hash, 'and keeps the page').toBe('/court/A?view=queue#top');
            expect(mod.versionState()).toBe('reloading');
            // The page that address loads, storage still refused, the server still on 'new'.
            history.replaceState(null, '', to.pathname + to.search + to.hash);
            await freshPage('old');
            expect(location.search, 'the note is taken off the address').toBe('?view=queue');
            expect(location.hash).toBe('#top');
            mod.noteServerBuild('new');
            expect(reload, 'never a second reload for the same build').not.toHaveBeenCalled();
            expect(mod.versionState()).toBe('stale');
            // A later build still reloads, once.
            mod.noteServerBuild('newer');
            expect(reload).toHaveBeenCalledTimes(1);
        } finally {
            refuse.mockRestore();
            said.mockRestore();
            history.replaceState(null, '', '/');
        }
    });
});
