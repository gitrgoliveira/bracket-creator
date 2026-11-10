import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { PauseList, MAX_PAUSES } from '../../pause_list.jsx';

describe('PauseList component', () => {
  it('renders empty state: intro text and Add button, no rows', () => {
    render(<PauseList pauses={[]} onChange={vi.fn()} durationDays={1} />);

    expect(screen.getByText(/Add a pause for lunch, an opening or closing ceremony/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '+ Add a pause' })).toBeInTheDocument();
    expect(screen.queryByText(/Matches stop/)).toBeNull();
    expect(screen.queryByRole('button', { name: /Remove/ })).toBeNull();
  });

  it('renders several pauses with their fields and computed lines', () => {
    const pauses = [
      { name: 'Opening ceremony', start: '09:30', length: '30m', day: 1 },
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
    ];
    render(
      <PauseList
        pauses={pauses}
        onChange={vi.fn()}
        durationDays={2}
        tournamentDays={['Sat 16 May', 'Sun 17 May']}
      />
    );

    expect(screen.getByDisplayValue('Opening ceremony')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Lunch')).toBeInTheDocument();
    expect(screen.getByText('Matches stop 09:30 to 10:00.')).toBeInTheDocument();
    expect(screen.getByText('Matches stop 12:00 to 13:00.')).toBeInTheDocument();
  });

  it('renders incomplete row status: "Matches will not stop for this pause: give it a start time."', () => {
    const pauses = [
      { name: 'Closing ceremony', start: '', length: '30m', day: 0 },
    ];
    render(<PauseList pauses={pauses} onChange={vi.fn()} durationDays={1} />);

    expect(screen.getByText('Matches will not stop for this pause: give it a start time.')).toBeInTheDocument();
  });

  it('renders overlapping rows status with merged window and other pause name', () => {
    const pauses = [
      { name: 'Lunch', start: '12:30', length: '1h', day: 2 },
      { name: 'Ceremony', start: '13:00', length: '45m', day: 2 },
    ];
    render(<PauseList pauses={pauses} onChange={vi.fn()} durationDays={2} />);

    expect(screen.getByText('Together with Ceremony: matches stop 12:30 to 13:45 on Day 2.')).toBeInTheDocument();
    expect(screen.getByText('Together with Lunch: matches stop 12:30 to 13:45 on Day 2.')).toBeInTheDocument();
  });

  it('hides the "On" select on a one-day tournament', () => {
    const pauses = [
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
    ];
    render(<PauseList pauses={pauses} onChange={vi.fn()} durationDays={1} />);

    expect(screen.queryByLabelText('On')).toBeNull();
  });

  it('shows the "On" select on a one-day tournament for a pause left on a day it no longer has', () => {
    const pauses = [
      { name: 'Grading', start: '14:00', length: '1h', day: 2 },
    ];
    render(<PauseList pauses={pauses} onChange={vi.fn()} durationDays={1} />);

    expect(screen.getByLabelText('On')).toHaveValue('2');
    expect(screen.getByRole('option', { name: 'Every day' })).toBeInTheDocument();
  });

  it('shows the "On" select on a multi-day tournament', () => {
    const pauses = [
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
    ];
    render(<PauseList pauses={pauses} onChange={vi.fn()} durationDays={2} />);

    expect(screen.getByLabelText('On')).toBeInTheDocument();
  });

  it('calls onChange with the row removed when Remove button is clicked', () => {
    const pauses = [
      { name: 'Opening', start: '09:00', length: '30m', day: 0 },
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
    ];
    const onChange = vi.fn();
    render(<PauseList pauses={pauses} onChange={onChange} durationDays={1} />);

    const removeButtons = screen.getAllByRole('button', { name: /Remove/ });
    expect(removeButtons).toHaveLength(2);

    fireEvent.click(removeButtons[0]);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith([
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
    ]);
  });

  it('calls onChange with a new pause row when Add button is clicked', () => {
    const pauses = [{ name: 'Lunch', start: '12:00', length: '1h', day: 0 }];
    const onChange = vi.fn();
    render(<PauseList pauses={pauses} onChange={onChange} durationDays={1} />);

    const addBtn = screen.getByRole('button', { name: '+ Add a pause' });
    fireEvent.click(addBtn);

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith([
      { name: 'Lunch', start: '12:00', length: '1h', day: 0 },
      { name: '', start: '', length: '', day: 0 },
    ]);
  });

  it('disables the Add button and shows notice when reaching 20 pauses', () => {
    const pauses = Array.from({ length: MAX_PAUSES }, (_, i) => ({
      name: `Pause ${i + 1}`,
      start: '12:00',
      length: '30m',
      day: 0,
    }));
    const onChange = vi.fn();
    render(<PauseList pauses={pauses} onChange={onChange} durationDays={1} />);

    const addBtn = screen.getByRole('button', { name: '+ Add a pause' });
    expect(addBtn).toBeDisabled();
    expect(screen.getByText(/Maximum of 20 pauses reached/)).toBeInTheDocument();

    fireEvent.click(addBtn);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('displays warning when drawn competitions exist and list is changed', () => {
    const pauses = [{ name: 'Lunch', start: '12:00', length: '1h', day: 0 }];
    render(
      <PauseList
        pauses={pauses}
        onChange={vi.fn()}
        durationDays={1}
        drawnCompetitions={["Men's Individual", 'Women Cup']}
        changed={true}
      />
    );

    expect(screen.getByText(/Already drawn, keeps its times: Men's Individual, Women Cup\./)).toBeInTheDocument();
  });

  it('does not display warning when drawn competitions exist but list is unchanged', () => {
    const pauses = [{ name: 'Lunch', start: '12:00', length: '1h', day: 0 }];
    render(
      <PauseList
        pauses={pauses}
        onChange={vi.fn()}
        durationDays={1}
        drawnCompetitions={["Men's Individual", 'Women Cup']}
        changed={false}
      />
    );

    expect(screen.queryByText(/Already drawn, keeps its times/)).not.toBeInTheDocument();
  });

  it('ticks "Matches stop for this pause" for a pause saved without the field', () => {
    render(<PauseList pauses={[{ name: 'Lunch', start: '12:00', length: '1h', day: 0 }]} onChange={vi.fn()} durationDays={1} />);
    expect(screen.getByRole('checkbox', { name: 'Matches stop for this pause' })).toBeChecked();
  });

  it('unticking the box saves matchesGoOn, and the row says matches go on', () => {
    const onChange = vi.fn();
    const pauses = [{ name: 'Opening ceremony', start: '09:00', length: '30m', day: 0 }];
    const { rerender } = render(<PauseList pauses={pauses} onChange={onChange} durationDays={1} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Matches stop for this pause' }));
    expect(onChange).toHaveBeenCalledWith([{ ...pauses[0], matchesGoOn: true }]);

    rerender(<PauseList pauses={[{ ...pauses[0], matchesGoOn: true }]} onChange={onChange} durationDays={1} />);
    expect(screen.getByRole('checkbox', { name: 'Matches stop for this pause' })).not.toBeChecked();
    expect(screen.getByText('Matches go on through it, 09:00 to 09:30.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Matches stop for this pause' }));
    expect(onChange).toHaveBeenLastCalledWith([{ ...pauses[0], matchesGoOn: false }]);
  });
});
