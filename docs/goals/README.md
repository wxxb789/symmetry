# Next goals for local Codex

These are separately finishable outcomes, not three alternative plans.
Each goal file contains one ready goal payload. No goal is activated by this PR.

| Goal | Desired outcome |
| --- | --- |
| [0006](0006-durable-engineering-work.md) | Trustworthy long-horizon work across native harnesses |
| [0007](0007-modern-workspace.md) | Maintainable, polished Phoenix-hosted TypeScript workspace |
| [0008](0008-model-independent-quality.md) | Evidence-based lower-cost development with unchanged acceptance |

Read [fixed design](../design/README.md) and root AGENTS.md before executing.
Core schemas, stack, boundaries and protocol choices are already specified in
the design documents. Goal payloads define completion, not an implementation plan.
Codex can choose bounded tasks within those decisions; it cannot silently reopen
them. Feed the selected payload and its linked contract to your local goal intake.
Do not run all goals as one undifferentiated implementation instruction.

0007 can finish on the existing product APIs independently of 0006; goal-specific
screens integrate when 0006 is available, without production stubs. 0008 can
evaluate current Go/Elixir changes, but completing its new TS-stack coverage
requires a representative implementation from 0007. Native credentialed harness
verification for 0006 requires locally installed/authenticated agents. Lack of
that environment is reported as missing completion evidence.

Historical 0001-0005 remain untouched under archive/. Next goal number is 0009.
