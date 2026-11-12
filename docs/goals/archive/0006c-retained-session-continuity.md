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

## Owner-approved scope clarification - 2026-11-11

The owner selected the preserved-unsupported scope for Linux v2 retained
resume. New Linux Pi/OpenCode native runs own a fresh B clone per RunKey;
reusing that clone requires a separately fenced B-clone reuse grant bound to
the predecessor stop receipt, target identity, new RunKey and workspace
fingerprint. That grant is not implemented by this goal. Linux resume admissions
therefore fail closed before workspace, clone, Control attach or native start.

Windows retained resume and Linux/Windows compatible fresh handoff remain the
continuity paths demonstrated by this goal. The missing Linux reuse grant is a
prerequisite for 0006f's Linux retained-resume evidence and must not be reported
as a passed capability. This clarification does not change the existing
fail-closed rules, fences, credential locality or stop-receipt requirements.
