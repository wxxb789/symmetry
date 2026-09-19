# 0007h - Goal, attention, and decision workspace

Make `/portal/attention` an operational entry point for real Goals. Users can
inspect Goal state, accepted evidence, blockers, next authorized actions,
decisions, dependencies, context, and receipts, and can issue only actions the
server currently permits.

Completion evidence uses the real Goal APIs merged in PR #6 and covers allowed
actions, mutation replay, stale revisions, decisions, dependency graph, context
authorization, pagination, refresh, and disconnect recovery. Owner visual
acceptance confirms that blockers and receipt truth are clear without exposing
raw policy internals.

The frontend does not infer Goal policy or fabricate unavailable capabilities.
This goal depends on 0007a, is one independently mergeable PR, and must fit one
agent run of at most 12 hours.
