export interface StageStatus {
  name: string;
  state: 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
  started_at?: string;
  completed_at?: string;
  error?: string;
}

export interface Check {
  name: string;
  passed: boolean;
  message?: string;
}

export interface GateResult {
  level: string;
  name?: string;
  execution_id?: string;
  passed: boolean;
  checks?: Check[];
  timestamp?: string;
  duration_ms?: number;
  executor?: string;
  started_at?: string;
  completed_at?: string;
  evidence_path?: string;
  error?: string;
}

export interface GateHistoryItem {
  id: string;
  result: GateResult;
  parent_execution_id?: string;
  retry_reason?: string;
}

export interface WorkflowRun {
  id: string;
  temporal_id?: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  current_stage?: string;
  stages?: StageStatus[];
  created_at?: string;
  updated_at?: string;
  completed_at?: string;
  error?: string;
  gates?: GateResult[];
  gate_history?: GateHistoryItem[];
}

export interface ListRunsResponse {
  runs: WorkflowRun[];
  count: number;
}

export interface SSEEvent {
  id: string;
  type: string;
  run_id: string;
  timestamp: string;
  payload: {
    stage_name?: string;
    stage_index?: number;
    error?: string;
  };
}

// Search types (v0.3b)
export interface SearchResult {
  path: string;
  score: number;
  chunk_index: number;
  sha256: string;
  snippet: string;
  content: string;
}

export interface SearchResponse {
  query: string;
  k: number;
  results: SearchResult[];
}

export interface IndexRequest {
  path: string;
  globs?: string[];
  exclude?: string[];
}

export interface IndexResponse {
  files_indexed: number;
  chunks_created: number;
  files: string[];
  duration_ms: number;
  error?: string;
}

// Gate policy types (v0.6)
export interface GateRef {
  level: string;
  name: string;
}

export interface GatePolicy {
  required_levels?: string[];
  required_gates?: GateRef[];
  max_age_seconds?: number;
  max_retries?: number;
  fail_open?: boolean;
}

export interface PolicyDecision {
  allowed: boolean;
  missing?: GateRef[];
  stale?: GateRef[];
  failing?: GateRef[];
  message?: string;
}

export interface TrustBreakdown {
  passed_required: number;
  failed: number;
  stale: number;
  missing: number;
  consecutive_failure_penalty?: number;
  flaky_penalty?: number;
  recovery_reward?: number;
}

export interface TrustIndex {
  score: number;
  grade: string;
  breakdown: TrustBreakdown;
}

// Gate lineage types (v0.8)
export interface FileDiff {
  name: string;
  sha256_a?: string;
  sha256_b?: string;
  size_a: number;
  size_b: number;
  modified: boolean;
}

export interface GateResultDiff {
  passed_a: boolean;
  passed_b: boolean;
  duration_ms_a: number;
  duration_ms_b: number;
  checks_pass_diff?: number[];
}

export interface GateDiffResponse {
  run_id: string;
  level: string;
  name: string;
  exec_a: string;
  exec_b: string;
  file_diffs: FileDiff[];
  result_diff?: GateResultDiff;
}

export interface LatestGateResponse {
  run_id: string;
  level: string;
  name: string;
  execution_id: string;
  result?: GateResult;
}
