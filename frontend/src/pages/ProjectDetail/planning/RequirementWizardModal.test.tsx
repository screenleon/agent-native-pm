import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { RequirementWizardModal } from './RequirementWizardModal'

function renderModal(overrides: Partial<React.ComponentProps<typeof RequirementWizardModal>> = {}) {
  const props = {
    initialTitle: 'Initial title',
    onSave: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  }
  return render(<RequirementWizardModal {...props} />)
}

describe('<RequirementWizardModal />', () => {
  it('T-6a-A3-1: renders with 3 fields and What pre-filled from initialTitle', () => {
    renderModal()
    expect(screen.getByLabelText(/What/i)).toHaveValue('Initial title')
    expect(screen.getByLabelText(/Who/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/How do we know/i)).toBeInTheDocument()
  })

  it('T-6a-A3-onSave: fill all fields then Save → onSave called with correct values', async () => {
    const { default: userEvent } = await import('@testing-library/user-event')
    const onSave = vi.fn()
    renderModal({ onSave })

    const whatInput = screen.getByLabelText(/What/i)
    await userEvent.clear(whatInput)
    await userEvent.type(whatInput, 'Build auth flow')

    const whoInput = screen.getByLabelText(/Who/i)
    await userEvent.type(whoInput, 'End users')

    const successInput = screen.getByLabelText(/How do we know/i)
    await userEvent.type(successInput, 'Login works')

    await userEvent.click(screen.getByRole('button', { name: /Save/i }))
    expect(onSave).toHaveBeenCalledWith('Build auth flow', 'End users', 'Login works')
  })
})
