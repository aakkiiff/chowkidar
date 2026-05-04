import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import Setup from './Setup';

vi.mock('../api/client', () => ({
  setupAdmin: vi.fn(),
}));

import * as client from '../api/client';

function renderSetup(onComplete = vi.fn()) {
  return render(<Setup onComplete={onComplete} />);
}

describe('Setup page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders password fields and submit button', () => {
    renderSetup();
    expect(screen.getByPlaceholderText(/password \(min/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/confirm password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create admin/i })).toBeInTheDocument();
  });

  it('shows error when passwords do not match', async () => {
    renderSetup();
    fireEvent.change(screen.getByPlaceholderText(/password \(min/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByPlaceholderText(/confirm password/i), { target: { value: 'different12345' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));
    await waitFor(() => {
      expect(screen.getByText(/passwords do not match/i)).toBeInTheDocument();
    });
    expect(client.setupAdmin).not.toHaveBeenCalled();
  });

  it('shows error when password is too short', async () => {
    renderSetup();
    fireEvent.change(screen.getByPlaceholderText(/password \(min/i), { target: { value: 'short' } });
    fireEvent.change(screen.getByPlaceholderText(/confirm password/i), { target: { value: 'short' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));
    await waitFor(() => {
      expect(screen.getByText(/at least 12 characters/i)).toBeInTheDocument();
    });
    expect(client.setupAdmin).not.toHaveBeenCalled();
  });

  it('calls setupAdmin and onComplete on success', async () => {
    (client.setupAdmin as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    const onComplete = vi.fn();
    renderSetup(onComplete);

    fireEvent.change(screen.getByPlaceholderText(/password \(min/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByPlaceholderText(/confirm password/i), { target: { value: 'strongpassword1' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));

    await waitFor(() => {
      expect(client.setupAdmin).toHaveBeenCalledWith('strongpassword1');
      expect(onComplete).toHaveBeenCalled();
    });
  });

  it('shows server error message on failure', async () => {
    (client.setupAdmin as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('setup already completed'));
    renderSetup();

    fireEvent.change(screen.getByPlaceholderText(/password \(min/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByPlaceholderText(/confirm password/i), { target: { value: 'strongpassword1' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));

    await waitFor(() => {
      expect(screen.getByText('setup already completed')).toBeInTheDocument();
    });
  });

  it('disables button while submitting', async () => {
    let resolve!: (v?: unknown) => void;
    (client.setupAdmin as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise(r => { resolve = r; })
    );
    renderSetup();

    fireEvent.change(screen.getByPlaceholderText(/password \(min/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByPlaceholderText(/confirm password/i), { target: { value: 'strongpassword1' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));

    await waitFor(() => {
      expect(screen.getByRole('button')).toBeDisabled();
    });

    resolve!();
  });
});
