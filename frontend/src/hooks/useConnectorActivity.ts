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

  useEffect(() => {
    if (!connectorId) return;

    // Initial fetch
    poll();

    // Try SSE
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
    };

    // Polling fallback (runs always; when SSE works it just confirms state)
    pollTimerRef.current = setInterval(poll, POLL_INTERVAL_MS);

    // Stale detection
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
      if (pollTimerRef.current) clearInterval(pollTimerRef.current);
      clearInterval(staleTimer);
    };
  }, [connectorId, poll, applyResponse]);

  return state;
}
