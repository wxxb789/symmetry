import type { QueryClient } from "@tanstack/react-query";
import { useState, type RefObject } from "react";
import { chatConversationOptions, chatOlderPageOptions, type Application, type ChatScopeKey } from "../../app/application";
import { Conflict, UnexpectedResponse, Validation, errorMessage } from "../../services/api";
import { ownsChainPointer, pendingOlderLoadFor, retainOlderPage, type ChatOlderLoad, type ChatScopeState, type ChatSendStatus } from "./scopes";

function definitiveReadRefusal(error: unknown): boolean {
  return error instanceof Conflict || error instanceof Validation ||
    (error instanceof UnexpectedResponse && error.status === 400);
}

export function useOlderPageLoading(options: {
  readonly key: string;
  readonly identity: ChatScopeKey;
  readonly cursor: string | null;
  readonly boundaryId: string | null;
  readonly latestStateOf: RefObject<(key: string) => ChatScopeState>;
  readonly application: Application;
  readonly queryClient: QueryClient;
  readonly updateWith: (key: string, change: (state: ChatScopeState) => ChatScopeState) => void;
}) {
  const { key, identity, cursor, boundaryId, latestStateOf, application, queryClient, updateWith } = options;
  const [olderLoads, setOlderLoads] = useState<readonly ChatOlderLoad[]>([]);

  const load = () => {
    if (cursor === null) return;
    const press: ChatOlderLoad = { scopeKey: key, cursor, boundaryId };
    if (pendingOlderLoadFor(olderLoads, press)) return;
    setOlderLoads((current) => [...current, press]);
    void application.queryClient
      .fetchQuery(chatOlderPageOptions(application.api, identity, press.cursor))
      .then(
        (fetched) => {
          updateWith(key, (previous) => retainOlderPage(previous, press.cursor, press.boundaryId, { messages: fetched.messages, nextBefore: fetched.next_before }));
        },
        (error: unknown) => {
          // Rejection callback, not a `.catch`: a throwing retain write must
          // not be reported as a read failure.
          const refused = definitiveReadRefusal(error);
          if (refused) {
            // The gate reads the last committed store while the write below
            // judges the queued record: one benign re-read, never a wrong cursor.
            const queryKey = chatConversationOptions(application.api, identity).queryKey;
            if (ownsChainPointer(latestStateOf.current(key), press.cursor, press.boundaryId) && queryClient.isFetching({ queryKey }) === 0) {
              void queryClient.refetchQueries({ queryKey });
            }
          }
          updateWith(key, (previous) => {
            if (refused && !ownsChainPointer(previous, press.cursor, press.boundaryId)) return previous;
            const status: ChatSendStatus = { kind: "notice", message: errorMessage(error) };
            return refused
              ? { ...previous, nextCursor: previous.pages[0]?.nextBefore ?? null, nextBoundaryId: previous.pages[0]?.messages[0]?.id ?? null, status }
              : { ...previous, status };
          });
        },
      )
      .finally(() => { setOlderLoads((current) => current.filter((load) => load !== press)); });
  };

  const pending = cursor !== null && pendingOlderLoadFor(olderLoads, { scopeKey: key, cursor, boundaryId });
  return { pending, load };
}
