import { Link } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { getMeta, listAccountBindings, updateAccountBinding, listLocalConnectors, listConnectorCliConfigs } from '../api/client';
import type { CliConfig } from '../api/client';
import type { AccountBinding, LocalConnector } from '../types';
import Jargon from '../components/Jargon';

// Landing page introduced in Phase 4 (P4-2). Redesigned in Phase 6a Part 2 UX
// to add a live setup banner (Direction A), simplified card titles (Direction B),
// live connector status in Option C (Direction C), and conditional "Still unsure?"
// guidance (Direction D).
// Tri-state: 'unknown' while getMeta() is in-flight or if it failed (so a
// transient network blip does not wrongly downgrade Option B to disabled),
// true if the server reports local mode, false otherwise. Copilot #8/#10.
type LocalModeState = 'unknown' | 'local' | 'server';
type SetupStatus = 'option-c' | 'option-a' | 'none';

export default function ModelSettingsHub() {
  const [localModeState, setLocalModeState] = useState<LocalModeState>('unknown');
  const [bindings, setBindings] = useState<AccountBinding[]>([]);
  const [connectors, setConnectors] = useState<LocalConnector[]>([]);
  const [connectorCliConfigs, setConnectorCliConfigs] = useState<Record<string, CliConfig[]>>({});
  const [dataLoading, setDataLoading] = useState(true);

  useEffect(() => {
    let mounted = true;
    getMeta()
      .then(resp => {
        if (mounted) setLocalModeState(resp.data.local_mode ? 'local' : 'server');
      })
      .catch(() => {
        // Leave the state as 'unknown' on transient failure; the UI renders
        // Option B as "loading / unable to determine" rather than "disabled".
        if (mounted) setLocalModeState('unknown');
      });
    return () => { mounted = false; };
  }, []);

  useEffect(() => {
    let mounted = true;
    setDataLoading(true);

    Promise.all([
      listAccountBindings().catch(() => ({ data: [] as AccountBinding[] })),
      listLocalConnectors().catch(() => ({ data: [] as LocalConnector[] })),
    ]).then(async ([bindingsResp, connectorsResp]) => {
      if (!mounted) return;
      const fetchedBindings = bindingsResp.data;
      const fetchedConnectors = connectorsResp.data;
      setBindings(fetchedBindings);
      setConnectors(fetchedConnectors);

      // Fetch CLI configs for all non-revoked connectors (not just online) so that
      // an offline connector with existing configs still counts as "set up".
      const activeConnectors = fetchedConnectors.filter(c => c.status !== 'revoked');
      const cliConfigEntries = await Promise.all(
        activeConnectors.map(c =>
          listConnectorCliConfigs(c.id)
            .then(r => [c.id, r.data] as [string, CliConfig[]])
            .catch(() => [c.id, [] as CliConfig[]] as [string, CliConfig[]])
        )
      );
      if (!mounted) return;
      const configMap: Record<string, CliConfig[]> = {};
      for (const [id, configs] of cliConfigEntries) {
        configMap[id] = configs;
      }
      setConnectorCliConfigs(configMap);
      setDataLoading(false);
    }).catch(() => {
      if (mounted) setDataLoading(false);
    });

    return () => { mounted = false; };
  }, []);

  async function makePrimary(id: string) {
    await updateAccountBinding(id, { is_primary: true });
    const resp = await listAccountBindings();
    setBindings(resp.data);
  }

  // Derive setup status: option-c > option-a > none
  // Use all non-revoked connectors so offline machines with configs still count.
  const connectorWithConfig = connectors.filter(c => c.status !== 'revoked').find(
    c => (connectorCliConfigs[c.id] ?? []).length > 0
  );
  const activeApiBinding = bindings.find(
    b => !b.provider_id.startsWith('cli:') && b.is_active
  );

  let setupStatus: SetupStatus = 'none';
  if (connectorWithConfig) {
    setupStatus = 'option-c';
  } else if (activeApiBinding) {
    setupStatus = 'option-a';
  }

  const apiBindings = bindings.filter(b => !b.provider_id.startsWith('cli:'));
  const cliBindings = bindings.filter(b => b.provider_id.startsWith('cli:'));

  // Banner border color
  const bannerBorderColor =
    setupStatus === 'none' ? 'var(--color-warning, #ca8a04)' : '#16a34a';

  // Connector list for Option C card (max 3)
  const visibleConnectors = connectors.filter(c => c.status !== 'revoked');
  const shownConnectors = visibleConnectors.slice(0, 3);
  const extraConnectorCount = visibleConnectors.length - shownConnectors.length;

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1>Model Settings</h1>
          <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
            Pick how planning runs should reach a model. You can mix and match — each project-level planning run can target a specific binding or connector.
          </p>
        </div>
      </div>

      {/* Direction A: "Your current setup" banner */}
      <div
        className="card"
        style={{
          marginBottom: '1rem',
          borderLeft: `4px solid ${bannerBorderColor}`,
          padding: '0.85rem 1rem',
        }}
      >
        {dataLoading ? (
          <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.9rem' }}>
            Checking your setup…
          </p>
        ) : setupStatus === 'option-c' && connectorWithConfig ? (
          <>
            <p style={{ margin: '0 0 0.25rem', fontWeight: 600 }}>
              <span style={{ color: '#4ade80', marginRight: '0.4rem' }}>●</span>
              Your setup: Option C (Your Machine's CLI)
            </p>
            <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
              {connectorWithConfig.label} {connectorWithConfig.status === 'online' ? 'is online' : 'is offline'}
              {(connectorCliConfigs[connectorWithConfig.id] ?? []).length > 0 && (
                <> · {(connectorCliConfigs[connectorWithConfig.id] ?? [])[0]?.label ?? ''}</>
              )}
            </p>
            <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)', fontSize: '0.88rem' }}>
              Planning runs will be dispatched to your machine.
            </p>
          </>
        ) : setupStatus === 'option-a' && activeApiBinding ? (
          <>
            <p style={{ margin: '0 0 0.25rem', fontWeight: 600 }}>
              <span style={{ color: '#4ade80', marginRight: '0.4rem' }}>●</span>
              Your setup: Option A (API Key)
            </p>
            <p style={{ margin: 0, color: 'var(--text-muted)', fontSize: '0.88rem' }}>
              {activeApiBinding.label} · {activeApiBinding.model_id}
            </p>
          </>
        ) : (
          // Direction D: "Still unsure?" shown here when no setup
          <>
            <p style={{ margin: '0 0 0.35rem', fontWeight: 600 }}>
              Not sure where to start?
            </p>
            <p style={{ margin: '0 0 0.5rem', color: 'var(--text-muted)', fontSize: '0.88rem' }}>
              If you use Claude Code or Codex through a subscription (no API key), pair your machine using Option C — it lets the server dispatch planning jobs to your local CLI session.
            </p>
            <Link to="/settings/connector" className="btn btn-primary btn-sm">
              Set up My Connector →
            </Link>
          </>
        )}
      </div>

      <div className="model-hub-grid">
        {/* Direction B: simplified card titles */}
        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option A</span>
            <h2>API Key</h2>
          </header>
          <p>
            Configure an API key for OpenAI, Mistral, Anthropic, or a local OpenAI-compatible server like Ollama. The server makes the LLM call.
          </p>
          <div className="model-hub-bindings">
            <strong style={{ fontSize: '0.85rem' }}>Your bindings</strong>
            {dataLoading ? (
              <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>Loading…</div>
            ) : apiBindings.length === 0 ? (
              <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>
                No bindings configured. <Link to="/settings/account-bindings">Add one →</Link>
              </div>
            ) : (
              <div style={{ display: 'grid', gap: '0.4rem', marginTop: '0.4rem' }}>
                {apiBindings.map(b => (
                  <div key={b.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                    <span>{b.label}</span>
                    <span style={{ color: 'var(--text-muted)' }}>·</span>
                    <span style={{ color: 'var(--text-muted)' }}>{b.model_id}</span>
                    {b.is_primary ? (
                      <span className="badge"><Jargon term="primary binding">Primary</Jargon></span>
                    ) : (
                      <button className="btn btn-secondary btn-sm" onClick={() => makePrimary(b.id)}>Make primary</button>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
          <div className="model-hub-actions">
            <Link to="/settings/account-bindings" className="btn btn-primary btn-sm">Configure API-key bindings →</Link>
          </div>
        </section>

        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option B</span>
            <h2>Local CLI (server-side) <span className="model-hub-mode-note">(local-mode only)</span></h2>
          </header>
          <p>
            Run a CLI subscription (Claude Code, Codex) that is installed on the same machine as this server. Local mode only.
          </p>
          {localModeState === 'local' && (
            <div className="model-hub-bindings">
              <strong style={{ fontSize: '0.85rem' }}>Your bindings</strong>
              {dataLoading ? (
                <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>Loading…</div>
              ) : cliBindings.length === 0 ? (
                <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>
                  No bindings configured. <Link to="/settings/account-bindings">Add one →</Link>
                </div>
              ) : (
                <div style={{ display: 'grid', gap: '0.4rem', marginTop: '0.4rem' }}>
                  {cliBindings.map(b => (
                    <div key={b.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                      <span>{b.label}</span>
                      <span style={{ color: 'var(--text-muted)' }}>·</span>
                      <span style={{ color: 'var(--text-muted)' }}>{b.model_id}</span>
                      {b.is_primary ? (
                        <span className="badge"><Jargon term="primary binding">Primary</Jargon></span>
                      ) : (
                        <button className="btn btn-secondary btn-sm" onClick={() => makePrimary(b.id)}>Make primary</button>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="model-hub-actions">
            {localModeState === 'local' && (
              <Link to="/settings/account-bindings" className="btn btn-primary btn-sm">Configure server-side CLI bindings →</Link>
            )}
            {localModeState === 'server' && (
              <span className="model-hub-disabled">Switch the server to local mode to enable this option.</span>
            )}
            {localModeState === 'unknown' && (
              <span className="model-hub-disabled">Checking server mode…</span>
            )}
          </div>
        </section>

        {/* Direction B + C: simplified title and live connector data */}
        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option C</span>
            <h2>Your Machine's CLI</h2>
          </header>
          <p>
            Pair a machine where <code>claude</code> or <code>codex</code> is installed. Planning jobs are dispatched there — the CLI call happens on your machine, not the server.
          </p>

          {/* Direction C: live connector status */}
          <div className="model-hub-bindings">
            <strong style={{ fontSize: '0.85rem' }}>Paired machines</strong>
            {dataLoading ? (
              <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>
                Checking connector status…
              </div>
            ) : visibleConnectors.length === 0 ? (
              <div style={{ fontSize: '0.85rem', color: 'var(--text-muted)', marginTop: '0.4rem' }}>
                No machines paired yet.{' '}
                <Link to="/settings/connector">Set up My Connector →</Link>
              </div>
            ) : (
              <div style={{ display: 'grid', gap: '0.5rem', marginTop: '0.4rem' }}>
                {shownConnectors.map(c => {
                  const isOnline = c.status === 'online';
                  const configs = connectorCliConfigs[c.id] ?? [];
                  const configLabel = configs.length > 0
                    ? configs.map(cfg => cfg.label).join(', ')
                    : 'none configured';
                  return (
                    <div key={c.id} style={{ fontSize: '0.85rem' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                        <span style={{ color: isOnline ? '#4ade80' : 'var(--text-muted)' }}>
                          {isOnline ? '●' : '○'}
                        </span>
                        <span>{c.label}</span>
                        <span style={{ color: 'var(--text-muted)' }}>·</span>
                        <span className={`connector-badge ${isOnline ? 'connector-badge-ready' : 'connector-badge-offline'}`}>
                          {c.status}
                        </span>
                      </div>
                      <div style={{ color: 'var(--text-muted)', marginLeft: '1.2rem', marginTop: '0.15rem' }}>
                        CLI configs: {configLabel}
                      </div>
                    </div>
                  );
                })}
                {extraConnectorCount > 0 && (
                  <div style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    + {extraConnectorCount} more on the{' '}
                    <Link to="/settings/connector">connector page</Link>
                  </div>
                )}
              </div>
            )}
          </div>

          <div className="model-hub-actions">
            <Link to="/settings/connector" className="btn btn-primary btn-sm">Set up My Connector →</Link>
          </div>
        </section>
      </div>
      {/* Direction D: "Still unsure?" block removed — shown conditionally in the banner above */}
    </div>
  );
}
