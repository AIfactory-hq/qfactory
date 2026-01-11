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
