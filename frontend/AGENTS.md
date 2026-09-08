# TypeScript frontend (target directory)

This directory initially contains instructions only. Build the application here
under goal 0007, following `docs/design/frontend.md` and `protocol.md`.

- React + Vite + pnpm; TanStack Router owns URL state; TanStack Query owns server
  cache; Effect owns IO decoding/errors/resource lifetime; React owns local UI.
- Use strict TypeScript, noUncheckedIndexedAccess, exactOptionalPropertyTypes,
  and discriminated unions. No `any`, double casts, or suppressions to bypass
  domain mismatches. External JSON starts as unknown and is decoded.
- Keep Effect execution at application/service boundaries, not scattered inside
  components. Pass cancellation through. One retry owner per operation.
- Never duplicate server permissions or transition policy in the UI. Display
  server allowed_actions and reject stale state after server conflicts.
- Reuse accessible primitives and design tokens. Preserve keyboard focus,
  drafts, selection, and scroll during refresh. Do not render raw agent HTML.
- Commands stay pending until authoritative acknowledgement. Optimistic state
  is appropriate for reversible view interactions, not execution or approval.
- Use the exact installed Effect major and its matching APIs. Do not mix v3
  examples with v4 RC documentation.

Target commands are defined in frontend.md; they do not exist in this PR.
Until migration, browser/ remains the actual Playwright suite and CI contract.
