import { useEffect, useState } from 'react';
import { Navigate } from 'react-router-dom';
import { getPlanningSettings, updatePlanningSettings } from '../api/client';
import type { PlanningSettingsView } from '../types';
import {
	getPlanningConnectionPreset,
	inferPlanningConnectionPreset,
	planningConnectionPresets,
	type PlanningConnectionPresetID,
} from '../utils/planningConnectionPresets';

type Props = {
	canEdit: boolean;
};

const deterministicModelId = 'deterministic-v1';

function parseConfiguredModels(value: string): string[] {
	return value
		.split(',')
		.map(item => item.trim())
		.filter(Boolean);
}

export default function ModelSettings({ canEdit }: Props) {
	const [view, setView] = useState<PlanningSettingsView | null>(null);
	const [loading, setLoading] = useState(true);
	const [saving, setSaving] = useState(false);
	const [error, setError] = useState('');
	const [success, setSuccess] = useState('');
	const [providerId, setProviderId] = useState('deterministic');
	const [modelId, setModelId] = useState(deterministicModelId);
	const [baseURL, setBaseURL] = useState('');
	const [configuredModelsText, setConfiguredModelsText] = useState(deterministicModelId);
	const [apiKey, setAPIKey] = useState('');
	const [clearAPIKey, setClearAPIKey] = useState(false);
	const [credentialMode, setCredentialMode] = useState('shared');
	const [selectedPresetId, setSelectedPresetId] = useState<PlanningConnectionPresetID>('ollama-docker');
	const [showAdvanced, setShowAdvanced] = useState(false);

	const selectedPreset = getPlanningConnectionPreset(selectedPresetId);

	useEffect(() => {
		if (!canEdit) return;
		let mounted = true;
		async function load() {
			try {
				const response = await getPlanningSettings();
				if (!mounted) return;
				setView(response.data);
				setProviderId(response.data.settings.provider_id);
				setModelId(response.data.settings.model_id);
				setBaseURL(response.data.settings.base_url);
				setConfiguredModelsText(response.data.settings.configured_models.join(', '));
				setCredentialMode(response.data.settings.credential_mode || 'shared');
				if (response.data.settings.provider_id === 'openai-compatible') {
					const presetId = inferPlanningConnectionPreset(response.data.settings.base_url);
					setSelectedPresetId(presetId);
					setShowAdvanced(getPlanningConnectionPreset(presetId).advancedOnly);
				}
				setError('');
			} catch (err) {
				if (!mounted) return;
				setError(err instanceof Error ? err.message : 'Failed to load model settings');
			} finally {
				if (mounted) setLoading(false);
			}
		}
		load();
		return () => {
			mounted = false;
		};
	}, [canEdit]);

	useEffect(() => {
		if (!canEdit || providerId !== 'openai-compatible') {
			return;
		}
		const configuredModels = parseConfiguredModels(configuredModelsText);
		if (configuredModels.length === 0) {
			return;
		}
		const trimmedModelId = modelId.trim();
		if (trimmedModelId === '' || trimmedModelId === deterministicModelId || !configuredModels.includes(trimmedModelId)) {
			setModelId(configuredModels[0]);
		}
	}, [canEdit, configuredModelsText, modelId, providerId]);

	if (!canEdit) {
		return <Navigate to="/" replace />;
	}

	function handleSelectPreset(nextPresetId: PlanningConnectionPresetID) {
		const previousPreset = getPlanningConnectionPreset(selectedPresetId);
		const nextPreset = getPlanningConnectionPreset(nextPresetId);
		setSelectedPresetId(nextPresetId);
		setProviderId('openai-compatible');
		setShowAdvanced(nextPreset.advancedOnly);
		setBaseURL(currentBaseURL => {
			const trimmed = currentBaseURL.trim();
			if (trimmed === '' || trimmed === previousPreset.baseURL) {
				return nextPreset.baseURL;
			}
			return currentBaseURL;
		});
	}

	async function handleSubmit(e: React.FormEvent) {
		e.preventDefault();
		setSaving(true);
		setError('');
		setSuccess('');
		try {
			const configuredModels = parseConfiguredModels(configuredModelsText);
			const response = await updatePlanningSettings({
				provider_id: providerId,
				model_id: providerId === 'deterministic' ? deterministicModelId : modelId.trim(),
				base_url: providerId === 'deterministic' ? '' : baseURL.trim(),
				configured_models: providerId === 'deterministic' ? [deterministicModelId] : configuredModels,
				...(apiKey.trim() ? { api_key: apiKey.trim() } : {}),
				...(clearAPIKey ? { clear_api_key: true } : {}),
				credential_mode: credentialMode,
			});
			setView(response.data);
			setProviderId(response.data.settings.provider_id);
			setModelId(response.data.settings.model_id);
			setBaseURL(response.data.settings.base_url);
			setConfiguredModelsText(response.data.settings.configured_models.join(', '));
			setCredentialMode(response.data.settings.credential_mode || 'shared');
			if (response.data.settings.provider_id === 'openai-compatible') {
				const presetId = inferPlanningConnectionPreset(response.data.settings.base_url);
				setSelectedPresetId(presetId);
				setShowAdvanced(getPlanningConnectionPreset(presetId).advancedOnly);
			}
			setAPIKey('');
			setClearAPIKey(false);
			setSuccess('Model settings updated. New planning runs will use this central configuration.');
		} catch (err) {
			setError(err instanceof Error ? err.message : 'Failed to update model settings');
		} finally {
			setSaving(false);
		}
	}

	return (
		<div className="page">
			<div className="page-header">
				<div>
					<h1>Model Settings</h1>
					<p style={{ margin: '0.35rem 0 0', color: 'var(--text-muted)' }}>
						Configure the workspace automation policy here. Planning runs resolve provider credentials from this shared policy plus each user&apos;s personal bindings when credential mode allows it.
					</p>
				</div>
			</div>

			{error && <div className="error-banner">{error}</div>}
			{success && <div className="alert alert-success">{success}</div>}

			<div className="card">
				<h2>Security</h2>
				<p>
					API keys are never returned after save. For private deployments, keep this app behind LAN-only access or a protected reverse proxy.
				</p>
				<p>
					If credential mode is set to personal, users manage their own provider access in My Bindings while this page still defines the workspace default provider and fallback behavior.
				</p>
				<p>
					Secret storage ready: <strong>{view?.secret_storage_ready ? 'Yes' : 'No'}</strong>
				</p>
			</div>

			<div className="card">
				<h2>Planning Provider</h2>
				<p style={{ marginTop: 0, color: 'var(--text-muted)' }}>
					This app can save provider settings that the server itself can call. Subscription logins handled only inside VS Code, Copilot Chat, or a local CLI are not directly reusable here unless you expose a local OpenAI-compatible bridge.
				</p>
				<div className="helper-note" style={{ marginBottom: '1rem' }}>
					If your team usually works through VS Code or CLI subscriptions, prefer one of the local presets below. That keeps setup close to your existing local model runtime and avoids asking admins to reason about raw URLs first.
				</div>
				{loading ? (
					<div className="loading">Loading…</div>
				) : (
					<form onSubmit={handleSubmit} className="stacked-form" style={{ display: 'grid', gap: '1rem' }}>
						<div className="form-group">
							<label>Provider</label>
							<select
								value={providerId}
								onChange={e => {
									const nextProviderId = e.target.value;
									setProviderId(nextProviderId);
									if (nextProviderId === 'deterministic') {
										setModelId(deterministicModelId);
										setShowAdvanced(false);
									} else {
										setShowAdvanced(getPlanningConnectionPreset(selectedPresetId).advancedOnly);
									}
								}}
								disabled={saving}
							>
								<option value="deterministic">Built-in Planning Fallback</option>
								<option value="openai-compatible">OpenAI-Compatible Planner</option>
							</select>
						</div>

						{providerId === 'openai-compatible' && (
							<>
								<div className="form-group">
									<label>Connection Template</label>
									<div className="preset-grid">
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
							</>
						)}

						<div className="form-group">
							<label>Default model</label>
							<input
								type="text"
								value={modelId}
								onChange={e => setModelId(e.target.value)}
								disabled={saving || providerId === 'deterministic'}
								placeholder={providerId === 'deterministic' ? 'deterministic-v1' : selectedPreset.modelPlaceholder}
							/>
							<small>When left blank, the first configured model becomes the default automatically.</small>
							{providerId === 'deterministic' && <small>This is an internal fallback engine, not a real external model such as Copilot, Codex, or ChatGPT.</small>}
						</div>

						{(providerId === 'deterministic' || showAdvanced) && (
							<>
								<div className="form-group">
									<label>Configured models</label>
									<input
										type="text"
										value={configuredModelsText}
										onChange={e => setConfiguredModelsText(e.target.value)}
										disabled={saving || providerId === 'deterministic'}
										placeholder={providerId === 'deterministic' ? 'deterministic-v1' : selectedPreset.configuredModelsPlaceholder}
									/>
									<small>Comma-separated model IDs exposed to the planning workspace.</small>
								</div>

								<div className="form-group">
									<label>Base URL</label>
									<input
										type="url"
										value={baseURL}
										onChange={e => setBaseURL(e.target.value)}
										disabled={saving || providerId === 'deterministic'}
										placeholder={selectedPreset.baseURL || 'https://api.openai.com/v1'}
									/>
								</div>

								<div className="form-group">
									<label>
										API key
										{providerId === 'openai-compatible' && selectedPreset.apiKeyMode === 'required' && !view?.settings.api_key_configured && (
											<span aria-label="required"> *</span>
										)}
									</label>
									<input
										type="password"
										value={apiKey}
										onChange={e => setAPIKey(e.target.value)}
										disabled={saving || providerId === 'deterministic'}
										placeholder={
											view?.settings.api_key_configured
												? 'Stored. Enter a new key only to rotate it.'
												: providerId === 'openai-compatible' && selectedPreset.apiKeyMode === 'required'
													? 'Required for this hosted provider'
													: 'Optional for LAN or local gateways'
										}
										required={providerId === 'openai-compatible' && selectedPreset.apiKeyMode === 'required' && !view?.settings.api_key_configured}
									/>
									<label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.5rem' }}>
										<input
											type="checkbox"
											checked={clearAPIKey}
											onChange={e => setClearAPIKey(e.target.checked)}
											disabled={saving || providerId === 'deterministic'}
										/>
										<span>Clear stored API key on save</span>
									</label>
									<small>Currently configured: {view?.settings.api_key_configured ? 'Yes' : 'No'}</small>
								</div>
							</>
						)}

						<div className="form-group">
							<label>Credential Mode</label>
							<select
								value={credentialMode}
								onChange={e => setCredentialMode(e.target.value)}
								disabled={saving}
							>
								<option value="shared">Shared — use server API key only</option>
								<option value="personal_preferred">Personal Preferred — try user binding, fall back to shared</option>
								<option value="personal_required">Personal Required — each user must bind their own key</option>
							</select>
							<small>Controls whether planning runs use the shared API key above or personal account bindings.</small>
						</div>

						<div className="form-group">
							<label>Last updated</label>
							<div className="planning-placeholder-note">
								{view?.settings.updated_at ? new Date(view.settings.updated_at).toLocaleString() : 'Never saved'}
								{view?.settings.updated_by ? ` by ${view.settings.updated_by}` : ''}
							</div>
						</div>

						<div>
							<button type="submit" className="btn btn-primary" disabled={saving}>
								{saving ? 'Saving…' : 'Save Model Settings'}
							</button>
						</div>
					</form>
				)}
			</div>
		</div>
	);
}