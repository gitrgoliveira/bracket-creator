// The demo tournament behind most of the committed captures.
//
// scripts/setup_tournament.py is what `make mobile-app-example` runs, and the
// committed shots show its output ("London Cup Demo", 6 competitions, 179
// participants). Re-implementing it in Node would fork the fixture and the
// captures would drift from the demo the docs describe, so the harness drives
// the existing script. It reads BASE_URL and TOURNAMENT_PASSWORD from the
// environment and its CSVs from test-data/, relative to the repo root.
import { spawn } from 'node:child_process';
import { REPO } from './server.mjs';
import { PASSWORD } from './api.mjs';

// The captures need a tournament that is mid-run, not finished: a fully scored
// one shows every competition as Completed, no "currently running" card on the
// dashboard and no match in progress in the viewer, which is not what the
// committed images show. The seeder takes an opt-in knob for this; `make
// mobile-app-example` leaves it unset and still completes everything.
//
// TWO categories, not one. "Teams" carries the dashboard and the home page.
// "Men up to 2D" is the competition viewer-competition opens, and its docs
// caption promises "upcoming matches" - with that category scored to the end
// the page renders "Nothing scheduled" and contradicts its own caption, which
// a capture of the right route at the right size hides perfectly.
const LEAVE_RUNNING = ['Teams', 'Men up to 2D'].join(',');

export function demoTournament(base) {
  return new Promise((resolve, reject) => {
    const child = spawn('python3', ['scripts/setup_tournament.py'], {
      cwd: REPO,
      env: {
        ...process.env,
        BASE_URL: base,
        TOURNAMENT_PASSWORD: PASSWORD,
        SEED_LEAVE_RUNNING: LEAVE_RUNNING,
        // Nobody watches the harness seed, so drop the demo's per-match pacing.
        SEED_SCORE_DELAY: '0',
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const out = [];
    child.stdout.on('data', (b) => out.push(b.toString()));
    child.stderr.on('data', (b) => out.push(b.toString()));
    child.on('close', (code) => {
      if (code === 0) return resolve({ log: out.join('') });
      reject(new Error(`setup_tournament.py exited ${code}\n${out.join('').slice(-1500)}`));
    });
  });
}
