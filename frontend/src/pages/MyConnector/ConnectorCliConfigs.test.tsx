import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConnectorCliConfigs } from './ConnectorCliConfigs';
import type { CliConfig } from '../../api/client';

// Phase 6a UX-B2 minimal coverage (Copilot review on PR #23).
// Asserts the empty state, a successful create, and set-primary behaviour
// against mocked client.ts endpoints.

const mockList = vi.fn();
const mockCreate = vi.fn();
const mockUpdate = vi.fn();
const mockDelete = vi.fn();
const mockSetPrimary = vi.fn();

vi.mock('../../api/client', () => ({
  listConnectorCliConfigs: (...args: unknown[]) => mockList(...args),
  createConnectorCliConfig: (...args: unknown[]) => mockCreate(...args),
  updateConnectorCliConfig: (...args: unknown[]) => mockUpdate(...args),
  deleteConnectorCliConfig: (...args: unknown[]) => mockDelete(...args),
  setPrimaryConnectorCliConfig: (...args: unknown[]) => mockSetPrimary(...args),
}));

function makeConfig(overrides: Partial<CliConfig> = {}): CliConfig {
  return {
    id: 'cfg-1',
    provider_id: 'cli:claude',
    model_id: 'claude-sonnet-4-6',
    label: 'My Claude',
    is_primary: true,
    created_at: '2026-04-23T00:00:00Z',
    updated_at: '2026-04-23T00:00:00Z',
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('<ConnectorCliConfigs />', () => {
  it('shows the empty state when the connector has no CLI configs', async () => {
    mockList.mockResolvedValue({ data: [] });

    render(<ConnectorCliConfigs connectorId="conn-1" />);

    await waitFor(() => {
      expect(screen.getByText(/No CLI configured on this machine yet/i)).toBeInTheDocument();
    });
    expect(mockList).toHaveBeenCalledWith('conn-1');
  });

  it('creates a new CLI config and reloads the list', async () => {
    mockList.mockResolvedValueOnce({ data: [] }); // initial load
    mockCreate.mockResolvedValue({ data: makeConfig() });
    mockList.mockResolvedValueOnce({ data: [makeConfig()] }); // reload after create

    render(<ConnectorCliConfigs connectorId="conn-1" />);

    await waitFor(() => {
      expect(screen.getByText(/No CLI configured on this machine yet/i)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /\+ Add CLI/i }));
    // Model field is pre-populated from the preset, so just submit.
    await userEvent.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockCreate).toHaveBeenCalledTimes(1);
    });
    expect(mockCreate).toHaveBeenCalledWith(
      'conn-1',
      expect.objectContaining({ provider_id: 'cli:claude', model_id: 'claude-sonnet-4-6' }),
    );
    // List was called twice: initial load + reload after create.
    expect(mockList).toHaveBeenCalledTimes(2);
  });

  it('promotes a non-primary config when "Set primary" is clicked', async () => {
    const primary = makeConfig({ id: 'cfg-1', label: 'Primary', is_primary: true });
    const secondary = makeConfig({
      id: 'cfg-2',
      label: 'Secondary',
      is_primary: false,
      provider_id: 'cli:codex',
      model_id: 'codex-mini',
    });
    mockList.mockResolvedValueOnce({ data: [primary, secondary] });
    mockSetPrimary.mockResolvedValue({ data: null });
    mockList.mockResolvedValueOnce({
      data: [{ ...primary, is_primary: false }, { ...secondary, is_primary: true }],
    });

    render(<ConnectorCliConfigs connectorId="conn-1" />);

    await waitFor(() => {
      expect(screen.getByText('Secondary')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: /Set primary/i }));

    await waitFor(() => {
      expect(mockSetPrimary).toHaveBeenCalledWith('conn-1', 'cfg-2');
    });
    expect(mockList).toHaveBeenCalledTimes(2);
  });
});
