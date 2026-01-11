import type { WorkflowRun, ListRunsResponse, GateResult, SearchResponse, IndexRequest, IndexResponse, TrustIndex, PolicyDecision, GatePolicy, GateDiffResponse, LatestGateResponse, FinalizeRequest, FinalizeResponse, CapsuleDescriptor, CapsuleVerifyResult } from './types';

const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8090';

async function fetchJSON<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(error.error || `HTTP ${res.status}`);
  }

  return res.json();
}

export interface CreateWorkflowRequest {
  mode: string;
  prompt: string;
}

export const api = {
  createWorkflow(req: CreateWorkflowRequest): Promise<WorkflowRun> {
    return fetchJSON<WorkflowRun>('/workflows', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  listRuns(): Promise<ListRunsResponse> {
    return fetchJSON<ListRunsResponse>('/runs');
  },

  getRun(id: string): Promise<WorkflowRun> {
    return fetchJSON<WorkflowRun>(`/runs/${id}`);
  },

  runPR1Gate(id: string): Promise<GateResult> {
    return fetchJSON<GateResult>(`/runs/${id}/gates/pr1`, { method: 'POST' });
  },

  runPR2Gate(id: string): Promise<GateResult> {
    return fetchJSON<GateResult>(`/runs/${id}/gates/pr2`, { method: 'POST' });
  },

  runPR3Gate(id: string): Promise<GateResult> {
    return fetchJSON<GateResult>(`/runs/${id}/gates/pr3`, { method: 'POST' });
  },

  getRunWithHistory(id: string): Promise<WorkflowRun> {
    return fetchJSON<WorkflowRun>(`/runs/${id}?include_history=1`);
  },

  getGateEvidenceZipUrl(id: string, level: string, name: string): string {
    return `${API_BASE}/runs/${id}/gates/${level}/${name}/evidence.zip`;
  },

  getExecEvidenceZipUrl(id: string, level: string, name: string, execId: string): string {
    return `${API_BASE}/runs/${id}/gates/${level}/${name}/exec/${execId}/evidence.zip`;
  },

  getEvidenceManifest(id: string): Promise<unknown> {
    return fetchJSON<unknown>(`/runs/${id}/evidence`);
  },

  getEvidenceZipUrl(id: string): string {
    return `${API_BASE}/runs/${id}/evidence.zip`;
  },

  subscribeToEvents(id: string): EventSource {
    return new EventSource(`${API_BASE}/runs/${id}/events`);
  },

  // Search API (v0.3b)
  search(query: string, k: number = 8): Promise<SearchResponse> {
    return fetchJSON<SearchResponse>(`/search?q=${encodeURIComponent(query)}&k=${k}`);
  },

  indexRepository(req: IndexRequest): Promise<IndexResponse> {
    return fetchJSON<IndexResponse>('/search/index', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  // Trust and policy API (v0.6)
  getTrustIndex(id: string): Promise<TrustIndex> {
    return fetchJSON<TrustIndex>(`/runs/${id}/trust`);
  },

  getPolicyDecision(id: string): Promise<PolicyDecision> {
    return fetchJSON<PolicyDecision>(`/runs/${id}/gate-policy/decision`);
  },

  updateGatePolicy(id: string, policy: GatePolicy): Promise<WorkflowRun> {
    return fetchJSON<WorkflowRun>(`/runs/${id}/gate-policy`, {
      method: 'PATCH',
      body: JSON.stringify(policy),
    });
  },

  // Gate lineage API (v0.8)
  retryGate(id: string, level: string, name: string, reason?: string): Promise<GateResult> {
    return fetchJSON<GateResult>(`/runs/${id}/gates/${level}/${name}/retry`, {
      method: 'POST',
      body: JSON.stringify({ reason: reason || '' }),
    });
  },

  getGateDiff(id: string, level: string, name: string, execA: string, execB: string): Promise<GateDiffResponse> {
    return fetchJSON<GateDiffResponse>(
      `/runs/${id}/gates/${level}/${name}/diff?exec_a=${encodeURIComponent(execA)}&exec_b=${encodeURIComponent(execB)}`
    );
  },

  getLatestGate(id: string, level: string, name: string): Promise<LatestGateResponse> {
    return fetchJSON<LatestGateResponse>(`/runs/${id}/gates/${level}/${name}/latest`);
  },

  // Finalization and Capsule API (v0.9)
  finalizeRun(id: string, req: FinalizeRequest): Promise<FinalizeResponse> {
    return fetchJSON<FinalizeResponse>(`/runs/${id}/finalize`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  getCapsule(id: string): Promise<CapsuleDescriptor> {
    return fetchJSON<CapsuleDescriptor>(`/runs/${id}/capsule`);
  },

  getCapsuleZipUrl(id: string): string {
    return `${API_BASE}/runs/${id}/capsule.zip`;
  },

  async verifyCapsule(file: File): Promise<CapsuleVerifyResult> {
    const formData = new FormData();
    formData.append('capsule', file);
    const res = await fetch(`${API_BASE}/capsules/verify`, {
      method: 'POST',
      body: formData,
    });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(error.error || `HTTP ${res.status}`);
    }
    return res.json();
  },
};
