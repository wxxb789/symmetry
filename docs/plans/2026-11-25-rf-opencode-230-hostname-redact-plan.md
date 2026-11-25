# rf-opencode-230-hostname-redact — implementation plan

Goal contract: `goal-rf-opencode-230-hostname-redact.md` (owner ruling K1, 2026-11-25). Dispatch
head `613999840a4e4bc1d1169a37d3775b1814671015`; branch
`refactor/rf-opencode-230-hostname-redact`. The contract settles the change itself; only the
implementation choices it leaves open are decided here.

Shape reference: PR #288's PRE-2 hostname edit of the 2.4.1 records — the value→`<HOSTNAME>`
constant-token substitution with an old→new digest table and a byte-level equality argument
(`docs/evidence/rf-acc-opencode-reeval-2026-11-25.md`, PRE-2 passage and §4) — applied to the
frozen 2.3.0 receipts.

## Decisions

| id | open choice | decision |
| --- | --- | --- |
| D-1 | substitution token and sites | `<HOSTNAME>` exactly as PRE-2, at the three sites the pre-change census locates (one occurrence per receipt: `/observation/uname/1` in the Linux receipt; the escaped `operations[0].body` `/api/info` 200 record in each Windows receipt). The quoted string value is replaced; the token needs no extra escaping inside or outside the `body` string. |
| D-2 | edit mechanics | byte-level replacement preserving LF-only endings and the final newline; the receipts are not reformatted, re-rendered or re-ordered. Proven afterwards by the mapped-byte equality (mapped pre-change blob == delivered file) and by the unchanged line counts (67/811/1550). |
| D-3 | digest rebinding | none needed, and none performed: the census found 0 hits for each pre-change `sha256` (full and 8/12/16/24/32-char prefixes) and each pre-change Git blob id (full and 8/12/16/24-char prefixes); the only digest citations near these receipts are the two `witness_sha256` collector-identity values (2.3.0 README and the receipts' own field), which bind the collector script's bytes, not the receipt's — recorded as exempt rows with that reason in the evidence note §3. |
| D-4 | replacement proof | no committed classifier or loader reads the receipts (`git grep 'testdata/opencode'` finds exactly one code hit, the witness suite's 2.4.1 glob), so no verdict comparison over two images exists to run. Replacement proof: mapped-byte equality, parsed-JSON equality under the mapping, and the receipts' embedded `classes` verdicts compared object-for-object. |
| D-5 | W6-04 freeze interaction | the parked keep-and-freeze contract names `testdata/opencode/2.3.0/README.md` as frozen bytes; this later owner ruling owns `2.3.0/**` and requires the one-line redaction note there, so the README gains that note while `daemon/internal/harness/opencode/**` (the package) and every other frozen path stay byte-identical. The freeze's purpose — keep the evidence and its citation from `docs/design/harness.md` — is preserved, not weakened. |
| D-6 | documents the change supersedes | the 2.4.1 README's residual sentence and the reeval evidence's §14 bullet both state that the 2.3.0 receipts still carry the value. They are historical records of PR #288, outside this contract's owned files, so they are left unedited and named in the evidence note §7 with the 2.3.0 README line superseding them. Adding the note's own file is within "the new evidence note"; `docs/plans/**` is this repo's designated plan location. |
| D-7 | TODO ledger | no `docs/TODO.md` entry: nothing in the contract's scope remains open. The D-6 pointer lives in the evidence note (§7) for the operator. |

## Evidence plan

1. Pre-change censuses (hostname per file and total; receipt digests and blob ids; loader census;
   relay-host, credential-shape and host-path censuses over the tree and the change's added lines).
2. The three substitutions, then mapped-byte/parsed-JSON/`classes` equality per receipt.
3. Records: the one-line 2.3.0 README note and
   `docs/evidence/rf-opencode-230-hostname-redact-2026-11-25.md` (census commands and counts,
   old→new digest table, equality proofs, gate table).
4. Gates on the final tree: `go test -count=1 ./internal/harness/...` from `daemon/`; the CI
   structure/self-test suites; `model-routing-record-sanitize.py --check --root-only` on the new
   record; `check-record-cleanliness.py`, `check-citation-existence.py` and `jscpd-changed.py`
   over a scratch-materialized head commit (the working tree is never committed by this stage).
5. Post-change census at that head: hostname, relay host, credential value, key shape and host
   absolute path — counts only, the values never printed.

## Out of scope (with reasons)

- Git history and the superseded raw artifacts outside the repository (contract: rewriting
  history is out of scope; the external artifacts are not in the tree).
- Every other byte of the three receipts, including their pre-existing recorded host absolute
  paths: the contract substitutes the hostname value and forbids other byte changes.
- The 2.4.1 README, the reeval evidence record §14, the package `daemon/internal/harness/opencode/**`,
  `docs/design/harness.md`, the CI assertions and the acceptance script (D-5, D-6).
- Any new test file: the contract's owned set holds no Go source, and no committed test reads
  these data files (D-4); the censuses and the equality proof are the evidence instrument.

## Fix pass (2026-11-26, conductor ruling after review round 1)

The goal contract's conductor ruling (review round `DECISION_NEEDED`) widens the scope from the
hostname value to whole-tree hygiene of three classes — the machine hostname, the relay host and
host absolute paths carrying the user-profile name — and settles W1-W5. This fix pass delivers
that widening, so the "Out of scope" list above and D-3/D-4/D-6 describe the implement pass only.

- W1 substitution: hostname value to `<HOSTNAME>`, relay host to `<RELAY_HOST>`, profile-name path
  prefix to `<HOST-PATH>`; each changed file proves its pre-change bytes map onto the delivered
  bytes under that one rule.
- Amendment with the exact conflict: the OmO Windows transcript fixture must keep holding absolute
  paths. `decodeSessionState` refuses a non-absolute `sessionFile`
  (`daemon/internal/harness/omo/rpc.go:289`) and `TestPinnedNativeTranscript` compares the
  fixture's binary path against its Go literal, so dropping the drive root reddens that test; the
  two Windows fixtures, the fixture README and the two Go files that pin the path keep the drive
  root and replace only the account-home run. Every other file replaces the whole prefix.
- W2-W5: the digest census, the old-to-new table, the frozen-record rationale and the supersede
  pointers are in the evidence note; the 2.4.1 README and the reeval record §14 are corrected in
  place.
- Residual (retired by the PR #291 round-1 ruling, 2026-11-26): the opt-in native acceptance pin
  is re-expressed home-relative in `daemon/internal/harness/omo/native_test.go` (size and digest
  pins unchanged), so the `docs/TODO.md` entry is closed and the pin resolves again.
