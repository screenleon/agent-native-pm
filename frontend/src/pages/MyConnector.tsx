import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { createLocalConnectorPairingSession, getConnectorRunStats, listLocalConnectors, revokeLocalConnector } from '../api/client';
import type { ConnectorRunStats } from '../api/client';
import type { CreateLocalConnectorPairingSessionResponse, LocalConnector } from '../types';

const LIVENESS_WINDOW_MS = 90_000;

function isLive(connector: LocalConnector): boolean {
	if (connector.status !== 'online') return false;
	if (!connector.last_seen_at) return false;
	return Date.now() - new Date(connector.last_seen_at).getTime() < LIVENESS_WINDOW_MS;
}

function adapterReady(connector: LocalConnector): boolean {
	const cap = connector.capabilities as Record<string, unknown>;
	return typeof cap?.adapter === 'string' && cap.adapter.length > 0;
}

function connectorReadiness(connector: LocalConnector): 'ready' | 'stale' | 'offline' | 'revoked' {
	if (connector.status === 'revoked') return 'revoked';
	if (isLive(connector) && adapterReady(connector)) return 'ready';
	if (connector.status === 'online') return 'stale';
	return 'offline';
}

function ReadinessBadge({ state }: { state: ReturnType<typeof connectorReadiness> }) {
	const map: Record<typeof state, { label: string; cls: string }> = {
		ready:   { label: '● Ready for planning', cls: 'connector-badge connector-badge-ready' },
		stale:   { label: '◑ Online — adapter not confirmed', cls: 'connector-badge connector-badge-stale' },
		offline: { label: '○ Offline', cls: 'connector-badge connector-badge-offline' },
		revoked: { label: '✕ Revoked', cls: 'connector-badge connector-badge-revoked' },
	};
	const { label, cls } = map[state];
	return <span className={cls}>{label}</span>;
}

function ServeCommand() {
	const [copied, setCopied] = useState(false);
	const cmd = `./bin/anpm-connector serve`;

	function handleCopy() {
		void navigator.clipboard.writeText(cmd).then(() => {
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		});
	}

	return (
		<div className="connector-serve-block">
			<div className="connector-serve-header">
				<span className="connector-serve-label">Start connector</span>
				<button className="btn btn-ghost btn-sm" onClick={handleCopy}>{copied ? 'Copied!' : 'Copy'}</button>
			</div>
			<pre className="connector-serve-pre">{cmd}</pre>
			<div className="connector-serve-note">
				Run this in a terminal inside the project directory. Requires <code>claude</code> or <code>codex</code> on PATH.
				Select which CLI to use per planning run from the Planning Workspace.
			</div>
		</div>
	);
}

export default function MyConnector() {
	const [connectors, setConnectors] = useState<LocalConnector[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState('');
	const [success, setSuccess] = useState('');
	const [pairingLabel, setPairingLabel] = useState('My Machine');
	const [pairing, setPairing] = useState<CreateLocalConnectorPairingSessionResponse | null>(null);
	const [creating, setCreating] = useState(false);
	const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
	const [runStats, setRunStats] = useState<ConnectorRunStats | null>(null);

	const serverOrigin = useMemo(() => window.location.origin, []);

	const activeConnectors = connectors.filter(c => c.status !== 'revoked');
	const hasLiveConnector = activeConnectors.some(isLive);

	useEffect(() => {
		void load();
		void loadStats();
		// Auto-refresh while page is open; faster when no live connector
		function startTimer() {
			timerRef.current = setInterval(() => void load(), hasLiveConnector ? 15_000 : 30_000);
		}
		startTimer();
		return () => {
			if (timerRef.current) clearInterval(timerRef.current);
		};
	}, [hasLiveConnector]);

	async function load() {
		try {
			const resp = await listLocalConnectors();
			setConnectors(resp.data);
			setError('');
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to load connectors');
		} finally {
			setLoading(false);
		}
	}

	async function loadStats() {
		try {
			const resp = await getConnectorRunStats();
			setRunStats(resp.data);
		} catch { /* non-critical */ }
	}

	async function handleCreatePairingSession() {
		setCreating(true);
		setError('');
		setSuccess('');
		try {
			const resp = await createLocalConnectorPairingSession({ label: pairingLabel.trim() || 'My Machine' });
			setPairing(resp.data);
			setSuccess('Pairing code created. Run the connector CLI on your machine to claim it.');
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to create pairing session');
		} finally {
			setCreating(false);
		}
	}

	async function handleRevoke(connector: LocalConnector) {
		if (!confirm(`Revoke connector "${connector.label}"?`)) {
			return;
		}
		try {
			await revokeLocalConnector(connector.id);
			setSuccess('Connector revoked.');
			await load();
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to revoke connector');
		}
	}

	return (
		<div className="page">
			<div className="page-header">
				<div>
					<h1>My Connector</h1>
					<p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
						Use a paired local connector when you want planning runs to execute on your own machine instead of asking the server to call an API-compatible provider directly.
					</p>
					<p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
						My Connector runs the CLI on <strong>your</strong> machine, not on the server. For a server-side CLI binding, see <Link to="/settings/account-bindings">Account Bindings</Link>.
					</p>
					<p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
						One paired machine serves <strong>all of your projects</strong>. When planning runs are queued concurrently for different projects, they are processed one at a time on this device (oldest first). Pair additional machines to run more than one planning job in parallel.
					</p>
				</div>
			</div>

			{/* ── Live status banner ── */}
			{activeConnectors.length > 0 && (
				<div className={`connector-status-banner ${hasLiveConnector ? 'is-ready' : 'is-offline'}`}>
					{hasLiveConnector ? (
						<>
							<strong>Connector is online and ready.</strong> Planning runs using "Run on this machine" will be picked up automatically.
							<span className="connector-status-auto-refresh"> Auto-refreshing every 15 s.</span>
						</>
					) : (
						<>
							<strong>No live connector detected.</strong> Start the connector on this machine to enable local planning runs.
							<span className="connector-status-auto-refresh"> Auto-refreshing every 30 s.</span>
						</>
					)}
				</div>
			)}

			{error && <div className="error-banner">{error}</div>}
			{success && <div className="alert alert-success">{success}</div>}

			{/* ── Start connector instructions ── */}
			<div className="card">
				<h2>Start the Connector</h2>
				<p style={{ margin: '0.35rem 0 0.9rem', color: 'var(--text-muted)' }}>
					Run the following command in a terminal <strong>inside this project directory</strong>.
					The connector will stay running in the foreground and pick up planning jobs as they arrive.
					Select which CLI binding (Claude or Codex) to use per planning run from the Planning Workspace.
				</p>

				<ServeCommand />

				<details style={{ marginTop: '0.85rem' }}>
					<summary style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: '0.88rem' }}>
						First time? Pair this machine first
					</summary>
					<div style={{ marginTop: '0.75rem' }}>
						<p style={{ margin: '0 0 0.6rem', color: 'var(--text-muted)', fontSize: '0.9rem' }}>
							Create a pairing code below, then run this command to register the machine:
						</p>
						<pre style={{ margin: 0 }}>{`./bin/anpm-connector pair --server ${serverOrigin} --code <pairing-code>`}</pre>
					</div>
				</details>
			</div>

			{/* ── Run stats ── */}
			{runStats && (
				<div className="card run-stats-card">
					<h2>Planning Run Usage</h2>
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
					<div className="run-stats-links">
						<a href="https://claude.ai/settings/usage" target="_blank" rel="noopener noreferrer">View Claude usage →</a>
						<a href="https://platform.openai.com/usage" target="_blank" rel="noopener noreferrer">View OpenAI usage →</a>
					</div>
				</div>
			)}

			{/* ── Pair new machine ── */}
			<div className="card">
				<h2>Pair This Machine</h2>
				<p style={{ margin: '0.45rem 0 1rem', color: 'var(--text-muted)' }}>
					Create a short-lived pairing code, then claim it from your local connector CLI.
				</p>
				<div className="form-group" style={{ maxWidth: '28rem' }}>
					<label>Device Label</label>
					<input value={pairingLabel} onChange={event => setPairingLabel(event.target.value)} placeholder="My Machine" />
				</div>
				<div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', marginTop: '1rem' }}>
					<button className="btn btn-primary" onClick={handleCreatePairingSession} disabled={creating}>
						{creating ? 'Creating…' : 'Create Pairing Code'}
					</button>
					<button className="btn btn-ghost" onClick={() => void load()} disabled={loading}>
						Refresh Status
					</button>
				</div>

				{pairing && (
					<div className="helper-note" style={{ marginTop: '1rem' }}>
						<div><strong>Pairing code:</strong> {pairing.pairing_code}</div>
						<div style={{ marginTop: '0.35rem' }}><strong>Expires at:</strong> {new Date(pairing.session.expires_at).toLocaleString()}</div>
						<div style={{ marginTop: '0.75rem' }}>Run this on the machine that should execute your planning runs:</div>
						<pre style={{ marginTop: '0.5rem' }}>{`./bin/anpm-connector pair --server ${serverOrigin} --code ${pairing.pairing_code}`}</pre>
					</div>
				)}
			</div>

			{/* ── Registered connectors ── */}
			<div className="card">
				<h2>Registered Connectors</h2>
				{loading ? (
					<div className="loading">Loading…</div>
				) : connectors.length === 0 ? (
					<p>No connectors registered yet.</p>
				) : (
					<div style={{ display: 'grid', gap: '0.75rem' }}>
						{connectors.map(connector => {
							const state = connectorReadiness(connector);
							const cap = connector.capabilities as Record<string, unknown>;
							return (
								<div key={connector.id} className="card connector-detail-card" style={{ margin: 0 }}>
									<div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start' }}>
										<div style={{ flex: 1, minWidth: 0 }}>
											<div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap' }}>
												<strong>{connector.label || 'Unnamed Connector'}</strong>
												<ReadinessBadge state={state} />
											</div>
											<div style={{ marginTop: '0.35rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
												Last seen: {connector.last_seen_at ? new Date(connector.last_seen_at).toLocaleString() : 'Never'}
												{connector.platform && ` • ${connector.platform}`}
												{connector.client_version && ` • v${connector.client_version}`}
											</div>
											{typeof cap?.adapter === 'string' && (
												<div style={{ marginTop: '0.25rem', color: 'var(--text-muted)', fontSize: '0.83rem' }}>
													Adapter: <code>{cap.adapter}</code>
													{typeof cap.connector_version === 'string' && <> • connector <code>{cap.connector_version}</code></>}
												</div>
											)}
											{connector.last_error && (
												<div style={{ marginTop: '0.35rem', color: 'var(--danger)', fontSize: '0.85rem' }}>
													Last error: {connector.last_error}
												</div>
											)}
											{(state === 'offline' || state === 'stale') && connector.status !== 'revoked' && (
												<div style={{ marginTop: '0.6rem' }}>
													<details>
														<summary style={{ cursor: 'pointer', fontSize: '0.85rem', color: 'var(--accent, #4f46e5)' }}>
															Show start command
														</summary>
														<div style={{ marginTop: '0.5rem' }}>
															<ServeCommand />
														</div>
													</details>
												</div>
											)}
										</div>
										{connector.status !== 'revoked' && (
											<button className="btn btn-ghost btn-sm" onClick={() => void handleRevoke(connector)}>
												Revoke
											</button>
										)}
									</div>
								</div>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
