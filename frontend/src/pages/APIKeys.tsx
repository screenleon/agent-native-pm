import { useState, useEffect } from 'react';
import { listAPIKeys, createAPIKey, revokeAPIKey } from '../api/client';
import type { APIKey, APIKeyWithSecret } from '../types';

export default function APIKeys() {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [newLabel, setNewLabel] = useState('');
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<APIKeyWithSecret | null>(null);

  async function loadKeys() {
    try {
      const resp = await listAPIKeys();
      setKeys(resp.data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load API keys');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { loadKeys(); }, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!newLabel.trim()) return;
    setCreating(true);
    try {
      const resp = await createAPIKey({ label: newLabel.trim() });
      setNewKey(resp.data);
      setNewLabel('');
      await loadKeys();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to create API key');
    } finally {
      setCreating(false);
    }
  }

  async function handleRevoke(id: string) {
    if (!confirm('Revoke this API key? This cannot be undone.')) return;
    try {
      await revokeAPIKey(id);
      setKeys(prev => prev.filter(k => k.id !== id));
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to revoke API key');
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <h1>API Keys</h1>
      </div>

      {error && <div className="error-banner">{error}</div>}

      {newKey && (
        <div className="alert alert-success">
          <strong>API Key created.</strong> Copy it now — it won't be shown again.
          <div className="apikey-secret">{newKey.key}</div>
          <button className="btn btn-sm" onClick={() => setNewKey(null)}>Dismiss</button>
        </div>
      )}

      <div className="card">
        <h2>Create New Key</h2>
        <form onSubmit={handleCreate} className="inline-form">
          <input
            type="text"
            placeholder="Key label (e.g. CI pipeline)"
            value={newLabel}
            onChange={e => setNewLabel(e.target.value)}
            required
          />
          <button type="submit" disabled={creating} className="btn btn-primary">
            {creating ? 'Creating…' : 'Create'}
          </button>
        </form>
      </div>

      <div className="card">
        <h2>Active Keys</h2>
        {loading ? (
          <div className="loading">Loading…</div>
        ) : keys.length === 0 ? (
          <div className="empty-state">No API keys yet.</div>
        ) : (
          <table className="data-table">
            <thead>
              <tr>
                <th>Label</th>
                <th>Last Used</th>
                <th>Created</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {keys.map(key => (
                <tr key={key.id} className={key.is_active ? '' : 'row-inactive'}>
                  <td>{key.label}</td>
                  <td>{key.last_used_at ? new Date(key.last_used_at).toLocaleString() : '—'}</td>
                  <td>{new Date(key.created_at).toLocaleDateString()}</td>
                  <td>
                    {key.is_active && (
                      <button
                        className="btn btn-sm btn-danger"
                        onClick={() => handleRevoke(key.id)}
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
