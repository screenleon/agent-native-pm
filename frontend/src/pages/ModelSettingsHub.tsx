import { Link } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { getMeta } from '../api/client';

// Landing page introduced in Phase 4 (P4-2). Disambiguates the three ways a
// planning run can reach a model: server-side API call, server-side CLI, or
// a CLI on the operator's own machine through the connector. The three cards
// below each link into the pre-existing settings surfaces instead of
// duplicating their forms.
// Tri-state: 'unknown' while getMeta() is in-flight or if it failed (so a
// transient network blip does not wrongly downgrade Option B to disabled),
// true if the server reports local mode, false otherwise. Copilot #8/#10.
type LocalModeState = 'unknown' | 'local' | 'server';

export default function ModelSettingsHub() {
  const [localModeState, setLocalModeState] = useState<LocalModeState>('unknown');

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

      <div className="model-hub-grid">
        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option A</span>
            <h2>Server calls a hosted API</h2>
          </header>
          <p>
            Use your API key (OpenAI, Mistral, Anthropic, …) or point at a local OpenAI-compatible server such as Ollama or LM Studio. Runs happen on the server that hosts Agent Native PM.
          </p>
          <ul className="model-hub-bullets">
            <li>Works in server mode and local mode.</li>
            <li>Easiest path if you already have an API key or a local LLM runtime.</li>
          </ul>
          <div className="model-hub-actions">
            <Link to="/settings/account-bindings" className="btn btn-primary btn-sm">Configure API-key bindings →</Link>
          </div>
        </section>

        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option B</span>
            <h2>Server runs a CLI on the same host <span className="model-hub-mode-note">(local-mode only)</span></h2>
          </header>
          <p>
            Reuse a CLI subscription (Claude Code, Codex) that is installed on the same machine as the server. The server invokes the CLI as a subprocess when a planning run starts.
          </p>
          <ul className="model-hub-bullets">
            <li>Available only when the server runs in local mode on your workstation.</li>
            <li>No API key needed — the CLI brings its own authenticated session.</li>
          </ul>
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

        <section className="card model-hub-card">
          <header className="model-hub-card-header">
            <span className="model-hub-card-tag">Option C</span>
            <h2>Your own machine runs the CLI</h2>
          </header>
          <p>
            Pair a laptop or workstation that has <code>claude</code> or <code>codex</code> installed. The server dispatches planning jobs to that machine; the CLI and the LLM call execute there, not on the server.
          </p>
          <ul className="model-hub-bullets">
            <li>Good fit when the server is remote but your CLI subscription is local.</li>
            <li>One paired machine serves all your projects; pair additional machines for concurrent runs.</li>
          </ul>
          <div className="model-hub-actions">
            <Link to="/settings/connector" className="btn btn-primary btn-sm">Set up My Connector →</Link>
          </div>
        </section>
      </div>

      <div className="card">
        <h3 style={{ marginTop: 0 }}>Still unsure?</h3>
        <p style={{ margin: 0, color: 'var(--text-muted)' }}>
          If you usually sign in to Claude or ChatGPT through a browser or a VS Code extension, Option C (My Connector) is almost always the right starting point.
        </p>
      </div>
    </div>
  );
}
