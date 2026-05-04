import { useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { getSetupStatus } from './api/client';
import Login from './pages/Login';
import Setup from './pages/Setup';
import Protected from './pages/Protected';
import Layout from './pages/Layout';
import AgentsList from './pages/AgentsList';
import AgentDetailPage from './pages/AgentDetailPage';
import EndpointDetailPage from './pages/EndpointDetailPage';
import Settings from './pages/Settings';
import NotFound from './pages/NotFound';

export default function App() {
  const [setupNeeded, setSetupNeeded] = useState<boolean | null>(null);

  useEffect(() => {
    getSetupStatus().then(s => setSetupNeeded(s.setup_needed));
  }, []);

  if (setupNeeded === null) return null;

  if (setupNeeded) {
    return (
      <Setup onComplete={() => { window.location.href = '/login'; }} />
    );
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />

        <Route element={<Protected />}>
          <Route element={<Layout />}>
            <Route index element={<Navigate to="/agents" replace />} />
            <Route path="/agents" element={<AgentsList />} />
            <Route path="/agents/:id" element={<Navigate to="overview" replace />} />
            <Route path="/agents/:id/:tab" element={<AgentDetailPage />} />
            <Route path="/agents/:id/endpoints/:eid" element={<EndpointDetailPage />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
