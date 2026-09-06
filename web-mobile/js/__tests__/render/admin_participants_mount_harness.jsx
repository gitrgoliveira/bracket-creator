import React from 'react';
import { render, act } from '@testing-library/react';
import { beforeAll } from 'vitest';

// Shared mount harness for the AdminParticipants render tests (PR #416
// finding 14), mirroring settings_mount_harness.jsx's shape: a builder for
// the competition fixture (each test's own differences layered on top via
// overrides) plus a mount helper, so admin_participants_ids.render.test.jsx
// and admin_participants_number_badge.render.test.jsx stop each carrying an
// almost-byte-identical copy of both.

const noop = () => {};

// Module-local, so mountParticipants below keeps the (c) signature the call
// sites already use. vitest isolates the module registry per test file, so
// this is per-file state, not shared across the suite.
let AdminParticipants = null;

// Call at module scope in a render test. Registers the beforeAll that
// imports the component under test (mounted for real, nothing stubbed,
// unlike installSettingsHarness -- AdminParticipants needs no window-global
// swap to render).
export function installParticipantsHarness() {
  beforeAll(async () => {
    await import('../../admin_participants.jsx');
    AdminParticipants = window.AdminParticipants;
  });
}

// The shared competition fixture both files mount against. Each test passes
// only ITS OWN differences (typically `players`) as `overrides`.
export function makeParticipantsCompetition(overrides = {}) {
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
      { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice' },
      { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob' },
    ],
    ...overrides,
  };
}

// Mounts AdminParticipants for `c` and returns whatever
// @testing-library/react's render() returned.
export async function mountParticipants(c) {
  if (!AdminParticipants) {
    throw new Error(
      'mountParticipants: AdminParticipants is not loaded. Call installParticipantsHarness() ' +
      'at module scope in this test file before using mountParticipants.'
    );
  }
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
