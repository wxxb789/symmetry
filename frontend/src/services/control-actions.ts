import type { ActionId } from "./api";

export type ActionKind = "start" | "cancel" | "retry" | "input";

const storageKey = (kind: ActionKind, workItemId: string, scope: string) => `symmetry:${kind}:${workItemId}:${scope}`;

/**
 * The idempotency identity of an unconfirmed control request. A reload, a double
 * click or a retry after a lost response reuses the same id, so the server
 * replays its recorded outcome instead of minting a second command. The id is
 * cleared only once the server has confirmed the request.
 */
export function readOrMintActionId(kind: ActionKind, workItemId: string, scope: string): ActionId | null {
  const key = storageKey(kind, workItemId, scope);
  try {
    const stored = sessionStorage.getItem(key);
    if (stored) return stored as ActionId;
    const minted = crypto.randomUUID();
    sessionStorage.setItem(key, minted);
    return minted as ActionId;
  } catch {
    // Without a session store there is no durable identity for the attempt, and
    // a control request without one could be applied twice. Refuse instead.
    return null;
  }
}

/**
 * The identity of an attempt this browser already minted, without minting one.
 * A reloaded detail reconstructs a still-stored attempt with this reader so an
 * unconfirmed request keeps rendering as pending instead of vanishing.
 */
export function readActionId(kind: ActionKind, workItemId: string, scope: string): ActionId | null {
  try {
    const stored = sessionStorage.getItem(storageKey(kind, workItemId, scope));
    return stored === null ? null : (stored as ActionId);
  } catch {
    return null;
  }
}

export function clearActionId(kind: ActionKind, workItemId: string, scope: string): void {
  try {
    sessionStorage.removeItem(storageKey(kind, workItemId, scope));
  } catch {
    // A store that cannot be written also cannot hold a stale id.
  }
}
