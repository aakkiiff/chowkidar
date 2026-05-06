import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { Project, Agent } from '../api/client';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual('../api/client') as Record<string, unknown>;
  return {
    ...actual,
    getProject: vi.fn(),
    updateProject: vi.fn(),
    deleteProject: vi.fn(),
    listProjectAgents: vi.fn(),
    listAgents: vi.fn(),
    registerAgent: vi.fn(),
  };
});

import * as client from '../api/client';
import ProjectDetail from './ProjectDetail';

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

function renderProject(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/projects/${id}`]}>
      <Routes>
        <Route path="/projects/:id" element={<ProjectDetail />} />
        <Route path="/projects" element={<div>Projects List</div>} />
      </Routes>
    </MemoryRouter>
  );
}

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    name: 'backend',
    environment: 'prod',
    created_at: '2026-05-06T00:00:00Z',
    agent_count: 0,
    ...overrides,
  };
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'agent-1', hostname: 'host-1', last_seen: null,
    cpu_percent: null, mem_used_gb: null, mem_total_gb: null,
    disk_used_gb: null, disk_total_gb: null,
    container_count: 0, alerts_enabled: false, active_issues: 0,
    project_id: 1, project_name: 'backend', project_environment: 'prod',
    ...overrides,
  };
}

describe('ProjectDetail page', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('renders project name and environment in header', async () => {
    (client.getProject as ReturnType<typeof vi.fn>).mockResolvedValue(
      makeProject({ name: 'backend', environment: 'prod' })
    );
    (client.listProjectAgents as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProject('1');

    await waitFor(() => {
      expect(screen.getByText('backend (prod)')).toBeInTheDocument();
    });
  });

  it('renders empty agents state when project has no agents', async () => {
    (client.getProject as ReturnType<typeof vi.fn>).mockResolvedValue(makeProject());
    (client.listProjectAgents as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProject('1');

    await waitFor(() => {
      expect(screen.getByText(/No agents registered/i)).toBeInTheDocument();
    });
  });

  it('renders agent cards from listProjectAgents (not listAgents)', async () => {
    (client.getProject as ReturnType<typeof vi.fn>).mockResolvedValue(makeProject());
    (client.listProjectAgents as ReturnType<typeof vi.fn>).mockResolvedValue([
      makeAgent({ hostname: 'scoped-host' }),
    ]);
    renderProject('1');

    await waitFor(() => {
      expect(screen.getByText('scoped-host')).toBeInTheDocument();
    });
    expect(client.listProjectAgents).toHaveBeenCalledWith(1);
    expect(client.listAgents).not.toHaveBeenCalled();
  });

  it('shows Add Agent button when admin and inside project context', async () => {
    (client.getProject as ReturnType<typeof vi.fn>).mockResolvedValue(makeProject());
    (client.listProjectAgents as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderProject('1');

    await waitFor(() => screen.getByText(/No agents registered/i));
    expect(screen.getByRole('button', { name: /add agent/i })).toBeInTheDocument();
  });
});
