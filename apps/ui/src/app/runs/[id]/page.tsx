'use client';

import { useEffect, useState, useRef, useCallback } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { api } from '@/lib/api';
import type { WorkflowRun, SSEEvent, SearchResult, GateHistoryItem } from '@/lib/types';

export default function RunDetailPage() {
  const params = useParams();
  const runId = params.id as string;

  const [run, setRun] = useState<WorkflowRun | null>(null);
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [runningGate, setRunningGate] = useState(false);
  const [runningPR2, setRunningPR2] = useState(false);
  const [runningPR3, setRunningPR3] = useState(false);
  const [paused, setPaused] = useState(false);
  const [gateHistory, setGateHistory] = useState<GateHistoryItem[]>([]);
  const [showHistory, setShowHistory] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);
  const eventsEndRef = useRef<HTMLDivElement>(null);

  // Search state
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [expandedResult, setExpandedResult] = useState<number | null>(null);

  const loadRun = useCallback(async () => {
    try {
      const data = await api.getRun(runId);
      setRun(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load run');
    } finally {
      setLoading(false);
    }
  }, [runId]);

  useEffect(() => {
    loadRun();
  }, [loadRun]);

  useEffect(() => {
    if (paused) return;

    const es = api.subscribeToEvents(runId);
    eventSourceRef.current = es;

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        setEvents((prev) => [...prev, data]);
        loadRun();
        // Reload history if visible and gate event received
        if (showHistory && data.type?.startsWith('gate.')) {
          loadHistory();
        }
      } catch {
        // Ignore parse errors
      }
    };

    es.onerror = () => {
      // SSE reconnects automatically
    };

    return () => {
      es.close();
    };
  }, [runId, paused, loadRun]);

  useEffect(() => {
    if (!paused) {
      eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [events, paused]);

  async function handleRunPR1() {
    setRunningGate(true);
    try {
      await api.runPR1Gate(runId);
      await loadRun();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to run PR1 gate');
    } finally {
      setRunningGate(false);
    }
  }

  async function handleRunPR2() {
    setRunningPR2(true);
    try {
      await api.runPR2Gate(runId);
      await loadRun();
      if (showHistory) await loadHistory();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to run PR2 gate');
    } finally {
      setRunningPR2(false);
    }
  }

  async function handleRunPR3() {
    setRunningPR3(true);
    try {
      await api.runPR3Gate(runId);
      await loadRun();
      if (showHistory) await loadHistory();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to run PR3 gate');
    } finally {
      setRunningPR3(false);
    }
  }

  async function loadHistory() {
    try {
      const data = await api.getRunWithHistory(runId);
      setGateHistory(data.gate_history || []);
    } catch (err) {
      console.error('Failed to load gate history:', err);
    }
  }

  async function toggleHistory() {
    if (!showHistory) {
      await loadHistory();
    }
    setShowHistory(!showHistory);
  }

  async function handleSearch(e: React.FormEvent) {
    e.preventDefault();
    if (!searchQuery.trim()) return;

    setSearching(true);
    setSearchError(null);
    setExpandedResult(null);
    try {
      const response = await api.search(searchQuery.trim(), 8);
      setSearchResults(response.results);
    } catch (err) {
      setSearchError(err instanceof Error ? err.message : 'Search failed');
      setSearchResults([]);
    } finally {
      setSearching(false);
    }
  }

  function formatDate(dateStr: string | undefined) {
    if (!dateStr) return '-';
    return new Date(dateStr).toLocaleString();
  }

  function formatTime(dateStr: string | undefined) {
    if (!dateStr) return '-';
    return new Date(dateStr).toLocaleTimeString();
  }

  function getStageState(stageName: string) {
    const stage = run?.stages?.find((s) => s.name === stageName);
    return stage?.state || 'pending';
  }

  function getStageClass(state: string) {
    switch (state) {
      case 'completed':
        return 'bg-green-100 border-green-500 text-green-800';
      case 'running':
        return 'bg-blue-100 border-blue-500 text-blue-800 animate-pulse';
      case 'failed':
        return 'bg-red-100 border-red-500 text-red-800';
      default:
        return 'bg-gray-100 border-gray-300 text-gray-600';
    }
  }

  const pr1Gate = run?.gates?.find((g) => g.level === 'PR1');
  const pr2Gate = run?.gates?.find((g) => g.level === 'PR2' && g.name === 'integration_smoke');
  const pr3Gate = run?.gates?.find((g) => g.level === 'PR3' && g.name === 'security_scan');

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-8">
        <div className="text-center py-12 text-gray-500">Loading run details...</div>
      </div>
    );
  }

  if (error && !run) {
    return (
      <div className="max-w-7xl mx-auto px-4 py-8">
        <div className="p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
          {error}
        </div>
        <Link href="/runs" className="mt-4 inline-block text-indigo-600 hover:text-indigo-900">
          &larr; Back to runs
        </Link>
      </div>
    );
  }

  const stages = ['spec', 'contract', 'plan', 'implement', 'verify'];

  return (
    <div className="max-w-7xl mx-auto px-4 py-8">
      <div className="mb-6">
        <Link href="/runs" className="text-indigo-600 hover:text-indigo-900 text-sm">
          &larr; Back to runs
        </Link>
        <h1 className="text-2xl font-bold text-gray-900 mt-2 font-mono">{runId}</h1>
        <div className="flex items-center gap-4 mt-2">
          <span
            className={`badge ${
              run?.status === 'completed'
                ? 'badge-completed'
                : run?.status === 'failed'
                ? 'badge-failed'
                : run?.status === 'running'
                ? 'badge-running'
                : 'badge-pending'
            }`}
          >
            {run?.status}
          </span>
          {run?.temporal_id && (
            <span className="text-sm text-gray-500">Temporal: {run.temporal_id}</span>
          )}
        </div>
      </div>

      {error && (
        <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-md text-red-700">
          {error}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Left Column: Stages and Gates */}
        <div className="space-y-6">
          {/* Stage Timeline */}
          <div className="card">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">Pipeline Stages</h2>
            <div className="space-y-3">
              {stages.map((stage, index) => {
                const state = getStageState(stage);
                const stageData = run?.stages?.find((s) => s.name === stage);
                return (
                  <div
                    key={stage}
                    className={`p-3 rounded-lg border-l-4 ${getStageClass(state)}`}
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="font-medium capitalize">{stage}</span>
                        <span className="text-xs opacity-75">({state})</span>
                      </div>
                      <div className="text-xs">
                        {stageData?.started_at && (
                          <span>Started: {formatTime(stageData.started_at)}</span>
                        )}
                        {stageData?.completed_at && (
                          <span className="ml-2">
                            Completed: {formatTime(stageData.completed_at)}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* PR1 Gate */}
          <div className="card">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">PR1 Gate (Unit Tests)</h2>
            {pr1Gate ? (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span
                    className={`badge ${pr1Gate.passed ? 'badge-passed' : 'badge-failed'}`}
                  >
                    {pr1Gate.passed ? 'Passed' : 'Failed'}
                  </span>
                  {pr1Gate.duration_ms && (
                    <span className="text-sm text-gray-500">
                      {(pr1Gate.duration_ms / 1000).toFixed(2)}s
                    </span>
                  )}
                </div>
                <div className="text-sm text-gray-600">
                  <div>Timestamp: {formatDate(pr1Gate.timestamp)}</div>
                  {pr1Gate.evidence_path && (
                    <div className="mt-1">Evidence: {pr1Gate.evidence_path}</div>
                  )}
                </div>
                {pr1Gate.checks?.map((check, i) => (
                  <div key={i} className="text-sm pl-2 border-l-2 border-gray-200">
                    <span className={check.passed ? 'text-green-600' : 'text-red-600'}>
                      {check.passed ? '✓' : '✗'}
                    </span>{' '}
                    {check.name}: {check.message}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-gray-500 text-sm">PR1 gate has not been executed yet.</p>
            )}
            <div className="mt-4 flex gap-3">
              <button
                onClick={handleRunPR1}
                disabled={runningGate}
                className="btn btn-primary"
              >
                {runningGate ? 'Running...' : 'Run PR1 Gate'}
              </button>
              <a
                href={api.getEvidenceZipUrl(runId)}
                className="btn btn-secondary"
                download
              >
                Download Evidence Bundle
              </a>
            </div>
          </div>

          {/* PR2 Gate */}
          <div className="card">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">PR2 Gate (Integration Tests)</h2>
            {pr2Gate ? (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span
                    className={`badge ${pr2Gate.passed ? 'badge-passed' : 'badge-failed'}`}
                  >
                    {pr2Gate.passed ? 'Passed' : 'Failed'}
                  </span>
                  {pr2Gate.duration_ms && (
                    <span className="text-sm text-gray-500">
                      {(pr2Gate.duration_ms / 1000).toFixed(2)}s
                    </span>
                  )}
                  {pr2Gate.executor && (
                    <span className="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">
                      {pr2Gate.executor}
                    </span>
                  )}
                </div>
                <div className="text-sm text-gray-600">
                  <div>Timestamp: {formatDate(pr2Gate.timestamp)}</div>
                  {pr2Gate.evidence_path && (
                    <div className="mt-1">Evidence: {pr2Gate.evidence_path}</div>
                  )}
                </div>
                {pr2Gate.checks?.map((check, i) => (
                  <div key={i} className="text-sm pl-2 border-l-2 border-gray-200">
                    <span className={check.passed ? 'text-green-600' : 'text-red-600'}>
                      {check.passed ? '✓' : '✗'}
                    </span>{' '}
                    {check.name}: {check.message}
                  </div>
                ))}
                {pr2Gate.error && (
                  <div className="text-sm text-red-600 bg-red-50 p-2 rounded">
                    {pr2Gate.error}
                  </div>
                )}
              </div>
            ) : (
              <p className="text-gray-500 text-sm">PR2 gate has not been executed yet.</p>
            )}
            <div className="mt-4 flex gap-3">
              <button
                onClick={handleRunPR2}
                disabled={runningPR2 || run?.status !== 'completed'}
                className="btn btn-primary"
                title={run?.status !== 'completed' ? 'Run must be completed to run PR2 gate' : ''}
              >
                {runningPR2 ? 'Running...' : 'Run PR2 Gate'}
              </button>
              {pr2Gate?.evidence_path && (
                <a
                  href={api.getGateEvidenceZipUrl(runId, 'PR2', 'integration_smoke')}
                  className="btn btn-secondary"
                  download
                >
                  Download Evidence
                </a>
              )}
            </div>
          </div>

          {/* PR3 Gate */}
          <div className="card">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">PR3 Gate (Security Scan)</h2>
            {pr3Gate ? (
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span
                    className={`badge ${pr3Gate.passed ? 'badge-passed' : 'badge-failed'}`}
                  >
                    {pr3Gate.passed ? 'Passed' : 'Failed'}
                  </span>
                  {pr3Gate.duration_ms && (
                    <span className="text-sm text-gray-500">
                      {(pr3Gate.duration_ms / 1000).toFixed(2)}s
                    </span>
                  )}
                  {pr3Gate.executor && (
                    <span className="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">
                      {pr3Gate.executor}
                    </span>
                  )}
                </div>
                <div className="text-sm text-gray-600">
                  <div>Timestamp: {formatDate(pr3Gate.timestamp)}</div>
                  {pr3Gate.evidence_path && (
                    <div className="mt-1">Evidence: {pr3Gate.evidence_path}</div>
                  )}
                </div>
                {pr3Gate.checks?.map((check, i) => (
                  <div key={i} className="text-sm pl-2 border-l-2 border-gray-200">
                    <span className={check.passed ? 'text-green-600' : 'text-red-600'}>
                      {check.passed ? '✓' : '✗'}
                    </span>{' '}
                    {check.name}: {check.message}
                  </div>
                ))}
                {pr3Gate.error && (
                  <div className="text-sm text-red-600 bg-red-50 p-2 rounded">
                    {pr3Gate.error}
                  </div>
                )}
              </div>
            ) : (
              <p className="text-gray-500 text-sm">PR3 gate has not been executed yet.</p>
            )}
            <div className="mt-4 flex gap-3">
              <button
                onClick={handleRunPR3}
                disabled={runningPR3 || run?.status !== 'completed'}
                className="btn btn-primary"
                title={run?.status !== 'completed' ? 'Run must be completed to run PR3 gate' : ''}
              >
                {runningPR3 ? 'Running...' : 'Run PR3 Gate'}
              </button>
              {pr3Gate?.evidence_path && (
                <a
                  href={api.getGateEvidenceZipUrl(runId, 'PR3', 'security_scan')}
                  className="btn btn-secondary"
                  download
                >
                  Download Evidence
                </a>
              )}
            </div>
          </div>

          {/* Gate History */}
          <div className="card">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-gray-900">Gate History</h2>
              <button
                onClick={toggleHistory}
                className="btn btn-secondary text-sm"
              >
                {showHistory ? 'Hide History' : 'Show History'}
              </button>
            </div>
            {showHistory && (
              <div className="space-y-2 max-h-60 overflow-y-auto">
                {gateHistory.length === 0 ? (
                  <p className="text-gray-500 text-sm">No gate executions yet.</p>
                ) : (
                  gateHistory.map((item) => (
                    <div
                      key={item.id}
                      className="p-2 bg-gray-50 rounded border border-gray-200 text-sm"
                    >
                      <div className="flex items-center gap-2 mb-1">
                        <span
                          className={`badge text-xs ${
                            item.result.passed ? 'badge-passed' : 'badge-failed'
                          }`}
                        >
                          {item.result.passed ? 'Passed' : 'Failed'}
                        </span>
                        <span className="font-medium">
                          {item.result.level}/{item.result.name || 'default'}
                        </span>
                        {item.result.duration_ms && (
                          <span className="text-gray-500">
                            {(item.result.duration_ms / 1000).toFixed(2)}s
                          </span>
                        )}
                      </div>
                      <div className="text-xs text-gray-500">
                        {formatDate(item.result.timestamp)}
                        {item.result.evidence_path && (
                          <a
                            href={api.getGateEvidenceZipUrl(
                              runId,
                              item.result.level,
                              item.result.name || 'default'
                            )}
                            className="ml-2 text-indigo-600 hover:underline"
                            download
                          >
                            evidence
                          </a>
                        )}
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}
          </div>

          {/* Search Panel */}
          <div className="card">
            <h2 className="text-lg font-semibold text-gray-900 mb-4">Code Search</h2>
            <form onSubmit={handleSearch} className="flex gap-2 mb-4">
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search code..."
                className="flex-1 px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
              />
              <button
                type="submit"
                disabled={searching || !searchQuery.trim()}
                className="btn btn-primary"
              >
                {searching ? 'Searching...' : 'Search'}
              </button>
            </form>

            {searchError && (
              <div className="p-3 bg-red-50 border border-red-200 rounded-md text-red-700 text-sm mb-4">
                {searchError}
              </div>
            )}

            {searchResults.length > 0 && (
              <div className="space-y-2 max-h-80 overflow-y-auto">
                {searchResults.map((result, index) => (
                  <div
                    key={`${result.path}-${result.chunk_index}`}
                    className="border border-gray-200 rounded-md overflow-hidden"
                  >
                    <button
                      onClick={() => setExpandedResult(expandedResult === index ? null : index)}
                      className="w-full px-3 py-2 bg-gray-50 hover:bg-gray-100 text-left flex items-center justify-between"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="font-mono text-sm text-gray-900 truncate">{result.path}</span>
                        <span className="text-xs text-gray-500 shrink-0">#{result.chunk_index}</span>
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        <span className="text-xs text-gray-500">{(result.score * 100).toFixed(1)}%</span>
                        <span className="text-gray-400">{expandedResult === index ? '▲' : '▼'}</span>
                      </div>
                    </button>
                    {expandedResult === index && (
                      <div className="p-3 bg-white border-t border-gray-200">
                        <pre className="text-xs font-mono text-gray-700 whitespace-pre-wrap overflow-x-auto">
                          {result.content}
                        </pre>
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}

            {searchResults.length === 0 && !searching && searchQuery && !searchError && (
              <div className="text-gray-500 text-sm text-center py-4">
                No results found. Try indexing the repository first.
              </div>
            )}
          </div>
        </div>

        {/* Right Column: Events */}
        <div className="card flex flex-col" style={{ maxHeight: '600px' }}>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-gray-900">Live Events</h2>
            <button
              onClick={() => setPaused(!paused)}
              className={`btn text-sm ${paused ? 'btn-primary' : 'btn-secondary'}`}
            >
              {paused ? 'Resume' : 'Pause'}
            </button>
          </div>
          <div className="flex-1 overflow-y-auto space-y-2 font-mono text-xs">
            {events.length === 0 ? (
              <div className="text-gray-500 text-center py-4">
                {paused ? 'Event stream paused' : 'Waiting for events...'}
              </div>
            ) : (
              events.map((event, index) => {
                const payload = event.payload || {};
                return (
                  <div
                    key={event.id || index}
                    className="p-2 bg-gray-50 rounded border border-gray-200"
                  >
                    <div className="flex items-center gap-2 mb-1">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          event.type?.includes('completed')
                            ? 'bg-green-100 text-green-800'
                            : event.type?.includes('failed')
                            ? 'bg-red-100 text-red-800'
                            : 'bg-blue-100 text-blue-800'
                        }`}
                      >
                        {event.type}
                      </span>
                      <span className="text-gray-400">{formatTime(event.timestamp)}</span>
                    </div>
                    {payload.stage_name && (
                      <div className="text-gray-600">Stage: {payload.stage_name}</div>
                    )}
                  </div>
                );
              })
            )}
            <div ref={eventsEndRef} />
          </div>
        </div>
      </div>
    </div>
  );
}
