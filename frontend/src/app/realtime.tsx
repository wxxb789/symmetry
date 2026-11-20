import { useRouteContext } from "@tanstack/react-router";
import { useCallback, useSyncExternalStore } from "react";
import type { RealtimeHandle, RefreshPass, RealtimeStatus } from "../services/realtime";

/**
 * The established-connection counter of the operator invalidation socket.
 *
 * A surface that holds a served cursor (the Goal receipt list continues from the
 * last `sequence` it rendered) watches this value: a change means the browser
 * just (re)established its connection, so the surface can continue from where it
 * stopped instead of dropping the notifications it missed while disconnected.
 */
export function useRealtimeEpoch(): number {
  return useRealtimeValue((realtime) => realtime.epoch());
}

/** The live connection status, for surfaces that must state degraded freshness. */
export function useRealtimeStatus(): RealtimeStatus {
  return useRealtimeValue((realtime) => realtime.status());
}

/**
 * The reads the client last refreshed, for surfaces that state how current what
 * they render is. The pass is the client's own act — it issues the reads — so a
 * surface can say which of the client's events refreshed the reads on screen and
 * when those reads finished.
 */
export function useRealtimeRefresh(): RefreshPass | null {
  return useRealtimeValue((realtime) => realtime.lastRefresh());
}

// The handle's members are closures over its own state, so the subscription is
// wrapped to keep `useSyncExternalStore` from being handed a bare method.
function useRealtimeValue<T>(select: (realtime: RealtimeHandle) => T): T {
  const { application } = useRouteContext({ from: "__root__" });
  const realtime = application.realtime;
  const subscribe = useCallback(
    (listener: () => void) => realtime.subscribe(listener),
    [realtime],
  );
  return useSyncExternalStore(subscribe, () => select(realtime));
}
