// vitest.config.js carries a ?tab= query from api_client.jsx onto its api_client_*
// pieces, so each simulated tab holds its own outbox. The tab name must be read
// whole: two tabs whose names differ only after a hyphen ("t-1", "t-2") must not
// share one set of pieces, or a test holding both would prove nothing.
import { describe, it, expect, afterEach, vi } from 'vitest';

const CLIENT = new URL('../api_client.jsx', import.meta.url).pathname;

afterEach(() => { vi.resetModules(); });

describe('simulated tabs', () => {
  it('whose names differ after a hyphen get separate api_client pieces', async () => {
    const one = await import(/* @vite-ignore */ `${CLIENT}?tab=t-1`);
    const two = await import(/* @vite-ignore */ `${CLIENT}?tab=t-2`);
    // subscribeQueueAlert lives in api_client_queue.jsx and is re-exported.
    expect(one.subscribeQueueAlert).not.toBe(two.subscribeQueueAlert);
  });

  it('with the same name share them', async () => {
    const one = await import(/* @vite-ignore */ `${CLIENT}?tab=t-1`);
    const again = await import(/* @vite-ignore */ `${CLIENT}?tab=t-1`);
    expect(one.subscribeQueueAlert).toBe(again.subscribeQueueAlert);
  });
});
