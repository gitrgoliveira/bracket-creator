// The `test` every journey imports: Playwright's own, plus a server per spec
// file, an operator page whose pointer is verified coarse, and the console
// policy every page of the test is held to.
import { test as base, expect } from '@playwright/test';
import { start } from '../../screenshots/lib/server.mjs';
import { auditsFor } from './audit.mjs';
import { isCoarse, restoreCoarse } from './pointer.mjs';

export { isCoarse };

// ---------------------------------------------------------------- console --
//
// The console policy (operator decision 2026-10-11). Every page of every
// browser context a journey opens is listened to: the default page, and every
// context a spec opens itself with browser.newContext, in a test or in a
// beforeAll hook. A console ERROR or an uncaught page error FAILS the test
// unless it matches CONSOLE_ALLOWLIST; a console WARNING is recorded (an audit
// row when the spec keeps an audit, else a CONSOLE line in the run log) and
// never fails. The check runs when the test ends, never at the first message,
// so a failing journey still leaves its screenshots, audit and trace.
//
// The allowlist is for noise a journey causes ON PURPOSE and nothing else.
// Each entry names the message as narrowly as it can, with one line saying
// which scenario makes it. An error that looks like the app's own fault is
// never added here: it is left failing for a person to decide. An entry's
// fields, all of which must match:
//
//   text        the message, as the browser or the app logs it
//   url         where it came from (a failed request's URL, or the script)
//   code        the app's refusal code in the failed /api answer's body
//   whileOffline  only while the page's context is offline (or just back)
const CONSOLE_ALLOWLIST = [
  {
    reason: 'A journey takes the device offline on purpose (clumsy interrupt "offline", the offline-finish journeys); the browser logs each /api request it could not send.',
    text: /^Failed to load resource: net::ERR_INTERNET_DISCONNECTED$/,
    url: /\/api\//,
    whileOffline: true,
  },
  {
    reason: 'The court console logs its queue refresh failing while the journey has the device offline (fetchCourtMatches, admin_shiaijo.jsx); it refreshes again once back online.',
    text: /^Failed to fetch court matches TypeError: Failed to fetch\b/,
    url: /\/dist\/admin_shiaijo\.js$/,
    whileOffline: true,
  },
  {
    reason: 'A start or reopen the app sends through the server\'s gate and shows refused (toast, row notice, court-busy panel, reopen error): the shiaijo is running another bout, a competitor is fighting elsewhere or barred by a withdrawal, or the knockout match it feeds is being fought.',
    text: /^Failed to load resource: the server responded with a status of 409 \(Conflict\)$/,
    url: /\/api\/competitions\/[^/]+\/matches\/[^/]+\/(score|reopen)$/,
    code: /^(court_busy|ineligible_competitor|downstream_knockout_running)$/,
  },
];

// The `error` field of a refusal body; read with a pattern, since the body is
// kept only in part.
const refusalCode = (body) => (/"error"\s*:\s*"([^"]+)"/.exec(body || '') || [])[1] || null;
const allowed = (e) => e.kind === 'console' && CONSOLE_ALLOWLIST.find((r) => r.text.test(e.text)
  && (!r.url || r.url.test(e.url || ''))
  && (!r.code || r.code.test(refusalCode(e.body) || ''))
  && (!r.whileOffline || e.offline));

// One collector per worker. Pages report into `pending`, and each test's
// policy fixture drains it when the test ends: what a beforeAll hook's pages
// said lands in the first test of the file, marked as such. Two facts are kept
// beside each message so an allowlist entry can be narrow: whether the page's
// context was offline when it was logged (a journey's setOffline, or the
// moment after it came back, while the requests sent offline still fail), and,
// for the browser's "Failed to load resource" line, the /api answer it refers
// to (status and the start of its body, where the app's refusal code is).
const OFFLINE_GRACE_MS = 3_000;
function consoleCollector() {
  const pending = [];
  const failed = []; // { url, status, body, at }
  const seen = new WeakSet();
  const offline = new WeakMap(); // context -> { now, until }
  const wasOffline = (ctx) => {
    const o = offline.get(ctx);
    return !!o && (o.now || Date.now() <= o.until);
  };
  const attachPage = (page) => {
    if (seen.has(page)) return;
    seen.add(page);
    const pageUrl = () => { try { return page.url(); } catch { return null; } };
    const ctx = page.context();
    page.on('console', (msg) => {
      const type = msg.type();
      if (type !== 'error' && type !== 'warning') return;
      const loc = msg.location() || {};
      pending.push({ kind: 'console', type, text: msg.text(), url: loc.url || null, line: loc.lineNumber ?? null,
        page: pageUrl(), offline: wasOffline(ctx), at: Date.now() });
    });
    page.on('pageerror', (err) => {
      pending.push({ kind: 'pageerror', type: 'error', text: err.message, stack: err.stack || null, page: pageUrl(),
        offline: wasOffline(ctx), at: Date.now() });
    });
    page.on('response', async (res) => {
      if (res.status() < 400 || !/\/api\//.test(res.url())) return;
      const at = Date.now();
      const body = await res.text().then((t) => t.slice(0, 1000), () => null);
      failed.push({ url: res.url(), status: res.status(), body, at });
    });
  };
  const attachContext = (ctx) => {
    if (seen.has(ctx)) return;
    seen.add(ctx);
    const setOffline = ctx.setOffline;
    ctx.setOffline = async (on) => {
      offline.set(ctx, { now: !!on, until: on ? Infinity : Date.now() + OFFLINE_GRACE_MS });
      await setOffline.call(ctx, on);
      if (!on) offline.set(ctx, { now: false, until: Date.now() + OFFLINE_GRACE_MS });
    };
    ctx.on('page', attachPage);
    for (const p of ctx.pages()) attachPage(p);
  };
  // The answer a "Failed to load resource" line is about: the same URL,
  // answered within a few seconds of it.
  const answerFor = (e) => {
    if (e.kind !== 'console' || !e.url || !/^Failed to load resource/.test(e.text)) return null;
    const near = failed.filter((f) => f.url === e.url && Math.abs(f.at - e.at) < 5_000);
    return near.length ? near[near.length - 1] : null;
  };
  const drain = () => {
    const out = pending.splice(0, pending.length).map((e) => {
      const answer = answerFor(e);
      return answer ? { ...e, status: answer.status, body: answer.body } : e;
    });
    failed.splice(0, failed.length);
    return out;
  };
  return { attachPage, attachContext, drain };
}

const describeEntry = (e) => `${e.kind === 'pageerror' ? 'uncaught page error' : `console ${e.type}`} on ${e.page || '(no page)'}: ${e.text}${e.url ? ` (from ${e.url}${e.line != null ? `:${e.line}` : ''})` : ''}`;

export const test = base.extend({
  // One mobile-app server per SPEC FILE, on a free port and a throwaway
  // TOURNAMENT_DATA_DIR, started and stopped by scripts/screenshots'
  // server.mjs (SIGTERM then SIGKILL by PID). Playwright hands a worker one
  // file after another, so a worker-scoped server would greet the second file
  // with the first file's tournament; this fixture keeps at most one server,
  // and `serverForFile(file)` stops it and starts a fresh one when the file
  // changes. Tests in a file share theirs, so a spec that seeds once and walks
  // several tests declares test.describe.configure({ mode: 'serial' }). A
  // beforeAll hook (worker scope) asks for it with its own testInfo.file.
  serverForFile: [async ({}, use) => {
    let current = null; // { file, server }
    const forFile = async (file) => {
      if (current && current.file === file) return current.server;
      if (current) {
        const old = current;
        current = null;
        await old.server.stop();
      }
      let server;
      try {
        server = await start('mobile');
      } catch (err) {
        throw new Error(`${err.message}\n(make e2e builds the binary before it runs the suite)`);
      }
      current = { file, server };
      return server;
    };
    try {
      await use(forFile);
    } finally {
      if (current) await current.server.stop();
    }
  }, { scope: 'worker' }],

  // The console policy's collector (above). Whatever is still pending when the
  // worker ends came from an afterAll hook, after the last test's check: it is
  // reported there, and an error fails the run.
  consoleCollector: [async ({}, use) => {
    const c = consoleCollector();
    await use(c);
    const late = c.drain();
    for (const e of late) console.log(`CONSOLE ${JSON.stringify({ phase: 'after the last test (an afterAll hook)', ...e })}`);
    const bad = late.filter((e) => e.type === 'error' && !allowed(e));
    if (bad.length) throw new Error(`console policy: ${bad.length} error(s) after the last test:\n${bad.map(describeEntry).join('\n')}`);
  }, { scope: 'worker' }],

  // Every context made through this browser, a spec's own browser.newContext
  // (and browser.newPage, which goes through it) as well as the default
  // `context` fixture's, is attached to the collector.
  browser: [async ({ browser, consoleCollector: c }, use) => {
    const newContext = browser.newContext;
    browser.newContext = async (...args) => {
      const ctx = await newContext.apply(browser, args);
      c.attachContext(ctx);
      return ctx;
    };
    try {
      await use(browser);
    } finally {
      browser.newContext = newContext;
    }
  }, { scope: 'worker' }],

  // The default context goes through browser.newContext today; attached here
  // as well so the policy does not depend on how Playwright builds it.
  context: async ({ context, consoleCollector: c }, use) => {
    c.attachContext(context);
    await use(context);
  },

  // The check, at the end of every test (auto, so no journey has to ask).
  consolePolicy: [async ({ consoleCollector: c }, use, testInfo) => {
    const startedAt = Date.now();
    await use();
    const entries = c.drain().map((e) => (e.at < startedAt ? { ...e, phase: 'before this test (a beforeAll hook)' } : e));
    if (!entries.length) return;
    await testInfo.attach('console.json', { body: JSON.stringify(entries, null, 2), contentType: 'application/json' });
    const audits = auditsFor(testInfo.file);
    for (const e of entries.filter((x) => x.type === 'warning')) {
      const row = { journey: 'console', test: testInfo.title, ...e };
      if (audits.length) for (const a of audits) a.record(row);
      else console.log(`CONSOLE ${JSON.stringify(row)}`);
      testInfo.annotations.push({ type: 'console-warning', description: describeEntry(e) });
    }
    const errors = entries.filter((e) => e.type === 'error');
    for (const e of errors) {
      const rule = allowed(e);
      console.log(`CONSOLE ${JSON.stringify({ test: testInfo.title, allowed: rule ? rule.reason : null, ...e })}`);
    }
    const bad = errors.filter((e) => !allowed(e));
    if (bad.length) {
      throw new Error(`console policy: ${bad.length} console error(s) not on CONSOLE_ALLOWLIST (fixtures/test.mjs):\n${bad.map(describeEntry).join('\n')}`);
    }
  }, { auto: true }],

  // The server of the file this test is in.
  server: async ({ serverForFile }, use, testInfo) => {
    await use(await serverForFile(testInfo.file));
  },

  // Every journey navigates by path; the host is the file's server.
  baseURL: async ({ server }, use) => {
    await use(server.base);
  },

  // The operator project's page must really have a coarse pointer. hasTouch +
  // isMobile (fixtures/devices.mjs) is what provides it today, measured. If
  // that ever stops holding, turn on CDP touch emulation in place
  // (restoreCoarse, which fails loudly if even that does not do it).
  page: async ({ page, consoleCollector: c }, use, testInfo) => {
    c.attachPage(page);
    if (testInfo.project.name === 'operator' && !(await isCoarse(page))) {
      await restoreCoarse(page);
      testInfo.annotations.push({ type: 'coarse-pointer', description: 'CDP Emulation.setTouchEmulationEnabled' });
    }
    await use(page);
  },
});

export { expect };
