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
import { freePort } from './net.mjs';

const REPO = path.resolve(new URL('../../..', import.meta.url).pathname);
const BIN = path.join(REPO, 'bin', 'bracket-creator');

// The two commands expose different readiness probes: /health is registered by
// the tournament app only (internal/mobileapp/server.go:96), while `serve` has
// /api/status (cmd/serve.go:112). Polling the wrong one never succeeds.
const READY_PATH = { mobile: '/health', web: '/api/status' };

async function waitForHealth(base, child, readyPath, timeoutMs = 30000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`server exited early with code ${child.exitCode}`);
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

  const base = `http://localhost:${port}`;
  try {
    await waitForHealth(base, child, readyPath);
  } catch (err) {
    child.kill('SIGKILL');
    throw new Error(`${err.message}\n--- server output ---\n${log.join('')}`);
  }

  let stopped = false;
  const stop = async () => {
    if (stopped) return;
    stopped = true;
    child.kill('SIGTERM');
    // The binary shuts SSE down gracefully; give it a moment before insisting.
    await new Promise((r) => setTimeout(r, 400));
    if (child.exitCode === null) child.kill('SIGKILL');
    fs.rmSync(dataDir, { recursive: true, force: true });
  };

  return { base, dataDir, stop };
}

export { REPO, BIN };
