import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import Login from './pages/Login';
import Protected from './pages/Protected';
import Layout from './pages/Layout';
import AgentsList from './pages/AgentsList';
import AgentDetailPage from './pages/AgentDetailPage';
import Settings from './pages/Settings';

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
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<Navigate to="/agents" replace />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
