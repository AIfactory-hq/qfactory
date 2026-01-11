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
  passed: boolean;
  checks?: Check[];
  timestamp?: string;
  duration_ms?: number;
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
