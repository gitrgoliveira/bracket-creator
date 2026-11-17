// familyFiles (helpers/source.js) reads a split entry together with the
// `<stem>_*.jsx` modules split out of it. Matching by stem alone would also
// read unrelated modules whose names merely begin with the stem
// (admin_schedule.jsx and admin_schedule_page.jsx), so only the entries in
// SPLIT_ENTRIES have a family, and each family is pinned to its exact modules:
// a module added to one is a decision made here, never a side effect of its name.
import { describe, it, expect } from 'vitest';
import { familyFiles, SPLIT_ENTRIES, modules } from './helpers/source.js';

const FAMILIES = {
  'api_client.jsx': [
    'api_client.jsx', 'api_client_clock.jsx', 'api_client_enqueue.jsx', 'api_client_events.jsx',
    'api_client_flush.jsx', 'api_client_ids.jsx', 'api_client_members.jsx', 'api_client_overlays.jsx',
    'api_client_queue.jsx', 'api_client_requests.jsx', 'api_client_status.jsx', 'api_client_taps.jsx',
  ],
  'admin_scoring_team.jsx': ['admin_scoring_team.jsx', 'admin_scoring_team_bouts.jsx', 'admin_scoring_team_kachinuki.jsx'],
  'admin_shiaijo.jsx': [
    'admin_shiaijo.jsx', 'admin_shiaijo_calls.jsx', 'admin_shiaijo_context.jsx', 'admin_shiaijo_feed.jsx',
    'admin_shiaijo_moves.jsx', 'admin_shiaijo_queue.jsx', 'admin_shiaijo_start.jsx',
  ],
};

describe('familyFiles', () => {
  it('names the split entries and no others', () => {
    expect([...SPLIT_ENTRIES].sort()).toEqual(Object.keys(FAMILIES).sort());
  });

  it.each(Object.entries(FAMILIES))('reads %s with exactly its split modules', (entry, files) => {
    expect(familyFiles(entry)).toEqual(files);
  });

  it('reads an entry that was never split as itself alone, though other modules begin with its stem', () => {
    expect(modules()).toContain('admin_schedule_page.jsx');
    expect(familyFiles('admin_schedule.jsx')).toEqual(['admin_schedule.jsx']);
  });
});
