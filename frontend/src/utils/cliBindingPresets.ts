export type CliBindingPresetID = 'claude-code' | 'codex'

export interface CliModelOption {
  id: string
  label: string
}

export interface CliBindingPreset {
  id: CliBindingPresetID
  providerId: string
  label: string
  description: string
  defaultLabel: string
  defaultCliCommand: string
  modelPlaceholder: string
  defaultModelId: string
  knownModels: CliModelOption[]
  isUntested?: boolean
}

export const cliBindingPresets: CliBindingPreset[] = [
  {
    id: 'claude-code',
    providerId: 'cli:claude',
    label: 'Claude Code',
    description: 'Uses the `claude` CLI that ships with your Claude Code subscription. Runs on the machine where the connector daemon is running.',
    defaultLabel: 'My Claude',
    defaultCliCommand: 'claude',
    modelPlaceholder: 'claude-sonnet-4-6',
    defaultModelId: 'claude-sonnet-4-6',
    // Source: https://platform.claude.com/docs/en/docs/about-claude/models/overview
    knownModels: [
      { id: 'claude-opus-4-7',           label: 'Claude Opus 4.7 — most capable' },
      { id: 'claude-sonnet-4-6',         label: 'Claude Sonnet 4.6 — recommended' },
      { id: 'claude-haiku-4-5-20251001', label: 'Claude Haiku 4.5 — fastest' },
      { id: 'claude-opus-4-6',           label: 'Claude Opus 4.6 (legacy)' },
      { id: 'claude-sonnet-4-5',         label: 'Claude Sonnet 4.5 (legacy)' },
      { id: 'claude-opus-4-5',           label: 'Claude Opus 4.5 (legacy)' },
    ],
  },
  {
    id: 'codex',
    providerId: 'cli:codex',
    label: 'OpenAI Codex CLI',
    description: 'Uses the `codex` CLI. Requires a separate OpenAI account and subscription.',
    defaultLabel: 'My Codex',
    defaultCliCommand: 'codex',
    modelPlaceholder: 'gpt-5.5',
    defaultModelId: 'gpt-5.5',
    // The Codex CLI fetches its model catalog dynamically from OpenAI — this list
    // covers commonly seen IDs but may be incomplete. Use "Other" to enter any ID.
    // Verified from openai/codex repo tests + user confirmation (2026-04-25).
    knownModels: [
      { id: 'gpt-5.5',       label: 'GPT-5.5 — latest' },
      { id: 'gpt-5.4',       label: 'GPT-5.4' },
      { id: 'gpt-5.4-mini',  label: 'GPT-5.4 Mini' },
      { id: 'gpt-5.3-codex', label: 'GPT-5.3 Codex' },
      { id: 'gpt-5.2',       label: 'GPT-5.2 (older)' },
    ],
    isUntested: true,
  },
]

export function getCliBindingPreset(id: CliBindingPresetID): CliBindingPreset {
  return cliBindingPresets.find(p => p.id === id) ?? cliBindingPresets[0]
}

export function inferCliBindingPreset(providerId: string): CliBindingPresetID {
  return (cliBindingPresets.find(p => p.providerId === providerId)?.id) ?? 'claude-code'
}

export function knownModelsForProvider(providerId: 'cli:claude' | 'cli:codex'): CliModelOption[] {
  return cliBindingPresets.find(p => p.providerId === providerId)?.knownModels ?? []
}
