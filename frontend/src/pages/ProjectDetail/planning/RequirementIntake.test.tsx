import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RequirementIntake } from './RequirementIntake'

const emptyForm = { title: '', summary: '', description: '', source: 'human' }

describe('<RequirementIntake />', () => {
  it('renders the form always open when no requirements exist', () => {
    render(
      <RequirementIntake
        requirementCount={0}
        form={emptyForm}
        onFormChange={() => {}}
        creating={false}
        showForm={false}
        onToggleForm={() => {}}
        onSubmit={e => e.preventDefault()}
        onReset={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Capture Requirement/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /New Requirement/i })).not.toBeInTheDocument()
  })

  it('hides the form behind a toggle when requirements already exist', () => {
    render(
      <RequirementIntake
        requirementCount={2}
        form={emptyForm}
        onFormChange={() => {}}
        creating={false}
        showForm={false}
        onToggleForm={() => {}}
        onSubmit={e => e.preventDefault()}
        onReset={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /New Requirement/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Capture Requirement/i })).not.toBeInTheDocument()
  })

  it('disables submit when the title is blank or when creating is in flight', () => {
    const { rerender } = render(
      <RequirementIntake
        requirementCount={0}
        form={emptyForm}
        onFormChange={() => {}}
        creating={false}
        showForm={false}
        onToggleForm={() => {}}
        onSubmit={e => e.preventDefault()}
        onReset={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Capture Requirement/i })).toBeDisabled()

    rerender(
      <RequirementIntake
        requirementCount={0}
        form={{ ...emptyForm, title: 'Improve thing' }}
        onFormChange={() => {}}
        creating={true}
        showForm={false}
        onToggleForm={() => {}}
        onSubmit={e => e.preventDefault()}
        onReset={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Capturing/i })).toBeDisabled()
  })

  it('invokes onToggleForm when the New Requirement button is clicked', async () => {
    const onToggleForm = vi.fn()
    render(
      <RequirementIntake
        requirementCount={1}
        form={emptyForm}
        onFormChange={() => {}}
        creating={false}
        showForm={false}
        onToggleForm={onToggleForm}
        onSubmit={e => e.preventDefault()}
        onReset={() => {}}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /New Requirement/i }))
    expect(onToggleForm).toHaveBeenCalledTimes(1)
  })
})
