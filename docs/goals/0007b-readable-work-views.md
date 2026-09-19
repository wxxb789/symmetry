# 0007b - Readable project and work views

Let an authenticated user inspect real projects and work through dense,
consistent list, board, and work-detail views with pagination and stable URLs.
Selection, filtering, navigation, refresh, and late responses must never show
data from the wrong project or replace newer state.

Completion evidence includes real-API browser tests for list, board, detail,
history, empty and error states; responsive and keyboard inspection; deep-link
reload; and owner visual acceptance for clarity, information density, and
consistency using the approved design references.

This goal is read-only and excludes editing, connections, resources, runtime
control, Chat, and Goal screens. It depends on 0007a, is one independently
mergeable PR, and must fit one agent run of at most 12 hours.
