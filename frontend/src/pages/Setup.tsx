import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { checkNeedsSetup, register } from '../api/client';

interface Props {
  onSetupComplete: () => void;
}

export default function Setup({ onSetupComplete }: Props) {
  const navigate = useNavigate();
  const [form, setForm] = useState({ username: '', email: '', password: '', confirm: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [checkingSetup, setCheckingSetup] = useState(true);

  useEffect(() => {
    let mounted = true;
    async function verifySetupState() {
      try {
        const resp = await checkNeedsSetup();
        if (mounted && !resp.data.needs_setup) {
          navigate('/login', { replace: true });
          return;
        }
      } catch {
        // Keep user on page if check fails; submit path is still backend-protected.
      } finally {
        if (mounted) setCheckingSetup(false);
      }
    }
    verifySetupState();
    return () => {
      mounted = false;
    };
  }, [navigate]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError('');

    if (form.password !== form.confirm) {
      setError('Passwords do not match');
      return;
    }
    if (form.password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }

    setLoading(true);
    try {
      await register({ username: form.username, email: form.email, password: form.password });
      onSetupComplete();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Setup failed');
    } finally {
      setLoading(false);
    }
  }

  if (checkingSetup) {
    return <div className="loading">Loading…</div>;
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <h1>Agent Native PM</h1>
        <p className="login-subtitle">Create your admin account to get started</p>
        <div style={{ background: 'var(--surface-hover)', borderRadius: '6px', padding: '0.75rem 1rem', marginBottom: '1.25rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
          No accounts exist yet. The first account will be the administrator.
        </div>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label>Username</label>
            <input
              type="text"
              value={form.username}
              onChange={e => setForm({ ...form, username: e.target.value })}
              required
              autoFocus
              placeholder="admin"
            />
          </div>
          <div className="form-group">
            <label>Email</label>
            <input
              type="email"
              value={form.email}
              onChange={e => setForm({ ...form, email: e.target.value })}
              required
              placeholder="you@example.com"
            />
          </div>
          <div className="form-group">
            <label>Password</label>
            <input
              type="password"
              value={form.password}
              onChange={e => setForm({ ...form, password: e.target.value })}
              required
              placeholder="At least 8 characters"
            />
          </div>
          <div className="form-group">
            <label>Confirm Password</label>
            <input
              type="password"
              value={form.confirm}
              onChange={e => setForm({ ...form, confirm: e.target.value })}
              required
            />
          </div>
          {error && <div className="error-banner">{error}</div>}
          <button type="submit" disabled={loading} className="btn btn-primary btn-full">
            {loading ? 'Creating account…' : 'Create Admin Account'}
          </button>
        </form>
      </div>
    </div>
  );
}
