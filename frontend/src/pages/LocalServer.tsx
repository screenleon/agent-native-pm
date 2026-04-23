import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { getMeta, listLocalConnectors, getConnectorRunStats } from '../api/client';
import type { ConnectorRunStats } from '../api/client';
import type { LocalConnector } from '../types';

type ServerMeta = {
  local_mode: boolean;
  project_id: string;
  project_name: string;
  port: string;
  version: string;
  db_type: string;
  db_path: string;
  db_size_bytes: number;
  started_at: string;
};

const LIVENESS_WINDOW_MS = 90_000;

function isLive(c: LocalConnector): boolean {
  if (c.status !== 'online') return false;
  if (!c.last_seen_at) return false;
  return Date.now() - new Date(c.last_seen_at).getTime() < LIVENESS_WINDOW_MS;
}

function adapterLabel(c: LocalConnector): string {
  const cap = c.capabilities as Record<string, unknown>;
  return typeof cap?.adapter === 'string' ? cap.adapter : '';
}

function formatBytes(bytes: number): string {
  if (!bytes) return '—';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatUptime(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  const h = Math.floor(ms / 3_600_000);
  const m = Math.floor((ms % 3_600_000) / 60_000);
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return 'just started';
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  function handleCopy() {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }
  return (
    <button className="btn btn-ghost btn-sm" onClick={handleCopy}>
      {copied ? 'Copied!' : 'Copy'}
    </button>
  );
}

export default function LocalServer() {
  const [meta, setMeta] = useState<ServerMeta | null>(null);
  const [connectors, setConnectors] = useState<LocalConnector[]>([]);
  const [runStats, setRunStats] = useState<ConnectorRunStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    void load();
    timerRef.current = setInterval(() => void loadConnectors(), 15_000);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, []);

  async function load() {
    setLoading(true);
    try {
      const [metaResp, connResp, statsResp] = await Promise.allSettled([
        getMeta(),
        listLocalConnectors(),
        getConnectorRunStats(),
      ]);
      if (metaResp.status === 'fulfilled') setMeta(metaResp.value.data as ServerMeta);
      if (connResp.status === 'fulfilled') setConnectors(connResp.value.data);
      if (statsResp.status === 'fulfilled') setRunStats(statsResp.value.data);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load server info');
    } finally {
      setLoading(false);
    }
  }

  async function loadConnectors() {
    try {
      const resp = await listLocalConnectors();
      setConnectors(resp.data);
    } catch { /* non-critical background refresh */ }
  }

  const activeConnectors = connectors.filter(c => c.status !== 'revoked');
  const liveConnector = activeConnectors.find(isLive) ?? null;
  const serveCmd = './bin/anpm-connector serve';

  if (loading) return <div className="loading">Loading…</div>;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1>Local Server</h1>
          {meta && (
            <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
              Port {meta.port}
              {meta.version && meta.version !== 'dev' && <> &middot; {meta.version}</>}
              {meta.started_at && <> &middot; Running {formatUptime(meta.started_at)}</>}
            </p>
          )}
        </div>
      </div>

      {error && <div className="error-banner">{error}</div>}

      {/* ── My Projects ── */}
      <div className="card">
        <h2>My Projects</h2>
        {meta ? (
          <div style={{ marginTop: '0.75rem' }}>
            <div className="card" style={{ margin: 0, display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <strong>{meta.project_name}</strong>
                  <span className="badge badge-low" style={{ fontSize: '0.72rem' }}>active</span>
                </div>
                <div style={{ marginTop: '0.3rem', color: 'var(--text-muted)', fontSize: '0.84rem' }}>
                  Port {meta.port}
                  {meta.db_type && <> &middot; {meta.db_type === 'sqlite' ? 'SQLite' : 'PostgreSQL'}</>}
                  {meta.db_path && <> &middot; <code>{meta.db_path}</code></>}
                </div>
              </div>
              <Link to={`/projects/${meta.project_id}`} className="btn btn-primary btn-sm" style={{ whiteSpace: 'nowrap' }}>
                Open Project →
              </Link>
            </div>
            <button
              className="btn btn-ghost btn-sm"
              style={{ marginTop: '0.75rem', opacity: 0.45, cursor: 'not-allowed' }}
              disabled
              title="Multi-project support coming in a future release"
            >
              + Add another project
            </button>
          </div>
        ) : (
          <p style={{ color: 'var(--text-muted)' }}>No project info available.</p>
        )}
      </div>

      {/* ── Server Status ── */}
      {meta && (
        <div className="card">
          <h2>Server Status</h2>
          <table style={{ borderCollapse: 'collapse', marginTop: '0.5rem', fontSize: '0.9rem', width: '100%', maxWidth: '28rem' }}>
            <tbody>
              <tr>
                <td style={{ padding: '0.3rem 1.25rem 0.3rem 0', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Port</td>
                <td><strong>{meta.port}</strong></td>
              </tr>
              {meta.version && (
                <tr>
                  <td style={{ padding: '0.3rem 1.25rem 0.3rem 0', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Version</td>
                  <td>{meta.version}</td>
                </tr>
              )}
              <tr>
                <td style={{ padding: '0.3rem 1.25rem 0.3rem 0', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Database</td>
                <td>
                  {meta.db_type === 'sqlite' ? 'SQLite' : 'PostgreSQL'}
                  {meta.db_path && <> &middot; <code style={{ fontSize: '0.82rem' }}>{meta.db_path}</code></>}
                  {meta.db_size_bytes > 0 && <> &middot; {formatBytes(meta.db_size_bytes)}</>}
                </td>
              </tr>
              {meta.started_at && (
                <tr>
                  <td style={{ padding: '0.3rem 1.25rem 0.3rem 0', color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>Started</td>
                  <td>
                    {new Date(meta.started_at).toLocaleString()}
                    <span style={{ color: 'var(--text-muted)', marginLeft: '0.5rem', fontSize: '0.85rem' }}>
                      (running {formatUptime(meta.started_at)})
                    </span>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* ── Connector ── */}
      <div className="card">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem', flexWrap: 'wrap' }}>
          <h2 style={{ margin: 0 }}>Connector</h2>
          <Link to="/settings/connector" className="btn btn-ghost btn-sm">Manage Connector →</Link>
        </div>

        {liveConnector ? (
          <div style={{ marginTop: '0.75rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap' }}>
              <span className="connector-badge connector-badge-ready">● Ready for planning</span>
              <strong>{liveConnector.label || 'Unnamed Connector'}</strong>
            </div>
            <div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              Last seen: {liveConnector.last_seen_at ? new Date(liveConnector.last_seen_at).toLocaleString() : 'Never'}
              {adapterLabel(liveConnector) && <> &middot; <code>{adapterLabel(liveConnector)}</code> adapter</>}
            </div>
          </div>
        ) : activeConnectors.length > 0 ? (
          <div style={{ marginTop: '0.75rem' }}>
            <span className="connector-badge connector-badge-offline">○ Offline</span>
            <p style={{ margin: '0.6rem 0 0.4rem', color: 'var(--text-muted)', fontSize: '0.9rem' }}>
              Start the connector to enable local planning runs:
            </p>
            <div className="connector-serve-block" style={{ marginTop: 0 }}>
              <div className="connector-serve-header">
                <span className="connector-serve-label">Start connector</span>
                <CopyButton text={serveCmd} />
              </div>
              <pre className="connector-serve-pre">{serveCmd}</pre>
            </div>
          </div>
        ) : (
          <div style={{ marginTop: '0.75rem' }}>
            <p style={{ margin: '0 0 0.75rem', color: 'var(--text-muted)', fontSize: '0.9rem' }}>
              No connector paired yet. Pair this machine to run planning jobs locally.
            </p>
            <div className="connector-serve-block">
              <div className="connector-serve-header">
                <span className="connector-serve-label">After pairing, start with</span>
                <CopyButton text={serveCmd} />
              </div>
              <pre className="connector-serve-pre">{serveCmd}</pre>
            </div>
            <div style={{ marginTop: '0.75rem' }}>
              <Link to="/settings/connector" className="btn btn-primary btn-sm">Set up Connector →</Link>
            </div>
          </div>
        )}
      </div>

      {/* ── Planning Activity ── */}
      {runStats && (
        <div className="card run-stats-card">
          <h2>Planning Activity</h2>
          <div className="run-stats-grid">
            <div className="run-stat-item">
              <span className="run-stat-value">{runStats.runs_today}</span>
              <span className="run-stat-label">Today</span>
            </div>
            <div className="run-stat-item">
              <span className="run-stat-value">{runStats.runs_week}</span>
              <span className="run-stat-label">Last 7 days</span>
            </div>
            <div className="run-stat-item">
              <span className="run-stat-value">{runStats.runs_month}</span>
              <span className="run-stat-label">Last 30 days</span>
            </div>
            <div className="run-stat-item">
              <span className="run-stat-value">{runStats.runs_total}</span>
              <span className="run-stat-label">All time</span>
            </div>
          </div>
        </div>
      )}

      {/* ── Settings quick-links ── */}
      <div className="card">
        <h2>Settings</h2>
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', marginTop: '0.5rem' }}>
          <Link to="/settings/account-bindings" className="btn btn-ghost">Model Providers</Link>
          <Link to="/settings/connector" className="btn btn-ghost">Local Connector</Link>
          <Link to="/settings/models" className="btn btn-ghost">Model Settings</Link>
          <Link to="/settings/api-keys" className="btn btn-ghost">API Keys</Link>
        </div>
      </div>
    </div>
  );
}
