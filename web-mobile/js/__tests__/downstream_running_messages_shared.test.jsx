// JS half of the shared Go/JS table for the downstream_knockout_running
// refusal sentence. Go half: TestDownstreamKnockoutRunningError_SharedMessages
// (internal/engine/downstream_running_refusal_test.go). Schema and rationale:
// internal/engine/testdata/downstream_running_messages.json's own "_comment".
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
    downstreamKnockoutRunningMessage,
    downstreamKnockoutRunningReopenMessage,
} from '../write_result.jsx';

describe('downstream_knockout_running: the SPA says what the server says (shared table)', () => {
    const table = JSON.parse(
        readFileSync(
            resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'downstream_running_messages.json'),
            'utf8'
        )
    );

    // Load-bearing: it.each over an empty array produces zero tests, no red.
    it('the shared table is present and covers both doors', () => {
        expect(table.cases?.length).toBeGreaterThan(0);
        expect(table.cases.some((c) => c.reopening)).toBe(true);
        expect(table.cases.some((c) => !c.reopening)).toBe(true);
    });

    it.each(table.cases)('$name', ({ reopening, running, message }) => {
        // The wire shape: the server sends each running match's id, number,
        // operator label and court (displayRound is folded into the label).
        const wire = running.map(({ id, number, label, court }) => ({ id, number, label, court }));
        const build = reopening ? downstreamKnockoutRunningReopenMessage : downstreamKnockoutRunningMessage;
        expect(build(wire)).toBe(message);
    });
});
