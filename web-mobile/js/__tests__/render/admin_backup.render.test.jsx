import React from 'react';
import { render, act, fireEvent, waitFor, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

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

const defaultTournament = {
  name: 'Spring Taikai',
  date: '01-06-2026',
  venue: 'Main Arena',
  courts: ['A', 'B'],
  competitions: [
    { id: 'c1', name: 'Open Individual', format: 'pools' },
    { id: 'c2', name: 'Women Individual', format: 'knockout' },
  ],
};

describe('AdminBackupPage render tests', () => {
  beforeEach(() => {
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
        password="test-pw"
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
        password="test-pw"
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    await waitFor(() => {
      expect(screen.getByText(/Restore Plan \(tournament archive\)/i)).toBeInTheDocument();
    });

    // Verify warnings and offline devices text
    expect(screen.getByText(/Let every court device sync before you restore/i)).toBeInTheDocument();
    expect(screen.getByText('Matches running')).toBeInTheDocument();
    expect(screen.getByText('Mode change')).toBeInTheDocument();
    expect(screen.getByText('Court missing')).toBeInTheDocument();
    expect(screen.getByText(/1 recorded results in "Open Individual" will be lost or changed/i)).toBeInTheDocument();

    // Verify both choice sets rendered with nothing preselected
    const radios = screen.getAllByRole('radio');
    radios.forEach((r) => {
      expect(r).not.toBeChecked();
    });

    // Check Restore button is disabled and names the open choice
    const restoreBtn = screen.getByRole('button', { name: /Choose what to do with "Open Individual"/i });
    expect(restoreBtn).toBeDisabled();

    // Answer choice for c1 as "replace"
    const replaceRadio = screen.getByLabelText(/Replace "Open Individual"/i);
    await act(async () => {
      fireEvent.click(replaceRadio);
    });

    // Now it should name the second open choice (Legacy Division)
    const restoreBtn2 = screen.getByRole('button', { name: /Choose what to do with "Legacy Division"/i });
    expect(restoreBtn2).toBeDisabled();

    // Answer choice for c3_missing as "remove"
    const removeRadio = screen.getByLabelText(/Remove "Legacy Division"/i);
    await act(async () => {
      fireEvent.click(removeRadio);
    });

    // Now all choices made: Restore button is enabled
    const enabledRestoreBtn = screen.getByRole('button', { name: /^Restore$/i });
    expect(enabledRestoreBtn).toBeEnabled();
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
        password="session-pw"
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
    expect(screen.getByText('20261104T150000Z-before-restore-competition.zip')).toBeInTheDocument();
    expect(screen.getByText(/Open Individual: number prefix changed from A to B\. Reprint its competitors' tags\./i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Undo this restore/i })).toBeInTheDocument();

    // Verify result SURVIVES tournament update (rerender)
    rerender(
      <AdminBackupPage
        tournament={{ ...defaultTournament, name: 'Updated Spring Taikai' }}
        password="session-pw"
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
        password="session-pw"
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
        'session-pw'
      );
    });

    // Confirm undo dialog
    expect(window.promptDialog).toHaveBeenCalledWith(
      expect.objectContaining({
        title: 'Confirm undo',
      })
    );
  });

  it('re-runs the dry run with only when "Restore only this one" is clicked', async () => {
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
        password="session-pw"
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    const onlyBtn = await screen.findByRole('button', { name: 'Restore only "Open Individual"' });
    expect(onlyBtn).toBeInTheDocument();

    const singlePlan = {
      scope: 'tournament',
      competitions: [{ id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] }],
    };
    mockApi.planRestore.mockResolvedValueOnce(singlePlan);

    await act(async () => {
      fireEvent.click(onlyBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          file: fakeFile,
          only: 'c1',
        }),
        'session-pw'
      );
    });
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
        password="session-pw"
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
      'session-pw'
    );

    // Expect choices to be asked
    await waitFor(() => {
      expect(screen.getByLabelText(/Replace "Open Individual"/i)).toBeInTheDocument();
    });
  });

  // Additional tests a-g:

  it('a. warnings: shows verbatim messages, runningNow and resultsChanged, and offline-device paragraph', async () => {
    const plan = {
      scope: 'tournament',
      warnings: [
        { code: 'mode_changes', message: 'Mode will change to individual only' },
        { code: 'court_missing', court: 'Court 3', message: 'Court 3 is missing in this tournament' },
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
    );

    const fileInput = container.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [new File(['dummy'], 'backup.zip')] } });
    });

    expect(await screen.findByText('Mode will change to individual only')).toBeInTheDocument();
    expect(screen.getByText('Court 3 is missing in this tournament')).toBeInTheDocument();
    expect(screen.getByText(/return to the backup's state/i)).toBeInTheDocument();
    expect(screen.getByText(/2 recorded results in "Open Individual" will be lost or changed/i)).toBeInTheDocument();
    expect(screen.getByText(/Let every court device sync before you restore/i)).toBeInTheDocument();
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
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
      'test-pw',
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
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
      'test-pw',
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" onLogout={onLogout} />
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
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
        password="test-pw"
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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
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
      <AdminBackupPage tournament={defaultTournament} password="session-pw" />
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
        'session-pw'
      );
    });
    expect(mockApi.planRestore.mock.calls[0][0].undo).not.toBe(true);

    // Asked choices render with nothing selected
    const radio = await screen.findByLabelText(/Replace "Open Individual"/i);
    expect(radio).toBeInTheDocument();
    expect(radio).not.toBeChecked();

    // Main restore button is disabled (names open choice)
    const restoreBtn = screen.getByRole('button', { name: /Choose what to do with "Open Individual"/i });
    expect(restoreBtn).toBeDisabled();

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
      <AdminBackupPage tournament={defaultTournament} password="test-pw" />
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
        password="session-pw"
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
      'session-pw',
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
        'session-pw'
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
      'session-pw',
      'correct-pw'
    );
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
        password="session-pw"
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
        password="test-pw"
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

  it('shows "Show all competitions" button after filtering to one competition, and clicking it restores full plan without only key', async () => {
    const multiPlan = {
      scope: 'tournament',
      competitions: [
        { id: 'c1', name: 'Open Individual', asked: true, choices: ['replace', 'copy'] },
        { id: 'c2', name: 'Women Individual', asked: true, choices: ['replace', 'copy'] },
      ],
    };
    const singlePlan = {
      scope: 'tournament',
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
        password="session-pw"
      />
    );

    const fileInput = container.querySelector('input[type="file"]');
    const fakeFile = new File(['fake-zip'], 'backup.zip', { type: 'application/zip' });
    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [fakeFile] } });
    });

    const onlyBtn = await screen.findByRole('button', { name: 'Restore only "Open Individual"' });
    expect(onlyBtn).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Show all competitions' })).toBeNull();

    // Click "Restore only this one"
    await act(async () => {
      fireEvent.click(onlyBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledWith(
        expect.objectContaining({
          file: fakeFile,
          only: 'c1',
        }),
        'session-pw'
      );
    });

    // "Show all competitions" button appears
    const showAllBtn = await screen.findByRole('button', { name: 'Show all competitions' });
    expect(showAllBtn).toBeInTheDocument();

    // Click "Show all competitions"
    await act(async () => {
      fireEvent.click(showAllBtn);
    });

    await waitFor(() => {
      expect(mockApi.planRestore).toHaveBeenCalledTimes(3);
    });

    const lastCallArg = mockApi.planRestore.mock.calls[2][0];
    expect(lastCallArg.file).toBe(fakeFile);
    expect(lastCallArg.only).toBeUndefined();
    expect(mockApi.planRestore.mock.calls[2][1]).toBe('session-pw');

    // Button is gone once full plan is back
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Show all competitions' })).toBeNull();
    });
    expect(screen.getByRole('button', { name: 'Restore only "Open Individual"' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Restore only "Women Individual"' })).toBeInTheDocument();
  });

  it.each([
    ['tournament', { name: 'Back up the whole tournament' }, 'backupTournament', ['test-pw'], 'spring-taikai-20261104T120000Z.zip', 'Tournament backup downloaded'],
    ['competition', { name: 'Back up' }, 'backupCompetition', ['c1', 'test-pw'], 'spring-taikai-c1-20261104T120000Z.zip', 'Backup for "Open Individual" downloaded'],
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
      render(<AdminBackupPage tournament={defaultTournament} password="test-pw" showToast={showToast} />);
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
});
