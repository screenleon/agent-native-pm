import { useState, useEffect, useRef, useCallback } from 'react';
import type { ConnectorActivity } from '../types';
import { getConnectorActivity } from '../api/client';

// UI-007: EventSource must live only in App.tsx. Connector activity uses a
// per-connector-ID stream that cannot be centralised in App.tsx without a
// dedicated global-stream endpoint (Phase 8 TODO). Until then this hook uses
// polling only. The 15s interval is acceptable for the dogfood use case.
const STALE_MS = 90_000;
const POLL_INTERVAL_MS = 15_000;

export type ActivitySource = 'polling' | 'stale';

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
  const lastUpdateRef = useRef<number>(Date.now());
  const pollTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const poll = useCallback(async () => {
    if (!connectorId) return;
    try {
      const res = await getConnectorActivity(connectorId);
      const src: ActivitySource =
        Date.now() - lastUpdateRef.current > STALE_MS ? 'stale' : 'polling';
      lastUpdateRef.current = Date.now();
      setState({ activity: res.data.activity, online: res.data.online, source: src });
    } catch {
      // ignore transient errors
    }
  }, [connectorId]);

  useEffect(() => {
    if (!connectorId) return;

    poll();
    pollTimerRef.current = setInterval(poll, POLL_INTERVAL_MS);

    const staleTimer = setInterval(() => {
      if (Date.now() - lastUpdateRef.current > STALE_MS) {
        setState(prev => ({ ...prev, source: 'stale' }));
      }
    }, 10_000);

    return () => {
      if (pollTimerRef.current) {
        clearInterval(pollTimerRef.current);
        pollTimerRef.current = null;
      }
      clearInterval(staleTimer);
    };
  }, [connectorId, poll]);

  return state;
}
