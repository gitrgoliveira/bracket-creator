import React from 'react';
import { render, act, fireEvent, waitFor, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const TEST_KEY = 'test-key';
const SESSION_KEY = 'session-key';

const Stub = (name) => {
  const C = ({ children }) => <div data-stub={name}>{children}</div>;
  C.displayName = `Stub(${name})`;
  return C;
};

let restoreGlobals;
let AdminBackupPage;

const mockApi = {
  backupTournament: vi.fn(),
  backupCompetition: vi.fn(),
  planRestore: vi.fn(),
  restore: vi.fn(),
  listBackups: vi.fn(),
  downloadBackup: vi.fn(),
  deleteBackup: vi.fn(),
};

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  promptDialog: vi.fn(),
  API: mockApi,
};

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_backup.jsx');
  AdminBackupPage = window.AdminBackupPage;
});

afterAll(() => {
  restoreGlobals();
});

// A competition's choices are one group, named by the competition; inside it
// each label says only what it does ("Replace it"), so the name is said once.
const choiceIn = (compName, label) =>
  within(screen.getByRole('group', { name: new RegExp(`^${compName} `) })).getByLabelText(label);

const defaultTournament = {
  name: 'Spring Taikai',
  date: '01-06-2026',
  venue: 'Main Arena',
  courts: ['A', 'B'],
  competitions: [
    { id: 'c1', name: 'Open Individual', format: 'mixed' },
    { id: 'c2', name: 'Women Individual', format: 'knockout' },
  ],
};

describe('AdminBackupPage render tests', () => {
  beforeEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    mockApi.planRestore.mockReset();
    mockApi.restore.mockReset();
    mockApi.listBackups.mockReset();
    mockApi.backupTournament.mockReset();
    mockApi.backupCompetition.mockReset();
    mockApi.downloadBackup.mockReset();
    mockApi.deleteBackup.mockReset();
    mockApi.backupTournament.mockResolvedValue({ blob: new Blob(['tournament-zip']), filename: 'spring-taikai-20261104T120000Z.zip' });
    mockApi.backupCompetition.mockResolvedValue({ blob: new Blob(['competition-zip']), filename: 'spring-taikai-c1-20261104T120000Z.zip' });
    mockApi.downloadBackup.mockResolvedValue(new Blob(['backup-zip']));
    mockApi.listBackups.mockResolvedValue([
      { name: '20261104T120000Z-scheduled.zip', kind: 'scheduled', time: '2026-11-04T12:00:00Z', size: 1024 },
      { name: '20261104T100000Z-before-restore-tournament.zip', kind: 'before-restore', time: '2026-11-04T10:00:00Z', size: 2048 },
    ]);
  });

  it('renders the copy list with both kinds, and the privacy note', async () => {
    render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={TEST_KEY}
      />
    );

    expect(screen.getByText(/A backup file does not contain the admin password/i)).toBeInTheDocument();
    expect(screen.getByText(/The app takes a daily copy by itself/i)).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText('20261104T120000Z-scheduled.zip')).toBeInTheDocument();
      expect(screen.getByText('20261104T100000Z-before-restore-tournament.zip')).toBeInTheDocument();
    });

    expect(screen.getByText('Daily')).toBeInTheDocument();
    expect(screen.getByText('Before restore')).toBeInTheDocument();
  });

  it('shows the plan with both kinds of choice with nothing preselected, and Restore disabled naming the open choice', async () => {
    const plan = {
      scope: 'tournament',
      warnings: [
        { code: 'matches_running', message: 'Matches running' },
        { code: 'mode_changes', message: 'Mode change' },
        { code: 'court_missing', court: 'C', message: 'Court missing' },
      ],
      competitions: [
        {
          id: 'c1',
          name: 'Open Individual',
          asked: true,
          choices: ['replace', 'copy'],
          runningNow: ['m1'],
          resultsChanged: ['m1'],
        },
        {
          id: 'c3_missing',
          name: 'Legacy Division',
          asked: true,
          choices: ['keep', 'remove'],
          runningNow: ['m-leg-1'],
        },
      ],
    };

    mockApi.planRestore.mockResolvedValueOnce(plan);

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={TEST_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Restore plan: whole tournament" })).toBeInTheDocument();
    });

    // Verify warnings and offline devices text
    expect(screen.getByText(/Let every court device sync before you restore/i)).toBeInTheDocument();
    expect(screen.getByText('Matches running')).toBeInTheDocument();
    expect(screen.getByText('Mode change')).toBeInTheDocument();
    expect(screen.getByText('Court missing')).toBeInTheDocument();
    expect(screen.getByText(/^1 recorded result in "Open Individual"$/)).toBeInTheDocument();

    // Verify both choice sets rendered with nothing preselected
    const radios = screen.getAllByRole('radio');
    radios.forEach((r) => {
      expect(r).not.toBeChecked();
    });

    // Check Restore button is disabled and names the open choice
    const restoreBtn = screen.getByRole('button', { name: /Choose what to do with "Open Individual"/i });
    expect(restoreBtn).toHaveAttribute('aria-disabled', 'true');
    // It stays focusable so Tab reaches what it says, and a tap on it asks nothing.
    expect(restoreBtn).toBeEnabled();
    window.promptDialog = vi.fn();
    fireEvent.click(restoreBtn);
    expect(window.promptDialog).not.toHaveBeenCalled();
    // The tap takes the operator to the choice the button names.
    expect(choiceIn('Open Individual', 'Replace it')).toHaveFocus();

    // Answer choice for c1 as "replace"
    const replaceRadio = choiceIn('Open Individual', 'Replace it');
    await act(async () => {
      fireEvent.click(replaceRadio);
    });

    // Now it should name the second open choice (Legacy Division)
    const restoreBtn2 = screen.getByRole('button', { name: /Choose what to do with "Legacy Division"/i });
    expect(restoreBtn2).toHaveAttribute('aria-disabled', 'true');

    // Answer choice for c3_missing as "remove"
    const removeRadio = choiceIn('Legacy Division', 'Remove it');
    await act(async () => {
      fireEvent.click(removeRadio);
    });

    // Now all choices made: Restore button is enabled
    const enabledRestoreBtn = screen.getByRole('button', { name: /^Restore$/i });
    expect(enabledRestoreBtn).not.toHaveAttribute('aria-disabled');
  });

  it('handles the confirm flow, keeps wrong password in the dialog, and updates result surviving refetch', async () => {
    const plan = {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const restoreResponse = {
      plan,
      safetyCopy: '20261104T150000Z-before-restore-competition.zip',
      prefixWarnings: [{ competitionId: 'c1', oldPrefix: 'A', newPrefix: 'B' }],
    };

    let dialogSubmitCb = null;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    const { container, rerender } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    // Click Restore
    await act(async () => {
      fireEvent.click(restoreBtn);
    });

    expect(window.promptDialog).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Confirm restore',
        password: true,
        danger: true,
      })
    );

    // Test wrong confirm password keeps error in dialog
    mockApi.restore.mockRejectedValueOnce(
      Object.assign(new Error('Password confirmation failed'), { code: 'confirm_password_wrong' })
    );

    let submitResult;
    await act(async () => {
      submitResult = await dialogSubmitCb('bad-pw');
    });
    expect(submitResult).toBe('Password confirmation failed');

    // Test correct password succeeds
    mockApi.restore.mockResolvedValueOnce(restoreResponse);
    await act(async () => {
      submitResult = await dialogSubmitCb('correct-pw');
    });
    expect(submitResult).toBeNull();

    // Verify result is shown
    expect(await screen.findByText(/Restore completed/i)).toBeInTheDocument();
    // The Restore button that opened the dialog is gone, so focus goes to the result.
    expect(screen.getByText('Restore completed')).toHaveFocus();
    expect(screen.getByText('20261104T150000Z-before-restore-competition.zip')).toBeInTheDocument();
    expect(screen.getByText(/Open Individual: number prefix changed from A to B\. Reprint its competitors' tags\./i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Undo this restore/i })).toBeInTheDocument();

    // Verify result SURVIVES tournament update (rerender)
    rerender(
      <AdminBackupPage
        tournament={{ ...defaultTournament, name: 'Updated Spring Taikai' }}
        password={SESSION_KEY}
      />
    );

    expect(screen.getByText(/Restore completed/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Undo this restore/i })).toBeInTheDocument();
  });

  it('runs Undo showing its plan and asking the password', async () => {
    let dialogSubmitCb = null;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    // 1. Initial restore setup
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    });
    mockApi.restore.mockResolvedValueOnce({
      safetyCopy: '20261104T150000Z-before-restore-competition.zip',
    });

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    const undoBtn = await screen.findByRole('button', { name: /Undo this restore/i });
    expect(undoBtn).toBeInTheDocument();

    // 2. Undo flow
    const undoPlan = {
      scope: 'competition',
      undo: true,
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    };
    mockApi.planRestore.mockResolvedValueOnce(undoPlan);
    mockApi.restore.mockResolvedValueOnce({ success: true });

    // Click "Undo this restore"
    await act(async () => {
      fireEvent.click(undoBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          from: '20261104T150000Z-before-restore-competition.zip',
          undo: true,
        }),
        SESSION_KEY
      );
    });

    // Confirm undo dialog
    expect(window.promptDialog).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Confirm undo',
      })
    );
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    // The undo is named as one, takes focus, and offers no second undo.
    expect(await screen.findByText('Restore undone')).toHaveFocus();
    expect(screen.queryByText('Restore completed')).toBeNull();
    expect(screen.queryByRole('button', { name: /Undo this restore/i })).toBeNull();
  });

  it('re-runs the dry run with only when the Restore picker names one competition', async () => {
    const multiPlan = {
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c2', name: 'Women Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(multiPlan);

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    const picker = await screen.findByRole('combobox', { name: 'Restore' });
    expect(picker.value).toBe('');
    expect(screen.queryByRole('button', { name: /Restore only/ })).toBeNull();

    const singlePlan = {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] }],
    };
    mockApi.planRestore.mockResolvedValueOnce(singlePlan);

    await act(async () => {
      fireEvent.change(picker, { target: { value: 'c1' } });
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          file: fakeFile,
          only: 'c1',
        }),
        SESSION_KEY
      );
    });
    await screen.findByRole('heading', { name: 'Restore plan: one competition' });
    const narrowed = screen.getByRole('combobox', { name: 'Restore' });
    expect(narrowed.value).toBe('c1');
    expect(Array.from(narrowed.options).map((o) => o.textContent)).toEqual([
      'All competitions',
      'Only Open Individual',
      'Only Women Individual',
    ]);
  });

  it('lists only the backup\'s competitions in the Restore picker, and shows none for a one-competition backup', async () => {
    const plan = {
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c3', name: 'Kids Individual', asked: false, action: 'create' },
        { id: 'c2_missing', name: 'Legacy Division', asked: true, choices: ['keep', 'remove'] },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    const picker = await screen.findByRole('combobox', { name: 'Restore' });
    expect(Array.from(picker.options).map((o) => o.textContent)).toEqual([
      'All competitions',
      'Only Open Individual',
      'Only Kids Individual',
    ]);

    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c2_missing', name: 'Legacy Division', asked: true, choices: ['keep', 'remove'] },
      ],
    });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });
    await waitFor(() => {
      expect(screen.queryByText('Kids Individual')).toBeNull();
    });
    expect(screen.queryByRole('combobox', { name: 'Restore' })).toBeNull();
  });

  it('loads plan for a listed copy, with no Undo button on the row, and asking choices even for before-restore', async () => {
    const beforeRestorePlan = {
      scope: 'tournament',
      kind: 'before-restore',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(beforeRestorePlan);

    render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    await waitFor(() => {
      expect(screen.getByText('20261104T100000Z-before-restore-tournament.zip')).toBeInTheDocument();
    });

    // Check that listed row buttons exist by aria-label and NEVER Undo
    expect(screen.queryByRole('button', { name: /Undo/i })).toBeNull();
    expect(screen.getByRole('button', { name: 'Download 20261104T100000Z-before-restore-tournament.zip' })).toBeInTheDocument();
    const restoreBtn = screen.getByRole('button', { name: 'Restore 20261104T100000Z-before-restore-tournament.zip' });
    expect(restoreBtn).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete 20261104T100000Z-before-restore-tournament.zip' })).toBeInTheDocument();

    // Click Restore on the before-restore row
    await act(async () => {
      fireEvent.click(restoreBtn);
    });

    expect(mockApi.planRestore).toHaveBeenCalledWith(
      expect.objectContaining({
        from: '20261104T100000Z-before-restore-tournament.zip',
        undo: false,
      }),
      SESSION_KEY
    );

    // Expect choices to be asked
    await waitFor(() => {
      expect(choiceIn('Open Individual', 'Replace it')).toBeInTheDocument();
    });
  });

  // Additional tests a-g:

  it('a. warnings: shows verbatim messages, runningNow and resultsChanged, and offline-device paragraph', async () => {
    const plan = {
      scope: 'tournament',
      warnings: [
        { code: 'mode_changes', message: 'Mode will change to individual only' },
        { code: 'court_missing', court: 'Court 3', message: 'Court 3 is missing in this tournament' },
        { code: 'matches_running', competitionId: 'c1', message: 'Open Individual has 1 match running now.' },
      ],
      competitions: [
        {
          id: 'c1',
          name: 'Open Individual',
          asked: false,
          runningNow: ['m101'],
          resultsChanged: ['m101', 'm102'],
        },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    expect(await screen.findByText('Mode will change to individual only')).toBeInTheDocument();
    expect(screen.getByText('Court 3 is missing in this tournament')).toBeInTheDocument();
    // The server's warning is the one place running matches are named: no second list repeats it.
    expect(screen.getByText('Open Individual has 1 match running now.')).toBeInTheDocument();
    expect(screen.queryByText(/Running matches affected/i)).not.toBeInTheDocument();
    expect(screen.getByText(/^2 recorded results in "Open Individual"$/)).toBeInTheDocument();
    expect(screen.getByText(/Let every court device sync before you restore/i)).toBeInTheDocument();
  });

  it('says each hint once: no second list, label or line repeats a warning or the button', async () => {
    const plan = {
      scope: 'tournament',
      warnings: [
        { code: 'matches_running', competitionId: 'c2', message: 'Juniors has 1 match running now; if you remove it, that match is removed with it.' },
        { code: 'prefix_reassigned', competitionId: 'c1', message: "Open's number prefix is used by another competition, so it gets a new one; reprint this competition's tags." },
      ],
      competitions: [
        { id: 'c1', name: 'Open', asked: false, prefixCollides: true },
        { id: 'c2', name: 'Juniors', asked: true, choices: ['keep', 'remove'], runningNow: ['m7'] },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );
    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    expect(await screen.findAllByText(/Juniors has 1 match running now/)).toHaveLength(1);
    expect(screen.getAllByText(/reprint/i)).toHaveLength(1);
    expect(screen.queryByText(/running:/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/m7/)).not.toBeInTheDocument();
    expect(screen.getAllByText(/Juniors/).filter((el) => /Choose what to do/.test(el.textContent))).toHaveLength(1);
    expect(screen.queryByText(/Please /)).not.toBeInTheDocument();
    // The competition's name heads its row once; the choices do not repeat it.
    const group = screen.getByRole('group', { name: /Juniors.*not in the backup archive/ });
    expect(within(group).getByLabelText('Keep it')).toBeInTheDocument();
    expect(within(group).getByLabelText('Remove it')).toBeInTheDocument();
    expect(within(group).queryByText(/Juniors/)).not.toBeInTheDocument();
  });

  it('names the copy box for its competition and shows each format by its label', async () => {
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] }],
    });
    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );
    expect(screen.getByText('Pools + Knockout')).toBeInTheDocument();
    expect(screen.getByText('Knockout')).toBeInTheDocument();
    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });
    const box = await screen.findByRole('textbox', { name: 'Name for the copy of "Open Individual"' });
    expect(box).not.toHaveAttribute('placeholder');
  });

  it('b. allowRunning: with running matches in the plan, restore is called with allowRunning true; without, it is not', async () => {
    let dialogSubmitCb;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    // 1. Plan WITH running matches
    const planWithRunning = {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false, runningNow: ['m1'] }],
    };
    mockApi.planRestore.mockResolvedValueOnce(planWithRunning);
    mockApi.restore.mockResolvedValueOnce({ plan: planWithRunning });

    const { container, unmount } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    let fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });
    await act(async () => {
      await dialogSubmitCb('confirm-pw');
    });

    expect(mockApi.restore).toHaveBeenLastCalledWith(
      expect.objectContaining({ allowRunning: true }),
      TEST_KEY,
      'confirm-pw'
    );

    unmount();

    // 2. Plan WITHOUT running matches
    mockApi.restore.mockClear();
    const planWithoutRunning = {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false, runningNow: [] }],
    };
    mockApi.planRestore.mockResolvedValueOnce(planWithoutRunning);
    mockApi.restore.mockResolvedValueOnce({ plan: planWithoutRunning });

    const rendered2 = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    fileInput = rendered2.container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy2'], 'backup2.zip')] } });
    });

    const restoreBtn2 = await rendered2.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn2).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn2);
    });
    await act(async () => {
      await dialogSubmitCb('confirm-pw');
    });

    expect(mockApi.restore).toHaveBeenLastCalledWith(
      expect.not.objectContaining({ allowRunning: true }),
      TEST_KEY,
      'confirm-pw'
    );
  });

  it('c. wrong confirm: the restore stub rejects with Object.assign(new Error(\'That password is not right.\'), {code: \'confirm_password_wrong\'}); promptDialog submit returns message (dialog stays open) and onLogout not called', async () => {
    const onLogout = vi.fn();
    let dialogSubmitCb;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    });
    mockApi.restore.mockRejectedValueOnce(
      Object.assign(new Error('That password is not right.'), { code: 'confirm_password_wrong' })
    );

    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} onLogout={onLogout} />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });

    let submitResult;
    await act(async () => {
      submitResult = await dialogSubmitCb('wrong-pw');
    });

    expect(submitResult).toBe('That password is not right.');
    expect(onLogout).not.toHaveBeenCalled();
  });

  it('d. the result survives a re-render with a new tournament prop', async () => {
    let dialogSubmitCb;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    });
    mockApi.restore.mockResolvedValueOnce({
      plan: { scope: 'competition' },
      safetyCopy: '20261104T123456Z-before-restore-competition.zip',
    });

    const { container, rerender } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(await screen.findByText('20261104T123456Z-before-restore-competition.zip')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Undo this restore/i })).toBeInTheDocument();

    // Rerender with a changed tournament object
    rerender(
      <AdminBackupPage
        tournament={{ ...defaultTournament, name: 'Brand New Tournament Name' }}
        password={TEST_KEY}
      />
    );

    expect(screen.getByText('20261104T123456Z-before-restore-competition.zip')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Undo this restore/i })).toBeInTheDocument();
  });

  it('e. prefix warning after restore names the competition', async () => {
    let dialogSubmitCb;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    });
    mockApi.restore.mockResolvedValueOnce({
      plan: {
        competitions: [{ id: 'c1', name: 'Open Individual', action: 'replace' }],
      },
      safetyCopy: '20261104T100000Z-before-restore-tournament.zip',
      prefixWarnings: [{ competitionId: 'c1', oldPrefix: 'A', newPrefix: 'B' }],
    });

    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(await screen.findByText(/Open Individual: number prefix changed from A to B\. Reprint its competitors' tags\./i)).toBeInTheDocument();
  });

  it('f. a listed "Before restore" copy\'s Restore calls planRestore with {from: name} and no undo true, asked choices with nothing selected, Restore disabled, no button named /Undo/', async () => {
    const beforeRestorePlan = {
      scope: 'tournament',
      kind: 'before-restore',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(beforeRestorePlan);

    render(
      <AdminBackupPage tournament={defaultTournament} password={SESSION_KEY} />
    );

    const rowRestoreBtn = await screen.findByRole('button', { name: 'Restore 20261104T100000Z-before-restore-tournament.zip' });
    expect(rowRestoreBtn).toBeInTheDocument();

    // Check no button named /Undo/
    expect(screen.queryByRole('button', { name: /Undo/i })).toBeNull();

    // Click listed copy's Restore
    await act(async () => {
      fireEvent.click(rowRestoreBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          from: '20261104T100000Z-before-restore-tournament.zip',
          undo: false,
        }),
        SESSION_KEY
      );
    });
    expect(mockApi.planRestore.mock.calls[0][0].undo).not.toBe(true);

    // Asked choices render with nothing selected
    await screen.findByRole('group', { name: /Open Individual/ });
    const radio = choiceIn('Open Individual', 'Replace it');
    expect(radio).toBeInTheDocument();
    expect(radio).not.toBeChecked();

    // Main restore button is disabled (names open choice)
    const restoreBtn = screen.getByRole('button', { name: /Choose what to do with "Open Individual"/i });
    expect(restoreBtn).toHaveAttribute('aria-disabled', 'true');

    // Still no button named /Undo/
    expect(screen.queryByRole('button', { name: /Undo/i })).toBeNull();
  });

  it('g. nothingToRestore plan shows its message and no button named "Restore"', async () => {
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      nothingToRestore: true,
      message: 'The backup archive has no files to restore.',
      competitions: [],
    });

    const { container } = render(
      <AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    expect(await screen.findByText('The backup archive has no files to restore.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Restore$/i })).toBeNull();
  });

  it('submits restore with undo: true when plan is an undo plan, and undo: false for normal plan', async () => {
    let dialogSubmitCb = null;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    // 1. Normal plan sends undo: false
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'competition',
      undo: false,
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    });
    mockApi.restore.mockResolvedValueOnce({
      safetyCopy: '20261104T150000Z-before-restore-competition.zip',
    });

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });
    expect(window.promptDialog).toHaveBeenLastCalledWith(
      expect.objectContaining({ title: 'Confirm restore' })
    );

    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(mockApi.restore).toHaveBeenLastCalledWith(
      expect.objectContaining({
        undo: false,
      }),
      SESSION_KEY,
      'correct-pw'
    );

    // 2. Undo plan sends undo: true from the primary Restore button
    const undoBtn = await screen.findByRole('button', { name: /Undo this restore/i });
    expect(undoBtn).toBeInTheDocument();

    const undoPlan = {
      scope: 'competition',
      undo: true,
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    };
    mockApi.planRestore.mockResolvedValueOnce(undoPlan);
    mockApi.restore.mockResolvedValueOnce({ success: true });

    // Click "Undo this restore" to run planRestore with undo: true
    await act(async () => {
      fireEvent.click(undoBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          from: '20261104T150000Z-before-restore-competition.zip',
          undo: true,
        }),
        SESSION_KEY
      );
    });

    // Click primary Restore button on the undo plan view
    const planRestoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    await act(async () => {
      fireEvent.click(planRestoreBtn);
    });

    expect(window.promptDialog).toHaveBeenLastCalledWith(
      expect.objectContaining({ title: 'Confirm restore' })
    );

    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(mockApi.restore).toHaveBeenLastCalledWith(
      expect.objectContaining({
        from: '20261104T150000Z-before-restore-competition.zip',
        undo: true,
      }),
      SESSION_KEY,
      'correct-pw'
    );
    // An undo made from the undo plan's Restore button reads as an undo too.
    expect(await screen.findByText('Restore undone')).toHaveFocus();
    expect(screen.queryByText('Restore completed')).toBeNull();
    expect(screen.queryByRole('button', { name: /Undo this restore/i })).toBeNull();
  });

  it('renders prefix warnings showing reassignment or refusal message when newPrefix is empty', async () => {
    const plan = {
      scope: 'competition',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: false },
        { id: 'c2', name: 'Women Individual', asked: false },
      ],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const restoreResponse = {
      plan,
      safetyCopy: '20261104T150000Z-before-restore-competition.zip',
      prefixWarnings: [
        { competitionId: 'c1', oldPrefix: 'K', newPrefix: 'L', message: 'number prefix "K" was already in use: assigned "L"; reprint this competition\'s tags' },
        { competitionId: 'c2', oldPrefix: 'M', newPrefix: '', message: 'number prefix "M" collided on restore and no replacement prefix could be derived: prefix collision' },
      ],
    };

    let dialogSubmitCb = null;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    await act(async () => {
      fireEvent.click(restoreBtn);
    });

    mockApi.restore.mockResolvedValueOnce(restoreResponse);
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(await screen.findByText(/Restore completed/i)).toBeInTheDocument();
    expect(screen.getByText(/Open Individual: number prefix changed from K to L\. Reprint its competitors' tags\./i)).toBeInTheDocument();
    expect(screen.getByText('number prefix "M" collided on restore and no replacement prefix could be derived: prefix collision')).toBeInTheDocument();
  });

  it('clears file input value after picking an archive so the same file can be reselected', async () => {
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [],
    });

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={TEST_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    let currentValue = 'C:\\fakepath\\backup.zip';
    const valueSetter = vi.fn((val) => {
      currentValue = val;
    });
    Object.defineProperty(fileInput, 'value', {
      get: () => currentValue,
      set: valueSetter,
      configurable: true,
    });

    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalled();
    });

    expect(valueSetter).toHaveBeenCalledWith('');
    expect(fileInput.value).toBe('');
  });

  it('goes back to the whole tournament when the Restore picker is set to All competitions', async () => {
    const multiPlan = {
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c2', name: 'Women Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };
    const singlePlan = {
      scope: 'competition',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };

    mockApi.planRestore.mockResolvedValueOnce(multiPlan);
    mockApi.planRestore.mockResolvedValueOnce(singlePlan);
    mockApi.planRestore.mockResolvedValueOnce(multiPlan);

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    await act(async () => {
      fireEvent.change(await screen.findByRole('combobox', { name: 'Restore' }), { target: { value: 'c1' } });
    });
    await screen.findByRole('heading', { name: 'Restore plan: one competition' });

    await act(async () => {
      fireEvent.change(screen.getByRole('combobox', { name: 'Restore' }), { target: { value: '' } });
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledTimes(3);
    });

    const lastCallArg = mockApi.planRestore.mock.calls[2][0];
    expect(lastCallArg.file).toBe(fakeFile);
    expect(lastCallArg.only).toBeUndefined();
    expect(mockApi.planRestore.mock.calls[2][1]).toBe(SESSION_KEY);

    await screen.findByRole('heading', { name: 'Restore plan: whole tournament' });
    expect(screen.getByRole('combobox', { name: 'Restore' }).value).toBe('');
    expect(Array.from(container.querySelectorAll('.backup-plan .backup-row__name')).map((n) => n.textContent)).toEqual([
      'Open Individual',
      'Women Individual',
    ]);
  });

  it.each([
    ['tournament', { name: 'Back up the whole tournament' }, 'backupTournament', [TEST_KEY], 'spring-taikai-20261104T120000Z.zip', 'Tournament backup downloaded'],
    ['competition', { name: 'Back up "Open Individual"' }, 'backupCompetition', ['c1', TEST_KEY], 'spring-taikai-c1-20261104T120000Z.zip', 'Backup for "Open Individual" downloaded'],
  ])('downloads the %s backup under the name the server sent', async (_scope, buttonQuery, apiName, apiArgs, serverName, toast) => {
    const showToast = vi.fn();
    vi.spyOn(window.URL, 'createObjectURL').mockReturnValue('blob:test');
    vi.spyOn(window.URL, 'revokeObjectURL').mockImplementation(() => {});
    const downloaded = [];
    const origCreateElement = document.createElement.bind(document);
    const createSpy = vi.spyOn(document, 'createElement').mockImplementation((tag) => {
      const el = origCreateElement(tag);
      if (tag === 'a') el.click = () => { downloaded.push(el.download); };
      return el;
    });
    try {
      render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} showToast={showToast} />);
      await act(async () => {
        fireEvent.click(screen.getAllByRole('button', buttonQuery)[0]);
      });
    } finally {
      createSpy.mockRestore();
    }
    expect(mockApi[apiName]).toHaveBeenCalledWith(...apiArgs);
    expect(downloaded).toEqual([serverName]);
    expect(showToast).toHaveBeenCalledWith(toast);
  });

  it('shows the plain sentence when an uploaded backup is over 64 MB (HTTP 413)', async () => {
    const err = new Error('request body too large');
    err.status = 413;
    mockApi.planRestore.mockRejectedValue(err);

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={TEST_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['dummy'], 'large.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    await waitFor(() => {
      expect(screen.getByText('The backup file is over 64 MB. Restore it from Copies kept on this computer or the command line.')).toBeInTheDocument();
    });
  });

  it('shows requeueError in the restore-done card when present on the restore result', async () => {
    const plan = {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false }],
    };
    mockApi.planRestore.mockResolvedValueOnce(plan);

    const requeueSentence = 'Could not return 1 running match to the queue; the app retries at its next start.';
    const restoreResponse = {
      plan,
      safetyCopy: '20261104T150000Z-before-restore-competition.zip',
      requeueError: requeueSentence,
    };

    let dialogSubmitCb = null;
    window.promptDialog = vi.fn().mockImplementation((opts) => {
      dialogSubmitCb = opts.submit;
      return Promise.resolve(null);
    });

    const { container } = render(
      <AdminBackupPage
        tournament={defaultTournament}
        password={SESSION_KEY}
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    const restoreBtn = await screen.findByRole('button', { name: /^Restore$/i });
    expect(restoreBtn).toBeEnabled();

    await act(async () => {
      fireEvent.click(restoreBtn);
    });

    mockApi.restore.mockResolvedValueOnce(restoreResponse);
    await act(async () => {
      await dialogSubmitCb('correct-pw');
    });

    expect(await screen.findByText(/Restore completed/i)).toBeInTheDocument();
    expect(screen.getByText(requeueSentence)).toBeInTheDocument();
  });
  // Picks a backup file and waits for its plan. Each later check of the
  // answers gets the same plan back, with no name taken.
  const openPlan = async (container, plan) => {
    mockApi.planRestore.mockResolvedValue(plan);
    await act(async () => {
      fireEvent.change(container.querySelector('input[type="file"]'), {
        target: { files: [new File(['zip'], 'full.zip')] },
      });
    });
  };

  it('says on each row what an undo does with it, and the undo dialog sums it up', async () => {
    window.promptDialog = vi.fn((opts) => { void opts; return Promise.resolve(null); });
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: false, action: 'create' }],
    });
    mockApi.restore.mockResolvedValueOnce({ safetyCopy: '20261104T150000Z-before-restore-tournament.zip' });
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await act(async () => {
      fireEvent.change(container.querySelector('input[type="file"]'), { target: { files: [new File(['zip'], 'full.zip')] } });
    });
    await act(async () => { fireEvent.click(await screen.findByRole('button', { name: /^Restore$/ })); });
    await act(async () => { await window.promptDialog.mock.calls[0][0].submit('pw'); });

    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      undo: true,
      tournamentReplaced: true,
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: false, action: 'replace', resultsChanged: ['m1', 'm2'] },
        { id: 'c9', name: 'Open Individual (copy)', asked: false, action: 'remove', resultsChanged: ['m3'] },
        { id: 'c2', name: 'Women Individual', asked: false, action: 'keep' },
        { id: 'c4', name: 'Juniors', asked: false, action: 'create' },
      ],
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Undo this restore' })); });

    expect(await screen.findByText('Will be put back as it was before the restore.')).toBeInTheDocument();
    expect(screen.getByText('Will be removed: the restore added it.')).toBeInTheDocument();
    expect(screen.getByText('Will be left as it is.')).toBeInTheDocument();
    expect(screen.getAllByText('Will be created as a new competition.')).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Undo plan' })).toHaveFocus();

    const undoOpts = window.promptDialog.mock.calls[1][0];
    expect(undoOpts.title).toBe('Confirm undo');
    expect(undoOpts.message).toBe(
      'This undo puts back 1 competition, adds 1 new competition, removes 1 competition and puts back the tournament settings. '
      + '3 recorded results will be lost or changed. '
      + 'Enter the admin password to undo the restore using safety copy 20261104T150000Z-before-restore-tournament.zip.',
    );
  });

  it('sums up the restore in the confirm dialog from the answers given', async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, {
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'], resultsChanged: ['m1', 'm2'] },
        { id: 'c2', name: 'Women Individual', asked: true, choices: ['replace', 'copy'], resultsChanged: ['m3'] },
        { id: 'c3', name: 'Legacy', asked: true, choices: ['keep', 'remove'] },
      ],
    });
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Replace it')); });
    await act(async () => { fireEvent.click(choiceIn('Women Individual', 'Restore as a copy named:')); });
    fireEvent.change(screen.getByRole('textbox', { name: 'Name for the copy of "Women Individual"' }), { target: { value: 'Women Old' } });
    await act(async () => { fireEvent.click(choiceIn('Legacy', 'Keep it')); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });

    // A copy loses nothing, so only the replaced competition's results are counted.
    expect(window.promptDialog.mock.calls[0][0].message).toBe(
      'This restore replaces 1 competition and adds 1 copy. 2 recorded results will be lost or changed. '
      + 'Enter the admin password to confirm the restore.',
    );
  });

  it('names the copy in the reprint warning when the copy got the new prefix', async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] }],
    });
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Restore as a copy named:')); });
    fireEvent.change(screen.getByRole('textbox', { name: 'Name for the copy of "Open Individual"' }), { target: { value: ' Open Copy ' } });
    mockApi.restore.mockResolvedValueOnce({
      plan: { competitions: [{ id: 'c1', name: 'Open Individual', action: 'copy', newId: 'open-copy' }] },
      safetyCopy: '20261104T100000Z-before-restore-competition.zip',
      prefixWarnings: [{ competitionId: 'open-copy', oldPrefix: 'A', newPrefix: 'OC' }],
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });
    await act(async () => { await window.promptDialog.mock.calls[0][0].submit('pw'); });

    expect(await screen.findByText("Open Copy: number prefix changed from A to OC. Reprint its competitors' tags.")).toBeInTheDocument();
  });

  it('focuses the plan when a kept copy asks for one, and Replace all existing answers every existing competition', async () => {
    render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    mockApi.planRestore.mockResolvedValueOnce({
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c2', name: 'Women Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c3', name: 'Legacy', asked: true, choices: ['keep', 'remove'] },
      ],
    });
    const rowRestore = await screen.findByRole('button', { name: 'Restore 20261104T120000Z-scheduled.zip' });
    await act(async () => { fireEvent.click(rowRestore); });

    expect(await screen.findByRole('heading', { name: 'Restore plan: whole tournament' })).toHaveFocus();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Replace all existing' })); });
    expect(choiceIn('Open Individual', 'Replace it')).toBeChecked();
    expect(choiceIn('Women Individual', 'Replace it')).toBeChecked();
    expect(choiceIn('Legacy', 'Keep it')).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Choose what to do with "Legacy"' })).toBeInTheDocument();
  });

  it('a tap on the Restore button that asks for a copy name goes to that name box', async () => {
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, {
      scope: 'competition',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] }],
    });
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Restore as a copy named:')); });
    fireEvent.click(screen.getByRole('button', { name: 'Enter name for copy of "Open Individual"' }));
    expect(screen.getByRole('textbox', { name: 'Name for the copy of "Open Individual"' })).toHaveFocus();
    // One existing competition is not a list worth a shortcut.
    expect(screen.queryByRole('button', { name: 'Replace all existing' })).toBeNull();
  });

  const keepPlan = {
    scope: 'tournament',
    tournamentReplaced: true,
    competitions: [
      { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy', 'keep'], resultsChanged: ['m1'] },
      { id: 'c4', name: 'Juniors', asked: false, action: 'create', choices: ['create', 'keep'] },
    ],
  };
  const copyBox = () => screen.getByRole('textbox', { name: 'Name for the copy of "Open Individual"' });
  const lastDecisionsSent = () => mockApi.planRestore.mock.calls.at(-1)[0].decisions;

  it('offers to keep a competition you already have, and sums it up as left out', async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, keepPlan);

    const keep = choiceIn('Open Individual', "Keep the one I have (don't restore it)");
    expect(keep).not.toBeChecked();
    // The row is still one the backup holds, so the Restore picker lists it.
    expect(screen.getByText('This competition already exists:')).toBeInTheDocument();
    await act(async () => { fireEvent.click(keep); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });

    expect(lastDecisionsSent()).toEqual({ c1: { action: 'keep', copyName: '' } });
    expect(window.promptDialog.mock.calls[0][0].message).toBe(
      'This restore adds 1 new competition, leaves out 1 competition and puts back the tournament settings. '
      + 'Enter the admin password to confirm the restore.',
    );
  });

  it("adds a competition only the backup holds unless Don't add it is ticked", async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    mockApi.restore.mockResolvedValue({ safetyCopy: '' });
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, keepPlan);
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Replace it')); });

    const leaveOut = choiceIn('Juniors', "Don't add it");
    expect(leaveOut).not.toBeChecked();
    expect(screen.getByText('Will be created as a new competition.')).toBeInTheDocument();
    // Nothing to choose for it: Restore is ready with the box unticked.
    expect(screen.getByRole('button', { name: /^Restore$/ })).not.toHaveAttribute('aria-disabled');

    await act(async () => { fireEvent.click(leaveOut); });
    expect(screen.getByText('Will be left out.')).toBeInTheDocument();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });
    expect(window.promptDialog.mock.calls[0][0].message).toMatch(/^This restore replaces 1 competition, leaves out 1 competition and puts back/);
    await act(async () => { await window.promptDialog.mock.calls[0][0].submit('pw'); });
    expect(mockApi.restore.mock.calls[0][0].decisions).toEqual({
      c1: { action: 'replace', copyName: '' },
      c4: { action: 'keep' },
    });

    // Unticked again, it sends no answer for it at all.
    await openPlan(container, keepPlan);
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Replace it')); });
    await act(async () => { fireEvent.click(choiceIn('Juniors', "Don't add it")); });
    await act(async () => { fireEvent.click(choiceIn('Juniors', "Don't add it")); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });
    await act(async () => { await window.promptDialog.mock.calls[1][0].submit('pw'); });
    expect(mockApi.restore.mock.calls[1][0].decisions).toEqual({ c1: { action: 'replace', copyName: '' } });
  });

  it('checks the answers again as they are typed and shows a taken copy name before the password, keeping the focus', async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, keepPlan);
    const takenPlan = {
      ...keepPlan,
      competitions: [{ ...keepPlan.competitions[0], action: 'copy', copyNameTaken: true }, keepPlan.competitions[1]],
    };
    mockApi.planRestore.mockResolvedValue(takenPlan);
    const planCalls = mockApi.planRestore.mock.calls.length;

    vi.useFakeTimers();
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Restore as a copy named:')); });
    copyBox().focus();
    fireEvent.change(copyBox(), { target: { value: 'Women Individual' } });
    await act(async () => { vi.advanceTimersByTime(399); });
    // Nothing is asked before the pause.
    expect(mockApi.planRestore.mock.calls.length).toBe(planCalls);
    await act(async () => { vi.advanceTimersByTime(1); });

    // One check, for the answers as they stand when the typing stops.
    expect(mockApi.planRestore.mock.calls.length).toBe(planCalls + 1);
    expect(lastDecisionsSent()).toEqual({ c1: { action: 'copy', copyName: 'Women Individual' } });
    expect(mockApi.planRestore.mock.calls.at(-1)[0].file).toBeInstanceOf(File);
    expect(screen.getByText('That name is already used.')).toBeInTheDocument();
    expect(copyBox()).toHaveAttribute('aria-invalid', 'true');
    // The plan stays, the answer stays, and the box keeps the focus.
    expect(copyBox()).toHaveFocus();
    expect(copyBox()).toHaveValue('Women Individual');
    expect(choiceIn('Open Individual', 'Restore as a copy named:')).toBeChecked();

    const btn = screen.getByRole('button', { name: 'Pick another name for the copy of "Open Individual"' });
    expect(btn).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(btn);
    expect(window.promptDialog).not.toHaveBeenCalled();
    expect(copyBox()).toHaveFocus();

    // A free name clears it.
    mockApi.planRestore.mockResolvedValue(keepPlan);
    fireEvent.change(copyBox(), { target: { value: 'Open Old' } });
    expect(screen.queryByText('That name is already used.')).toBeNull();
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(screen.getByRole('button', { name: /^Restore$/ })).not.toHaveAttribute('aria-disabled');
  });

  it('checks once more when Restore is pressed and does not ask the password over a taken name', async () => {
    window.promptDialog = vi.fn(() => Promise.resolve(null));
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, keepPlan);
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Restore as a copy named:')); });
    fireEvent.change(copyBox(), { target: { value: 'Juniors' } });
    mockApi.planRestore.mockResolvedValue({
      ...keepPlan,
      competitions: [{ ...keepPlan.competitions[0], copyNameTaken: true }, keepPlan.competitions[1]],
    });

    // Pressed at once, before the pause would have asked.
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Restore$/ })); });

    expect(lastDecisionsSent()).toEqual({ c1: { action: 'copy', copyName: 'Juniors' } });
    expect(window.promptDialog).not.toHaveBeenCalled();
    expect(screen.getByText('That name is already used.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Pick another name for the copy of "Open Individual"' })).toHaveAttribute('aria-disabled', 'true');
  });

  it('names a competition whose own name is taken on its row and on the Restore button', async () => {
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, {
      scope: 'tournament',
      competitions: [{ id: 'c4', name: 'Juniors', asked: false, action: 'create', choices: ['create', 'keep'], nameTaken: true }],
    });
    expect(screen.getByText('Another competition already has this name.')).toBeInTheDocument();
    const btn = screen.getByRole('button', { name: '"Juniors": its name is already used' });
    fireEvent.click(btn);
    expect(choiceIn('Juniors', "Don't add it")).toHaveFocus();
  });

  it('drops a check that answers after a newer one', async () => {
    const { container } = render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    await openPlan(container, keepPlan);
    const answers = [];
    mockApi.planRestore.mockImplementation(() => new Promise((resolve) => { answers.push(resolve); }));
    const taken = { ...keepPlan, competitions: [{ ...keepPlan.competitions[0], copyNameTaken: true }, keepPlan.competitions[1]] };

    vi.useFakeTimers();
    await act(async () => { fireEvent.click(choiceIn('Open Individual', 'Restore as a copy named:')); });
    fireEvent.change(copyBox(), { target: { value: 'Same' } });
    await act(async () => { vi.advanceTimersByTime(400); });
    fireEvent.change(copyBox(), { target: { value: 'Same 2' } });
    fireEvent.change(copyBox(), { target: { value: 'Same' } });
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(answers).toHaveLength(2);

    // Both were asked with the same answers; the older one answers last.
    await act(async () => { answers[1](keepPlan); });
    await act(async () => { answers[0](taken); });
    expect(screen.queryByText('That name is already used.')).toBeNull();
    expect(screen.getByRole('button', { name: /^Restore$/ })).not.toHaveAttribute('aria-disabled');
  });

  it('downloads once for a double tap while the backup is being made', async () => {
    let finish;
    mockApi.backupTournament.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    vi.spyOn(window.URL, 'createObjectURL').mockReturnValue('blob:test');
    vi.spyOn(window.URL, 'revokeObjectURL').mockImplementation(() => {});
    render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} showToast={vi.fn()} />);
    const btn = screen.getByRole('button', { name: 'Back up the whole tournament' });
    fireEvent.click(btn);
    fireEvent.click(btn);
    await waitFor(() => expect(btn).toBeDisabled());
    expect(screen.getByRole('button', { name: 'Back up "Open Individual"' })).toBeDisabled();
    await act(async () => { finish({ blob: new Blob(['zip']), filename: 'spring-taikai.zip' }); });
    expect(mockApi.backupTournament).toHaveBeenCalledTimes(1);
    expect(btn).toBeEnabled();
  });

  it('leads a kept copy with its kind and local time, the file name below', async () => {
    render(<AdminBackupPage tournament={defaultTournament} password={TEST_KEY} />);
    const when = new Date('2026-11-04T10:00:00Z').toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' });
    const name = await screen.findByText(when);
    expect(name).toHaveClass('backup-row__name');
    expect(screen.getByText('20261104T100000Z-before-restore-tournament.zip')).toHaveClass('backup-row__file');
  });
});
