// The create-competition wizard (admin_setup.jsx, /admin/create-competition).
//
// This is the ONE file that knows the wizard's copy. Every journey seeds its
// competitions through here (operator ruling: UI-only seeding), so a change to
// the wizard breaks this file and nothing else. The labels mirror
// competition_shape.jsx's LABEL_* constants and option lists.
import { expect } from '@playwright/test';

const KIND = { individual: 'Individual', team: 'Team' };
const FORMAT = {
  knockout: 'Knockout only',
  mixed: 'Pools + Knockout',
  league: 'League',
  swiss: 'Swiss',
};
const TEAM_MATCH_TYPE = { fixed: 'Regular', kachinuki: 'Kachinuki (winner stays on)' };
const POOL_SIZE_MODE = { maximum: 'maximum', minimum: 'minimum' };

// A form row by its visible label. PillGroup, NumberField and TextField all
// render `.field > label.field__label` with no htmlFor (competition_fields.jsx),
// so getByLabel cannot reach them. Anchored, because "Pool match duration"
// and "Knockout match duration" share words with other labels.
const field = (page, label) => page.locator('.field')
  .filter({ has: page.locator('.field__label', { hasText: new RegExp(`^${label}`) }) })
  .first();

// A pill in a group. The pills carry their state as the `is-active` class
// only (no aria-pressed), so that is what is read back.
const pill = (group, label) => group.locator('.radio-pill', { hasText: new RegExp(`^${escape(label)}$`) });
const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

async function choose(page, groupLabel, optionLabel) {
  const btn = pill(field(page, groupLabel), optionLabel);
  await btn.tap();
  await expect(btn).toHaveClass(/\bis-active\b/);
}

async function setNumber(page, label, value) {
  const input = field(page, label).locator('input');
  await input.fill(String(value));
  await expect(input).toHaveValue(String(value));
}

// Create one competition and return its id (read from the URL the wizard
// lands on, /admin/competition/<id>/participants). Only the options given are
// touched; everything else keeps the wizard's default, which is itself part of
// what a journey walks.
//
//   kind           'individual' | 'team'
//   format         'knockout' | 'mixed' | 'league' | 'swiss'
//   teamSize       number (team only)
//   teamMatchType  'fixed' | 'kachinuki' (team only)
//   poolSizeMode   'maximum' | 'minimum' (mixed only)
//   poolSize, poolWinners  numbers (mixed only)
//   courts         e.g. ['A', 'B']: exactly these shiaijo end up selected
//   numberPrefix   up to 3 characters
//   twoThirdPlaces boolean: "Award two joint 3rd places" (on by default;
//                  untick it for a bronze match)
export async function createCompetition(page, opts = {}) {
  const {
    name, kind, format, teamSize, teamMatchType, poolSizeMode, poolSize, poolWinners,
    courts, numberPrefix, twoThirdPlaces,
  } = opts;
  await page.goto('/admin/create-competition');
  await expect(page.getByRole('heading', { level: 1, name: 'Add competition' })).toBeVisible();

  if (name !== undefined) await field(page, 'Display name').locator('input').fill(name);
  if (kind) await choose(page, 'Competition type', KIND[kind]);
  if (format) await choose(page, 'Format', FORMAT[format]);
  if (teamSize !== undefined) await setNumber(page, 'Team size', teamSize);
  if (teamMatchType) await choose(page, 'Team match format', TEAM_MATCH_TYPE[teamMatchType]);
  if (poolSizeMode) await choose(page, 'Pool size is a', POOL_SIZE_MODE[poolSizeMode]);
  if (poolSize !== undefined) await setNumber(page, 'Players per pool', poolSize);
  if (poolWinners !== undefined) await setNumber(page, 'Winners per pool', poolWinners);
  if (twoThirdPlaces !== undefined) {
    await page.getByRole('checkbox', { name: 'Award two joint 3rd places' }).setChecked(twoThirdPlaces);
  }
  if (courts) {
    const group = field(page, 'Assigned shiaijo');
    const pills = group.locator('.radio-pill');
    // Select the wanted ones first, then drop the rest, so the selection is
    // never empty on the way (the wizard refuses an empty or uneven count).
    for (const want of [true, false]) {
      for (let i = 0; i < await pills.count(); i += 1) {
        const p = pills.nth(i);
        const court = (await p.textContent()).trim().split(' ').pop();
        const active = /\bis-active\b/.test(await p.getAttribute('class'));
        if (courts.includes(court) === want && active !== want) await p.tap();
      }
    }
    for (const court of courts) {
      await expect(pill(group, `Shiaijo (court) ${court}`)).toHaveClass(/\bis-active\b/);
    }
  }
  if (numberPrefix !== undefined) {
    await field(page, 'Player number prefix').locator('input').fill(numberPrefix);
  }

  await page.getByRole('button', { name: /^Create & continue/ }).tap();
  await expect(page).toHaveURL(/\/admin\/competition\/[^/]+\/participants$/);
  return new URL(page.url()).pathname.split('/')[3];
}
