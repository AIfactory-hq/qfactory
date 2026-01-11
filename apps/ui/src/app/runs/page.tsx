'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { api } from '@/lib/api';
import type { WorkflowRun, GateResult } from '@/lib/types';

type StatusFilter = 'all' | 'running' | 'completed' | 'failed';

export default function RunsPage() {
  const router = useRouter();
  const [runs, setRuns] = useState<WorkflowRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchId, setSearchId] = useState('');
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [showNewRun, setShowNewRun] = useState(false);
  const [prompt, setPrompt] = useState('');
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    loadRuns();
    const interval = setInterval(loadRuns, 5000);
    return () => clearInterval(interval);
  }, []);

  async function loadRuns() {
    try {
      const data = await api.listRuns();
      setRuns(data.runs || []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load runs');
    } finally {
      setLoading(false);
    }
  }

  async function handleCreateRun(e: React.FormEvent) {
    e.preventDefault();
    if (!prompt.trim()) return;

    setCreating(true);
    setError(null);
    try {
      const run = await api.createWorkflow({ mode: 'generate', prompt: prompt.trim() });
      setShowNewRun(false);
      setPrompt('');
      router.push(`/runs/${run.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create run');
    } finally {
      setCreating(false);
    }
  }

  const filteredRuns = runs.filter((run) => {
    if (searchId && !run.id.toLowerCase().includes(searchId.toLowerCase())) {
      return false;
    }
    if (statusFilter !== 'all' && run.status !== statusFilter) {
      return false;
    }
    return true;
  });

  function formatDate(dateStr: string | undefined) {
    if (!dateStr) return '-';
    return new Date(dateStr).toLocaleString();
  }

  function getStatusBadge(status: string) {
    const classes: Record<string, string> = {
      pending: 'badge badge-pending',
      running: 'badge badge-running',
      completed: 'badge badge-completed',
      failed: 'badge badge-failed',
    };
    return <span className={classes[status] || 'badge'}>{status}</span>;
  }

  function getPR1Badge(gates: GateResult[] | undefined) {
    const pr1 = gates?.find((g) => g.level === 'PR1');
    if (!pr1) {
      return <span className="badge badge-pending">pending</span>;
    }
    const duration = pr1.duration_ms ? `${(pr1.duration_ms / 1000).toFixed(1)}s` : '';
    if (pr1.passed) {
      return (
        <span className="badge badge-passed">
          passed {duration && <span className="ml-1 opacity-75">{duration}</span>}
        </span>
      );
    }
    return (
      <span className="badge badge-failed">
        failed {duration && <span className="ml-1 opacity-75">{duration}</span>}
      </span>
    );
  }

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-8">
        <div className="text-center py-12 text-gray-500">Loading runs...</div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto px-4 py-8">
      <div className="mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Workflow Runs</h1>
          <p className="text-gray-600 mt-1">Monitor and manage workflow executions</p>
        </div>
        <button
          onClick={() => setShowNewRun(true)}
          className="btn btn-primary"
        >
          + New Run
        </button>
      </div>

      {/* New Run Modal */}
      {showNewRun && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg shadow-xl p-6 w-full max-w-lg mx-4">
            <h2 className="text-xl font-semibold text-gray-900 mb-4">Create New Run</h2>
            <form onSubmit={handleCreateRun}>
              <div className="mb-4">
                <label className="block text-sm font-medium text-gray-700 mb-2">
                  Prompt
                </label>
                <textarea
                  value={prompt}
                  onChange={(e) => setPrompt(e.target.value)}
                  placeholder="Describe what you want to build... e.g., Create a Go HTTP service with /health endpoint"
                  rows={4}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  autoFocus
                />
              </div>
              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => { setShowNewRun(false); setPrompt(''); }}
                  className="btn btn-secondary"
                  disabled={creating}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={creating || !prompt.trim()}
                >
                  {creating ? 'Creating...' : 'Create Run'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {error && (
        <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
          {error}
        </div>
      )}

      <div className="card mb-6">
        <div className="flex flex-wrap gap-4">
          <div className="flex-1 min-w-[200px]">
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Search Run ID
            </label>
            <input
              type="text"
              placeholder="Enter run ID..."
              value={searchId}
              onChange={(e) => setSearchId(e.target.value)}
              className="input px-3 py-2 border rounded-md w-full"
            />
          </div>
          <div className="w-48">
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Status
            </label>
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
              className="select px-3 py-2 border rounded-md w-full"
            >
              <option value="all">All</option>
              <option value="running">Running</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
            </select>
          </div>
        </div>
      </div>

      <div className="card overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="table-header">Run ID</th>
              <th className="table-header">Status</th>
              <th className="table-header">Created</th>
              <th className="table-header">Updated</th>
              <th className="table-header">Current Stage</th>
              <th className="table-header">PR1</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {filteredRuns.length === 0 ? (
              <tr>
                <td colSpan={6} className="table-cell text-center text-gray-500 py-8">
                  No runs found
                </td>
              </tr>
            ) : (
              filteredRuns.map((run) => (
                <tr key={run.id} className="hover:bg-gray-50">
                  <td className="table-cell">
                    <Link
                      href={`/runs/${run.id}`}
                      className="text-indigo-600 hover:text-indigo-900 font-mono"
                    >
                      {run.id}
                    </Link>
                  </td>
                  <td className="table-cell">{getStatusBadge(run.status)}</td>
                  <td className="table-cell text-gray-500">{formatDate(run.created_at)}</td>
                  <td className="table-cell text-gray-500">{formatDate(run.updated_at)}</td>
                  <td className="table-cell">
                    {run.current_stage || <span className="text-gray-400">-</span>}
                  </td>
                  <td className="table-cell">{getPR1Badge(run.gates)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
