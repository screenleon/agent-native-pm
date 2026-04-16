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
