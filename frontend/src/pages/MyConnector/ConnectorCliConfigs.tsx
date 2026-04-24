import { useEffect, useRef, useState } from 'react';
import {
  listConnectorCliConfigs,
  createConnectorCliConfig,
  updateConnectorCliConfig,
  deleteConnectorCliConfig,
  setPrimaryConnectorCliConfig,
} from '../../api/client';
import type { CliConfig, CreateCliConfigPayload } from '../../api/client';
import Jargon from '../../components/Jargon';
import { knownModelsForProvider } from '../../utils/cliBindingPresets';

// Phase 6a UX-B2: inline CLI config management for one connector card.
// Each connector owns its cli_configs[] (stored in local_connectors.metadata);
// this component exposes Add / Edit / Delete / Set-Primary for them.

const CUSTOM_MODEL_SENTINEL = '__custom__';

type PresetID = 'cli:claude' | 'cli:codex';
const PRESETS: Record<PresetID, {
  label: string;
  commandPlaceholder: string;
  modelPlaceholder: string;
  defaultLabel: string;
  defaultModelId: string;
}> = {
  'cli:claude': {
    label: 'Claude Code',
    commandPlaceholder: 'claude',
    modelPlaceholder: 'claude-sonnet-4-6',
    defaultLabel: 'My Claude',
    defaultModelId: 'claude-sonnet-4-6',
  },
  'cli:codex': {
    label: 'OpenAI Codex',
    commandPlaceholder: 'codex',
    modelPlaceholder: 'gpt-5.5',
    defaultLabel: 'My Codex',
    defaultModelId: 'gpt-5.5',
  },
};

// Renders a model picker: <select> of known models + "Other" option that
// reveals a free-text input for any model ID not in the list.
function ModelPicker({ providerId, value, onChange, disabled }: {
  providerId: PresetID;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  const known = knownModelsForProvider(providerId);
  // `isCustom` handles the round-trip case: existing config with an unknown model ID.
  const isCustom = value !== '' && !known.some(m => m.id === value);
  // `intentCustom` is a local flag that persists while the user intends to type a
  // custom ID — we cannot derive this from `value` alone because `value` starts
  // as '' right after the user picks "Other".
  const intentCustomRef = useRef(isCustom);
  if (isCustom) intentCustomRef.current = true;

  const showCustomInput = intentCustomRef.current || isCustom;
  const selectValue = showCustomInput ? CUSTOM_MODEL_SENTINEL : value;

  function handleSelect(e: React.ChangeEvent<HTMLSelectElement>) {
    const v = e.target.value;
    if (v === CUSTOM_MODEL_SENTINEL) {
      intentCustomRef.current = true;
      onChange('');
    } else {
      intentCustomRef.current = false;
      onChange(v);
    }
  }

  return (
    <div style={{ display: 'grid', gap: '0.35rem' }}>
      <select value={selectValue} onChange={handleSelect} disabled={disabled} required>
        <option value="">— select a model —</option>
        {known.map(m => (
          <option key={m.id} value={m.id}>{m.label} ({m.id})</option>
        ))}
        <option value={CUSTOM_MODEL_SENTINEL}>Other (enter model ID manually)…</option>
      </select>
      {showCustomInput && (
        <input
          type="text"
          value={value}
          onChange={e => onChange(e.target.value)}
          placeholder="e.g. claude-sonnet-4-6"
          disabled={disabled}
          required
          aria-label="Custom model ID"
        />
      )}
    </div>
  );
}

interface Props {
  connectorId: string;
}

export function ConnectorCliConfigs({ connectorId }: Props) {
  const [configs, setConfigs] = useState<CliConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const [showAddForm, setShowAddForm] = useState(false);
  const [addSaving, setAddSaving] = useState(false);
  const [addPreset, setAddPreset] = useState<PresetID>('cli:claude');
  const [addLabel, setAddLabel] = useState('');
  const [addCommand, setAddCommand] = useState('');
  const [addModel, setAddModel] = useState('');

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editPreset, setEditPreset] = useState<PresetID>('cli:claude');
  const [editLabel, setEditLabel] = useState('');
  const [editCommand, setEditCommand] = useState('');
  const [editModel, setEditModel] = useState('');
  const [editSaving, setEditSaving] = useState(false);

  async function load() {
    setLoading(true);
    try {
      const resp = await listConnectorCliConfigs(connectorId);
      setConfigs(resp.data);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load CLI configs');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectorId]);

  function resetAdd(preset: PresetID = addPreset) {
    const p = PRESETS[preset];
    setAddLabel('');
    setAddCommand('');
    setAddPreset(preset);
    setAddModel(p.defaultModelId);
  }

  function openAddForm() {
    resetAdd('cli:claude');
    setShowAddForm(true);
  }

  async function submitAdd() {
    if (!addModel.trim()) {
      setError('Model is required');
      return;
    }
    setAddSaving(true);
    setError('');
    try {
      const payload: CreateCliConfigPayload = {
        provider_id: addPreset,
        model_id: addModel.trim(),
      };
      if (addLabel.trim()) payload.label = addLabel.trim();
      if (addCommand.trim()) payload.cli_command = addCommand.trim();
      await createConnectorCliConfig(connectorId, payload);
      setShowAddForm(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create CLI config');
    } finally {
      setAddSaving(false);
    }
  }

  function openEdit(c: CliConfig) {
    setEditingId(c.id);
    setEditPreset((c.provider_id as PresetID) in PRESETS ? c.provider_id as PresetID : 'cli:claude');
    setEditLabel(c.label);
    // c.cli_command may be absent (PATH lookup) — coerce to '' so React's
    // controlled <input> never receives `undefined` (Copilot review on
    // PR #23).
    setEditCommand(c.cli_command ?? '');
    setEditModel(c.model_id);
  }

  async function submitEdit(id: string) {
    if (!editModel.trim()) {
      setError('Model is required');
      return;
    }
    setEditSaving(true);
    setError('');
    try {
      await updateConnectorCliConfig(connectorId, id, {
        label: editLabel.trim(),
        cli_command: editCommand.trim(),
        model_id: editModel.trim(),
      });
      setEditingId(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update CLI config');
    } finally {
      setEditSaving(false);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this CLI configuration? This only removes it from this connector.')) return;
    try {
      await deleteConnectorCliConfig(connectorId, id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete CLI config');
    }
  }

  async function handleSetPrimary(id: string) {
    try {
      await setPrimaryConnectorCliConfig(connectorId, id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to set primary');
    }
  }

  return (
    <div className="cli-configs-section" style={{ marginTop: '0.85rem', paddingTop: '0.85rem', borderTop: '1px dashed var(--border)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
        <strong style={{ fontSize: '0.9rem' }}><Jargon term="CLI config">CLIs on this machine</Jargon></strong>
        {!showAddForm && (
          <button className="btn btn-ghost btn-sm" onClick={openAddForm} disabled={loading}>
            + Add CLI
          </button>
        )}
      </div>

      {error && <div className="error-banner" style={{ marginBottom: '0.5rem' }}>{error}</div>}

      {loading ? (
        <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>Loading…</div>
      ) : configs.length === 0 && !showAddForm ? (
        <div style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }}>
          No CLI configured on this machine yet. Click "+ Add CLI" to pick your installed Claude or Codex and a model.
        </div>
      ) : (
        <div style={{ display: 'grid', gap: '0.5rem' }}>
          {configs.map(c => {
            const preset = PRESETS[c.provider_id as PresetID] ?? { label: c.provider_id, defaultLabel: 'CLI' };
            const isEditing = editingId === c.id;
            return (
              <div
                key={c.id}
                style={{
                  border: '1px solid var(--border)',
                  borderRadius: '0.4rem',
                  padding: '0.6rem 0.75rem',
                  fontSize: '0.85rem',
                  background: 'var(--bg)',
                }}
              >
                {isEditing ? (
                  <div style={{ display: 'grid', gap: '0.45rem' }}>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                      <label style={{ fontSize: '0.78rem' }}>Label</label>
                      <input type="text" value={editLabel} onChange={e => setEditLabel(e.target.value)} disabled={editSaving} />
                    </div>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                      <label style={{ fontSize: '0.78rem' }}>Model ID</label>
                      <ModelPicker providerId={editPreset} value={editModel} onChange={setEditModel} disabled={editSaving} />
                    </div>
                    <div className="form-group" style={{ marginBottom: 0 }}>
                      <label style={{ fontSize: '0.78rem' }}>CLI command (optional — leave empty for PATH lookup)</label>
                      <input type="text" value={editCommand} onChange={e => setEditCommand(e.target.value)} disabled={editSaving} />
                    </div>
                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button type="button" className="btn btn-primary btn-sm" onClick={() => submitEdit(c.id)} disabled={editSaving}>
                        {editSaving ? 'Saving…' : 'Save'}
                      </button>
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => setEditingId(null)} disabled={editSaving}>
                        Cancel
                      </button>
                    </div>
                  </div>
                ) : (
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem' }}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: 'flex', gap: '0.4rem', alignItems: 'center', flexWrap: 'wrap' }}>
                        <strong>{c.label || preset.defaultLabel}</strong>
                        <span style={{ color: 'var(--text-muted)', fontSize: '0.78rem' }}>{preset.label}</span>
                        {c.is_primary && (
                          <span style={{
                            background: 'rgba(34,197,94,0.12)',
                            color: 'var(--success, #4ade80)',
                            borderRadius: '999px',
                            padding: '0.05rem 0.4rem',
                            fontSize: '0.72rem',
                          }}>
                            Primary
                          </span>
                        )}
                      </div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8rem', marginTop: '0.15rem' }}>
                        {c.cli_command ? <code>{c.cli_command}</code> : <em>PATH lookup</em>}
                        {' · '}
                        <code>{c.model_id}</code>
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: '0.35rem' }}>
                      {!c.is_primary && (
                        <button className="btn btn-ghost btn-sm" onClick={() => handleSetPrimary(c.id)}>
                          Set primary
                        </button>
                      )}
                      <button className="btn btn-ghost btn-sm" onClick={() => openEdit(c)}>Edit</button>
                      <button className="btn btn-ghost btn-sm" onClick={() => handleDelete(c.id)}>Delete</button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {showAddForm && (
        <div style={{ marginTop: '0.6rem', padding: '0.6rem', border: '1px solid var(--border)', borderRadius: '0.4rem' }}>
          <div style={{ fontSize: '0.85rem', marginBottom: '0.45rem', fontWeight: 600 }}>Add CLI</div>
          <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.6rem' }}>
            {(Object.keys(PRESETS) as PresetID[]).map(p => (
              <button
                key={p}
                type="button"
                className={`btn btn-sm ${addPreset === p ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => { setAddPreset(p); setAddModel(PRESETS[p].defaultModelId); }}
                disabled={addSaving}
              >
                {PRESETS[p].label}
              </button>
            ))}
          </div>
          <div className="form-group" style={{ marginBottom: '0.45rem' }}>
            <label style={{ fontSize: '0.78rem' }}>Label (optional)</label>
            <input
              type="text"
              value={addLabel}
              onChange={e => setAddLabel(e.target.value)}
              placeholder={PRESETS[addPreset].defaultLabel}
              disabled={addSaving}
            />
          </div>
          <div className="form-group" style={{ marginBottom: '0.45rem' }}>
            <label style={{ fontSize: '0.78rem' }}>Model ID</label>
            <ModelPicker providerId={addPreset} value={addModel} onChange={setAddModel} disabled={addSaving} />
          </div>
          <div className="form-group" style={{ marginBottom: '0.6rem' }}>
            <label style={{ fontSize: '0.78rem' }}>CLI command (optional — leave empty for PATH lookup)</label>
            <input
              type="text"
              value={addCommand}
              onChange={e => setAddCommand(e.target.value)}
              placeholder={PRESETS[addPreset].commandPlaceholder}
              disabled={addSaving}
            />
          </div>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button type="button" className="btn btn-primary btn-sm" onClick={submitAdd} disabled={addSaving}>
              {addSaving ? 'Saving…' : 'Create'}
            </button>
            <button type="button" className="btn btn-ghost btn-sm" onClick={() => setShowAddForm(false)} disabled={addSaving}>
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
