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
// The "Knockout qualifiers" pills (competition_shape.jsx extraQualifiersLabel),
// which only a minimum pool size offers.
const EXTRA_QUALIFIERS = { standard: 'Standard', 'larger-pools': 'Oversized send +1', 'fill-bracket': 'Fit the knockout' };

// The wizard's controls by their accessible names (competition_fields.jsx): a
// NumberField or TextField input is named by its label through htmlFor, and a
// PillGroup's row is a role="group" named by its label (aria-labelledby), each
// pill a button whose aria-pressed says whether it is chosen. Labels are
// matched from the start, because an optional field's label carries an
// "(optional)" suffix and "Format" is also the end of "Team match format".
const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const input = (page, label) => page.getByLabel(new RegExp(`^${escape(label)}`));
const group = (page, label) => page.getByRole('group', { name: new RegExp(`^${escape(label)}`) });
const pill = (grp, label) => grp.getByRole('button', { name: label, exact: true });

async function choose(page, groupLabel, optionLabel) {
  const btn = pill(group(page, groupLabel), optionLabel);
  await btn.tap();
  await expect(btn).toHaveAttribute('aria-pressed', 'true');
}

async function setNumber(page, label, value) {
  const box = input(page, label);
  await box.fill(String(value));
  await expect(box).toHaveValue(String(value));
}

// Create one competition and return its id (read from the URL the wizard
// lands on, /admin/competition/<id>/participants). Only the options given are
// touched; everything else keeps the wizard's default, which is itself part of
// what a journey walks.
//
//   kind           'individual' | 'team'
//   format         'knockout' | 'mixed' | 'league' | 'swiss'
//   engi           boolean: "Engi (kata competition)" (individual only): the
//                  roster is pairs and a match is scored in flags
//   teamSize       number (team only)
//   teamMatchType  'fixed' | 'kachinuki' (team only)
//   poolSizeMode   'maximum' | 'minimum' (mixed only)
//   poolSize, poolWinners  numbers (mixed only)
//   extraQualifiers 'standard' | 'larger-pools' | 'fill-bracket': the "Knockout
//                  qualifiers" pills (mixed with a minimum pool size only).
//                  Either non-standard choice sets Winners per pool to 1 and
//                  locks it, so give poolWinners with 'standard' only
//   courts         e.g. ['A', 'B']: exactly these shiaijo end up selected
//   numberPrefix   up to 3 characters
//   twoThirdPlaces boolean: "Award two joint 3rd places" (on by default;
//                  untick it for a bronze match)
//   checkIn        boolean: "Check-in tracking" (officiated tournaments only)
export async function createCompetition(page, opts = {}) {
  const {
    name, kind, format, teamSize, teamMatchType, poolSizeMode, poolSize, poolWinners, extraQualifiers,
    courts, numberPrefix, twoThirdPlaces, engi, checkIn,
  } = opts;
  await page.goto('/admin/create-competition');
  await expect(page.getByRole('heading', { level: 1, name: 'Add competition' })).toBeVisible();

  if (name !== undefined) await input(page, 'Display name').fill(name);
  if (kind) await choose(page, 'Competition type', KIND[kind]);
  if (format) await choose(page, 'Format', FORMAT[format]);
  if (engi !== undefined) await page.getByRole('checkbox', { name: /^Engi \(kata competition\)/ }).setChecked(engi);
  if (teamSize !== undefined) await setNumber(page, 'Team size', teamSize);
  if (teamMatchType) await choose(page, 'Team match format', TEAM_MATCH_TYPE[teamMatchType]);
  if (poolSizeMode) await choose(page, 'Pool size is a', POOL_SIZE_MODE[poolSizeMode]);
  if (poolSize !== undefined) await setNumber(page, 'Players per pool', poolSize);
  if (poolWinners !== undefined) await setNumber(page, 'Winners per pool', poolWinners);
  if (extraQualifiers) {
    await choose(page, 'Knockout qualifiers', EXTRA_QUALIFIERS[extraQualifiers]);
    if (extraQualifiers !== 'standard') {
      // The wizard locks the field at 1: oversized pools send one more than that.
      await expect(input(page, 'Winners per pool')).toHaveValue('1');
      await expect(input(page, 'Winners per pool')).toBeDisabled();
    }
  }
  if (twoThirdPlaces !== undefined) {
    await page.getByRole('checkbox', { name: 'Award two joint 3rd places' }).setChecked(twoThirdPlaces);
  }
  if (checkIn !== undefined) await page.getByRole('checkbox', { name: /^Check-in tracking/ }).setChecked(checkIn);
  if (courts) {
    const grp = group(page, 'Assigned shiaijo');
    const pills = grp.getByRole('button');
    // Select the wanted ones first, then drop the rest, so the selection is
    // never empty on the way (the wizard refuses an empty or uneven count).
    for (const want of [true, false]) {
      for (let i = 0; i < await pills.count(); i += 1) {
        const p = pills.nth(i);
        const court = (await p.textContent()).trim().split(' ').pop();
        const active = (await p.getAttribute('aria-pressed')) === 'true';
        if (courts.includes(court) === want && active !== want) await p.tap();
      }
    }
    for (const court of courts) {
      await expect(pill(grp, `Shiaijo (court) ${court}`)).toHaveAttribute('aria-pressed', 'true');
    }
  }
  if (numberPrefix !== undefined) {
    await input(page, 'Player number prefix').fill(numberPrefix);
  }

  await page.getByRole('button', { name: /^Create & continue/ }).tap();
  await expect(page).toHaveURL(/\/admin\/competition\/[^/]+\/participants$/);
  return new URL(page.url()).pathname.split('/')[3];
}
