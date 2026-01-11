import type { WorkflowRun, ListRunsResponse, GateResult, SearchResponse, IndexRequest, IndexResponse, TrustIndex, PolicyDecision, GatePolicy } from './types';

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

export const api = {
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
};
