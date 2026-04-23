export type CliBindingPresetID = 'claude-code' | 'codex'

export interface CliBindingPreset {
  id: CliBindingPresetID
  providerId: string
  label: string
  description: string
  defaultLabel: string
  defaultCliCommand: string
  modelPlaceholder: string
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
    modelPlaceholder: 'claude-sonnet-4-5',
  },
  {
    id: 'codex',
    providerId: 'cli:codex',
    label: 'OpenAI Codex CLI',
    description: 'Uses the `codex` CLI. Requires a separate OpenAI account and subscription.',
    defaultLabel: 'My Codex',
    defaultCliCommand: 'codex',
    modelPlaceholder: 'codex-mini-latest',
    isUntested: true,
  },
]

export function getCliBindingPreset(id: CliBindingPresetID): CliBindingPreset {
  return cliBindingPresets.find(p => p.id === id) ?? cliBindingPresets[0]
}

export function inferCliBindingPreset(providerId: string): CliBindingPresetID {
  return (cliBindingPresets.find(p => p.providerId === providerId)?.id) ?? 'claude-code'
}
