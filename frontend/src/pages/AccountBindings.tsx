import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import type { CliProbeResult } from '../api/client';
import {
  listLocalConnectors,
  probeBindingOnConnector,
  getCliProbeResult,
} from '../api/client';
import {
  listAccountBindings,
  createAccountBinding,
  updateAccountBinding,
  deleteAccountBinding,
  getMeta,
  fetchRemoteModels,
  probeModel,
} from '../api/client';
import type { ProbeModelResult } from '../api/client';
import type { AccountBinding, CreateAccountBindingPayload } from '../types';
import {
  getPlanningConnectionPreset,
  inferPlanningConnectionPreset,
  planningConnectionPresets,
  type PlanningConnectionPresetID,
} from '../utils/planningConnectionPresets';
import {
  cliBindingPresets,
  getCliBindingPreset,
  inferCliBindingPreset,
  type CliBindingPresetID,
} from '../utils/cliBindingPresets';
import { formatRelativeTime } from '../utils/formatters';

function PersistentProbeStatus({ binding }: { binding: { last_probe_at: string | null; last_probe_ok: boolean | null; last_probe_ms: number | null } }) {
  if (!binding.last_probe_at) return null;
  const ok = binding.last_probe_ok;
  return (
    <div style={{ marginTop: '0.35rem', fontSize: '0.8rem', color: 'var(--text-muted)' }}>
      Last tested: {formatRelativeTime(binding.last_probe_at)}{' '}
      {ok === true && (
        <span style={{ color: 'var(--success, #4ade80)' }}>
          ✓{binding.last_probe_ms != null ? ` ${binding.last_probe_ms} ms` : ''}
        </span>
      )}
      {ok === false && (
        <span style={{ color: 'var(--danger, #f87171)' }}>✗ failed</span>
      )}
    </div>
  );
}

function ProbeReport({ result }: { result: ProbeModelResult }) {
  if (!result.ok) {
    return (
      <div style={{
        marginTop: '0.5rem', padding: '0.5rem 0.75rem', borderRadius: '6px',
        background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
        fontSize: '0.85rem', color: 'var(--danger, #f87171)',
      }}>
        ✗ {result.error || 'Connection failed'}
      </div>
    );
  }
  return (
    <div style={{
      marginTop: '0.5rem', padding: '0.5rem 0.75rem', borderRadius: '6px',
      background: 'rgba(34,197,94,0.08)', border: '1px solid rgba(34,197,94,0.25)',
      fontSize: '0.85rem',
    }}>
      <div style={{ color: 'var(--success, #4ade80)', fontWeight: 600 }}>
        ✓ Connected in {result.latency_ms} ms
        {result.model_used && <span style={{ fontWeight: 400, color: 'var(--text-muted)', marginLeft: '0.5rem' }}>· {result.model_used}</span>}
      </div>
      {result.content && (
        <div style={{ marginTop: '0.25rem', color: 'var(--text-muted)' }}>
          Response: &ldquo;{result.content}&rdquo;
        </div>
      )}
      {result.usage && (
        <div style={{ marginTop: '0.2rem', color: 'var(--text-muted)' }}>
          {result.usage.prompt_tokens} prompt + {result.usage.completion_tokens} completion tokens
        </div>
      )}
    </div>
  );
}

export default function AccountBindings() {
  const [bindings, setBindings] = useState<AccountBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [isLocalMode, setIsLocalMode] = useState(false);

  // openai-compatible form state
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [fetchingModels, setFetchingModels] = useState(false);
  const [formProbeResult, setFormProbeResult] = useState<ProbeModelResult | null>(null);
  const [probingForm, setProbingForm] = useState(false);
  const [cardProbeResults, setCardProbeResults] = useState<Record<string, ProbeModelResult>>({});
  const [probingCardId, setProbingCardId] = useState<string | null>(null);
  const [selectedPresetId, setSelectedPresetId] = useState<PlanningConnectionPresetID>('ollama-docker');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [label, setLabel] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [modelId, setModelId] = useState('');
  const [configuredModelsText, setConfiguredModelsText] = useState('');
  const [apiKey, setApiKey] = useState('');

  // CLI binding form state
  const [showCliForm, setShowCliForm] = useState(false);
  // Tracks explicit user dismissal so load() doesn't re-expand the auto-opened
  // form after unrelated actions (e.g. toggling an API-key binding) trigger a reload.
  // Reset to false after CLI deletion so the onboarding flow can re-open cleanly.
  const userDismissedCliForm = useRef(false);
  const [cliSaving, setCliSaving] = useState(false);
  const [selectedCliPresetId, setSelectedCliPresetId] = useState<CliBindingPresetID>('claude-code');
  const [cliLabel, setCliLabel] = useState('');
  const [cliCommand, setCliCommand] = useState('');
  const [cliModelId, setCliModelId] = useState('');
  const [cliIsPrimary, setCliIsPrimary] = useState(true);

  // CLI binding inline-edit state (P4-3). One editor at a time so opening
  // Edit on a second binding cancels the first — keeps the card list simple.
  const [editingCliId, setEditingCliId] = useState<string | null>(null);
  const [cliEditLabel, setCliEditLabel] = useState('');
  const [cliEditModelId, setCliEditModelId] = useState('');
  const [cliEditCommand, setCliEditCommand] = useState('');
  const [cliEditSaving, setCliEditSaving] = useState(false);
  const [cliEditError, setCliEditError] = useState('');

  // P4-4 probe-on-connector state. Keyed by CLI binding id so each row
  // displays its own progress independently. `probingCliBindingId` marks the
  // row that currently has a live poll loop attached.
  const [probingCliBindingId, setProbingCliBindingId] = useState<string | null>(null);
  const [cliProbeResults, setCliProbeResults] = useState<Record<string, CliProbeResult>>({});
  const [cliProbeErrors, setCliProbeErrors] = useState<Record<string, string>>({});
  // `probeActiveRef` is incremented on each fresh probe click; the poll loop
  // captures its value at start and bails if it no longer matches. This
  // cancels in-flight polls on unmount or when a new probe starts.
  const probeActiveRef = useRef(0);
  useEffect(() => () => { probeActiveRef.current = -1; }, []);

  const selectedPreset = getPlanningConnectionPreset(selectedPresetId);
  const selectedCliPreset = getCliBindingPreset(selectedCliPresetId);

  useEffect(() => {
    load();
  }, []);

  async function load() {
    try {
      const [bindingsResp, metaResp] = await Promise.all([
        listAccountBindings(),
        getMeta(),
      ]);
      const allBindings = bindingsResp.data;
      const isLocal = metaResp.data.local_mode;
      setBindings(allBindings);
      setIsLocalMode(isLocal);
      setError('');
      // T-S3-3: auto-expand form when no CLI bindings exist in local mode.
      // After a deletion that leaves 0 bindings the form re-expands too, which
      // is the intended "first binding of a namespace" onboarding flow.
      const hasAnyCli = allBindings.some((b: AccountBinding) => b.provider_id.startsWith('cli:'));
      if (isLocal && !hasAnyCli && !userDismissedCliForm.current) {
        setShowCliForm(true);
        setCliIsPrimary(true);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load bindings');
    } finally {
      setLoading(false);
    }
  }

  // openai-compatible handlers

  async function handleFetchModels() {
    if (!baseURL.trim()) return;
    setFetchingModels(true);
    setError('');
    try {
      const resp = await fetchRemoteModels(baseURL.trim(), apiKey.trim());
      const all = resp.data.models;
      const MAX = 16; // must match models.MaxAccountBindingConfiguredModels on the backend
      const models = all.slice(0, MAX);
      if (models.length > 0) {
        setConfiguredModelsText(models.join(', '));
        if (!modelId.trim()) setModelId(models[0]);
        if (all.length > MAX) {
          setError(`Provider returned ${all.length} models; showing the first ${MAX}. Edit the list to keep only the ones you need.`);
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch models from provider');
    } finally {
      setFetchingModels(false);
    }
  }

  async function handleProbeForm() {
    setProbingForm(true);
    setFormProbeResult(null);
    setError('');
    try {
      const resp = await probeModel({
        base_url: baseURL.trim(),
        api_key: apiKey.trim(),
        model_id: modelId.trim(),
      });
      setFormProbeResult(resp.data);
    } catch (err) {
      setFormProbeResult({ ok: false, latency_ms: 0, model_used: '', content: '', error: err instanceof Error ? err.message : 'Request failed' });
    } finally {
      setProbingForm(false);
    }
  }

  async function handleProbeCard(binding: AccountBinding) {
    const modelToProbe = binding.model_id || (binding.configured_models?.[0] ?? '');
    setProbingCardId(binding.id);
    setCardProbeResults(prev => { const next = { ...prev }; delete next[binding.id]; return next; });
    try {
      const resp = await probeModel({ binding_id: binding.id, model_id: modelToProbe });
      setCardProbeResults(prev => ({ ...prev, [binding.id]: resp.data }));
    } catch (err) {
      setCardProbeResults(prev => ({
        ...prev,
        [binding.id]: { ok: false, latency_ms: 0, model_used: '', content: '', error: err instanceof Error ? err.message : 'Request failed' },
      }));
    } finally {
      setProbingCardId(null);
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError('');
    setSuccess('');
    try {
      const configuredModels = configuredModelsText
        .split(',')
        .map(s => s.trim())
        .filter(Boolean);
      const normalizedModelId = modelId.trim();
      const payload: CreateAccountBindingPayload = {
        provider_id: 'openai-compatible',
        label: label.trim() || selectedPreset.defaultLabel,
        base_url: baseURL.trim(),
        model_id: normalizedModelId,
        configured_models: configuredModels.length > 0 ? configuredModels : normalizedModelId ? [normalizedModelId] : undefined,
        api_key: apiKey.trim() || undefined,
      };
      await createAccountBinding(payload);
      resetForm(selectedPresetId);
      setShowForm(false);
      setSuccess('Binding created.');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create binding');
    } finally {
      setSaving(false);
    }
  }

  async function handleToggleActive(binding: AccountBinding) {
    try {
      await updateAccountBinding(binding.id, { is_active: !binding.is_active });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update binding');
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this binding? This cannot be undone.')) return;
    try {
      await deleteAccountBinding(id);
      setSuccess('Binding deleted.');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete binding');
    }
  }

  function handleSelectPreset(nextPresetId: PlanningConnectionPresetID) {
    const previousPreset = getPlanningConnectionPreset(selectedPresetId);
    const nextPreset = getPlanningConnectionPreset(nextPresetId);

    setSelectedPresetId(nextPresetId);
    setShowAdvanced(nextPreset.advancedOnly);
    setLabel(currentLabel => {
      const trimmed = currentLabel.trim();
      if (trimmed === '' || trimmed === previousPreset.defaultLabel) {
        return nextPreset.defaultLabel;
      }
      return currentLabel;
    });
    setBaseURL(currentBaseURL => {
      const trimmed = currentBaseURL.trim();
      if (trimmed === '' || trimmed === previousPreset.baseURL) {
        return nextPreset.baseURL;
      }
      return currentBaseURL;
    });
  }

  function handleOpenCreateForm() {
    setShowForm(true);
    resetForm(selectedPresetId);
  }

  function resetForm(presetId = selectedPresetId) {
    const preset = getPlanningConnectionPreset(presetId);
    setLabel(preset.defaultLabel);
    setBaseURL(preset.baseURL);
    setModelId('');
    setConfiguredModelsText('');
    setApiKey('');
    setShowAdvanced(preset.advancedOnly);
    setFormProbeResult(null);
  }

  function bindingPresetLabel(binding: AccountBinding) {
    return getPlanningConnectionPreset(inferPlanningConnectionPreset(binding.base_url)).label;
  }

  // CLI binding handlers

  async function handleCreateCli(e: React.FormEvent) {
    e.preventDefault();
    setCliSaving(true);
    setError('');
    setSuccess('');
    try {
      const payload: CreateAccountBindingPayload = {
        provider_id: selectedCliPreset.providerId,
        label: cliLabel.trim() || selectedCliPreset.defaultLabel,
        base_url: '',
        model_id: cliModelId.trim(),
        cli_command: cliCommand.trim() || undefined,
        is_primary: cliIsPrimary,
      };
      await createAccountBinding(payload);
      resetCliForm(selectedCliPresetId);
      setShowCliForm(false);
      setSuccess('CLI binding created.');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create CLI binding');
    } finally {
      setCliSaving(false);
    }
  }

  async function handleSetPrimary(id: string) {
    try {
      await updateAccountBinding(id, { is_primary: true });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to set primary binding');
    }
  }

  async function handleDeleteCli(id: string) {
    if (!confirm('Delete this CLI binding? This cannot be undone.')) return;
    try {
      await deleteAccountBinding(id);
      setSuccess('CLI binding deleted.');
      // Reset so load() can re-expand the form if this was the last CLI binding.
      userDismissedCliForm.current = false;
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete CLI binding');
    }
  }

  function handleDismissCliForm() {
    userDismissedCliForm.current = true;
    setShowCliForm(false);
  }

  function handleSelectCliPreset(nextPresetId: CliBindingPresetID) {
    const previousPreset = getCliBindingPreset(selectedCliPresetId);
    const nextPreset = getCliBindingPreset(nextPresetId);

    setSelectedCliPresetId(nextPresetId);
    setCliLabel(current => {
      const trimmed = current.trim();
      if (trimmed === '' || trimmed === previousPreset.defaultLabel) {
        return nextPreset.defaultLabel;
      }
      return current;
    });
    setCliCommand(current => {
      const trimmed = current.trim();
      if (trimmed === '' || trimmed === previousPreset.defaultCliCommand) {
        return nextPreset.defaultCliCommand;
      }
      return current;
    });
    // Re-evaluate is_primary default: checked when no existing binding for the
    // newly selected provider namespace (design §8 S3 "first binding of a namespace").
    const hasForNewProvider = cliBindings.some(b => b.provider_id === nextPreset.providerId);
    setCliIsPrimary(!hasForNewProvider);
  }

  function resetCliForm(presetId: CliBindingPresetID = selectedCliPresetId) {
    const preset = getCliBindingPreset(presetId);
    setCliLabel(preset.defaultLabel);
    setCliCommand(preset.defaultCliCommand);
    setCliModelId('');
    setCliIsPrimary(true);
  }

  function handleOpenCliForm() {
    resetCliForm(selectedCliPresetId);
    const preset = getCliBindingPreset(selectedCliPresetId);
    setCliIsPrimary(!cliBindings.some(b => b.provider_id === preset.providerId));
    setShowCliForm(true);
  }

  function handleOpenCliEdit(binding: AccountBinding) {
    setEditingCliId(binding.id);
    setCliEditLabel(binding.label || '');
    setCliEditModelId(binding.model_id || '');
    setCliEditCommand(binding.cli_command || '');
    setCliEditError('');
  }

  function handleCancelCliEdit() {
    setEditingCliId(null);
    setCliEditError('');
  }

  async function handleSaveCliEdit(binding: AccountBinding) {
    const nextModelId = cliEditModelId.trim();
    if (!nextModelId) {
      setCliEditError('Model ID is required.');
      return;
    }
    setCliEditSaving(true);
    setCliEditError('');
    try {
      await updateAccountBinding(binding.id, {
        label: cliEditLabel.trim(),
        model_id: nextModelId,
        cli_command: cliEditCommand.trim(),
      });
      setEditingCliId(null);
      setSuccess('CLI binding updated.');
      await load();
    } catch (err) {
      setCliEditError(err instanceof Error ? err.message : 'Failed to update CLI binding');
    } finally {
      setCliEditSaving(false);
    }
  }

  // P4-4: run a probe for a CLI binding on the user's first online connector,
  // then poll every 2s up to 30s until the connector reports back. A fresh
  // click (or component unmount) sets probeActiveRef to a new value which the
  // loop checks every iteration to exit cleanly.
  async function handleProbeOnConnector(binding: AccountBinding) {
    probeActiveRef.current += 1;
    const myProbeToken = probeActiveRef.current;
    setCliProbeErrors(prev => { const next = { ...prev }; delete next[binding.id]; return next; });
    setCliProbeResults(prev => { const next = { ...prev }; delete next[binding.id]; return next; });
    setProbingCliBindingId(binding.id);
    const cancelled = () => probeActiveRef.current !== myProbeToken;
    try {
      const connectorsResp = await listLocalConnectors();
      if (cancelled()) return;
      const online = connectorsResp.data.find(c => c.status === 'online');
      if (!online) {
        setCliProbeErrors(prev => ({ ...prev, [binding.id]: 'No online connector. Start `./bin/anpm-connector serve` on your paired machine first.' }));
        setProbingCliBindingId(null);
        return;
      }
      const enqueueResp = await probeBindingOnConnector(online.id, binding.id);
      if (cancelled()) return;
      const probeId = enqueueResp.data.probe_id;
      const deadline = Date.now() + 30_000;
      while (Date.now() < deadline) {
        await new Promise(r => setTimeout(r, 2_000));
        if (cancelled()) return;
        try {
          const statusResp = await getCliProbeResult(online.id, probeId);
          if (cancelled()) return;
          if (statusResp.data.status === 'completed' && statusResp.data.result) {
            setCliProbeResults(prev => ({ ...prev, [binding.id]: statusResp.data.result! }));
            setProbingCliBindingId(null);
            return;
          }
          if (statusResp.data.status === 'not_found') {
            setCliProbeErrors(prev => ({ ...prev, [binding.id]: 'Probe record was evicted before completion.' }));
            setProbingCliBindingId(null);
            return;
          }
        } catch {
          // Transient poll failure — try again until the deadline.
        }
      }
      if (cancelled()) return;
      setCliProbeErrors(prev => ({ ...prev, [binding.id]: 'Probe still pending after 30s. The connector may be offline or busy; check the connector log.' }));
      setProbingCliBindingId(null);
    } catch (err) {
      if (cancelled()) return;
      setCliProbeErrors(prev => ({ ...prev, [binding.id]: err instanceof Error ? err.message : 'Failed to start probe' }));
      setProbingCliBindingId(null);
    }
  }

  const apiBindings = bindings.filter(b => !b.provider_id.startsWith('cli:'));
  const cliBindings = bindings.filter(b => b.provider_id.startsWith('cli:'));

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1>My Account Bindings</h1>
          <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
            Link your personal provider access for planning runs. When credential mode is set to &quot;personal_preferred&quot; or &quot;personal_required&quot;,
            planning runs will use your binding instead of the shared key.
          </p>
          <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
            Only one active binding per provider is used for automation. Activating a binding automatically deactivates the previous active binding for the same provider.
          </p>
          <p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
            For CLI-subscription bindings that run on <strong>your own machine</strong>, see <Link to="/settings/connector">My Connector</Link> instead.
          </p>
        </div>
      </div>

      <div className="card">
        <h2 style={{ marginBottom: '0.5rem' }}>Quick Start</h2>
        <div className="helper-note" style={{ marginBottom: '0.75rem' }}>
          If you usually sign in through GitHub Copilot, ChatGPT, or another VS Code or CLI subscription flow, this server cannot reuse that subscription session directly yet.
        </div>
        <div className="helper-note">
          The shortest setup path is to pick a local preset below, point it at Ollama or LM Studio, enter one model name, and skip API keys entirely.
        </div>
      </div>

      {error && <div className="error-banner">{error}</div>}
      {success && <div className="alert alert-success">{success}</div>}

      {loading ? (
        <div className="loading">Loading...</div>
      ) : (
        <>
          {/* API-key bindings section */}
          <div className="card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <h2 style={{ margin: 0 }}>API-Key Bindings</h2>
              {!showForm && (
                <button className="btn btn-primary btn-sm" onClick={handleOpenCreateForm}>
                  + Add API-Key Binding
                </button>
              )}
            </div>

            {apiBindings.length === 0 && !showForm && (
              <p style={{ color: 'var(--text-muted)', margin: 0 }}>No API-key bindings configured yet.</p>
            )}

            {apiBindings.map(binding => (
              <div className="card" key={binding.id} style={{ opacity: binding.is_active ? 1 : 0.6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div>
                    <strong>{binding.label || 'default'}</strong>
                    <span style={{ marginLeft: '0.75rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                      {bindingPresetLabel(binding)}
                    </span>
                    {binding.is_primary && (
                      <span style={{ marginLeft: '0.5rem', color: 'var(--success)', fontSize: '0.85rem' }}>
                        Primary
                      </span>
                    )}
                    {!binding.is_active && (
                      <span style={{ marginLeft: '0.5rem', color: 'var(--warning)', fontSize: '0.85rem' }}>
                        (inactive)
                      </span>
                    )}
                  </div>
                  <div style={{ display: 'flex', gap: '0.5rem' }}>
                    <button
                      className="btn btn-ghost btn-sm"
                      onClick={() => handleProbeCard(binding)}
                      disabled={probingCardId === binding.id}
                    >
                      {probingCardId === binding.id ? 'Testing…' : 'Test'}
                    </button>
                    <button className="btn btn-ghost btn-sm" onClick={() => handleToggleActive(binding)}>
                      {binding.is_active ? 'Deactivate' : 'Set Active'}
                    </button>
                    <button className="btn btn-ghost btn-sm" onClick={() => handleDelete(binding.id)}>
                      Delete
                    </button>
                  </div>
                </div>
                <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                  <div>Base URL: {binding.base_url || '(none)'}</div>
                  <div>Model: {binding.model_id}</div>
                  <div>API key configured: {binding.api_key_configured ? 'Yes' : 'No'}</div>
                  <div>Models: {binding.configured_models?.join(', ') || binding.model_id}</div>
                </div>
                <PersistentProbeStatus binding={binding} />
                {cardProbeResults[binding.id] && (
                  <div style={{ marginTop: '0.5rem' }}>
                    <ProbeReport result={cardProbeResults[binding.id]} />
                  </div>
                )}
              </div>
            ))}

            {showForm && (
              <div className="card">
                <h2>New API-Key Binding</h2>
                <p style={{ margin: '0.45rem 0 1rem', color: 'var(--text-muted)' }}>
                  Pick the environment that already exists on your machine. Only switch to the custom API option when you really need a non-local endpoint.
                </p>
                <div className="preset-grid" style={{ marginBottom: '1rem' }}>
                  {planningConnectionPresets.map(preset => (
                    <button
                      key={preset.id}
                      type="button"
                      className={`preset-card ${selectedPresetId === preset.id ? 'is-selected' : ''}`}
                      onClick={() => handleSelectPreset(preset.id)}
                      disabled={saving}
                    >
                      <strong>{preset.label}</strong>
                      <span>{preset.description}</span>
                    </button>
                  ))}
                </div>
                <form onSubmit={handleCreate} className="stacked-form" style={{ display: 'grid', gap: '1rem' }}>
                  <div className="form-group">
                    <label>Label</label>
                    <input type="text" value={label} onChange={e => setLabel(e.target.value)} placeholder={selectedPreset.defaultLabel} disabled={saving} />
                    <small>Name this binding so you can distinguish it from your other local or remote connections.</small>
                  </div>
                  <div className="form-group">
                    <label>Model ID</label>
                    <input type="text" value={modelId} onChange={e => setModelId(e.target.value)} placeholder={selectedPreset.modelPlaceholder} disabled={saving} required />
                    <small>Use exactly the model name that your local runtime or remote gateway exposes.</small>
                  </div>
                  <div className="helper-note">
                    <strong>{selectedPreset.label}</strong>
                    <div style={{ marginTop: '0.35rem' }}>{selectedPreset.description}</div>
                    {selectedPreset.baseURL && <div style={{ marginTop: '0.35rem' }}>Default endpoint: {selectedPreset.baseURL}</div>}
                  </div>
                  <div className="form-inline-actions">
                    <button type="button" className="btn btn-ghost btn-sm" onClick={() => setShowAdvanced(current => !current)} disabled={saving}>
                      {showAdvanced ? 'Hide Advanced Fields' : 'Show Advanced Fields'}
                    </button>
                  </div>
                  {showAdvanced && (
                    <>
                      <div className="form-group">
                        <label>Base URL</label>
                        <input type="url" value={baseURL} onChange={e => setBaseURL(e.target.value)} placeholder={selectedPreset.baseURL || 'https://api.openai.com/v1'} disabled={saving} required />
                        <small>Leave the preset value as-is unless your local bridge or gateway listens on a different address.</small>
                      </div>
                      <div className="form-group">
                        <label style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <span>Configured models</span>
                          {baseURL.trim() && (
                            <button
                              type="button"
                              className="btn btn-ghost btn-sm"
                              onClick={handleFetchModels}
                              disabled={fetchingModels || saving}
                            >
                              {fetchingModels ? 'Fetching…' : 'Fetch from API →'}
                            </button>
                          )}
                        </label>
                        <input type="text" value={configuredModelsText} onChange={e => setConfiguredModelsText(e.target.value)} placeholder={selectedPreset.configuredModelsPlaceholder} disabled={saving} />
                        <small>Comma-separated. If empty, this binding will use the single model ID above.</small>
                      </div>
                      {selectedPreset.apiKeyMode !== 'hidden' && (
                        <div className="form-group">
                          <label>
                            API key
                            {selectedPreset.apiKeyMode === 'required' && <span aria-label="required"> *</span>}
                          </label>
                          <input
                            type="password"
                            value={apiKey}
                            onChange={e => setApiKey(e.target.value)}
                            placeholder={selectedPreset.apiKeyMode === 'required' ? 'Required for this hosted provider' : 'Only needed when your remote gateway requires one'}
                            disabled={saving}
                            required={selectedPreset.apiKeyMode === 'required'}
                          />
                        </div>
                      )}
                      {/* Test Connection */}
                      {baseURL.trim() && modelId.trim() && (
                        <div>
                          <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            onClick={handleProbeForm}
                            disabled={probingForm || saving}
                          >
                            {probingForm ? 'Testing…' : 'Test Connection'}
                          </button>
                          {formProbeResult && <ProbeReport result={formProbeResult} />}
                        </div>
                      )}
                    </>
                  )}
                  <div style={{ display: 'flex', gap: '0.75rem' }}>
                    <button type="submit" className="btn btn-primary" disabled={saving}>
                      {saving ? 'Creating...' : 'Create'}
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={() => resetForm()} disabled={saving}>
                      Reset
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={() => setShowForm(false)} disabled={saving}>
                      Cancel
                    </button>
                  </div>
                </form>
              </div>
            )}
          </div>

          {/* CLI bindings section */}
          <div className="card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
              <h2 style={{ margin: 0 }}>Server-side CLI Bindings <span style={{ fontSize: '0.75rem', fontWeight: 400, color: 'var(--text-muted)', marginLeft: '0.5rem' }}>(local-mode only)</span></h2>
              {isLocalMode && !showCliForm && (
                <button className="btn btn-primary btn-sm" onClick={() => handleOpenCliForm()}>
                  + Add CLI Binding
                </button>
              )}
            </div>

            {!isLocalMode ? (
              <div className="helper-note">
                CLI bindings are only available in local mode. Switch to local mode to manage CLI bindings.
              </div>
            ) : (
              <>
                {cliBindings.length === 0 && !showCliForm && (
                  <p style={{ color: 'var(--text-muted)', margin: 0 }}>No CLI bindings configured yet.</p>
                )}

                {cliBindings.map(binding => {
                  const presetId = inferCliBindingPreset(binding.provider_id);
                  const preset = getCliBindingPreset(presetId);
                  const isEditing = editingCliId === binding.id;
                  return (
                    <div className="card" key={binding.id} style={{ opacity: binding.is_active ? 1 : 0.6 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <div>
                          <strong>{binding.label || 'default'}</strong>
                          <span style={{ marginLeft: '0.75rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                            {preset.label}
                          </span>
                          {binding.is_primary && (
                            <span style={{ marginLeft: '0.5rem', color: 'var(--success)', fontSize: '0.85rem' }}>
                              Primary
                            </span>
                          )}
                          {!binding.is_active && (
                            <span style={{ marginLeft: '0.5rem', color: 'var(--warning)', fontSize: '0.85rem' }}>
                              (inactive)
                            </span>
                          )}
                        </div>
                        <div style={{ display: 'flex', gap: '0.5rem' }}>
                          {!isEditing && (
                            <button
                              className="btn btn-ghost btn-sm"
                              onClick={() => handleProbeOnConnector(binding)}
                              disabled={probingCliBindingId === binding.id}
                            >
                              {probingCliBindingId === binding.id ? 'Testing…' : 'Test on connector'}
                            </button>
                          )}
                          {!isEditing && !binding.is_primary && (
                            <button className="btn btn-ghost btn-sm" onClick={() => handleSetPrimary(binding.id)}>
                              Switch to this binding
                            </button>
                          )}
                          {!isEditing && (
                            <button className="btn btn-ghost btn-sm" onClick={() => handleOpenCliEdit(binding)}>
                              Edit
                            </button>
                          )}
                          {!isEditing && (
                            <button className="btn btn-ghost btn-sm" onClick={() => handleDeleteCli(binding.id)}>
                              Delete
                            </button>
                          )}
                        </div>
                      </div>
                      {isEditing ? (
                        <div style={{ marginTop: '0.75rem', display: 'grid', gap: '0.75rem' }}>
                          <div className="form-group">
                            <label>Label</label>
                            <input
                              type="text"
                              value={cliEditLabel}
                              onChange={e => setCliEditLabel(e.target.value)}
                              placeholder={preset.defaultLabel}
                              disabled={cliEditSaving}
                            />
                          </div>
                          <div className="form-group">
                            <label>Model ID</label>
                            <input
                              type="text"
                              value={cliEditModelId}
                              onChange={e => setCliEditModelId(e.target.value)}
                              placeholder={preset.modelPlaceholder}
                              disabled={cliEditSaving}
                              required
                            />
                          </div>
                          <div className="form-group">
                            <label>CLI command (optional — leave empty for PATH lookup)</label>
                            <input
                              type="text"
                              value={cliEditCommand}
                              onChange={e => setCliEditCommand(e.target.value)}
                              placeholder={preset.defaultCliCommand}
                              disabled={cliEditSaving}
                            />
                          </div>
                          {cliEditError && <div className="error-banner">{cliEditError}</div>}
                          <div style={{ display: 'flex', gap: '0.5rem' }}>
                            <button
                              type="button"
                              className="btn btn-primary btn-sm"
                              onClick={() => handleSaveCliEdit(binding)}
                              disabled={cliEditSaving}
                            >
                              {cliEditSaving ? 'Saving…' : 'Save'}
                            </button>
                            <button
                              type="button"
                              className="btn btn-ghost btn-sm"
                              onClick={handleCancelCliEdit}
                              disabled={cliEditSaving}
                            >
                              Cancel
                            </button>
                          </div>
                        </div>
                      ) : (
                        <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', color: 'var(--text-muted)' }}>
                          <div>Provider: {binding.provider_id}</div>
                          <div>Model: {binding.model_id}</div>
                          {binding.cli_command && <div>CLI command: {binding.cli_command}</div>}
                        </div>
                      )}
                      {cliProbeResults[binding.id] && (
                        <div style={{
                          marginTop: '0.5rem', padding: '0.5rem 0.75rem', borderRadius: '6px',
                          background: cliProbeResults[binding.id].ok ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.1)',
                          border: `1px solid ${cliProbeResults[binding.id].ok ? 'rgba(34,197,94,0.25)' : 'rgba(239,68,68,0.3)'}`,
                          fontSize: '0.85rem',
                        }}>
                          {cliProbeResults[binding.id].ok ? (
                            <>
                              <div style={{ color: 'var(--success, #4ade80)', fontWeight: 600 }}>
                                ✓ Connector replied in {cliProbeResults[binding.id].latency_ms} ms
                              </div>
                              {cliProbeResults[binding.id].content && (
                                <div style={{ marginTop: '0.25rem', color: 'var(--text-muted)' }}>
                                  Response: &ldquo;{cliProbeResults[binding.id].content}&rdquo;
                                </div>
                              )}
                            </>
                          ) : (
                            <div style={{ color: 'var(--danger, #f87171)' }}>
                              ✗ {cliProbeResults[binding.id].error_message || 'Probe failed'}
                            </div>
                          )}
                        </div>
                      )}
                      {cliProbeErrors[binding.id] && !cliProbeResults[binding.id] && (
                        <div style={{ marginTop: '0.5rem', padding: '0.5rem 0.75rem', borderRadius: '6px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', fontSize: '0.85rem', color: 'var(--danger, #f87171)' }}>
                          ✗ {cliProbeErrors[binding.id]}
                        </div>
                      )}
                    </div>
                  );
                })}

                {showCliForm && (
                  <div className="card">
                    <h2>New CLI Binding</h2>
                    <div className="preset-grid" style={{ marginBottom: '1rem' }}>
                      {cliBindingPresets.map(preset => (
                        <button
                          key={preset.id}
                          type="button"
                          className={`preset-card ${selectedCliPresetId === preset.id ? 'is-selected' : ''}`}
                          onClick={() => handleSelectCliPreset(preset.id)}
                          disabled={cliSaving}
                        >
                          <strong>
                            {preset.label}
                            {preset.isUntested && (
                              <span style={{ marginLeft: '0.5rem', fontSize: '0.75rem', color: 'var(--warning)' }}>
                                (Untested by maintainer)
                              </span>
                            )}
                          </strong>
                          <span>{preset.description}</span>
                        </button>
                      ))}
                    </div>
                    <form onSubmit={handleCreateCli} className="stacked-form" style={{ display: 'grid', gap: '1rem' }}>
                      <div className="form-group">
                        <label>Label</label>
                        <input
                          type="text"
                          value={cliLabel}
                          onChange={e => setCliLabel(e.target.value)}
                          placeholder={selectedCliPreset.defaultLabel}
                          disabled={cliSaving}
                        />
                      </div>
                      <div className="form-group">
                        <label>Model ID</label>
                        <input
                          type="text"
                          value={cliModelId}
                          onChange={e => setCliModelId(e.target.value)}
                          placeholder={selectedCliPreset.modelPlaceholder}
                          disabled={cliSaving}
                          required
                        />
                        <small>Use the model name this CLI accepts, e.g. claude-sonnet-4-5.</small>
                      </div>
                      <div className="form-group">
                        <label>CLI command (optional — leave empty for PATH lookup)</label>
                        <input
                          type="text"
                          value={cliCommand}
                          onChange={e => setCliCommand(e.target.value)}
                          placeholder={selectedCliPreset.defaultCliCommand}
                          disabled={cliSaving}
                        />
                        <small>Full path preferred, e.g. /usr/local/bin/claude. Leave empty to use whichever is on PATH.</small>
                      </div>
                      <div className="form-group">
                        <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}>
                          <input
                            type="checkbox"
                            checked={cliIsPrimary}
                            onChange={e => setCliIsPrimary(e.target.checked)}
                            disabled={cliSaving}
                          />
                          Make this the primary binding for this CLI
                        </label>
                      </div>
                      <div style={{ display: 'flex', gap: '0.75rem' }}>
                        <button type="submit" className="btn btn-primary" disabled={cliSaving}>
                          {cliSaving ? 'Creating...' : 'Create'}
                        </button>
                        <button type="button" className="btn btn-ghost" onClick={() => resetCliForm()} disabled={cliSaving}>
                          Reset
                        </button>
                        <button type="button" className="btn btn-ghost" onClick={handleDismissCliForm} disabled={cliSaving}>
                          Cancel
                        </button>
                      </div>
                    </form>
                  </div>
                )}

                {cliBindings.length > 0 && !showCliForm && (
                  <button className="btn btn-ghost btn-sm" onClick={() => handleOpenCliForm()}>
                    + Add another CLI binding
                  </button>
                )}
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
