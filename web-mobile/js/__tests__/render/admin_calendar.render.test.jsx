import React from 'react';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AdminCalendarPage } from '../../admin_calendar.jsx';

const Stub = (id) => function StubComp({ children }) {
  return <div data-testid={id}>{children}</div>;
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('admin-topbar'),
  Breadcrumbs: ({ items }) => (
    <nav data-testid="breadcrumbs">
      {(items || []).map((it) => (
        <span key={it.label} onClick={it.onClick}>{it.label}</span>
      ))}
    </nav>
  ),
};

describe('AdminCalendarPage render tests', () => {
  let restoreGlobals;
  let sseHandler = null;

  beforeAll(() => {
    restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  });

  afterAll(() => {
    restoreGlobals();
  });

  beforeEach(() => {
    sseHandler = null;
    window.API = {
      fetchCalendar: vi.fn(),
      subscribeToEvents: vi.fn((cb) => {
        sseHandler = cb;
        return () => { sseHandler = null; };
      }),
    };
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  const TOURNAMENT = {
    id: 't1',
    name: 'National Championship',
    courts: ['A', 'B'],
  };

  it('renders loading state verbatim: "Loading the calendar…"', async () => {
    window.API.fetchCalendar.mockReturnValue(new Promise(() => {})); // Hangs
    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
      />
    );
    expect(screen.getByText('Loading the calendar…')).toBeInTheDocument();
  });

  it('renders failed state verbatim: "Could not load the calendar." / "Check the connection to the tournament server." and [Try again] re-fetches', async () => {
    let callCount = 0;
    window.API.fetchCalendar = vi.fn(async () => {
      callCount++;
      if (callCount === 1) throw new Error('Server unreachable');
      return {
        days: [{ day: 1, date: '16-05-2026', breaks: [] }],
        courts: ['A'],
        competitions: [{ id: 'c1', name: 'Open Individual', placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 600 } } } }],
      };
    });

    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
      />
    );

    await waitFor(() => {
      expect(screen.getByText('Could not load the calendar.')).toBeInTheDocument();
    });
    expect(screen.getByText('Check the connection to the tournament server.')).toBeInTheDocument();

    const tryAgainBtn = screen.getByRole('button', { name: 'Try again' });
    fireEvent.click(tryAgainBtn);

    await waitFor(() => {
      expect(screen.getByText('Open Individual')).toBeInTheDocument();
    });
    expect(callCount).toBe(2);
  });

  it('renders no competitions state verbatim: "No competitions yet." / "Add one from the dashboard and it appears here on its day." and [Back to the dashboard]', async () => {
    window.API.fetchCalendar.mockResolvedValue({
      days: [{ day: 1, date: '16-05-2026', breaks: [] }],
      courts: ['A'],
      competitions: [],
    });

    const onBack = vi.fn();
    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={onBack}
      />
    );

    await waitFor(() => {
      expect(screen.getByText('No competitions yet.')).toBeInTheDocument();
    });
    expect(screen.getByText('Add one from the dashboard and it appears here on its day.')).toBeInTheDocument();

    const backBtn = screen.getByRole('button', { name: 'Back to the dashboard' });
    fireEvent.click(backBtn);
    expect(onBack).toHaveBeenCalled();
  });

  it('renders a normal two-day calendar with blocks, pause bands, key line, tray, and verbatim copy', async () => {
    const onEditTournament = vi.fn();
    const onOpenCompetition = vi.fn();

    const CALENDAR_DATA = {
      days: [
        {
          day: 1,
          date: '16-05-2026',
          breaks: [
            { name: 'Lunch', start: '12:30', end: '13:30', startMins: 750, endMins: 810 },
            { name: 'Opening ceremony', start: '09:00', end: '09:45', startMins: 540, endMins: 585, matchesGoOn: true },
          ],
        },
        {
          day: 2,
          date: '17-05-2026',
          breaks: [],
        },
      ],
      courts: ['A', 'B'],
      unreadableCompetitions: ['kids'],
      unusedPauses: [
        { name: 'Closing ceremony', reason: 'no_start_time' },
      ],
      competitions: [
        {
          id: 'c1',
          name: "Men's Individual",
          status: 'setup',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            lengthSource: 'estimate',
            windows: { A: { start: 570, end: 818 } }, // 09:30 to 13:38
          },
        },
        {
          id: 'c2',
          name: "Women's Individual",
          status: 'draw-ready',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            lengthSource: 'draw',
            windows: { B: { start: 855, end: 962 } }, // 14:15 to 16:02
          },
        },
        {
          id: 'c3',
          name: 'Junior Open',
          status: 'setup',
          expectedParticipants: 18,
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            lengthSource: 'expected',
            windows: { A: { start: 840, end: 960 } },
          },
        },
        {
          id: 'c4',
          name: 'Senior Open',
          status: 'setup',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            lengthSource: 'minimum',
            lengthReason: 'no_participants',
            windows: { B: { start: 970, end: 1000 } },
          },
        },
        {
          id: 'c5',
          name: 'Late Bout',
          status: 'setup',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            pastMidnight: true,
            lengthSource: 'estimate',
            windows: { A: { start: 1320, end: 1480 } }, // 22:00 to 00:40
          },
        },
        { id: 'np1', name: 'Teams A', placement: { placed: false, notPlacedReason: 'no_start_time' } },
        { id: 'np2', name: 'Teams B', placement: { placed: false, notPlacedReason: 'no_date' } },
        { id: 'np3', name: 'Teams C', placement: { placed: false, notPlacedReason: 'date_outside_tournament' } },
      ],
    };

    window.API.fetchCalendar.mockResolvedValue(CALENDAR_DATA);

    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
        onEditTournament={onEditTournament}
        onOpenCompetition={onOpenCompetition}
      />
    );

    // Wait for calendar to load
    await waitFor(() => {
      expect(screen.getByText("Men's Individual")).toBeInTheDocument();
    });

    // Pause band copy verbatim
    expect(screen.getByText('Lunch 12:30 to 13:30')).toBeInTheDocument();
    // A pause matches go on through is drawn lighter and says so, and the
    // block it overlaps neither strips it nor names it among its pauses.
    const goesOn = screen.getByText('Opening ceremony 09:00 to 09:45 · matches go on');
    expect(goesOn.closest('.cal-pause-band')).toHaveClass('cal-pause-band--matches-go-on');
    expect(screen.getByText('Lunch 12:30 to 13:30').closest('.cal-pause-band')).not.toHaveClass('cal-pause-band--matches-go-on');
    const mens = screen.getAllByTestId('cal-block').find(b => b.textContent.includes("Men's Individual"));
    const mensLabel = mens.querySelector('[aria-label]').getAttribute('aria-label');
    expect(mensLabel).toContain('Pauses: Lunch 12:30 to 13:30.');
    expect(mensLabel).not.toContain('Opening ceremony');

    // The day heading reads the DD-MM-YYYY date the server sends.
    expect(screen.getAllByText('Sat 16 May').length).toBeGreaterThan(0);

    // A competition whose settings could not be read is named, not dropped.
    expect(screen.getByText(/The settings of competition kids could not be read/)).toBeInTheDocument();

    // Key line skipped pause verbatim: "Matches will not stop for Closing ceremony: it has no start time."
    expect(screen.getByText(/Matches will not stop for Closing ceremony: it has no start time\./)).toBeInTheDocument();
    const editTournBtns = screen.getAllByRole('button', { name: 'Edit tournament' });
    expect(editTournBtns.length).toBeGreaterThan(0);
    fireEvent.click(editTournBtns[0]);
    expect(onEditTournament).toHaveBeenCalled();

    // Block time copy verbatim:
    // Estimate has "to about"
    expect(screen.getByText('09:30 to about 13:38')).toBeInTheDocument();
    // Draw has no "about"
    expect(screen.getByText('14:15 to 16:02')).toBeInTheDocument();

    // Length source with expected count, the count the payload carries.
    expect(screen.getByText('Estimate for 18 expected')).toBeInTheDocument();

    // Placeholder block: "?" chip and "No participants yet: length unknown"
    expect(screen.getByText('No participants yet: length unknown')).toBeInTheDocument();

    // Past-midnight block verbatim: "Ends after midnight, at about 00:40 on Sun 17 May. Start it earlier or give it more shiaijo."
    expect(screen.getByText('Ends after midnight, at about 00:40 on Sun 17 May. Start it earlier or give it more shiaijo.')).toBeInTheDocument();

    // Tray line above grid verbatim: "3 competitions are not on the calendar. [Show them]"
    expect(screen.getByText(/3 competitions are not on the calendar\./)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Show them' })).toBeInTheDocument();

    // Tray below the grid verbatim:
    expect(screen.getByText('Not on the calendar (3)')).toBeInTheDocument();
    expect(screen.getByText('These competitions cannot be placed yet. Each says what it needs.')).toBeInTheDocument();
    expect(screen.getByText('1 more is on the calendar with no real length.')).toBeInTheDocument();

    // Empty day head verbatim: "No competitions"
    expect(screen.getByText('No competitions')).toBeInTheDocument();
  });

  it('renders off-screen overlap line verbatim and handles click to show day', async () => {
    const CALENDAR_WITH_OFFSCREEN = {
      days: Array.from({ length: 6 }, (_, i) => ({ day: i + 1, date: `2026-05-${16 + i}` })),
      courts: ['A', 'B'],
      competitions: [
        {
          id: 'c1',
          name: 'Day 1 Comp',
          placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 660 } } },
        },
        {
          id: 'c2',
          name: 'Senior Individual',
          placement: { placed: true, day: 6, courts: ['B'], windows: { B: { start: 600, end: 720 } } },
          clashPartners: ['c3'],
        },
        {
          id: 'c3',
          name: 'Junior Individual',
          placement: { placed: true, day: 6, courts: ['B'], windows: { B: { start: 660, end: 780 } } },
          clashPartners: ['c2'],
        },
      ],
    };

    window.API.fetchCalendar.mockResolvedValue(CALENDAR_WITH_OFFSCREEN);

    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
      />
    );

    // Off-screen overlap on Day 6, Shiaijo B
    await waitFor(() => {
      expect(screen.getByText('Overlap on Day 6, Shiaijo B')).toBeInTheDocument();
    });
    const showDayBtn = screen.getByRole('button', { name: 'Show Day 6' });
    expect(showDayBtn).toBeInTheDocument();

    fireEvent.click(showDayBtn);
    // Should navigate to page showing Day 6
    await waitFor(() => {
      expect(screen.getByText('Senior Individual')).toBeInTheDocument();
    });
  });

  it('names every line of a block for a screen reader, keeps the kachinuki range off the time line, and nests no control in another', async () => {
    window.API.fetchCalendar.mockResolvedValue({
      days: [{ day: 1, date: '16-05-2026', breaks: [] }],
      courts: ['A', 'B'],
      competitions: [
        {
          id: 'k1', name: 'Kachinuki Teams', status: 'setup',
          placement: { placed: true, day: 1, courts: ['A'], lengthSource: 'estimate', bestEnd: '11:39', worstEnd: '13:58', windows: { A: { start: 570, end: 800 } } },
          clashPartners: ['k2'],
        },
        {
          id: 'k2', name: 'Open', status: 'pools',
          placement: { placed: true, day: 1, courts: ['A'], lengthSource: 'draw', windows: { A: { start: 660, end: 720 } } },
          clashPartners: ['k1'],
        },
        {
          id: 'k3', name: 'Late', status: 'setup',
          placement: { placed: true, day: 1, courts: ['B'], pastMidnight: true, lengthSource: 'estimate', windows: { B: { start: 1320, end: 1480 } } },
        },
      ],
    });
    const { container } = render(<AdminCalendarPage tournament={TOURNAMENT} password="pass" onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText('Kachinuki Teams')).toBeInTheDocument());

    expect(screen.getByText('09:30 to about 13:20').className).toBe('cal-block__time');
    expect(screen.getByText('Could end 11:39 to 13:58').className).toBe('cal-block__meta');

    const kachinuki = screen.getByRole('button', { name: /^Kachinuki Teams, / });
    expect(kachinuki.getAttribute('aria-label')).toContain('Could end 11:39 to 13:58.');
    expect(kachinuki.getAttribute('aria-label')).toContain('Overlaps Open on Shiaijo A, 1h.');
    const running = screen.getByRole('button', { name: /^Open, / });
    expect(running.getAttribute('aria-label')).toContain('Running: cannot be moved.');
    const late = screen.getByRole('button', { name: /^Late, / });
    expect(late.getAttribute('aria-label')).toContain('Ends after midnight, at about 00:40 on Sun 17 May.');

    // The Move button sits beside the part that opens the competition.
    expect(screen.getByRole('button', { name: 'Move Kachinuki Teams' }).closest('[role="button"]')).toBeNull();
    expect(container.querySelector('[role="button"] button, button button, [role="button"] [role="button"]')).toBeNull();
  });

  it('on a touch screen, gives a block too short for Move its name only and no Move button, and M still moves it', async () => {
    const savedMatchMedia = window.matchMedia;
    window.matchMedia = (q) => ({ matches: q === '(pointer: coarse)', addEventListener() {}, removeEventListener() {} });
    try {
      const onOpenCompetition = vi.fn();
      // The Move dialog reads the competition afresh when it opens.
      window.API.fetchCompetitions = vi.fn(() => new Promise(() => {}));
      window.API.fetchCalendar.mockResolvedValue({
        days: [{ day: 1, date: '16-05-2026', breaks: [] }],
        courts: ['A', 'B'],
        competitions: [
          { id: 's1', name: 'Short One', status: 'setup', placement: { placed: true, day: 1, courts: ['A'], lengthSource: 'estimate', windows: { A: { start: 600, end: 630 } } } },
          { id: 'l1', name: 'Long One', status: 'setup', placement: { placed: true, day: 1, courts: ['B'], lengthSource: 'estimate', windows: { B: { start: 600, end: 720 } } } },
        ],
      });
      render(<AdminCalendarPage tournament={TOURNAMENT} password="pass" onBack={() => {}} onOpenCompetition={onOpenCompetition} />);
      await waitFor(() => expect(screen.getByText('Short One')).toBeInTheDocument());

      const short = screen.getByRole('button', { name: /^Short One, / });
      expect(short.getAttribute('aria-label')).toBe('Short One, 10:00 to about 10:30.');
      expect(short.closest('[data-testid="cal-block"]').className).toContain('cal-block--name-only');
      expect(screen.queryByRole('button', { name: 'Move Short One' })).toBeNull();
      expect(screen.getByRole('button', { name: 'Move Long One' })).toBeInTheDocument();

      fireEvent.keyDown(short, { key: 'm' });
      await waitFor(() => expect(screen.getByTestId('cal-move-dialog')).toBeInTheDocument());
      expect(screen.getByRole('heading', { name: 'Move Short One' })).toBeInTheDocument();
    } finally {
      window.matchMedia = savedMatchMedia;
    }
  });

  it('calls onOpenCompetition on click under MOUSE_SLOP_PX and on Enter key', async () => {
    const onOpenCompetition = vi.fn();
    window.API.fetchCalendar.mockResolvedValue({
      days: [{ day: 1, date: '16-05-2026' }],
      courts: ['A'],
      competitions: [
        {
          id: 'comp-101',
          name: 'Championship Bout',
          placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 660 } } },
        },
      ],
    });

    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
        onOpenCompetition={onOpenCompetition}
      />
    );

    await waitFor(() => {
      expect(screen.getByText('Championship Bout')).toBeInTheDocument();
    });

    const block = screen.getByText('Championship Bout').closest('[role="button"]');

    // Click with displacement under MOUSE_SLOP_PX (e.g. 2px dx, 2px dy = 2.8px < 6px)
    fireEvent.pointerDown(block, { clientX: 100, clientY: 100, pointerType: 'mouse' });
    fireEvent.click(block, { clientX: 102, clientY: 102, detail: 1 });
    expect(onOpenCompetition).toHaveBeenCalledWith('comp-101', 'overview');

    // Keyboard activation with Enter
    onOpenCompetition.mockClear();
    fireEvent.keyDown(block, { key: 'Enter' });
    expect(onOpenCompetition).toHaveBeenCalledWith('comp-101', 'overview');
  });

  it('debounces SSE events by 1000 ms so multiple events trigger only one re-fetch', async () => {
    vi.useFakeTimers();

    let fetchCount = 0;
    window.API.fetchCalendar = vi.fn(async () => {
      fetchCount++;
      return {
        days: [{ day: 1, date: '16-05-2026' }],
        courts: ['A'],
        competitions: [{ id: 'c1', name: 'Open', placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 600 } } } }],
      };
    });

    render(
      <AdminCalendarPage
        tournament={TOURNAMENT}
        password="pass"
        onBack={() => {}}
      />
    );

    // Initial fetch settles
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetchCount).toBe(1);
    expect(typeof sseHandler).toBe('function');

    // Fire 3 SSE events within 1 s
    act(() => {
      sseHandler({ type: 'tournament_updated' });
    });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    act(() => {
      sseHandler({ type: 'schedule_updated' });
    });
    act(() => {
      vi.advanceTimersByTime(400);
    });
    act(() => {
      sseHandler({ type: 'match_updated' });
    });

    // Still only the initial fetch so far
    expect(fetchCount).toBe(1);

    // Advance past 1000 ms from the last event
    await act(async () => {
      vi.advanceTimersByTime(1000);
      await Promise.resolve();
    });

    // Exactly one re-fetch occurred
    expect(fetchCount).toBe(2);
  });

  it('keeps showing the loading state while the fetch that aborted an earlier one is still out', async () => {
    vi.useFakeTimers();

    let resolveSecond;
    let calls = 0;
    window.API.fetchCalendar = vi.fn((_pw, signal) => {
      calls++;
      if (calls === 1) {
        return new Promise((_, reject) => {
          signal.addEventListener('abort', () => {
            const err = new Error('aborted');
            err.name = 'AbortError';
            reject(err);
          });
        });
      }
      return new Promise((resolve) => { resolveSecond = resolve; });
    });

    render(<AdminCalendarPage tournament={TOURNAMENT} password="pass" onBack={() => {}} />);
    expect(screen.getByText('Loading the calendar…')).toBeInTheDocument();

    // A push starts a second fetch, which aborts the first.
    act(() => { sseHandler({ type: 'tournament_updated' }); });
    await act(async () => {
      vi.advanceTimersByTime(1000);
      await Promise.resolve();
    });
    expect(calls).toBe(2);
    expect(screen.getByText('Loading the calendar…')).toBeInTheDocument();

    await act(async () => {
      resolveSecond({
        days: [{ day: 1, date: '16-05-2026' }],
        courts: ['A'],
        competitions: [{ id: 'c1', name: 'Open', placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 600 } } } }],
      });
      await Promise.resolve();
    });
    expect(screen.queryByText('Loading the calendar…')).not.toBeInTheDocument();
  });
});
