# rf-opencode-230-hostname-redact — redacting the machine hostname from the frozen OpenCode 2.3.0 receipts

Delivered 2026-11-25 under goal `rf-opencode-230-hostname-redact` (owner ruling K1). The
machine hostname value, the relay host and the credential never appear in this record: the
censuses below name the value abstractly and print counts and file locations only.

## 1. Why, and the pre-change census

The repository is public and the rule is that no host identifier stays in the current tree.
Before this change the machine hostname value was carried by three committed receipts, one
occurrence each. Census over the head revision `613999840a4e4bc1d1169a37d3775b1814671015`:

```sh
git grep -i -c -F -e "<hostname value>" HEAD        # per-file counts
git grep -i -o -F -e "<hostname value>" HEAD | wc -l  # occurrence total
```

| receipt | occurrences | site (content-located) |
| --- | --- | --- |
| `daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json` | 1 | line 19, `/observation/uname/1` (the `uname` list's node-name entry) |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json` | 1 | line 29, inside `operations[0].body` — the recorded `GET /api/info` 200 response body (a JSON-escaped string; inner pointer `/hostname`) |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json` | 1 | line 29, same shape as above |

Result: 3 files, 1 occurrence each, 3 occurrences total; no other tracked file held the value
in any case variant.

The redaction follows the PRE-2 precedent of PR #288
(`docs/evidence/rf-acc-opencode-reeval-2026-11-25.md`, the PRE-2 fix-pass passage, and the
2.4.1 README's §2 replacement table): the value is replaced by the constant token
`<HOSTNAME>` and nothing else changes. Out of scope by the contract, and therefore untouched:
Git history (the pre-change bytes stay reachable), every other byte of the three receipts —
including the pre-existing recorded host absolute paths inside them, which are captured
history and not this contract's subject — and the receipts' `witness_sha256` field (§3).

## 2. The substitution

One byte-level substitution per file, `"<hostname value>"` → `"<HOSTNAME>"` as the quoted JSON
string value (the token carries no character that needs escaping inside either the plain
receipt text or the escaped `body` string, so the nested JSON stays well-formed):

| receipt | bytes | lines (LF only, final newline) |
| --- | --- | --- |
| `linux-2026-11-14.json` | 4263 → 4258 (−5) | 67 → 67 |
| `windows-2026-11-14.json` | 29956 → 29951 (−5) | 811 → 811 |
| `windows-2026-11-14-r1.json` | 52069 → 52064 (−5) | 1550 → 1550 |

The −5 bytes are the token length difference (15 → 10); line endings stay LF-only with no `CR`
byte introduced, and the final newline is preserved. §4 proves that no other byte moved.

## 3. Digest census — every digest that could bind the receipt bytes

No in-tree citation binds the pre-change receipt bytes. For each receipt the pre-change full
`sha256` and its 8/12/16/24/32-character prefixes, plus the pre-change Git blob object id and
its 8/12/16/24-character prefixes, were probed against every tracked file of the head
revision:

```sh
git grep -F -c -e "<probe>" HEAD    # one probe per row; 11 probes per receipt
```

| receipt | sha256 probes (8/12/16/24/32/64) | blob probes (8/12/16/24/40) |
| --- | --- | --- |
| `linux-2026-11-14.json` | 0, 0, 0, 0, 0, 0 | 0, 0, 0, 0, 0 |
| `windows-2026-11-14.json` | 0, 0, 0, 0, 0, 0 | 0, 0, 0, 0, 0 |
| `windows-2026-11-14-r1.json` | 0, 0, 0, 0, 0, 0 | 0, 0, 0, 0, 0 |

No `SHA256SUMS` file names these receipts (`find . -name 'SHA256SUMS*'`: none), no test, CI
step or evidence text cites their digests, and the tree holds no self-digest inside the
receipts themselves. There is therefore no digest of the old bytes left stale and none to
re-derive. The digest census hits that do exist are exempt rows:

| digest | cited at | reason it is not re-derived |
| --- | --- | --- |
| `1cee1970f95fd9d5800a3bf7c890d5d21134ff656671fbf0bbab786280cff607` | `2.3.0/README.md:22` and `windows-2026-11-14-r1.json:14` (`witness_sha256`) | the collecting collector script's digest at collection time, carried as the collector-identity field; it does not hash the receipt bytes, so the redaction cannot and must not change it |
| `55501a9f01d3dcd76edbb1983efe0dfe474b38190f6fb07d5cd9558621a6e65e` | `2.3.0/README.md:25` and `windows-2026-11-14.json:14` (`witness_sha256`) | same: the collector identity of the earlier, superseded collection |

## 4. The substitution is the only change

Per receipt, the pre-change blob is read from Git at the pinned base revision
`613999840a4e4bc1d1169a37d3775b1814671015`, the claimed substitution is applied to it,
and the result must be byte-identical to the delivered file; additionally the parsed JSON must
be equal under the same mapping and the recorded classifier verdicts (`classes`) must be equal
object-for-object:

```sh
python3 - <<'PY'
import pathlib, re, subprocess
# pinned base revision: a re-run at the delivered commit must not compare the redacted bytes with themselves
BASE = "613999840a4e4bc1d1169a37d3775b1814671015"
VALUE_SOURCE = "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json"
FILES = ["daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json",
         VALUE_SOURCE,
         "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json"]
def base_bytes(path):
    return subprocess.run(["git", "show", f"{BASE}:{path}"], capture_output=True, check=True).stdout
VALUE = re.search(rb'\\"hostname\\":\\"([^\\"]+)\\"', base_bytes(VALUE_SOURCE)).group(1)
for path in FILES:
    old = base_bytes(path)
    assert old.count(VALUE) == 1, path
    new = pathlib.Path(path).read_bytes()
    assert old.replace(VALUE, b"<HOSTNAME>") == new, path
print("mapped-old == new for all three receipts (1 occurrence each)")
PY
```

Parsed-JSON and `classes` equality were checked with the same value mapping applied to every
string of the pre-change parse before the comparison.

| receipt | mapped-old == new (bytes) | parsed JSON equal | recorded `classes` equal | occurrences mapped |
| --- | --- | --- | --- | --- |
| `linux-2026-11-14.json` | yes | yes | yes (7 classes) | 1 |
| `windows-2026-11-14.json` | yes | yes | yes (7 classes) | 1 |
| `windows-2026-11-14-r1.json` | yes | yes | yes (7 classes) | 1 |

Both redacted receipts re-parse, including the escaped `operations[0].body` string: its inner
object parses and still carries the `hostname` key with the token value, so the recorded
`/api/info` body shape is preserved.

Loader census: no committed code loads the three receipts. `git grep -n 'testdata/opencode'
HEAD` finds exactly one executable reference to that tree — the OpenCode witness suite's own
`daemon/internal/harness/testdata/opencode/2.4.1` glob
(`scripts/ci/test-opencode-v2-support-witness.py:771`) — plus prose in
`docs/design/harness.md`, the 2.4.1 README and evidence records; the `2.3.0` token elsewhere in
code is the adapter version string (`daemon/internal/harness/opencode/adapter.go:28`), not a
receipt read. With no classifier or loader to run over both images, the replacement proof for
that clause is this section's parsed-equality plus the receipts' own embedded classifier
verdicts, shown equal row by row.

## 5. New digests

| receipt | old sha256 | new sha256 | old blob | new blob |
| --- | --- | --- | --- | --- |
| `linux-2026-11-14.json` | `9bf30bb4d51a24dc5bd911efaa1755219a584afa65db6155601424158cba0c1f` | `afbfd51ce34223f36e5c33e2716aa0ad4b2f8931d0b6d909139bf4fbf6214ebc` | `f55c0684771b0cd2ca0b22ee561a340e5a860501` | `adc9ecabaab3fe14f11d590375ece27b2b95fda9` |
| `windows-2026-11-14.json` | `4fc793d4c63dbee4c1f3dae011bd7aa806bedc30e74fe012dbf98f4356222d46` | `6c201d10477625ec41a8999cb5668602807ebd919b6eec05cec973dac15ea157` | `c0318edf5765be3bbec169ec65c6db026baee4a7` | `703f879012e4f7229d391dcf8bdaa00d9b8d54ff` |
| `windows-2026-11-14-r1.json` | `ed6707f11defd7c60483f152d1002be47fd2bc5d75fd08388e10c0372889275a` | `0b702f03ca6eaed3121e1172ee24569c5363fcea1f91c2e508fc319c86f0cb2d` | `cb3b997ef1eadc75ad1fa28d284549d6da1c2c2f` | `db80114815af6493fcd726a424a830e0c7ee6ed5` |

The pre-change digests stay quoted above and in this note only; §3 shows the tree carried no
citation of them, so none of the four documents the change edits gains a stale digest.

## 6. Gates and censuses run for this change

Local, foreground, against a scratch-materialized head commit of the delivered tree
(`git commit-tree` over a temporary index; the branch and its index are untouched). `exit` is
the command's own exit status.

| gate | command (working directory) | exit | result |
| --- | --- | --- | --- |
| Go harness suite | `go test -count=1 -timeout 900s ./internal/harness/...` (`daemon/`) | 0 | 9 packages `ok` |
| CI self-test suites (structure extraction, record cleanliness, citation existence, record sanitizer) | `python3 scripts/ci/test-ci-structure-extraction.py`, `test-check-record-cleanliness.py`, `test-check-citation-existence.py`, `test-model-routing-record-sanitize.py` | `OK` each | 40 / 19 / 49 / 57 tests |
| record cleanliness over the change | `python3 scripts/ci/check-record-cleanliness.py --base 613999840a4e… --head <scratch>` | 0 | `1 changed record(s) pass the root-only check` |
| record sanitizer, root-only, on the new record | `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only < docs/evidence/rf-opencode-230-hostname-redact-2026-11-25.md` | 0 | no host absolute path, no refusal |
| citation existence over the change | `python3 scripts/ci/check-citation-existence.py --base 613999840a4e… --head <scratch>` | 0 | `no audited document added or modified` |
| clone gate over the change | `python3 scripts/ci/jscpd-changed.py --base 613999840a4e… --head <scratch>` | 0 | 6 changed files, 1748 scanned sources, 1262 corpus clones, 1 tolerated (the two Windows receipts' shared 28-line block), none new |
| clone gate self-test | `python3 scripts/ci/test-jscpd-changed.py` | `OK` | see the stage report |
| hostname census, whole tree, case-insensitive | `git grep -i -c -F -e "<hostname value>" <scratch>` | 1 = no match | 0 files |
| added-line censuses over the change | `git diff -U0 613999840a4e… <scratch>` added lines | 0 introduced | hostname 0, relay host 0, credential value 0, sk-shaped 0, unescaped host absolute path 0; the two substituted Windows receipt lines keep their pre-existing escaped temp paths verbatim (the byte-equality proof in §4 is what makes them byte-for-byte the base lines minus the value), because the contract's substitution clause forbids any other byte change |

Context the census prints for completeness, unchanged by this contract: the relay host is
carried by 8 pre-existing tree files before and after the change (the two Windows receipts'
own unchanged `provider` field, `opencode/2.3.0/provider-discovery.json`, the omo 5.1.69
capture pair, two earlier evidence records and one archived goal); five pre-existing files
carry `sk-`-shaped tokens (contract fixtures and recorded harness help text, not credentials
of this change). Neither set is part of this change's bytes — the change's 247 added lines
carry 0 occurrences of either — and the contract's substitution clause forbids touching any
other receipt byte, so they are recorded here rather than edited.

The pinned jscpd 4.3.0 for the clone gate came from this repository's sibling worktree
install (`node_modules` junction, `jscpd --version` = 4.3.0): the registry install failed
locally on a TLS handshake and then on the lockfile supply-chain policy. CI installs the same
pinned version with `pnpm install --frozen-lockfile`; the gate's version pin is unchanged.

## 7. Residuals and superseded statements

- The pre-change bytes remain in Git history. Rewriting history is out of scope by the
  contract; the census §1 is the check that no present-tree file carries the value.
- Two committed statements describe the 2.3.0 receipts as still carrying the value, and this
  change supersedes them:
  `daemon/internal/harness/testdata/opencode/2.4.1/README.md` (the paragraph after the
  re-collection table — "The frozen 2.3.0 receipts keep their `hostname` field verbatim — not
  edited, recorded as a residual") and
  `docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` §14's residual bullet ("The machine
  hostname value survives in two places outside this change: the frozen 2.3.0 receipts …").
  Both are historical records of PR #288 and lie outside this contract's owned files, so they
  were not edited; the 2.3.0 README now states the redaction and supersedes them, and this
  section is the pointer for a follow-up should the operator want the two historical
  sentences annotated in place.
- No test, CI structure suite or acceptance step loads or pins these receipt bytes (§3, §4),
  so the change deliberately adds no test; the equality proof and the censuses are the
  evidence. The 2.3.0 capture tree stays frozen in shape: same six files, same names, same
  line counts.
