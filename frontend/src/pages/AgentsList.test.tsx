import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { Agent } from '../api/client';

// Mock the client module
vi.mock('../api/client', async () => {
  const actual = await vi.importActual('../api/client') as Record<string, unknown>;
  return {
    ...actual,
    listAgents: vi.fn(),
    registerAgent: vi.fn(),
  };
});

import * as client from '../api/client';
import AgentsList from './AgentsList';

// Mock react-router-dom outlet context
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

function renderAgentsList() {
  return render(
    <MemoryRouter>
      <AgentsList />
    </MemoryRouter>
  );
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'agent-1',
    hostname: 'test-host',
    last_seen: null,
    cpu_percent: null,
    mem_used_gb: null,
    mem_total_gb: null,
    disk_used_gb: null,
    disk_total_gb: null,
    container_count: 0,
    alerts_enabled: false,
    active_issues: 0,
    ...overrides,
  };
}

describe('AgentsList page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders empty state when agent list is empty', async () => {
    (client.listAgents as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    renderAgentsList();

    await waitFor(() => {
      expect(screen.getByText(/No agents registered/i)).toBeInTheDocument();
    });
  });

  it('renders agent card when agents are returned', async () => {
    (client.listAgents as ReturnType<typeof vi.fn>).mockResolvedValue([
      makeAgent({ hostname: 'prod-server-01' }),
    ]);
    renderAgentsList();

    await waitFor(() => {
      expect(screen.getByText('prod-server-01')).toBeInTheDocument();
    });
  });

  it('shows issue badge when active_issues > 0', async () => {
    (client.listAgents as ReturnType<typeof vi.fn>).mockResolvedValue([
      makeAgent({ hostname: 'broken-host', active_issues: 3 }),
    ]);
    renderAgentsList();

    await waitFor(() => {
      expect(screen.getByText('3')).toBeInTheDocument();
    });
  });

  it('does not show issue badge when active_issues is 0', async () => {
    (client.listAgents as ReturnType<typeof vi.fn>).mockResolvedValue([
      makeAgent({ hostname: 'healthy-host', active_issues: 0 }),
    ]);
    renderAgentsList();

    await waitFor(() => {
      expect(screen.getByText('healthy-host')).toBeInTheDocument();
    });
    // No badge element with a number should exist
    const badges = screen.queryAllByText(/^\d+$/);
    expect(badges).toHaveLength(0);
  });
});
