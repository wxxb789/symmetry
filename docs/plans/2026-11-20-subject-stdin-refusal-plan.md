# subject-stdin-refusal plan — pin the subject batch stream's stdin-write refusal

Contract: `goal-subject-stdin-refusal.md` (GDD, 2026-11-20). Baseline for every coordinate:
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, origin/main = this worktree's HEAD),
branch `fix/subject-stdin-refusal`. The increment adds one Go test and one scripted helper-child
variant in `daemon/internal/workspace/subject_test.go` and moves the covered TODO entry to
`docs/archive/CLOSED-TODO.md`; no production source change is planned — the contract admits
`subject.go` changes "only if the new case exposes a defect".

## Public/API shape

No API change. The pin is a package-internal test of the unexported batch stream:

- Production branch under test: `subjectGitBatchStream.streamObject`
  (`daemon/internal/workspace/subject.go:744`), whose request write (`:750`) refuses through
  `stream.desync(stream.streamError(fmt.Errorf("request object %s from Git batch stream: %w", …)))`
  (`:751`, `streamError` `:808`, `desync` `:819`) and whose `close` (`:828`) then takes the
  desynced teardown route: `terminate()` through the path's cancellation policy, one `Wait`, and
  the route's unresolved-process `sentinel` joined into the digest error.
- New test: `TestSubjectBatchStreamWriteRefusalDesyncsAndReapsChild`.
- New scripted child variant: `close-frame-block` in `TestWorkspaceBatchHelper`, served by
  `serveWorkspaceBatchCloseReadEndThenFrame` beside `serveWorkspaceBatchFrames`
  (`subject_test.go:1155`).

## Exact edge-case semantics

Ordering — the contract's central edge case. The child:

1. reads request #1;
2. closes its own stdin read end (`os.Stdin.Close()`);
3. writes the complete, in-sync frame for request #1 (echoing the requested oid with the real
   committed blob bytes) and flushes it;
4. blocks without exiting.

The digest reaches request #2 only after consuming response #1 to its record boundary (header,
exact content, trailer; `subject.go:753-783`), which requires step 3's bytes to have reached the
pipe, which is ordered after step 2's close returned. So when the digest writes request #2, the
pipe provably has no reader: the write is refused on every platform, and the refusal cannot hide
in the pipe buffer. A variant that answers first and only then closes or exits can leave request
#2 inside the pipe buffer and move the failure onto the response read; this variant cannot.

Assertions of the pin, each bound to a clause the entry names:

1. the digest fails and the error text contains
   `request object <oid of the second tree entry> from Git batch stream:` — the refusal is the
   write, and it names the object it could not request (a response-read refusal would spell
   `read Git batch response header for object …`; a first-request refusal would name the first
   entry);
2. `stream.desynced` is true — the branch marks the stream desynced;
3. the route's unresolved-process fence (`startSubjectGitBatch`'s `sentinel`) is joined — the
   digest's error proves the desynced routing, not just the child's death;
4. the context stays alive (`ctx.Err() == nil`, no `context.Canceled`) so `streamError` surfaces
   the pipe error instead of a cancellation; the test's own deadline is `testHangGuard` away and
   only bounds a regression-induced hang;
5. the child is waited for and reaped (`ProcessState` pid identity) and did not exit on its own
   (`!ProcessState.Success()`); because the child blocks forever, the digest's own terminate is
   the only way the call can return at all.

## Fixtures and entry order

`newSubjectRepository` (one committed blob) plus `writeSubjectDigestManyBlobsFixture`
(`subject_test.go:810`, 250 blobs) give a multi-blob fixture commit, per the entry's direction.
Entry order for the two requests is read from `git ls-tree --full-tree -r -z` — the same command,
source and order that `parseTreeManifest`/`hashTreeManifest` consume — so the assertion names the
object the digest actually requests second.

## Evidence plan

The change is behavior-neutral (test-only), so the deliverable's failure control is a re-break of
the pinned branch with both runs recorded in `answer.md`:

- baseline on the frozen tree: new test green (Windows and WSL Ubuntu-24.04);
- mutation A: drop the object name from the refusal in `streamObject` → the pin goes red on
  assertion 1;
- mutation B: drop `desync` from the write-refusal branch → the pin goes red on the desynced
  assertion (assertion 2; the live-context assertion would also fail if it were reached). The
  fence observable is not separated by this mutation: `finish()` joins the same sentinel on its
  `ctxErr` path. Bounded by the test's own context deadline rather than the harness timeout;
- mutation reverted (source back to the HEAD bytes), re-run green; the tree left for
  verification carries no `subject.go` diff.

Gates, run on the frozen tree: Windows `gofmt -l internal/workspace`, `go vet ./...`,
`go test ./internal/workspace/... -count=1` — on the final test bytes the loaded host aborted
this plain command twice at the default 10m test-binary timeout (600.509s/600.388s, 0 test
failures), and green came with `-timeout 45m` (`ok 1119.686s`); CI's `daemon-windows` job runs
the package under `-timeout 600s`. WSL Ubuntu-24.04 the same three plus
`go test -race ./internal/workspace/... -count=1` (the new path exercises process/pipe teardown);
`git diff --check`. Not required by the touched paths: Elixir/Portal/contracts/frontend suites
(no such file touched), no measurement harness is involved. No measurement/benchmark run is
executed concurrently with the full test suite.

## Design trade-offs recorded

- **Close-before-response over wait-for-exit.** The entry allows either ordering. Closing before
  the response needs no readiness file or acknowledgement seam in the digest loop, and its
  happens-before argument is one step: the digest cannot read the response before the close
  completed. A "wait for the child's exit" variant would need the parent to observe the death
  before request #2, which `hashTreeManifest` gives no seam to observe.
- **The child blocks instead of exiting.** With a live child the reap assertion stays falsifiable:
  the digest call returning at all proves the teardown killed a live owned child and waited for
  it, matching the desynced-teardown contract. A regression that removes the kill shows up as the
  test's own context deadline expiring, not as a harness timeout.
- **Real blob bytes for request #1.** The parent fetches the first entry's committed content so
  the served frame is exactly the response the protocol expects for that oid; serving synthetic
  content would leave "complete in-sync frame" unobservable byte-wise.
- **Reuse of the many-blob fixture.** The entry names it, and a many-entry digest makes "the
  refusal is on the second request" independent of any single entry's shape.

## Out of scope

- Other `docs/TODO.md` entries and any source refactor beyond the fix (contract clause 4).
- Any lasting `subject.go` edit: the baseline behavior is pinned as correct; the recorded
  mutations exist only to falsify the pin and are reverted before the tree is handed off.
- `docs/design/protocol.md` and other contract documents: a test-only addition makes no wording
  stale, and no canonicalization/grammar rule changes.
- CI workflow edits: the existing Windows and Linux Go gates already run this package.
