export interface Project {
  id: string;
  name: string;
  description: string;
  repo_url: string;
  repo_path: string;
  default_branch: string;
  last_sync_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectRepoMapping {
  id: string;
  project_id: string;
  alias: string;
  repo_path: string;
  default_branch: string;
  is_primary: boolean;
  created_at: string;
  updated_at: string;
}

export interface MirrorRepoCandidate {
  repo_name: string;
  repo_path: string;
  suggested_alias: string;
  detected_default_branch: string;
  is_mapped_to_project: boolean;
  is_primary_for_project: boolean;
}

export interface MirrorRepoDiscovery {
  mirror_root: string;
  repos: MirrorRepoCandidate[];
}

export interface CreateProjectPayload {
  name: string;
  description?: string;
  repo_url?: string;
  repo_path?: string;
  default_branch?: string;
  initial_repo_mapping?: {
    alias: string;
    repo_path: string;
    default_branch?: string;
  };
}

export interface Task {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: 'todo' | 'in_progress' | 'done' | 'cancelled';
  priority: 'low' | 'medium' | 'high';
  assignee: string;
  source: string;
  /** Phase 6b — connector execution lifecycle status. */
  dispatch_status?: 'none' | 'queued' | 'running' | 'completed' | 'failed';
  /** Phase 6b — raw JSON result from connector execution. */
  execution_result?: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

export interface Document {
  id: string;
  project_id: string;
  title: string;
  file_path: string;
  doc_type: 'api' | 'architecture' | 'guide' | 'adr' | 'general';
  last_updated_at: string | null;
  staleness_days: number;
  is_stale: boolean;
  source: string;
  created_at: string;
  updated_at: string;
}

export interface DocumentContent {
  path: string;
  language: string;
  content: string;
  size_bytes: number;
  truncated: boolean;
}

export interface ProjectSummary {
  project_id: string;
  snapshot_date: string;
  total_tasks: number;
  tasks_todo: number;
  tasks_in_progress: number;
  tasks_done: number;
  tasks_cancelled: number;
  total_documents: number;
  stale_documents: number;
  health_score: number;
}

export interface ProjectDashboardSummary {
  project_id: string;
  summary: ProjectSummary;
  latest_sync_run: SyncRun | null;
  open_drift_count: number;
  recent_agent_runs: AgentRun[];
  avg_planning_acceptance_rate?: number;
  planning_runs_reviewed_count?: number;
}

export interface Requirement {
  id: string;
  project_id: string;
  title: string;
  summary: string;
  description: string;
  status: 'draft' | 'planned' | 'archived';
  source: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRequirementPayload {
  title: string;
  summary?: string;
  description?: string;
  source?: string;
  audience?: string;
  success_criteria?: string;
}

export interface CreatePlanningRunPayload {
  trigger_source?: string;
  provider_id?: string;
  model_id?: string;
  execution_mode?: PlanningExecutionMode;
  adapter_type?: string;
  model_override?: string;
  // Phase 3 Path B legacy — references a cli:* account_bindings row. Still
  // works; prefer connector_id + cli_config_id for new code.
  account_binding_id?: string;
  // Phase 6a UX-B3: per-connector CLI config selection. Both must be set
  // together; only valid for execution_mode === "local_connector".
  connector_id?: string;
  cli_config_id?: string;
}

export type PlanningExecutionMode = 'deterministic' | 'server_provider' | 'local_connector';

export type PlanningDispatchStatus = 'not_required' | 'queued' | 'leased' | 'returned' | 'expired';

export interface PlanningSettings {
  provider_id: string;
  model_id: string;
  base_url: string;
  configured_models: string[];
  api_key_configured: boolean;
  credential_mode: string;
  updated_by: string;
  created_at: string | null;
  updated_at: string | null;
}

export interface PlanningSettingsView {
  settings: PlanningSettings;
  secret_storage_ready: boolean;
}

export interface UpdatePlanningSettingsPayload {
  provider_id: string;
  model_id: string;
  base_url: string;
  configured_models: string[];
  api_key?: string;
  clear_api_key?: boolean;
  credential_mode?: string;
}

export interface AccountBinding {
  id: string;
  user_id: string;
  provider_id: string;
  label: string;
  base_url: string;
  model_id: string;
  configured_models: string[];
  api_key_configured: boolean;
  is_active: boolean;
  cli_command: string;
  is_primary: boolean;
  created_at: string;
  updated_at: string;
  // Probe history (migration 024); null until first probe.
  last_probe_at: string | null;
  last_probe_ok: boolean | null;
  last_probe_ms: number | null;
}

export interface CreateAccountBindingPayload {
  provider_id: string;
  label?: string;
  base_url: string;
  model_id: string;
  configured_models?: string[];
  api_key?: string;
  cli_command?: string;
  is_primary?: boolean;
}

export interface UpdateAccountBindingPayload {
  label?: string;
  base_url?: string;
  model_id?: string;
  configured_models?: string[];
  api_key?: string;
  clear_api_key?: boolean;
  is_active?: boolean;
  is_primary?: boolean;
  cli_command?: string;
}

export interface LocalConnector {
  id: string;
  user_id: string;
  label: string;
  platform: string;
  client_version: string;
  status: 'pending' | 'online' | 'offline' | 'revoked';
  capabilities: Record<string, unknown>;
  metadata?: {
    cli_last_healthy_at?: string;
  };
  last_seen_at: string | null;
  last_error: string;
  created_at: string;
  updated_at: string;
}

export type ConnectorPhase =
  | 'idle'
  | 'claiming_run'
  | 'planning'
  | 'claiming_task'
  | 'dispatching'
  | 'submitting';

export interface ConnectorActivity {
  phase: ConnectorPhase;
  subject_kind?: string;
  subject_id?: string;
  subject_title?: string;
  role_id?: string;
  step?: string;
  started_at: string;
  updated_at: string;
}

export interface ConnectorActivityResponse {
  activity: ConnectorActivity | null;
  online: boolean;
  age_seconds: number;
}

export interface ActiveConnectorEntry {
  connector_id: string;
  label: string;
  activity: ConnectorActivity | null;
  online: boolean;
  age_seconds: number;
}

export interface ConnectorPairingSession {
  id: string;
  user_id: string;
  label: string;
  status: 'pending' | 'claimed' | 'expired' | 'cancelled';
  expires_at: string;
  connector_id?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateLocalConnectorPairingSessionPayload {
  label?: string;
}

export interface CreateLocalConnectorPairingSessionResponse {
  session: ConnectorPairingSession;
  pairing_code: string;
}

export interface PlanningProviderModel {
  id: string;
  label: string;
  description: string;
  enabled: boolean;
}

export interface PlanningProviderDescriptor {
  id: string;
  label: string;
  kind: string;
  description: string;
  default_model_id: string;
  models: PlanningProviderModel[];
}

export interface PlanningProviderSelection {
  provider_id: string;
  model_id: string;
  selection_source: 'server_default' | 'request_override';
  binding_source?: 'system' | 'shared' | 'personal';
  binding_label?: string;
}

export interface PlanningProviderOptions {
  default_selection: PlanningProviderSelection;
  providers: PlanningProviderDescriptor[];
  credential_mode: 'shared' | 'personal_preferred' | 'personal_required';
  resolved_binding_source?: 'system' | 'shared' | 'personal';
  resolved_binding_label?: string;
  available_execution_modes: PlanningExecutionMode[];
  paired_connector_available: boolean;
  active_connector_label?: string;
  can_run: boolean;
  unavailable_reason?: string;
  allow_model_override: boolean;
}

export interface PlanningRun {
  id: string;
  project_id: string;
  requirement_id: string;
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled';
  trigger_source: string;
  provider_id: string;
  model_id: string;
  selection_source: 'server_default' | 'request_override';
  binding_source: 'system' | 'shared' | 'personal';
  binding_label?: string;
  requested_by_user_id?: string;
  execution_mode: PlanningExecutionMode;
  dispatch_status: PlanningDispatchStatus;
  connector_id?: string;
  connector_label?: string;
  lease_expires_at: string | null;
  dispatch_error: string;
  error_message: string;
  adapter_type?: string;
  model_override?: string;
  connector_cli_info?: {
    cli_invocation?: {
      agent: string;
      model?: string;
      model_source?: string;
    };
    binding_snapshot?: {
      provider_id: string;
      model_id?: string;
      cli_command?: string;
      label?: string;
      is_primary: boolean;
    };
    dispatch_warning?: string;
    error_kind?: string;
    remediation_hint?: string;
    agent?: string;
    model?: string;
    model_source?: string;
  };
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
  // Phase 3B PR-3: quality summary — only populated on single-run GET
  // (GET /api/planning-runs/:id), not on list responses.
  quality_summary?: QualitySummary;
}

export interface PlanningDocumentEvidence {
  document_id: string;
  title: string;
  file_path: string;
  doc_type: string;
  is_stale: boolean;
  staleness_days: number;
  matched_keywords: string[];
  contribution_reasons: string[];
}

export interface PlanningDriftSignalEvidence {
  drift_signal_id: string;
  document_id: string;
  document_title: string;
  severity: number;
  trigger_type: string;
  trigger_detail: string;
  contribution_reasons: string[];
}

export interface PlanningSyncRunEvidence {
  sync_run_id: string;
  status: string;
  commits_scanned: number;
  files_changed: number;
  error_message: string;
  contribution_reasons: string[];
}

export interface PlanningAgentRunEvidence {
  agent_run_id: string;
  agent_name: string;
  action_type: string;
  status: string;
  summary: string;
  error_message: string;
  contribution_reasons: string[];
}

export interface PlanningDuplicateEvidence {
  title: string;
  contribution_reasons: string[];
}

export interface PlanningScoreBreakdown {
  impact: number;
  urgency: number;
  dependency_unlock: number;
  risk_reduction: number;
  effort: number;
  confidence_seed: number;
  evidence_bonus: number;
  duplicate_penalty: number;
  final_priority_score: number;
  final_confidence: number;
}

export interface PlanningEvidenceDetail {
  summary: string[];
  documents: PlanningDocumentEvidence[];
  drift_signals: PlanningDriftSignalEvidence[];
  sync_run: PlanningSyncRunEvidence | null;
  agent_runs: PlanningAgentRunEvidence[];
  duplicates: PlanningDuplicateEvidence[];
  score_breakdown: PlanningScoreBreakdown;
}

export interface BacklogCandidate {
  id: string;
  project_id: string;
  requirement_id: string;
  planning_run_id: string;
  parent_candidate_id?: string;
  suggestion_type: string;
  title: string;
  description: string;
  status: 'draft' | 'approved' | 'rejected' | 'applied';
  rationale: string;
  validation_criteria?: string;
  po_decision?: string;
  priority_score: number;
  confidence: number;
  rank: number;
  evidence: string[];
  evidence_detail: PlanningEvidenceDetail;
  duplicate_titles: string[];
  // Phase 5 B2 + Phase 6c PR-2: execution specialist hint (nullable).
  // Phase 6c PR-2 added catalog enforcement — non-empty values must
  // match a role in /api/roles.
  execution_role: string | null;
  // Phase 6c PR-2: latest actor_audit row for this candidate's
  // execution_role field, populated server-side. Nil when no audit
  // row exists (pre-Phase-6c data; never set; cleared).
  execution_role_authoring?: ExecutionRoleAuthoring | null;
  // Phase 3B PR-3: optional operator feedback on the PO decision.
  feedback_kind?: string;
  feedback_note?: string;
  created_at: string;
  updated_at: string;
}

// Phase 6c PR-2: read-side projection of the authoring trail.
export interface ExecutionRoleAuthoring {
  actor_kind: 'user' | 'api_key' | 'router' | 'system' | 'connector';
  actor_id?: string;
  rationale?: string;
  confidence?: number; // router-only
  set_at: string;
}

// Phase 3B PR-3: per-run quality summary computed server-side from
// backlog_candidates. Only populated on single-run GET responses.
export interface QualitySummary {
  total: number;
  approved: number;
  rejected: number;
  pending: number;
  acceptance_rate: number;
  feedback_distribution: Record<string, number>;
}

export interface TaskLineage {
  id: string;
  project_id: string;
  task_id: string;
  requirement_id?: string;
  planning_run_id?: string;
  backlog_candidate_id?: string;
  lineage_kind: 'applied_candidate' | 'manual_requirement' | 'merged_requirement';
  created_at: string;
}

export interface UpdateBacklogCandidatePayload {
  title?: string;
  description?: string;
  status?: 'draft' | 'approved' | 'rejected';
  // Phase 5 B2: set to a role id to earmark the candidate for auto-dispatch;
  // empty string clears (NULL in DB). Not validated against the role catalog
  // on the server today; see DECISIONS.md 2026-04-24 Phase 5 B2.
  execution_role?: string;
  // Phase 3B PR-3: optional quality feedback — never required, never blocks
  // approve/reject flow.
  feedback_kind?: string;
  feedback_note?: string;
}

export interface ApplyBacklogCandidateResponse {
  task: Task;
  candidate: BacklogCandidate;
  lineage: TaskLineage;
  already_applied: boolean;
}

export interface CandidateEvidenceSummary {
  id: string;
  title: string;
  status: BacklogCandidate['status'];
  planning_run_id: string;
  requirement_id: string;
  requirement_title: string;
}

export interface BatchUpdateTaskChanges {
  status?: Task['status'];
  priority?: Task['priority'];
  assignee?: string;
}

export interface BatchUpdateTaskResponse {
  updated_count: number;
  tasks: Task[];
}

export interface ApiResponse<T> {
  data: T;
  error: string | null;
  meta: PaginationMeta | null;
}

export interface PaginationMeta {
  page: number;
  per_page: number;
  total: number;
}

  // Phase 2 types
  export interface SyncRun {
    id: string;
    project_id: string;
    started_at: string;
    completed_at: string | null;
    status: 'running' | 'completed' | 'failed';
    commits_scanned: number;
    files_changed: number;
    error_message: string;
  }

  export interface AgentRun {
    id: string;
    project_id: string;
    agent_name: string;
    action_type: 'create' | 'update' | 'review' | 'sync';
    status: 'running' | 'completed' | 'failed';
    summary: string;
    files_affected: string[];
    needs_human_review: boolean;
    started_at: string;
    completed_at: string | null;
    error_message: string;
    idempotency_key: string | null;
    created_at: string;
  }

  export interface TriggerMeta {
    /** Present on code_change signals — structured list of changed files. */
    changed_files?: Array<{ path: string; change_type: string }>;
    /** "high" = document has an explicit link; "medium" = registry path match only; "low" = heuristic. */
    confidence?: 'high' | 'medium' | 'low';
    /** Present on time_decay signals — number of days since last update. */
    days_stale?: number;
  }

  export interface DriftSignal {
    id: string;
    project_id: string;
    document_id: string | null;
    document_title: string;
    trigger_type: 'code_change' | 'time_decay' | 'manual';
    trigger_detail: string;
    trigger_meta?: TriggerMeta;
    /** 1 = low, 2 = medium, 3 = high — used for triage sorting. */
    severity: number;
    sync_run_id?: string;
    status: 'open' | 'resolved' | 'dismissed';
    resolved_by: string;
    resolved_at: string | null;
    created_at: string;
  }

  export interface DocumentLink {
    id: string;
    document_id: string;
    code_path: string;
    link_type: 'covers' | 'references' | 'depends_on';
    created_at: string;
  }

  // Phase 3 types
  export interface APIKey {
    id: string;
    project_id: string | null;
    label: string;
    is_active: boolean;
    last_used_at: string | null;
    created_at: string;
  }

  export interface APIKeyWithSecret extends APIKey {
    key: string;
  }

  // Phase 4 types
  export interface User {
    id: string;
    username: string;
    email: string;
    role: 'admin' | 'member' | 'viewer';
    is_active: boolean;
    created_at: string;
  }

  export interface Notification {
    id: string;
    user_id: string;
    project_id: string | null;
    kind: string;
    title: string;
    body: string;
    is_read: boolean;
    link: string;
    created_at: string;
  }

  export interface SearchResult {
    tasks: Task[];
    documents: Document[];
  }
