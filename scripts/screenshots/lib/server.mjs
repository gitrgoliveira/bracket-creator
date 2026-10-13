// Boot one bracket-creator server per run, on a throwaway data dir.
//
// Two commands are captured from: `mobile-app` (the tournament app, most of the
// shots) and `serve` (the CLI web UI behind the webui-* shots). They differ in
// how state arrives - the first over its API, the second by uploading a CSV
// through the page - so the recipe says which one it needs.
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { freePort } from './net.mjs';

// fileURLToPath, not URL.pathname: the latter leaves a space in the checkout
// path percent-encoded, and the binary is then reported missing.
const REPO = path.resolve(fileURLToPath(new URL('../../..', import.meta.url)));
const BIN = path.join(REPO, 'bin', 'bracket-creator');

// The two commands expose different readiness probes: /health is registered by
// the tournament app only (internal/mobileapp/server.go:96), while `serve` has
// /api/status (cmd/serve.go:112). Polling the wrong one never succeeds.
const READY_PATH = { mobile: '/health', web: '/api/status' };

// Has the child gone? Both halves matter: a process killed by a SIGNAL reports
// a null exitCode, so testing exitCode alone reads a dead server as still
// starting and polls the full 30 seconds before blaming the readiness probe.
const exited = (child) => child.exitCode !== null || child.signalCode !== null;

async function waitForHealth(base, child, readyPath, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (exited(child)) {
      throw new Error('server exited early '
        + `(${child.signalCode ? `signal ${child.signalCode}` : `code ${child.exitCode}`})`);
    }
    try {
      const res = await fetch(base + readyPath);
      if (res.ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error(`server did not answer ${readyPath} within ${timeoutMs}ms at ${base}`);
}

// start("mobile" | "web") -> { base, dataDir, stop() }
export async function start(kind) {
  if (!fs.existsSync(BIN)) {
    throw new Error(`missing ${BIN} - run "make go/build" first`);
  }
  const readyPath = READY_PATH[kind];
  if (!readyPath) {
    throw new Error(`unknown server kind "${kind}" - a recipe's server must be 'mobile' or 'web'`);
  }
  const port = await freePort();
  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bc-shots-'));
  // --folder defaults from TOURNAMENT_DATA_DIR (cmd/mobile_app.go), so the env
  // var alone is enough and works for both commands. `serve` keeps no state.
  const command = kind === 'web' ? 'serve' : 'mobile-app';

  const child = spawn(BIN, [command], {
    env: { ...process.env, PORT: String(port), TOURNAMENT_DATA_DIR: dataDir },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  // Only the startup failure path reads this, so keep a bounded tail rather
  // than every byte the server writes for the life of the family (measured at
  // 124 KB for one seed).
  const log = [];
  const keep = (b) => {
    log.push(b.toString());
    if (log.length > 50) log.shift();
  };
  child.stdout.on('data', keep);
  child.stderr.on('data', keep);

  // A worker or runner that exits without reaching stop() (an uncaught error,
  // process.exit) must not leave the server running or its data folder behind.
  // A synchronous 'exit' listener is the only thing that still runs then. A
  // SIGKILL runs nothing, and no hook can cover it.
  const removeData = () => fs.rmSync(dataDir, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  const onExit = () => {
    if (!exited(child)) child.kill('SIGKILL');
    try {
      removeData();
    } catch {
      // the process is going away; nothing left to report to
    }
  };
  process.on('exit', onExit);

  const base = `http://localhost:${port}`;
  try {
    await waitForHealth(base, child, readyPath);
    // A health answer is only ours if our child is still alive: when another
    // process took the port between freePort() and the bind, this child exits
    // with a bind error and the answer came from the other server. A short
    // settle, then the check.
    await new Promise((r) => setTimeout(r, 250));
    if (exited(child)) {
      throw new Error(`server exited right after answering ${readyPath} `
        + `(${child.signalCode ? `signal ${child.signalCode}` : `code ${child.exitCode}`}); `
        + `the answer may have come from another process on port ${port}`);
    }
  } catch (err) {
    process.off('exit', onExit);
    child.kill('SIGKILL');
    // stop() is never handed out on this path, so the data dir it would have
    // removed is removed here.
    removeData();
    throw new Error(`${err.message}\n--- server output ---\n${log.join('')}`);
  }

  let stopped = false;
  const stop = async () => {
    if (stopped) return;
    stopped = true;
    process.off('exit', onExit);
    if (!exited(child)) {
      const gone = new Promise((resolve) => child.once('exit', resolve));
      child.kill('SIGTERM');
      // The binary shuts SSE down gracefully; give it a moment before insisting.
      const timer = { id: null };
      const graceful = await Promise.race([
        gone.then(() => true),
        new Promise((r) => { timer.id = setTimeout(() => r(false), 3000); }),
      ]);
      clearTimeout(timer.id);
      if (!graceful) {
        child.kill('SIGKILL');
        await gone;
      }
    }
    removeData();
  };

  return { base, dataDir, stop, logTail: () => log.join('') };
}

export { REPO, BIN };
