// bc-sync: on pagehide the autosave writes a pending edit marked `durable`,
// and recordScore puts it into the persisted outbox. That only lands if the
// write reaches API.recordScore before the page goes. The score editor hosts
// call editMatchScore (admin.jsx), which calls attemptScoreWrite first thing;
// this pins that attemptScoreWrite hands the write to recordScore in the same
// tick. An `await` on I/O added in front of that call would let the page go
// first and silently lose the edit again.
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import { attemptScoreWrite } from '../write_result.jsx';

describe('attemptScoreWrite reaches recordScore in the same tick', () => {
  it('calls recordScore before returning control to its caller', () => {
    const recordScore = vi.fn(() => new Promise(() => {}));
    attemptScoreWrite({
      recordScore,
      confirmDialog: vi.fn(),
      compId: 'c1', matchId: 'm1', result: { status: 'running' }, password: 'pw', match: null,
    });
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledWith('c1', 'm1', { status: 'running' }, 'pw', null);
  });
});

// The rest of the chain, read from source because editMatchScore lives inside
// AdminApp and each host's handler inside its component: the editor calls the
// host's onSubmit, which must reach onEditScore (editMatchScore) or
// API.recordScore before its first await, and editMatchScore must reach
// attemptScoreWrite before its own. Any other await in front of those lets
// the page go before the durable write is queued.
const here = dirname(fileURLToPath(import.meta.url));
const source = (file) => readFileSync(resolve(here, '..', file), 'utf8');

// The function the first `await` at or after `from` calls.
function firstAwaited(text, from) {
  const at = text.indexOf('await ', from);
  const m = at < 0 ? null : /^await\s+([\w.]+)\(/.exec(text.slice(at));
  return m ? m[1] : null;
}

describe('every score editor host reaches the write before its first await', () => {
  const HOSTS = [
    ['admin_shiaijo.jsx', 'onEditScore'],
    ['admin_pools.jsx', 'onEditScore'],
    ['admin_schedule_score_editor.jsx', 'onEditScore'],
    ['admin_competition_bracket.jsx', 'onEditScore'],
    ['viewer_match.jsx', 'window.API.recordScore'],
  ];

  it.each(HOSTS)('%s', (file, write) => {
    const text = source(file);
    const handlers = [...text.matchAll(/onSubmit(?:AndNext)?(?:=\{|:)[^{]*?async\s*\(patch\)\s*=>\s*\{/g)];
    expect(handlers.length, `${file}: no async (patch) => onSubmit handler found`).toBeGreaterThan(0);
    for (const h of handlers) {
      expect(firstAwaited(text, h.index + h[0].length),
        `${file}: the score editor's ${h[0].split(/[=:]/)[0]} must reach ${write} before any other await`).toBe(write);
    }
  });

  it('admin.jsx editMatchScore', () => {
    const text = source('admin.jsx');
    const at = text.indexOf('const editMatchScore = async (');
    expect(at, 'editMatchScore not found in admin.jsx').toBeGreaterThan(0);
    expect(firstAwaited(text, at), 'editMatchScore must reach attemptScoreWrite before any other await').toBe('attemptScoreWrite');
  });
});
