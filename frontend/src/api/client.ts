import type {
  ApiResponse, Project, Task, Document, ProjectSummary,
  SyncRun, AgentRun, DriftSignal, DocumentLink, DocumentContent,
  APIKey, APIKeyWithSecret, User, Notification, SearchResult, ProjectRepoMapping,
  CreateProjectPayload, MirrorRepoDiscovery, ProjectDashboardSummary,
  BatchUpdateTaskChanges, BatchUpdateTaskResponse, Requirement, CreateRequirementPayload, PlanningRun, BacklogCandidate, UpdateBacklogCandidatePayload, ApplyBacklogCandidateResponse, CreatePlanningRunPayload, PlanningProviderOptions,
  PlanningSettingsView, UpdatePlanningSettingsPayload,
  AccountBinding, CreateAccountBindingPayload, UpdateAccountBindingPayload,
  LocalConnector, CreateLocalConnectorPairingSessionPayload, CreateLocalConnectorPairingSessionResponse,
} from '../types';

const BASE_URL = '/api';

function getToken(): string {
  return localStorage.getItem('anpm_token') || '';
}

async function request<T>(path: string, options?: RequestInit): Promise<ApiResponse<T>> {
  const token = getToken();
  const authHeaders: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {};
  const res = await fetch(`${BASE_URL}${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...authHeaders },
    ...options,
    // merge headers so callers can't accidentally strip Content-Type
    ...(options?.headers ? { headers: { 'Content-Type': 'application/json', ...authHeaders, ...options.headers } } : {}),
  });
  const json = await res.json();
  if (!res.ok) {
    throw new Error(json.error || `Request failed with status ${res.status}`);
  }
  return json;
}

export async function getMeta() {
  return request<{ local_mode: boolean; project_id: string; project_name: string; port: string }>('/meta');
}

export async function checkNeedsSetup() {
  return request<{ needs_setup: boolean }>('/auth/needs-setup');
}

// Projects
export async function listProjects() {
  return request<Project[]>('/projects');
}

export async function getProject(id: string) {
  return request<Project>(`/projects/${encodeURIComponent(id)}`);
}

export async function createProject(data: CreateProjectPayload) {
  return request<Project>('/projects', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateProject(id: string, data: Partial<Project>) {
  return request<Project>(`/projects/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deleteProject(id: string) {
  return request<null>(`/projects/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export async function listProjectRepoMappings(projectId: string) {
  return request<ProjectRepoMapping[]>(`/projects/${encodeURIComponent(projectId)}/repo-mappings`);
}

export async function createProjectRepoMapping(projectId: string, data: Partial<ProjectRepoMapping>) {
  return request<ProjectRepoMapping>(`/projects/${encodeURIComponent(projectId)}/repo-mappings`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateProjectRepoMapping(id: string, data: Partial<ProjectRepoMapping>) {
  return request<ProjectRepoMapping>(`/repo-mappings/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deleteProjectRepoMapping(id: string) {
  return request<null>(`/repo-mappings/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function discoverMirrorRepos(projectId?: string) {
  const params = new URLSearchParams();
  if (projectId) {
    params.set('project_id', projectId);
  }
  const query = params.toString();
  return request<MirrorRepoDiscovery>(`/repo-mappings/discover${query ? `?${query}` : ''}`);
}

// Tasks
export async function listTasks(projectId: string, page = 1, perPage = 20, sort?: string, order?: string) {
  const params = new URLSearchParams({
    page: page.toString(),
    per_page: perPage.toString(),
    ...(sort && { sort }),
    ...(order && { order }),
  });
  return request<Task[]>(`/projects/${encodeURIComponent(projectId)}/tasks?${params}`);
}

export type TaskListFilters = {
  status?: string;
  priority?: string;
  assignee?: string;
};

export async function listTasksFiltered(
  projectId: string,
  page = 1,
  perPage = 20,
  sort?: string,
  order?: string,
  filters?: TaskListFilters,
) {
  const params = new URLSearchParams({
    page: page.toString(),
    per_page: perPage.toString(),
    ...(sort && { sort }),
    ...(order && { order }),
    ...(filters?.status && { status: filters.status }),
    ...(filters?.priority && { priority: filters.priority }),
    ...(filters?.assignee && { assignee: filters.assignee }),
  });
  return request<Task[]>(`/projects/${encodeURIComponent(projectId)}/tasks?${params}`);
}

export async function getTask(id: string) {
  return request<Task>(`/tasks/${encodeURIComponent(id)}`);
}

export async function createTask(projectId: string, data: Partial<Task>) {
  return request<Task>(`/projects/${encodeURIComponent(projectId)}/tasks`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateTask(id: string, data: Partial<Task>) {
  return request<Task>(`/tasks/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deleteTask(id: string) {
  return request<null>(`/tasks/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// Documents
export async function listDocuments(projectId: string, page = 1, perPage = 20) {
  return request<Document[]>(`/projects/${encodeURIComponent(projectId)}/documents?page=${page}&per_page=${perPage}`);
}

export async function getDocument(id: string) {
  return request<Document>(`/documents/${encodeURIComponent(id)}`);
}

export async function getDocumentContent(id: string) {
  return request<DocumentContent>(`/documents/${encodeURIComponent(id)}/content`);
}

export async function createDocument(projectId: string, data: Partial<Document>) {
  return request<Document>(`/projects/${encodeURIComponent(projectId)}/documents`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateDocument(id: string, data: Partial<Document>) {
  return request<Document>(`/documents/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deleteDocument(id: string) {
  return request<null>(`/documents/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

// Summary
export async function getProjectSummary(projectId: string) {
  return request<ProjectSummary>(`/projects/${encodeURIComponent(projectId)}/summary`);
}

export async function getProjectDashboardSummary(projectId: string) {
  return request<ProjectDashboardSummary>(`/projects/${encodeURIComponent(projectId)}/dashboard-summary`);
}

export async function getSummaryHistory(projectId: string) {
  return request<ProjectSummary[]>(`/projects/${encodeURIComponent(projectId)}/summary/history`);
}

export async function batchUpdateTasks(projectId: string, taskIds: string[], changes: BatchUpdateTaskChanges) {
  return request<BatchUpdateTaskResponse>(`/projects/${encodeURIComponent(projectId)}/tasks/batch-update`, {
    method: 'POST',
    body: JSON.stringify({ task_ids: taskIds, changes }),
  });
}

// Requirements
export async function listRequirements(projectId: string, page = 1, perPage = 100) {
  return request<Requirement[]>(`/projects/${encodeURIComponent(projectId)}/requirements?page=${page}&per_page=${perPage}`);
}

export async function getRequirement(id: string) {
  return request<Requirement>(`/requirements/${encodeURIComponent(id)}`);
}

export async function createRequirement(projectId: string, data: CreateRequirementPayload) {
  return request<Requirement>(`/projects/${encodeURIComponent(projectId)}/requirements`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getPlanningProviderOptions(projectId: string) {
  return request<PlanningProviderOptions>(`/projects/${encodeURIComponent(projectId)}/planning-provider-options`);
}

export async function createPlanningRun(requirementId: string, data: CreatePlanningRunPayload = {}) {
  return request<PlanningRun>(`/requirements/${encodeURIComponent(requirementId)}/planning-runs`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getPlanningSettings() {
  return request<PlanningSettingsView>('/settings/planning');
}

export async function updatePlanningSettings(data: UpdatePlanningSettingsPayload) {
  return request<PlanningSettingsView>('/settings/planning', {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function listPlanningRuns(requirementId: string, page = 1, perPage = 20) {
  return request<PlanningRun[]>(`/requirements/${encodeURIComponent(requirementId)}/planning-runs?page=${page}&per_page=${perPage}`);
}

export async function getPlanningRun(id: string) {
  return request<PlanningRun>(`/planning-runs/${encodeURIComponent(id)}`);
}

export interface AppliedLineageEntry {
  lineage_id: string
  project_id: string
  task_id: string
  task_title: string
  task_status: string
  requirement_id?: string
  requirement_title?: string
  planning_run_id?: string
  planning_run_status?: string
  backlog_candidate_id?: string
  backlog_candidate_title?: string
  lineage_kind: string
  created_at: string
}

export async function listProjectTaskLineage(projectId: string) {
  return request<AppliedLineageEntry[]>(`/projects/${encodeURIComponent(projectId)}/task-lineage`);
}

export async function cancelPlanningRun(id: string) {
  return request<PlanningRun>(`/planning-runs/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
  });
}

export async function listPlanningRunBacklogCandidates(planningRunId: string, page = 1, perPage = 50) {
  return request<BacklogCandidate[]>(`/planning-runs/${encodeURIComponent(planningRunId)}/backlog-candidates?page=${page}&per_page=${perPage}`);
}

export async function updateBacklogCandidate(id: string, data: UpdateBacklogCandidatePayload) {
  return request<BacklogCandidate>(`/backlog-candidates/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function applyBacklogCandidate(id: string) {
  return request<ApplyBacklogCandidateResponse>(`/backlog-candidates/${encodeURIComponent(id)}/apply`, {
    method: 'POST',
  });
}

// ─── Phase 2: Sync ──────────────────────────────────────────────────────────
export async function triggerSync(projectId: string) {
  return request<SyncRun>(`/projects/${encodeURIComponent(projectId)}/sync`, { method: 'POST' });
}
export async function listSyncRuns(projectId: string) {
  return request<SyncRun[]>(`/projects/${encodeURIComponent(projectId)}/sync-runs`);
}

// ─── Phase 2: Agent runs ──────────────────────────────────────────────────────
export async function createAgentRun(data: Partial<AgentRun>) {
  return request<AgentRun>('/agent-runs', { method: 'POST', body: JSON.stringify(data) });
}
export async function listAgentRuns(projectId: string) {
  return request<AgentRun[]>(`/projects/${encodeURIComponent(projectId)}/agent-runs`);
}
export async function getAgentRun(id: string) {
  return request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}`);
}
export async function updateAgentRun(id: string, data: Partial<AgentRun>) {
  return request<AgentRun>(`/agent-runs/${encodeURIComponent(id)}`, {
    method: 'PATCH', body: JSON.stringify(data),
  });
}

// ─── Phase 2: Drift signals ──────────────────────────────────────────────────
export async function listDriftSignals(projectId: string, status?: string) {
  const qs = status ? `?status=${encodeURIComponent(status)}` : '';
  return request<DriftSignal[]>(`/projects/${encodeURIComponent(projectId)}/drift-signals${qs}`);
}
export async function createDriftSignal(projectId: string, data: Partial<DriftSignal>) {
  return request<DriftSignal>(`/projects/${encodeURIComponent(projectId)}/drift-signals`, {
    method: 'POST', body: JSON.stringify(data),
  });
}
export async function updateDriftSignal(id: string, data: { status?: string; resolved_by?: string }) {
  return request<DriftSignal>(`/drift-signals/${encodeURIComponent(id)}`, {
    method: 'PATCH', body: JSON.stringify(data),
  });
}
export async function bulkResolveDriftSignals(projectId: string, resolvedBy = 'human') {
  return request<{ resolved: number }>(`/projects/${encodeURIComponent(projectId)}/drift-signals/resolve-all`, {
    method: 'POST', body: JSON.stringify({ resolved_by: resolvedBy }),
  });
}

// ─── Phase 2: Document links ─────────────────────────────────────────────────
export async function listDocumentLinks(documentId: string) {
  return request<DocumentLink[]>(`/documents/${encodeURIComponent(documentId)}/links`);
}
export async function createDocumentLink(documentId: string, data: Partial<DocumentLink>) {
  return request<DocumentLink>(`/documents/${encodeURIComponent(documentId)}/links`, {
    method: 'POST', body: JSON.stringify(data),
  });
}
export async function deleteDocumentLink(id: string) {
  return request<null>(`/document-links/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// ─── Phase 3: API Keys ───────────────────────────────────────────────────────
export async function listAPIKeys(projectId?: string) {
  const qs = projectId ? `?project_id=${encodeURIComponent(projectId)}` : '';
  return request<APIKey[]>(`/keys${qs}`);
}
export async function createAPIKey(data: { label: string; project_id?: string }) {
  return request<APIKeyWithSecret>('/keys', { method: 'POST', body: JSON.stringify(data) });
}
export async function revokeAPIKey(id: string) {
  return request<null>(`/keys/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// ─── Phase 3: Document refresh ───────────────────────────────────────────────
export async function refreshDocumentSummary(id: string) {
  return request<Document>(`/documents/${encodeURIComponent(id)}/refresh-summary`, { method: 'POST' });
}

// ─── Phase 4: Auth ───────────────────────────────────────────────────────────
export async function register(data: { username: string; email: string; password: string }) {
  return request<User>('/auth/register', { method: 'POST', body: JSON.stringify(data) });
}
export async function login(data: { username: string; password: string }) {
  return request<{ token: string; user: User }>('/auth/login', {
    method: 'POST', body: JSON.stringify(data), credentials: 'include',
  });
}
export async function logout() {
  return request<null>('/auth/logout', { method: 'POST', credentials: 'include' });
}
export async function getMe() {
  return request<User>('/auth/me', { credentials: 'include' });
}

// ─── Phase 4: Users ──────────────────────────────────────────────────────────
export async function listUsers() {
  return request<User[]>('/users');
}

// ─── Phase 4: Notifications ──────────────────────────────────────────────────
export async function listNotifications(unreadOnly = false) {
  return request<Notification[]>(`/notifications${unreadOnly ? '?unread_only=true' : ''}`);
}
export async function markNotificationRead(id: string) {
  return request<Notification>(`/notifications/${encodeURIComponent(id)}/read`, { method: 'PATCH' });
}
export async function markNotificationUnread(id: string) {
  return request<null>(`/notifications/${encodeURIComponent(id)}/unread`, { method: 'PATCH' });
}
export async function markAllNotificationsRead() {
  return request<null>('/notifications/read-all', { method: 'POST' });
}
export async function getUnreadCount() {
  return request<{ unread: number }>('/notifications/unread-count');
}

// ─── Phase 4: Search ─────────────────────────────────────────────────────────
export interface SearchFilters {
  projectId?: string;
  type?: 'all' | 'tasks' | 'documents';
  status?: 'todo' | 'in_progress' | 'done' | 'cancelled';
  docType?: 'api' | 'architecture' | 'guide' | 'adr' | 'general';
  staleness?: 'all' | 'stale' | 'fresh';
}

export async function search(q: string, filters: SearchFilters = {}) {
  const qs = new URLSearchParams({ q });
  if (filters.projectId) qs.set('project_id', filters.projectId);
  if (filters.type && filters.type !== 'all') qs.set('type', filters.type);
  if (filters.status) qs.set('status', filters.status);
  if (filters.docType) qs.set('doc_type', filters.docType);
  if (filters.staleness && filters.staleness !== 'all') qs.set('staleness', filters.staleness);
  return request<SearchResult>(`/search?${qs}`);
}

// ─── Account Bindings (Personal Credentials) ────────────────────────────────
export async function listAccountBindings() {
  return request<AccountBinding[]>('/me/account-bindings');
}
export async function createAccountBinding(data: CreateAccountBindingPayload) {
  return request<AccountBinding>('/me/account-bindings', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}
export async function updateAccountBinding(id: string, data: UpdateAccountBindingPayload) {
  return request<AccountBinding>(`/me/account-bindings/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}
export async function deleteAccountBinding(id: string) {
  return request<null>(`/me/account-bindings/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export async function listLocalConnectors() {
  return request<LocalConnector[]>('/me/local-connectors');
}

export async function createLocalConnectorPairingSession(data: CreateLocalConnectorPairingSessionPayload = {}) {
  return request<CreateLocalConnectorPairingSessionResponse>('/me/local-connectors/pairing-sessions', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function revokeLocalConnector(id: string) {
  return request<null>(`/me/local-connectors/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export interface ConnectorRunStats {
  runs_today: number;
  runs_week: number;
  runs_month: number;
  runs_total: number;
}

export async function getConnectorRunStats() {
  return request<ConnectorRunStats>('/me/local-connectors/run-stats');
}

export interface AdapterModels {
  claude: string[];
  codex: string[];
}

export async function getAdapterModels() {
  return request<AdapterModels>('/adapter-models');
}
