# 0006c - Retained-session continuity

Make a retained native session resumable or handoff-eligible only when its exact
machine, runtime, harness version, workspace, binding, stop receipt, Goal,
revision, WorkItem, Task, Run, generation, and Subject remain compatible.
Handoff creates a new session and never transfers an opaque native handle.

Completion evidence covers fresh start, retained resume, compatible fresh
handoff, restart recovery, duplicate delivery, stale source rejection, session
directory provenance, attachment and stop receipts, and cleanup after partial
failure on both supported operating systems.

This goal preserves existing unsupported results and does not promote a harness
without its separate capability goal. It depends on 0006a and 0006b, is one
independently mergeable PR, and must fit one agent run of at most 12 hours.
