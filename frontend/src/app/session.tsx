import { createContext, useContext, type ReactNode } from "react";

// The authenticated shell owns the session bootstrap, so the current CSRF token
// is handed to pages that issue same-origin commands instead of every page
// fetching /portal/api/session again.
const SessionToken = createContext<string>("");

export function SessionTokenProvider({ token, children }: { token: string; children: ReactNode }) {
  return <SessionToken.Provider value={token}>{children}</SessionToken.Provider>;
}

export function useSessionToken(): string {
  return useContext(SessionToken);
}
