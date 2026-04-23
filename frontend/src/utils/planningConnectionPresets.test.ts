import { describe, it, expect } from 'vitest'
import {
  getPlanningConnectionPreset,
  inferPlanningConnectionPreset,
  planningConnectionPresets,
} from './planningConnectionPresets'

describe('planningConnectionPresets', () => {
  it('exposes the expected preset ids', () => {
    const ids = planningConnectionPresets.map(preset => preset.id).sort()
    expect(ids).toEqual([
      'custom-openai-compatible',
      'lmstudio-docker',
      'lmstudio-native',
      'mistral-cloud',
      'ollama-docker',
      'ollama-native',
    ])
  })

  it('returns the matching preset by id', () => {
    const preset = getPlanningConnectionPreset('mistral-cloud')
    expect(preset.baseURL).toBe('https://api.mistral.ai/v1')
    expect(preset.modelPlaceholder).toBe('mistral-small-latest')
    expect(preset.advancedOnly).toBe(true)
  })

  it('falls back to the first preset when id is unknown', () => {
    // @ts-expect-error intentionally pass an unknown id to verify fallback behavior
    const preset = getPlanningConnectionPreset('does-not-exist')
    expect(preset.id).toBe('ollama-docker')
  })

  it('infers Mistral preset from its hosted base URL', () => {
    expect(inferPlanningConnectionPreset('https://api.mistral.ai/v1')).toBe('mistral-cloud')
    expect(inferPlanningConnectionPreset('https://api.mistral.ai/v1/')).toBe('mistral-cloud')
    expect(inferPlanningConnectionPreset('HTTPS://API.MISTRAL.AI/v1')).toBe('mistral-cloud')
  })

  it('infers custom preset for unmatched URLs', () => {
    expect(inferPlanningConnectionPreset('https://openrouter.ai/api/v1')).toBe('custom-openai-compatible')
    expect(inferPlanningConnectionPreset('')).toBe('custom-openai-compatible')
  })
})
