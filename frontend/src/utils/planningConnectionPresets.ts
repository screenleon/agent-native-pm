export type PlanningConnectionPresetID =
  | 'ollama-docker'
  | 'ollama-native'
  | 'lmstudio-docker'
  | 'lmstudio-native'
  | 'mistral-cloud'
  | 'custom-openai-compatible'

export type PlanningConnectionPreset = {
  id: PlanningConnectionPresetID
  label: string
  description: string
  baseURL: string
  defaultLabel: string
  modelPlaceholder: string
  configuredModelsPlaceholder: string
  apiKeyMode: 'hidden' | 'optional' | 'required'
  advancedOnly: boolean
}

export const planningConnectionFallbackPresetId: PlanningConnectionPresetID = 'ollama-docker'

export const planningConnectionInferenceFallbackPresetId: PlanningConnectionPresetID = 'custom-openai-compatible'

export const planningConnectionPresets: PlanningConnectionPreset[] = [
  {
    id: 'ollama-docker',
    label: 'Ollama via Docker',
    description: 'Recommended when this app runs in Docker Compose and Ollama runs on your host machine.',
    baseURL: 'http://host.docker.internal:11434/v1',
    defaultLabel: 'My Ollama',
    modelPlaceholder: 'llama3.2',
    configuredModelsPlaceholder: 'llama3.2, qwen3',
    apiKeyMode: 'hidden',
    advancedOnly: false,
  },
  {
    id: 'ollama-native',
    label: 'Ollama on This Machine',
    description: 'Use when the app itself is running on your host and talks to local Ollama directly.',
    baseURL: 'http://localhost:11434/v1',
    defaultLabel: 'My Ollama',
    modelPlaceholder: 'llama3.2',
    configuredModelsPlaceholder: 'llama3.2, qwen3',
    apiKeyMode: 'hidden',
    advancedOnly: false,
  },
  {
    id: 'lmstudio-docker',
    label: 'LM Studio via Docker',
    description: 'Recommended when this app runs in Docker Compose and LM Studio serves the OpenAI-compatible endpoint on your host.',
    baseURL: 'http://host.docker.internal:1234/v1',
    defaultLabel: 'My LM Studio',
    modelPlaceholder: 'local-model',
    configuredModelsPlaceholder: 'local-model',
    apiKeyMode: 'hidden',
    advancedOnly: false,
  },
  {
    id: 'lmstudio-native',
    label: 'LM Studio on This Machine',
    description: 'Use when the app itself is running on your host and LM Studio is already exposing a local OpenAI-compatible port.',
    baseURL: 'http://localhost:1234/v1',
    defaultLabel: 'My LM Studio',
    modelPlaceholder: 'local-model',
    configuredModelsPlaceholder: 'local-model',
    apiKeyMode: 'hidden',
    advancedOnly: false,
  },
  {
    id: 'mistral-cloud',
    label: 'Mistral AI (Cloud)',
    description: 'Hosted Mistral chat completions endpoint. Requires a Mistral API key from console.mistral.ai.',
    baseURL: 'https://api.mistral.ai/v1',
    defaultLabel: 'My Mistral',
    modelPlaceholder: 'mistral-large-latest',
    configuredModelsPlaceholder: 'mistral-large-latest, mistral-small-latest, codestral-latest, open-mistral-nemo, open-mixtral-8x22b, open-mixtral-8x7b, open-mistral-7b, pixtral-large-latest',
    apiKeyMode: 'required',
    advancedOnly: true,
  },
  {
    id: 'custom-openai-compatible',
    label: 'Custom OpenAI-Compatible API',
    description: 'For OpenAI-compatible gateways such as OpenRouter, self-hosted proxies, or other remote endpoints.',
    baseURL: '',
    defaultLabel: 'Custom Provider',
    modelPlaceholder: 'gpt-5-mini',
    configuredModelsPlaceholder: 'gpt-5-mini, gpt-4.1-mini',
    apiKeyMode: 'optional',
    advancedOnly: true,
  },
]

export function getPlanningConnectionPreset(id: PlanningConnectionPresetID): PlanningConnectionPreset {
  const exact = planningConnectionPresets.find(preset => preset.id === id)
  if (exact) return exact
  const fallback = planningConnectionPresets.find(preset => preset.id === planningConnectionFallbackPresetId)
  return fallback ?? planningConnectionPresets[0]
}

export function inferPlanningConnectionPreset(baseURL: string): PlanningConnectionPresetID {
  const normalizedBaseURL = normalizePresetBaseURL(baseURL)
  const matchedPreset = planningConnectionPresets.find(preset => preset.baseURL !== '' && normalizePresetBaseURL(preset.baseURL) === normalizedBaseURL)
  return matchedPreset?.id ?? planningConnectionInferenceFallbackPresetId
}

function normalizePresetBaseURL(value: string): string {
  return value.trim().replace(/\/+$/, '').toLowerCase()
}