import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AttentionRow } from './AttentionRow'

function renderRow(overrides: Partial<React.ComponentProps<typeof AttentionRow>> = {}) {
  const base: React.ComponentProps<typeof AttentionRow> = {
    requirementsAwaitingPlanning: 0,
    candidatesAwaitingReview: 0,
    appliedOpenTasks: 0,
    openDriftCount: 0,
    onJumpToRequirements: vi.fn(),
    onJumpToCandidates: vi.fn(),
    onJumpToTasks: vi.fn(),
    onJumpToDrift: vi.fn(),
  }
  return {
    props: { ...base, ...overrides },
    ...render(<AttentionRow {...base} {...overrides} />),
  }
}

describe('<AttentionRow />', () => {
  it('renders all four tiles with their counts', () => {
    renderRow({
      requirementsAwaitingPlanning: 3,
      candidatesAwaitingReview: 5,
      appliedOpenTasks: 2,
      openDriftCount: 4,
    })
    expect(screen.getByRole('button', { name: /Requirements awaiting planning: 3/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Candidates awaiting review: 5/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Applied tasks still open: 2/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Open drift signals: 4/i })).toBeInTheDocument()
  })

  it('disables tiles whose count is zero', () => {
    renderRow({ requirementsAwaitingPlanning: 0, openDriftCount: 1 })
    expect(screen.getByRole('button', { name: /Requirements awaiting planning: 0/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Open drift signals: 1/i })).not.toBeDisabled()
  })

  it('fires onJumpToDrift when the drift tile is clicked', async () => {
    const onJumpToDrift = vi.fn()
    renderRow({ openDriftCount: 2, onJumpToDrift })
    await userEvent.click(screen.getByRole('button', { name: /Open drift signals: 2/i }))
    expect(onJumpToDrift).toHaveBeenCalledTimes(1)
  })

  it('fires onJumpToTasks when the applied-tasks tile is clicked', async () => {
    const onJumpToTasks = vi.fn()
    renderRow({ appliedOpenTasks: 1, onJumpToTasks })
    await userEvent.click(screen.getByRole('button', { name: /Applied tasks still open: 1/i }))
    expect(onJumpToTasks).toHaveBeenCalledTimes(1)
  })

  it('does not render the What\'s Next button when onRunWhatsnext is not provided', () => {
    renderRow()
    expect(screen.queryByRole('button', { name: /What's Next/i })).not.toBeInTheDocument()
  })

  it('renders the What\'s Next button when onRunWhatsnext is provided', () => {
    renderRow({ onRunWhatsnext: vi.fn(), whatsnextReady: true })
    expect(screen.getByRole('button', { name: /Run What's Next/i })).toBeInTheDocument()
  })

  it('disables the What\'s Next button when whatsnextReady is false', () => {
    renderRow({ onRunWhatsnext: vi.fn(), whatsnextReady: false })
    expect(screen.getByRole('button', { name: /Run What's Next/i })).toBeDisabled()
  })

  it('shows Analysing… and disables when runningWhatsnext is true', () => {
    renderRow({ onRunWhatsnext: vi.fn(), whatsnextReady: true, runningWhatsnext: true })
    const btn = screen.getByRole('button', { name: /Run What's Next/i })
    expect(btn).toHaveTextContent('Analysing…')
    expect(btn).toBeDisabled()
  })

  it('fires onRunWhatsnext when the button is clicked', async () => {
    const onRunWhatsnext = vi.fn()
    renderRow({ onRunWhatsnext, whatsnextReady: true })
    await userEvent.click(screen.getByRole('button', { name: /Run What's Next/i }))
    expect(onRunWhatsnext).toHaveBeenCalledTimes(1)
  })
})
