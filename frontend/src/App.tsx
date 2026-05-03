import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import Login from './pages/Login';
import Protected from './pages/Protected';
import Layout from './pages/Layout';
import AgentsList from './pages/AgentsList';
import AgentDetailPage from './pages/AgentDetailPage';
import EndpointDetailPage from './pages/EndpointDetailPage';
import Settings from './pages/Settings';
import NotFound from './pages/NotFound';

export default function App() {
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
