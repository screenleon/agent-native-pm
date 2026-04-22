import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { PlanningRun } from '../../../types'
import { PlanningRunList } from './PlanningRunList'

function makeRun(overrides: Partial<PlanningRun> = {}): PlanningRun {
  return {
    id: 'run-1',
    project_id: 'p1',
    requirement_id: 'r1',
    status: 'queued',
    trigger_source: 'manual',
    provider_id: 'deterministic',
    model_id: 'deterministic-v1',
    selection_source: 'server_default',
    binding_source: 'shared',
    binding_label: '',
    execution_mode: 'server_provider',
    dispatch_status: 'none',
    dispatch_error: '',
    dispatch_expires_at: null,
    connector_id: null,
    connector_label: '',
    connector_cli_info: null,
    adapter_type: '',
    model_override: '',
    requested_by_user_id: 'u1',
    error_message: '',
    started_at: null,
    completed_at: null,
    created_at: '2026-04-22T00:00:00Z',
    updated_at: '2026-04-22T00:00:00Z',
    ...(overrides as Partial<PlanningRun>),
  } as PlanningRun
}

describe('<PlanningRunList />', () => {
  const baseProps = {
    selectedRunId: null,
    cancellingRunId: null,
    providerOptions: null,
    onSelectRun: () => {},
    onCancelRun: () => {},
  }

  it('renders the empty state when there are no runs', () => {
    render(<PlanningRunList {...baseProps} runs={[]} loading={false} errorMessage={null} />)
    expect(screen.getByText(/No planning runs yet/i)).toBeInTheDocument()
  })

  it('renders the error banner when an error message is supplied', () => {
    render(<PlanningRunList {...baseProps} runs={[]} loading={false} errorMessage="boom" />)
    expect(screen.getByText(/boom/i)).toBeInTheDocument()
  })

  it('renders the loading state when a fetch is in flight', () => {
    render(<PlanningRunList {...baseProps} runs={[]} loading={true} errorMessage={null} />)
    expect(screen.getByText(/Loading planning runs/i)).toBeInTheDocument()
  })

  it('surfaces a Cancel run button for active runs and fires onCancelRun', async () => {
    const onCancelRun = vi.fn()
    render(
      <PlanningRunList
        {...baseProps}
        runs={[makeRun({ status: 'running' })]}
        loading={false}
        errorMessage={null}
        onCancelRun={onCancelRun}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /Cancel run/i }))
    expect(onCancelRun).toHaveBeenCalledWith('run-1')
  })
})
