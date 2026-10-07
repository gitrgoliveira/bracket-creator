// bc-dhas: on a self-run tournament the public score sheet puts a note under a
// representative bout the judges decided, and the server refuses a
// participant's change to that bout (409 hantei_organiser_only). They are one
// sentence, so the participant reads the same words whichever side says them.
// Both halves read internal/mobileapp/testdata/rep_bout_hantei_messages.json;
// the Go half is in internal/mobileapp.

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { REP_BOUT_DECIDED_NOTE } from '../admin_scoring_team.jsx';

const messages = JSON.parse(readFileSync(
  resolve(__dirname, '..', '..', '..', 'internal', 'mobileapp', 'testdata', 'rep_bout_hantei_messages.json'),
  'utf8',
));

describe('the note under a representative bout the judges decided', () => {
  it('is the sentence the server refuses a participant\'s change with', () => {
    expect(messages.recorded, 'the shared fixture carries the sentence').toBeTruthy();
    expect(REP_BOUT_DECIDED_NOTE).toBe(messages.recorded);
  });
});
