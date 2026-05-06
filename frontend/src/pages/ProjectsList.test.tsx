import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Project } from '../api/client';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual('../api/client') as Record<string, unknown>;
  return {
    ...actual,
    listProjects: vi.fn(),
    createProject: vi.fn(),
  };
});

import * as client from '../api/client';
import ProjectsList from './ProjectsList';

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom') as Record<string, unknown>;
  return {
    ...actual,
    useOutletContext: vi.fn().mockReturnValue({
      token: 'test-token',
      role: 'admin',
      onLogout: vi.fn(),
    }),
  };
});

function renderProjects() {
  return render(<MemoryRouter><ProjectsList /></MemoryRouter>);
}

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    name: 'p1',
    environment: '',
    created_at: '2026-05-06T00:00:00Z',
    agent_count: 0,
    ...overrides,
  };
}

describe('ProjectsList page', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('shows empty state when no projects', async () => {
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProjects();
    await waitFor(() => {
      expect(screen.getByText(/No projects yet/i)).toBeInTheDocument();
    });
  });

  it('renders project cards with name + agent count', async () => {
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([
      makeProject({ name: 'backend', environment: 'prod', agent_count: 3 }),
    ]);
    renderProjects();
    await waitFor(() => {
      expect(screen.getByText('backend')).toBeInTheDocument();
      expect(screen.getByText('prod')).toBeInTheDocument();
      expect(screen.getByText(/3 agents/i)).toBeInTheDocument();
    });
  });

  it('opens create modal on button click', async () => {
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProjects();
    await waitFor(() => screen.getByRole('button', { name: /new project/i }));

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    expect(screen.getByText('New Project')).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/backend-services/i)).toBeInTheDocument();
  });

  it('calls createProject on form submit', async () => {
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    (client.createProject as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeProject({ name: 'new-proj' })
    );
    renderProjects();
    await waitFor(() => screen.getByRole('button', { name: /new project/i }));

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    fireEvent.change(screen.getByPlaceholderText(/backend-services/i), {
      target: { value: 'new-proj' },
    });
    fireEvent.change(screen.getByPlaceholderText(/prod, staging, dev/i), {
      target: { value: 'prod' },
    });
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(client.createProject).toHaveBeenCalledWith('new-proj', 'prod');
    });
  });

  it('shows server error in modal on create failure', async () => {
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    (client.createProject as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('project already exists')
    );
    renderProjects();
    await waitFor(() => screen.getByRole('button', { name: /new project/i }));

    fireEvent.click(screen.getByRole('button', { name: /new project/i }));
    fireEvent.change(screen.getByPlaceholderText(/backend-services/i), {
      target: { value: 'dup' },
    });
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }));

    await waitFor(() => {
      expect(screen.getByText('project already exists')).toBeInTheDocument();
    });
  });

  it('hides New Project button for non-admin users', async () => {
    const router = await import('react-router-dom');
    (router.useOutletContext as ReturnType<typeof vi.fn>).mockReturnValue({
      token: 'test', role: 'developer', onLogout: vi.fn(),
    });
    (client.listProjects as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProjects();
    await waitFor(() => screen.getByText(/No projects available/i));
    expect(screen.queryByRole('button', { name: /new project/i })).toBeNull();

    // Reset for other tests
    (router.useOutletContext as ReturnType<typeof vi.fn>).mockReturnValue({
      token: 'test-token', role: 'admin', onLogout: vi.fn(),
    });
  });
});
