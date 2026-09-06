import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// bc-pnum ruling 1e: "When the participants list is applied, then the ids
// are created and displayed." The roster PUT response re-serialises the
// saved roster (server-minted UUIDs included), so once a roster has been
// applied at least once, every row's server-assigned id shows in a compact,
// muted slot beside the dojo line: the first 8 characters, with the full id
// available on hover via a title attribute. A row with no id (never applied,
// or a load failure -- 1b's data-issues banner names that case separately)
// shows nothing in that slot.
//
// Mounted for REAL, same setup as
// admin_participants_provisional_numbers.render.test.jsx: the render setup
// preloads everything admin_participants.jsx needs at module-eval time.

let AdminParticipants;

beforeAll(async () => {
  await import('../../admin_participants.jsx');
  AdminParticipants = window.AdminParticipants;
});

const noop = () => {};

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Autumn Cup',
    status: 'setup',
    format: 'mixed',
    kind: 'individual',
    poolSize: 4,
    poolWinners: 2,
    checkInEnabled: false,
    withZekkenName: false,
    numberPrefix: 'K',
    players: [
      { id: '11111111-2222-4333-8444-555555555555', name: 'Alice', dojo: 'Dojo Alice' },
      { id: '', name: 'Bob', dojo: 'Dojo Bob' },
    ],
    ...overrides,
  };
}

async function mount(c) {
  let result;
  await act(async () => {
    result = render(
      <AdminParticipants
        c={c}
        tournament={{ name: 'Spring Taikai', courts: ['A'] }}
        onUpdate={noop}
        password=""
        showToast={noop}
        onSection={noop}
        onBack={noop}
      />
    );
  });
  return result;
}

describe('AdminParticipants id display (bc-pnum ruling 1e)', () => {
  it('shows the short id, with the full id on hover, for a stamped row', async () => {
    const { container } = await mount(makeCompetition());

    const idSpans = container.querySelectorAll('.seed-row__id');
    // Only Alice (the stamped row) gets a slot; Bob's id-less row gets none.
    expect(idSpans.length).toBe(1);
    const span = idSpans[0];
    expect(span.textContent).toContain('11111111');
    expect(span.textContent).not.toContain('555555555555', 'only the first 8 characters show inline');
    expect(span.getAttribute('title')).toBe('11111111-2222-4333-8444-555555555555');
  });

  it('shows no id slot at all for a roster with no ids yet', async () => {
    const { container } = await mount(makeCompetition({
      players: [
        { id: '', name: 'Alice', dojo: 'Dojo Alice' },
        { id: '', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    }));

    expect(container.querySelectorAll('.seed-row__id').length).toBe(0);
  });
});
