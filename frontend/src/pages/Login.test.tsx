import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Login from './Login';

// Mock the client module
vi.mock('../api/client', async () => {
  const actual = await vi.importActual('../api/client') as Record<string, unknown>;
  return {
    ...actual,
    login: vi.fn(),
    getToken: vi.fn().mockReturnValue(null),
    saveSession: vi.fn(),
  };
});

import * as client from '../api/client';

function renderLogin() {
  return render(
    <MemoryRouter>
      <Login />
    </MemoryRouter>
  );
}

describe('Login page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (client.getToken as ReturnType<typeof vi.fn>).mockReturnValue(null);
  });

  it('renders username and password fields and submit button', () => {
    renderLogin();
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/^password$/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
  });

  it('shows error message on failed login', async () => {
    (client.login as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('invalid credentials'));
    renderLogin();

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText(/^password$/i), { target: { value: 'wrong' } });
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(screen.getByText('invalid credentials')).toBeInTheDocument();
    });
  });

  it('calls saveSession with username and role on success', async () => {
    (client.login as ReturnType<typeof vi.fn>).mockResolvedValue({
      username: 'admin',
      role: 'admin',
    });
    renderLogin();

    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'admin' } });
    fireEvent.change(screen.getByLabelText(/^password$/i), { target: { value: 'adminpass' } });
    fireEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => {
      expect(client.saveSession).toHaveBeenCalledWith('admin', 'admin');
    });
  });
});
