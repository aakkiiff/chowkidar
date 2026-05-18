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
    expect(screen.getByLabelText(/^password$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/confirm password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create admin/i })).toBeInTheDocument();
  });

  it('shows error when passwords do not match', async () => {
    renderSetup();
    // Mismatch is enforced client-side via button-disabled; force submit by
    // typing matching valid pw, then breaking the confirm field and submitting
    // via the form's submit event so we hit the handler validation branch.
    const pw = screen.getByLabelText(/^password$/i);
    const cf = screen.getByLabelText(/confirm password/i);
    fireEvent.change(pw, { target: { value: 'strongpassword1' } });
    fireEvent.change(cf, { target: { value: 'strongpassword1' } });
    fireEvent.change(cf, { target: { value: 'different12345' } });
    // Button is disabled when mismatched; submit the form directly.
    fireEvent.submit(pw.closest('form')!);
    await waitFor(() => {
      expect(screen.getByText(/passwords do not match/i)).toBeInTheDocument();
    });
    expect(client.setupAdmin).not.toHaveBeenCalled();
  });

  it('shows error when password is too short', async () => {
    renderSetup();
    const pw = screen.getByLabelText(/^password$/i);
    const cf = screen.getByLabelText(/confirm password/i);
    fireEvent.change(pw, { target: { value: 'short' } });
    fireEvent.change(cf, { target: { value: 'short' } });
    fireEvent.submit(pw.closest('form')!);
    await waitFor(() => {
      expect(screen.getByText(/at least 12 characters/i)).toBeInTheDocument();
    });
    expect(client.setupAdmin).not.toHaveBeenCalled();
  });

  it('calls setupAdmin and onComplete on success', async () => {
    (client.setupAdmin as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    const onComplete = vi.fn();
    renderSetup(onComplete);

    fireEvent.change(screen.getByLabelText(/^password$/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByLabelText(/confirm password/i), { target: { value: 'strongpassword1' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));

    await waitFor(() => {
      expect(client.setupAdmin).toHaveBeenCalledWith('strongpassword1');
      expect(onComplete).toHaveBeenCalled();
    });
  });

  it('shows server error message on failure', async () => {
    (client.setupAdmin as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('setup already completed'));
    renderSetup();

    fireEvent.change(screen.getByLabelText(/^password$/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByLabelText(/confirm password/i), { target: { value: 'strongpassword1' } });
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

    fireEvent.change(screen.getByLabelText(/^password$/i), { target: { value: 'strongpassword1' } });
    fireEvent.change(screen.getByLabelText(/confirm password/i), { target: { value: 'strongpassword1' } });
    fireEvent.click(screen.getByRole('button', { name: /create admin/i }));

    await waitFor(() => {
      // Submit button is disabled when loading. Use name match to avoid the
      // show/hide password toggle button.
      expect(screen.getByRole('button', { name: /creating account/i })).toBeDisabled();
    });

    resolve!();
  });
});
