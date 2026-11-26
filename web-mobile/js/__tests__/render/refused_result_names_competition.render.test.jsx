// The walk's finding S7: phone A's refused result was told as "A result in
// Match 1 was refused by the server ...". Every competition has a Match 1,
// while the held-writes list names the same write "Refused Final · Match 1".
// An outbox alert about a match now names the match's competition too, in the
// list's own form ("<competition> · <match>"), whenever this page knows the
// competition; otherwise it is worded as before.
import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let alertNamedInCompetition;
let queueAlertMessage;
let restoreGlobals;

// app.jsx mounts the App at load; these tests read two of its exports only.
beforeAll(async () => {
  restoreGlobals = installWindowStubs({ ReactDOM: { createRoot: () => ({ render() {} }) } });
  ({ alertNamedInCompetition, queueAlertMessage } = await import('../../app.jsx'));
});

afterAll(() => restoreGlobals());

const SENTENCE = 'This match result has already been reported. Contact the tournament organizer to correct it.';
const competitions = [{ id: 'c1', name: 'Refused Final' }, { id: 'c2', name: 'Other Cup' }];
const refused = (over = {}) => ({
  kind: 'rejected', count: 1, terminalCount: 1, compID: 'c1', matchID: 'Pool A-0',
  where: 'Match 1', sentence: true, detail: SENTENCE, ...over,
});

describe('S7: an outbox alert about a match names its competition', () => {
  it('the refused result names the competition and the match, then the server sentence', () => {
    const text = queueAlertMessage(alertNamedInCompetition(refused(), competitions));
    expect(text).toBe(`A result in Refused Final · Match 1 was refused by the server and cannot be saved. ${SENTENCE}`);
  });

  it('a competition this page does not hold leaves the alert as it was (never an id)', () => {
    const alert = refused({ compID: 'gone' });
    expect(alertNamedInCompetition(alert, competitions)).toBe(alert);
    expect(alertNamedInCompetition(refused(), undefined).where).toBe('Match 1');
  });

  it('an alert that names no match is left as it is', () => {
    const alert = { kind: 'rejected', count: 1, terminalCount: 0, compID: 'c1', where: 'Lineup of Kodokan' };
    expect(alertNamedInCompetition(alert, competitions)).toBe(alert);
  });

  it('a tap notice names the competition the same way', () => {
    const alert = alertNamedInCompetition({ kind: 'tap_applied', count: 1, compID: 'c2', matchID: 'm-1', where: 'Match 3 (Final)' }, competitions);
    expect(queueAlertMessage(alert)).toBe('A tap shown as not applied was applied after all (Other Cup · Match 3 (Final)). Check the score.');
  });
});
