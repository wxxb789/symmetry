/**
 * The Phoenix JavaScript client ships no TypeScript declarations, and the portal
 * consumes one narrow surface of it: a socket with a per-connect `authToken`
 * function, connection state callbacks, and one receive-only channel. Declaring
 * exactly that surface here keeps the adapter type-checked without maintaining a
 * hand-written copy of the whole client API.
 */
declare module "phoenix" {
  export interface PhoenixPush {
    receive(status: string, callback: (reply?: unknown) => unknown): PhoenixPush;
  }

  export interface PhoenixChannel {
    on(event: string, callback: (payload: unknown) => void): number;
    join(): PhoenixPush;
  }

  export interface PhoenixSocketOptions {
    authToken?: string | (() => string);
    params?: Record<string, unknown>;
    heartbeatIntervalMs?: number;
    reconnectAfterMs?: (tries: number) => number;
  }

  export class Socket {
    constructor(endPoint: string, options?: PhoenixSocketOptions);
    connect(): void;
    disconnect(callback?: () => void): void;
    isConnected(): boolean;
    onOpen(callback: () => void): void;
    onClose(callback: (event?: unknown) => void): void;
    onError(callback: (error?: unknown) => void): void;
    channel(topic: string, params?: Record<string, unknown>): PhoenixChannel;
  }
}
