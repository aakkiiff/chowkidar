import { useNavigate } from 'react-router-dom';

export default function NotFound() {
  const navigate = useNavigate();
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '60vh', gap: 16 }}>
      <span style={{ fontSize: 64, opacity: 0.15, fontWeight: 700 }}>404</span>
      <p style={{ color: 'var(--text-muted)', margin: 0 }}>Page not found.</p>
      <button className="btn" onClick={() => navigate('/agents')}>Back to agents</button>
    </div>
  );
}
