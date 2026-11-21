# rf-w2-02 strict-json-decoder plan — one ordered decode per strict mutation

Contract: `goal-rf-w2-02-strict-json-decoder.md` (GDD, 2026-11-21; reconciled conductor decision
in the same file, C1-C7), the Elixir half of program-plan §2 M11 / §5 W2-02, from finding CW-1
(`<gdd-state>/refactor/findings/control-web.md:166-196`). Coordinate baseline: this worktree's
HEAD `27486cbc5410bfa311b1646d1d67fad9abefaba2` (`27486cbc`, #147 merged) — every base line
below was read at that revision. Shape reference: the Go half of the same finding, W1-09
(#140, `daemon/internal/contracts/schema.go`), whose increment deleted a duplicate traversal
between an entry point and its validator while keeping every verdict and message.

## Pass inventory at the baseline (why 2 decodes + 1 discarded walk)

A strict-scope JSON mutation (`POST|PUT|PATCH|DELETE` on `/api/v1/**` or `/portal/api/**`,
`application/json` or `+json`) runs:

| # | Pass | Carrier | Anchor (base) | Disposition |
| --- | --- | --- | --- | --- |
| 1 | `Plug.Conn.read_body` via the wrapped body reader `read_json_body/3` | raw bytes | `strict_evidence_json.ex:188-199` | **remove** the wrapper (the body reader stays `{Plug.Conn, :read_body, []}`) |
| 2 | `Jason.decode(..., objects: :ordered_objects)` duplicate scan | whole document | `:201-213` (`validate_duplicate_json_members/1`) | keep once, inside the decoder |
| 3 | materialize walk building plain maps, result discarded | decoded tree | `:225-259` | keep once, same walk, result *kept* as `body_params` |
| 4 | `Plug.Parsers.JSON` → plain `Jason.decode!/1` | raw bytes | `deps/plug/lib/plug/parsers/json.ex:79-96`; decoder set at `endpoint.ex:70-73` | **remove** (the strict options install the ordered decoder instead) |

So one mutation today decodes the same bytes twice and walks the first result only to throw it
away; the second parse is the one whose result is used. The deferred evidence route
(`capture/2` `:87-111`, `raw_body_private_key/0` `:37`, `materialize/1` `:47-53`) is already
single-pass by design and keeps its shape (C2); `daemon_controller.ex:401` stays its caller.

## Deliverable shape

1. `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex`:
   - new top-level module `SymmetryControlWeb.StrictJSONDecoder` **in this same file** (see
     trade-off 1): `decode!/1` is the `Plug.Parsers.JSON` decoder MFA — one
     `Jason.decode/2` (`objects: :ordered_objects`) followed by one duplicate-check-and-
     materialize walk; a repeated member raises `DuplicateJSONKeyError`, a malformed body
     re-raises the `Jason.DecodeError` the parser returned (so Plug wraps it exactly as it
     wraps the plain decoder's). `decode_object/1` is the same ordered decode restricted to a
     top-level object with the current tagged errors (`:malformed_json`,
     `:expected_json_object`, `:duplicate_json_key`), and is what `materialize/1` now calls;
   - `init/1` builds the strict options by swapping only the JSON parser's decoder for
     `{StrictJSONDecoder, :decode!, []}`; the plain options keep `Phoenix.json_library()`
     (C1). `endpoint.ex` therefore needs no edit — `json_decoder: Phoenix.json_library()`
     at `:73` already is the C1 shape; it stays in the owned set untouched;
   - `parse_strict_json_mutation/2` rescues `Plug.Parsers.ParseError` whose `:exception` is the
     duplicate-key error and answers the current envelope; every other `ParseError` is
     re-raised unchanged (`reraise/2`);
   - delete `read_json_body/3`, `strict_parser_options/1`, `strict_json_parser/1`,
     `validate_duplicate_json_members/1`, the plug-local `decode_object/1` and the
     `materialize_object/1` / `materialize_pairs/2` / `materialize_value/1` /
     `materialize_list/2` walk (it moves, once, into the decoder). Keep `capture/2`,
     `raw_body_private_key/0`, `materialize/1`, `json_options!/1` (its callers are `capture/2` and
     `init/1`, which validates the parser options at boot) and the duplicate-member →
     `Protocol.error(:invalid_request)` mapping.
2. `control/test/symmetry_control_web/plugs/strict_evidence_json_test.exs`: characterization
   tests that are green on the base bytes (endpoint-level duplicate envelope byte-for-byte,
   malformed-body parser rendering, `+json` on a mutation route, top-level array `_json`,
   empty body, `/portal/login` first-wins) plus one wiring pin that is red on base: the strict
   options hold `{StrictJSONDecoder, :decode!, []}` with the untouched body reader while the
   plain options keep `{Jason, :decode!, []}`. No existing test is renamed, deleted or
   weakened.
3. This plan file.

No in-repo document describes the double parse or the wiring (checked: `docs/design/**`,
`docs/runbooks/**`, `contracts/README.md` name the duplicate-key *policy*, not its
implementation; the 2026-09-29 ablation evidence records bind their own revision), so nothing
outside the owned set goes stale. `docs/TODO.md` holds no `W2-02` entry, so no TODO closes.

## Exact edge-case semantics (base → delivered)

| Input on the strict scope | Base | Delivered |
| --- | --- | --- |
| valid object | `body_params` = plain map | identical (same keys, same values, same types; ordered objects materialized) |
| duplicate member, top level or nested, incl. `\uXXXX` aliases | 400 `Protocol.error(:invalid_request)` + halt | identical, byte-for-byte (`{"error":{"code":"invalid_request","message":"request payload is invalid"}}`) |
| duplicate member inside a top-level array | rejected (walk recurses into lists) | identical |
| equal member names in distinct objects | accepted | identical |
| empty body, JSON content type | `body_params` `%{}`, decoder not invoked (`json.ex:77-78`) | identical |
| top-level array / scalar | accepted, `body_params` `%{"_json" => value}` (`json.ex:82-86`) | identical |
| malformed JSON | `Plug.Parsers.ParseError` propagates, 400 rendered by `ErrorJSON` fallback ("request is invalid") | identical class and rendering |
| malformed *and* duplicate | malformed wins (decode fails before the walk) | identical precedence |
| `application/vnd.*+json` | strict decoder applies | identical |
| non-JSON content type, non-mutation method, non-`/api/v1\|/portal/api` path, evidence route | plain parser / capture | identical |
| `POST /portal/login` with duplicate members | plain decoder, first-wins, not rejected | identical |

## Evidence plan (all gates foreground; never a measurement beside the suite)

1. Characterization tests first, on base bytes: `sh <gate-runner> test test/.../strict_evidence_json_test.exs`
   — the eight C3/C4 additions green, the wiring pin red (red-on-base is the pin's purpose).
2. Implementation; then the same file green with the pin.
3. Contract gates, final bytes, serially:
   - `mix format --check-formatted` → empty, exit 0
   - `mix compile --warnings-as-errors` → exit 0
   - targeted `mix test test/symmetry_control_web/plugs/strict_evidence_json_test.exs
     test/symmetry_control_web/controllers/protocol_controller_test.exs
     test/symmetry_control_web/controllers/goal_controller_test.exs
     test/symmetry_control_web/controllers/goal_machine_controller_test.exs` (C5: the two goal
     files are read-only oracles) → exit 0 with counts
   - `git diff --check` → exit 0; profile-path grep over the whole diff → no match
   - the mix gate runner defaults `MIX_TEST_PARTITION=rf-w2-02` and unsets
     `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`; full `mix test` and the CI `control`
     job stay the conductor's gate-runner steps (never two full suites on this host).
4. Single-pass accounting (C6), two independent witnesses:
   - structural: the wiring pin above, one `Jason.decode` call site in the delivered decoder
     path (`grep -c 'Jason.decode'` = 1 in the plug file, against 2 base call sites plus the
     library's plain decode), and the deleted body-reader wrapper;
   - measured: a throwaway script on the CW-1 160 KB reference body (a goal-contract-shaped
     document padded to 160 KB) timing, in one process on the same bytes, the base composition
     (ordered decode + walk, then plain decode — the walk inlined verbatim from the base file,
     since the delivered tree no longer holds it) against the delivered
     `StrictJSONDecoder.decode!/1`; medians over repeated runs. This host's timer is noisy
     (W1-09 shipped a fairness check for exactly that), so the report carries the medians and
     the counters, not a per-run speed claim.
5. Mutation checks (red shown, then restored by re-applying the final file bytes and
   re-verifying their hash):
   - M1: `parse_strict_json_mutation/2` re-raises the wrapped `ParseError` instead of mapping
     it → the duplicate-envelope byte pin goes red (the response becomes the ErrorJSON
     rendering "request is invalid"), the malformed pin stays green;
   - M2: the strict options keep the plain decoder (decoder swap deleted) → the wiring pin
     goes red, and the duplicate tests on the strict path go red because the base body-reader
     wrapper is gone too (the duplicate is then accepted first-wins by the modular evidence
     path test but rejected by… measured, then restored);
   - M3: the non-strict branch also gets the strict options → the `/portal/login` first-wins
     test goes red (a 400 replaces the redirect).
6. `mix credo` on the changed file: same (or lower) issue count as base — the contract forbids
   new `credo:disable` suppressions; recorded as supporting evidence, not a contract gate
   (the CI `control` job does not run credo).

## Design trade-offs

1. **Decoder module lives in the owned plug file, not a new file.** The owned set is STRICT
   and the program-plan row names exactly the plug, `endpoint.ex` and the test file; the plan
   file bullet is the only new file the conductor declared. `plugs/auth.ex` already carries
   three top-level modules in one file, so the form is local convention. Defining
   `SymmetryControlWeb.StrictJSONDecoder` first in the file also keeps the compile-time struct
   pattern (`%DuplicateJSONKeyError{}`) resolvable; the plug module keeps the file's name.
   A separate `strict_json_decoder.ex` would read marginally cleaner but would write outside
   the declared set for no functional gain. Recommendation on record: same file.
2. **The duplicate-key exception moves under the decoder.** It is raised by the decoder and
   matched by the plug, so it belongs with its raiser; `git grep DuplicateJSONKeyError` finds
   no reference outside this file at base, so moving
   `StrictEvidenceJSON.DuplicateJSONKeyError` → `StrictJSONDecoder.DuplicateJSONKeyError`
   changes no caller and no wire behavior. (C3's `strict_evidence_json.ex:16` citation is a
   base coordinate, not a module-path requirement.)
3. **Rescue the wrapped `ParseError` in the plug rather than raising `ParseError` from the
   decoder.** Plug wraps *any* decoder exception (`json.ex:89-96`), so a decoder-raised
   `ParseError` would be wrapped a second time and malformed bodies would change their
   exception shape; matching on the inner exception keeps malformed bytes on exactly the base
   rendering. The plug-level match is also where `Protocol.error/2` already lives.
4. **One walk, shared by `decode!/1` and `materialize/1`.** The deferred evidence path needs
   the same duplicate check and materialization; keeping the base's two copies would leave
   `materialize/1` on its own decoder and re-open the same asymmetry. The walk's error tags
   (`:malformed_json`, `:expected_json_object`, `:duplicate_json_key`) are unchanged, pinned by
   the existing tests; the empty-capture clause (`{:ok, ""}` → `{:ok, %{}}`) is pinned by the new
   plug-level empty-evidence-capture test.
5. **Not endpoint-wide.** CW-1's proposal installed the decoder in `endpoint.ex`; the
   conductor ruled strict scope stays the strict route set (C1), so non-strict routes keep the
   plain decoder. Cheaper and narrower than the finding.
6. **The ordered decode is the floor.** The measured target is one ordered parse plus one
   materialize walk (≈1.3× plain decode), not "1×"; the second parse and the discarded walk
   are what disappear. No hand-rolled single-pass Jason replacement is attempted —
   `:ordered_objects` is the only duplicate-capable decode the dependency offers, and a
   hand-rolled parser would trade a library call for owned tokenizer code.

## Out of scope (contract clause 4, with reasons)

- Controller consolidation (W2-03), `portal_json.ex` (W2-04), Go strictness parity (W1-09),
  client-side strictness (W2-12), the test-only seam removal (W5-06): other slices' files.
- `endpoint.ex`: no edit is needed once the decoder is installed through the plug's `init/1`
  (C1); the file stays untouched rather than carrying a comment-only diff.
- The evidence route's `capture/2`/`materialize/1` shape (C2) and the deferred-materialization
  design behind it.
- Plug-internal behavior (body-reader negotiation, `pass`, multipart): W1-02's area, already
  settled.
