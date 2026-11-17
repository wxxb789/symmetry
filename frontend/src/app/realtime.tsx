import { useRouteContext } from "@tanstack/react-router";
import { useCallback, useSyncExternalStore } from "react";

/**
 * The established-connection counter of the operator invalidation socket.
 *
 * A surface that holds a served cursor (the Goal receipt list continues from the
 * last `sequence` it rendered) watches this value: a change means the browser
 * just (re)established its connection, so the surface can continue from where it
 * stopped instead of dropping the notifications it missed while disconnected.
 */
export function useRealtimeEpoch(): number {
  const { application } = useRouteContext({ from: "__root__" });
  const realtime = application.realtime;
  // The handle's members are closures over its own state, so the subscription is
  // wrapped to keep `useSyncExternalStore` from being handed a bare method.
  const subscribe = useCallback(
    (listener: () => void) => realtime.subscribe(listener),
    [realtime],
  );
  return useSyncExternalStore(subscribe, () => realtime.epoch());
}

/** The live connection status, for surfaces that must state degraded freshness. */
export function useRealtimeStatus(): string {
  const { application } = useRouteContext({ from: "__root__" });
  const realtime = application.realtime;
  const subscribe = useCallback(
    (listener: () => void) => realtime.subscribe(listener),
    [realtime],
  );
  return useSyncExternalStore(subscribe, () => realtime.status());
}
