import { useState, useEffect, useRef, useCallback } from 'react';
import type { ConnectorActivity } from '../types';
import { getConnectorActivity, connectorActivityStreamURL } from '../api/client';

const STALE_MS = 90_000;
const POLL_INTERVAL_MS = 15_000;

export type ActivitySource = 'sse' | 'polling' | 'stale';

export interface ConnectorActivityState {
  activity: ConnectorActivity | null;
  online: boolean;
  source: ActivitySource;
}

// useConnectorActivity subscribes to connector activity via SSE and degrades
// to polling only when SSE fails. Both transports run simultaneously only for
// the initial fetch; after that, polling is suppressed while SSE is healthy.
// DECISIONS.md 2026-04-25 §(g): "auto-degrades SSE → polling → stale".
export function useConnectorActivity(connectorId: string | null): ConnectorActivityState {
  const [state, setState] = useState<ConnectorActivityState>({
    activity: null,
    online: false,
    source: 'polling',
  });
  const sseActiveRef = useRef(false);
  const lastUpdateRef = useRef<number>(0);
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const esRef = useRef<EventSource | null>(null);

  const applyResponse = useCallback((activity: ConnectorActivity | null, online: boolean, src: ActivitySource) => {
    lastUpdateRef.current = Date.now();
    setState({ activity, online, source: src });
  }, []);

  const poll = useCallback(async () => {
    if (!connectorId) return;
    try {
      const res = await getConnectorActivity(connectorId);
      const src: ActivitySource =
        sseActiveRef.current ? 'sse' :
        Date.now() - lastUpdateRef.current > STALE_MS ? 'stale' : 'polling';
      applyResponse(res.data.activity, res.data.online, src);
    } catch {
      // ignore transient errors
    }
  }, [connectorId, applyResponse]);

  const startPolling = useCallback(() => {
    if (pollTimerRef.current) return; // already running
    pollTimerRef.current = setInterval(poll, POLL_INTERVAL_MS);
  }, [poll]);

  useEffect(() => {
    if (!connectorId) return;

    // Initial fetch so the UI is not blank while SSE connects.
    poll();

    // Try SSE first. Only fall back to interval polling if it errors.
    let cancelled = false;
    const url = connectorActivityStreamURL(connectorId);
    const es = new EventSource(url, { withCredentials: true });
    esRef.current = es;

    es.addEventListener('activity', (e: MessageEvent) => {
      if (cancelled) return;
      try {
        const data = JSON.parse(e.data) as { activity: ConnectorActivity | null; online: boolean };
        sseActiveRef.current = true;
        applyResponse(data.activity, data.online, 'sse');
      } catch { /* malformed */ }
    });

    es.onerror = () => {
      sseActiveRef.current = false;
      es.close();
      // SSE failed — start polling as fallback.
      if (!cancelled) startPolling();
    };

    // Stale detection: if neither SSE nor polling have delivered an update
    // recently, mark the source as stale.
    const staleTimer = setInterval(() => {
      if (Date.now() - lastUpdateRef.current > STALE_MS) {
        setState(prev => ({ ...prev, source: 'stale' }));
      }
    }, 10_000);

    return () => {
      cancelled = true;
      sseActiveRef.current = false;
      es.close();
      esRef.current = null;
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current);
        pollTimerRef.current = null;
      }
      clearInterval(staleTimer);
    };
  }, [connectorId, poll, applyResponse, startPolling]);

  return state;
}
