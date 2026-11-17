// Two tabs of one browser share one stored outbox (the operator's console and a
// second tab of the same device), and only the tab that holds a base's lease
// sends it (web-mobile/js/base_sender_lease.jsx). The journey counts what the
// browser puts on the wire: a court call is ONE announce POST and, once the
// match starts, its withdrawal ONE DELETE, though a second tab of the device
// holds both writes too.
import { test, expect } from '../fixtures/test.mjs';
import { createTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import { generateDraw, pasteRoster, startCompetition } from '../fixtures/competition.mjs';
import { openShiaijo, startUpNext, upNextCard } from '../fixtures/shiaijo.mjs';
import { settled } from '../fixtures/scoring.mjs';

const ROSTER = [
  ['Akira Tanaka', 'Gyokusen'], ['Ben Carter', 'Thames'], ['Chie Mori', 'Musashi'],
  ['Dan Evans', 'Seishinkan'], ['Eri Kato', 'Gyokusen'], ['Finn Hale', 'Thames'],
  ['Goro Abe', 'Musashi'], ['Hana Ito', 'Seishinkan'],
];

test('a court call and its withdrawal are each sent once with a second tab open', async ({ page, context }) => {
  const posts = [];
  const deletes = [];
  context.on('request', (req) => {
    const { pathname } = new URL(req.url());
    if (req.method() === 'POST' && pathname === '/api/tournament/announce') posts.push(pathname);
    if (req.method() === 'DELETE' && pathname.startsWith('/api/announcements/')) deletes.push(pathname);
  });

  let compId;
  await test.step('seed a started competition through the interface', async () => {
    await createTournament(page, { name: 'Two Tabs Cup', courts: 1 });
    compId = await createCompetition(page, {
      name: 'Men Individual', kind: 'individual', format: 'mixed', courts: ['A'], numberPrefix: 'T',
    });
    await pasteRoster(page, compId, ROSTER);
    await generateDraw(page, compId);
    await startCompetition(page, compId);
  });

  const second = await context.newPage();
  await test.step('open shiaijo A in both tabs', async () => {
    await openShiaijo(page, 'A');
    await openShiaijo(second, 'A');
    await expect(upNextCard(second)).toBeVisible();
  });

  await test.step('call the match to court from the first tab', async () => {
    await upNextCard(page).getByRole('button', { name: 'Call to court' }).click();
    await settled(page);
    await settled(second);
    expect(posts, 'the court call is sent once').toHaveLength(1);
  });

  await test.step('start the match: the call is withdrawn once', async () => {
    await startUpNext(page);
    await settled(page);
    await settled(second);
    await expect.poll(() => deletes.length, { message: 'the withdrawal reached the server' }).toBe(1);
    expect(posts, 'and no call was sent again').toHaveLength(1);
  });
});
