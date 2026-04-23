import { useEffect, useState } from 'react';
import {
  listAccountBindings,
  createAccountBinding,
  updateAccountBinding,
  deleteAccountBinding,
} from '../api/client';
import type { AccountBinding, CreateAccountBindingPayload } from '../types';
import {
  getPlanningConnectionPreset,
  inferPlanningConnectionPreset,
  planningConnectionPresets,
  type PlanningConnectionPresetID,
} from '../utils/planningConnectionPresets';

export default function AccountBindings() {
  const [bindings, setBindings] = useState<AccountBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [selectedPresetId, setSelectedPresetId] = useState<PlanningConnectionPresetID>('ollama-docker');
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [label, setLabel] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [modelId, setModelId] = useState('');
  const [configuredModelsText, setConfiguredModelsText] = useState('');
  const [apiKey, setApiKey] = useState('');

  const selectedPreset = getPlanningConnectionPreset(selectedPresetId);

  useEffect(() => {
    load();
  }, []);

  async function load() {
    try {
      const resp = await listAccountBindings();
      setBindings(resp.data);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load bindings');
    } finally {
      setLoading(false);
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
  }

  function bindingPresetLabel(binding: AccountBinding) {
    return getPlanningConnectionPreset(inferPlanningConnectionPreset(binding.base_url)).label;
  }

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
        <div className="loading">Loading…</div>
      ) : (
        <>
          {bindings.length === 0 && !showForm && (
            <div className="card">
              <p>No personal bindings configured yet.</p>
            </div>
          )}

          {bindings.map(binding => (
            <div className="card" key={binding.id} style={{ opacity: binding.is_active ? 1 : 0.6 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <strong>{binding.label || 'default'}</strong>
                  <span style={{ marginLeft: '0.75rem', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                    {bindingPresetLabel(binding)}
                  </span>
                  {!binding.is_active && (
                    <span style={{ marginLeft: '0.5rem', color: 'var(--warning)', fontSize: '0.85rem' }}>
                      (inactive)
                    </span>
                  )}
                </div>
                <div style={{ display: 'flex', gap: '0.5rem' }}>
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
            </div>
          ))}

          {!showForm ? (
            <button className="btn btn-primary" onClick={handleOpenCreateForm}>
              Add Binding
            </button>
          ) : (
            <div className="card">
              <h2>New Binding</h2>
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
                      <label>Configured models</label>
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
                  </>
                )}
                <div style={{ display: 'flex', gap: '0.75rem' }}>
                  <button type="submit" className="btn btn-primary" disabled={saving}>
                    {saving ? 'Creating…' : 'Create'}
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
        </>
      )}
    </div>
  );
}
