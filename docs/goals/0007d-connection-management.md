# 0007d - GitHub and Azure DevOps connection management

Let users create, inspect, update, test, and remove GitHub and Azure DevOps
connections while clearly seeing authentication type, provider capabilities,
health, sync status, and actionable failure reasons. Credentials remain server
side and are never exposed through browser state, logs, or errors.

Completion evidence includes real Portal API and browser flows for both
providers, idempotent submission, stale updates, concurrent deletion, connection
tests, redacted errors, expired authentication, and owner acceptance of the
connection-management experience.

This goal excludes project resource binding, provider actions, work execution,
Chat, and Goal screens. It depends on 0007a, is one independently mergeable PR,
and must fit one agent run of at most 12 hours.
