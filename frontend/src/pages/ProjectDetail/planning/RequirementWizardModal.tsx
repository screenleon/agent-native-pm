import { useState } from 'react'

interface Props {
  initialTitle: string
  onSave: (title: string, audience: string, successCriteria: string) => void
  onClose: () => void
}

export function RequirementWizardModal({ initialTitle, onSave, onClose }: Props) {
  const [title, setTitle] = useState(initialTitle)
  const [audience, setAudience] = useState('')
  const [successCriteria, setSuccessCriteria] = useState('')

  function handleSave() {
    if (!title.trim()) return
    onSave(title.trim(), audience.trim(), successCriteria.trim())
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="wizard-modal-title"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
    >
      <div className="card" style={{ width: '100%', maxWidth: '36rem', padding: '1.5rem', display: 'grid', gap: '1rem' }}>
        <h2 id="wizard-modal-title" style={{ margin: 0 }}>Refine Requirement</h2>

        <div className="form-group">
          <label htmlFor="wizard-what">What</label>
          <input
            id="wizard-what"
            type="text"
            value={title}
            onChange={e => setTitle(e.target.value)}
            placeholder="What are you building or fixing?"
          />
        </div>

        <div className="form-group">
          <label htmlFor="wizard-who">Who (audience)</label>
          <input
            id="wizard-who"
            type="text"
            value={audience}
            onChange={e => setAudience(e.target.value)}
            placeholder="Who will use this feature?"
          />
        </div>

        <div className="form-group">
          <label htmlFor="wizard-success">How do we know it worked?</label>
          <textarea
            id="wizard-success"
            value={successCriteria}
            onChange={e => setSuccessCriteria(e.target.value)}
            placeholder="Describe the success criteria…"
            rows={3}
            style={{ width: '100%', boxSizing: 'border-box', resize: 'vertical' }}
          />
        </div>

        <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
          <button className="btn btn-secondary" onClick={onClose} type="button">
            Cancel
          </button>
          <button className="btn btn-primary" onClick={handleSave} type="button" disabled={!title.trim()}>
            Save
          </button>
        </div>
      </div>
    </div>
  )
}
