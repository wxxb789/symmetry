# rf-opencode-230-hostname-redact — redacting host identifiers from the frozen OpenCode 2.3.0 receipts and the tracked tree

Delivered 2026-11-25; widened 2026-11-26 under the goal contract's conductor ruling (review round
`DECISION_NEEDED`); PR #291 round-1 review corrections applied 2026-11-26 (§8). Neither the machine
hostname value, the relay host, the user-profile name nor the credential appears in this record:
the censuses below name the value classes abstractly and print counts, file locations and digests
only.

## 1. Why, and the pre-change censuses

The repository is public and the owner's K1 intent is that the current tree keeps no host
identifier. The three value classes are the machine hostname, the relay host (the base host of the
operator's tencent-litellm relay as written in the OMP models config) and host absolute paths that
carry the user-profile name, plain and JSON-escaped. Git history keeps the pre-change bytes;
rewriting history is out of scope.

Hostname class, over the dispatch head `613999840a4e4bc1d1169a37d3775b1814671015`:

```sh
git grep -I -i -c -F -e "<hostname value>" <revision>   # per-file counts
git grep -I -i -o -F -e "<hostname value>" <revision>   # one line per occurrence
```

| receipt | occurrences | site (content-located) |
| --- | --- | --- |
| `daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json` | 1 | line 19, `/observation/uname/1` |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json` | 1 | line 29, escaped `operations[0].body`, inner `/hostname` |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json` | 1 | line 29, same shape |

3 files, 3 occurrences; no other tracked file held the value in any case variant.

Widened classes, over the pre-fix revision `c9606af4c3db5bab5de3db7bac22b75b4d038fe0` this fix
pass starts from, whole tree, counts only:

| class | files | occurrences | carriers |
| --- | --- | --- | --- |
| relay host, full spelling | 8 | 10 | the two Windows receipts, `provider-discovery.json`, the omo capture pair, two evidence records, the archived goal `0006k` |
| relay host, truncated to its first label | 1 | 1 | the evidence record `docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md` |
| user-profile path prefix, plain or escaped | 22 | 167 | the three receipts, the omo fixture set and its README, four harness run records under `testdata/`, the omp fixture manifest, two Go test files, three archived goals, two plans, the control-plane runbook |

The path-class probe is described in prose in §4; per-file counts are in §5. The class is absolute
paths containing the profile name under a `Users` or home component; the separate `/home`-rooted
spelling of the profile path probes 0 at every revision (§6).

## 2. The substitution

One per-class replacement, per file: the hostname value becomes `<HOSTNAME>`; the relay host
becomes `<RELAY_HOST>` (scheme and `/v1` path kept); a user-profile path prefix becomes
`<HOST-PATH>`, with the remainder of the path kept. The three receipts keep LF-only endings and a
final newline; their byte deltas are token-length deltas (4263 → 4242, 29956 → 29669,
52069 → 51630; line counts 67, 811 and 1550 unchanged).

Amendment with the exact conflict (2026-11-26; smallest deviation from W1). Removing the drive or
mount root from the OmO Windows transcript fixture is infeasible: `decodeSessionState` refuses a
non-absolute `sessionFile` (`daemon/internal/harness/omo/rpc.go:289`, sha256
`1e706bb27c87a3dd424eed0473518806d5a6d1f001747a238ea8b539ad0d45c1`) and
`TestPinnedNativeTranscript` compares the fixture's binary path against its Go literal; holding the
ruling's plain reading reddens that test (observed: `entry 16 observe response: invalid OmO RPC
record`, from a foreground run of the package suite on those bytes). Four OmO files therefore keep
the drive root and replace only the account-home run — `omo/5.1.69/windows.json`,
`omo/5.1.69/adapter-windows.json`, `omo/5.1.69/README.md` and `omo/transcript_test.go`;
`omo/native_test.go` carried the same shape until the round-1 review replaced its Windows pin with
a home-relative expression (§8). Every other file replaces the whole prefix.

## 3. Digest census — every digest that binds a changed file

Each changed file's pre-change sha256 (full and 8/12/16/24/32-character prefixes), its pre-change
Git blob id (full and 8/12/16/24-character prefixes) and — where its bytes at the PR base differ
from its §5 pre revision — its PR-base blob id (same five probes) were matched case-insensitively
across every tracked file: 324 probe terms at the PR base
`613999840a4e4bc1d1169a37d3775b1814671015` and at the head
`a5d622434959fc110d50c6f8d35e614ad36d3a0d`; the fix-round tree (§6 `<scratch>`) re-run adds the
round-1 test edit's superseded `a5d62243` sha256 and blob id of `omo/native_test.go` (§8), 335
terms:

```sh
git grep -I -i -n -F -e "<probe>" <revision>    # one probe per row
```

Probe lines at matching files: 4 lines / 3 files at the PR base; 32 lines / 4 files at the head;
33 lines / 4 files at the fix-round tree. No `SHA256SUMS` names a changed file; no loader, test,
CI assertion or acceptance script cites one. The probes that hit, and their disposition:

| probe | cited at | disposition |
| --- | --- | --- |
| every pre-change sha256 §5 lists — all 28 rows, the two correction-file rows (`2.4.1/README.md`, `rf-acc-opencode-reeval-2026-11-25.md`) among them | this record §5 (the mandated old→new table, W4) | exempt: the table is the mandated self-citation of these digests; rewriting it would erase the old→new mapping |
| pre-change sha256 of `omo/native_test.go` and `omo/transcript_test.go` | `docs/evidence/goal-0009-source-identity-2026-11-16.json:336` and `:343` (sha256 `8bd6d70ef71b9e73682b8d39d5f26d792e49459a218aa6f6917bb3c7d360ad8c`) | exempt: a historical source-identity snapshot of an operator-supplied past revision; re-deriving it would falsify that snapshot |
| pre-change sha256 of the control-plane runbook | `docs/evidence/rf-daemon-state-dir-default-2026-11-25.md:26` (sha256 `fcf236e4444241380f29532ccd8e2cb1569c430a9b2180ab3d8f255bc26fe197`) | exempt: an earlier goal's delivery-state table; it records that goal's bytes, not the current tree |
| the PR-base Git blob id `ac3f5740…` of `2.3.0/README.md` | `docs/plans/2026-11-23-rf-w5-10-harness-capture-fixtures-plan.md:237` (a mutation-census row marked "= base") and this record §3 (its own disposition row) | exempt: a historical base-revision blob pointer inside frozen plan bytes this fix does not edit, plus the census's self-citation of the same value; the §5 row for that file uses the fix pass's pre revision, a different blob |
| the pre-change blob ids of the changed files | — | probe 0: no tracked file cites one, and §5 lists sha256 values only |
| the two `witness_sha256` collector identities | `2.3.0/README.md` and the receipts | exempt: the collecting collector script's identity, not receipt bytes |

Everything else probed 0; the two correction files' pre-change sha256 values hit only this record's
§5 rows, which the first row above covers.

## 4. The substitution is the only change

The proof tool (§Appendix, sha256
`68caef0c710503aa830022775beca9352b1b10574a20c83585bee00197add756`) reads each changed file's
pre-change bytes from Git, recovers the three values at run time from the pinned base bytes — the
hostname from the recorded `/api/info` field, the relay host from the recorded `base_url`, the
profile name from the recorded WSL command's mount-root path, each by its recorded position, never
printed — applies the per-class mapping, and asserts the result against the delivered file:

```sh
python3 proof_note_final.py    # the appendix listing, pinned by the sha256 above
```

| proof class | files | assertion |
| --- | --- | --- |
| exact | 20 | mapped pre-change bytes are byte-identical to the delivered file |
| exact, drive root kept (§2 amendment) | 4 | same, with the four-file rule of §2 |
| exact, drive root kept, plus the §8 test edit | 1 | `omo/native_test.go`: mapped pre-change bytes differ only in the declared pin block, replaced by the home-relative expression of §8 |
| one declared paragraph replace | 1 | `2.3.0/README.md`: the single replaced block's delivered side equals the declared redaction paragraph of §2, line for line |
| insert-only | 2 | the 2.4.1 README and the reeval record: pre-change bytes plus the §7 pointer insertions only |

Parsed JSON is equal under the same mapping for all nine JSON files among them. The whole-tree
residual census the tool closes with: hostname 0, relay host 0, profile-name path 0, bare profile
mention 1 (§7).

## 5. Digests, per changed file

`pre` is the pinned dispatch base for the three receipts and the pre-fix revision for every other
file; `delivered` is the final byte identity (never a commit). Path-class occurrences are the file's
matches of the §4 class probe; hostname and relay counts are §1's.

| file | change | path-class occurrences | pre sha256 | delivered sha256 |
| --- | --- | --- | --- | --- |
| `daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json` | exact | 3 | `9bf30bb4d51a24dc5bd911efaa1755219a584afa65db6155601424158cba0c1f` | `0700794b6890450fa5ebea2e215dfff68ad490869ba3e828972ce5c3eaddd590` |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json` | exact | 34 | `4fc793d4c63dbee4c1f3dae011bd7aa806bedc30e74fe012dbf98f4356222d46` | `2c570e5a59884811e62d27580c95fe1bc0d36f87c6942a00f5531a6c6fca58e1` |
| `daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json` | exact | 70 | `ed6707f11defd7c60483f152d1002be47fd2bc5d75fd08388e10c0372889275a` | `ebf42f815e25a625c65b5f31a5705d3f90de4c9aaa025ae9a9715073973235c0` |
| `daemon/internal/harness/testdata/opencode/2.3.0/provider-discovery.json` | exact | 2 | `21cf0287542cb94f62a214d357c38bce1b547f009019ede171dec20392db9f20` | `2235978402951f31daaaf5d6e25921bbc60205420285229e0e21b6545dcecdbf` |
| `daemon/internal/harness/testdata/opencode/2.3.0/probe-windows.json` | exact | 3 | `219f1066bf61107d5769d56ad59d4fdf44f8c24f6609027f9b197b0517dd5a6b` | `77b9493de132a63b9df6b34fea463fc3ec203555ff152a66a59fb35e66e79e2a` |
| `daemon/internal/harness/testdata/opencode/2.3.0/README.md` | one-replace-block | 1 | `4f11a79585d3f3fa963b51619f94947ae3ed58fd3d635ce5de13ab5022ee830d` | `3ad9b668e21aa2fbede70e66eb398636e4172e435cc078739f59f07dc293a2ef` |
| `daemon/internal/harness/testdata/omo/5.1.69/linux.json` | exact | 0 | `4ea2054d0980e7e13b6d0ff1ca1d215892657cd313f24ceb1345d407b67d2910` | `478351e165bbd70dd746487701b5610a52f1314404b2615b73150ed3a2d3fc71` |
| `daemon/internal/harness/testdata/omo/5.1.69/windows.json` | exact, root kept | 13 | `b482b14cea9f3663657cae2b9a55d4b393483590f338b8b45aaeac362347ece8` | `fd7f64d96fc2096d18f341dc925674bf0188c4ee737959831a11856571f3a00e` |
| `daemon/internal/harness/testdata/omo/5.1.69/adapter-windows.json` | exact, root kept | 20 | `e2eb11777677a3235593f9310bbdfc66d69153f7964e7fab7aeaab02be880b5c` | `f25257e96537a006a7094288922b2d71b176796f83aa7c23d7c6a7a7cb5a47de` |
| `daemon/internal/harness/testdata/omo/5.1.69/README.md` | exact, root kept | 1 | `bd8b562b8ca11ede84eedb6a59b15d55ad173d6d780f0d20fb74a8ef115e632e` | `f133e212e6d53efa5ed469dc8d52acf97114966775b6606e7a5ecaaf7c388152` |
| `daemon/internal/harness/omo/native_test.go` | exact, root kept, plus the §8 test edit | 1 | `dfd20ac60d251f308d6273d4411f89426eec50a4d066496392b7b407f4eadb5f` | `7d1d5716a79510b636256c78703de1e18ec1cb4b4dc251cd8b878f0adc97aa8e` |
| `daemon/internal/harness/omo/transcript_test.go` | exact, root kept | 1 | `8c68cb2faaceba7bdaad769e36d77b8c7e40f6e44850f51a3ef2cc46a95056e5` | `cca00162827d8d1c79724715198a49e06427bd43dece2b1affc820a9c7530fc0` |
| `daemon/internal/harness/testdata/claude/2.1.323/native-repository-turn-windows-2026-11-13.md` | exact | 3 | `001e81ce5080f4e35dfc58144f2c9a2855b5bddc174e013dda2a04d6612ffb2c` | `cb4ed187355c39e68bcb7ea17e1684f99cb01532dca2c019bb12e93cc9f4f892` |
| `daemon/internal/harness/testdata/codex/0.168.0/native-windows-2026-11-13.md` | exact | 2 | `debf1f8ce9b19c2f88a23cc3bbe060b90ee446d23d47cea08f44b8a799f92d34` | `574d5b74f1f92ab9c285df702e1939207ed85280aaca753f9bc01a88726174c9` |
| `daemon/internal/harness/testdata/omp/18.20.0/fixture-manifest.json` | exact | 4 | `4e6e380e856c0b55a09cca33f55993bcf31246d687c2fa20524185170ac085bd` | `0c57397f014136fcddb176c682265cbf79f658adc01a7c0711788973febb8900` |
| `daemon/internal/harness/testdata/opencode/1.18.30/native-synthetic-tool-current-run-2026-09-13.md` | exact | 1 | `31307deadfa1f7d931b7e13b5b6a274405a318e227f42900992839cb6b1eccfa` | `0bd5633007c26a2d5038421f55bbfa88ca9211d07dbf0b6029cd2f279bab317a` |
| `daemon/internal/harness/testdata/pi/1.5.2/native-promotion-windows-2026-11-14.md` | exact | 2 | `a74fd0a249f61260c08a01eee1ebeeded423585d8d74443a67720b642ca9bca4` | `0cdd7308372dab589baccfd0c474d39b401ae420cfb3a2dc237b45a562d080f5` |
| `docs/goals/archive/0006g-opencode-support-decision.md` | exact | 1 | `1d2fcda7f41db22be1eb00312ed7d1d09e4d97aa826f8ca5053e06bc2302e0c2` | `63e7209cd4f85bd229e7e5b22c9955a80070b213b4ea95664f83ac3633bab4b9` |
| `docs/goals/archive/0006j-omp-native-capability.md` | exact | 1 | `938f7188579f3df14b4ff399310f76d69e7888f25e713ae724a47bf9408e2b32` | `762fee977e68fa13e919127eb97953c091eeb66c5c7c3c774f0dee22d5c9699d` |
| `docs/goals/archive/0006k-omo-native-capability.md` | exact | 1 | `5dcb7e33f0ea9a99e492d7a4a7a75942ad3fa2c5e3f4cf8530bc4d4599ee4d5b` | `03c0888fa244abb2ec72d9443d1fe9665a668513caa49821b374c8792869ec05` |
| `docs/plans/2026-11-14-0006f-pi-native-capability-plan.md` | exact | 1 | `08605724fa581def26087780a93b9a19fe4ed8e157913a709a66f664dbdd89c8` | `9870be71aea0335931d5372bb82b93be72f73042f22a83bc3c99434a925a9a4d` |
| `docs/plans/2026-11-21-goal-command-stale-race-plan.md` | exact | 1 | `8f9a3058896489b9612768253f8b4690f3d6fe28f7de8d70164eaff0786097ca` | `e884da41649491968be6bbf82314de9b6d0b8542b01f9a99de327bb9c9606ee7` |
| `docs/runbooks/control-plane-and-daemon.md` | exact | 1 | `efd66e1f98e29970615673a17415d7ef69ef8976faa0babf4dbd060dbcfcd042` | `fc9226eb42473216956790c06c4ad5982e26ff5f426147b8ccac96ce62baebac` |
| `docs/evidence/rf-acc-pi-durability-matrices-2026-11-24.md` | exact | 0 | `0e6375100da6436f33253bef219225909559a9ee9bff7e709497a429b7db7ac8` | `230d80c08d01014d70d42fe7330875524e2dd6beab686c2a6c3ec04372cd6c85` |
| `docs/evidence/rf-acc-witness-unresolved-exit-2026-11-25.md` | exact | 0 | `d34cd15bed8f3de2e19e51cc6cfbbe268822e49ae55340546edef6061bbe3d34` | `64cb939d92453bd4b10800e6079cb4ec936fe539f5794e979529068e1cf19f43` |
| `docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md` | exact | 0 | `c8b5c03f27ccc45373139634463d8a4e844831b3be593990a37d203da601fff2` | `c26f3748c7b502be1394af5056d01090215a1c36d64d34a7541a0b7cb32487f2` |
| `daemon/internal/harness/testdata/opencode/2.4.1/README.md` | insert-only | 0 | `2aae7455d27ca1959a655b410cf01b3ffe2010c912c0487759054608e50a05e3` | `5581c088801f2f8e1051a480f9d9c3678bfad592ab5f2ba6c2ba7bbb144a06c9` |
| `docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` | insert-only | 0 | `48649955775771b82bebfd1eaa4de36368bece499bc51b82b39bad7143f443f0` | `36e735b81938b12b0d751dd44230f443d591b82c890d765e0e08e362eede7847` |

## 6. Gates and censuses

Local, foreground, against a scratch-materialized head commit of the delivered tree
(`git commit-tree` over a temporary index; the branch and its index are untouched). `exit` is the
command's own exit status.

| gate | command (working directory) | exit | result |
| --- | --- | --- | --- |
| Go harness suite | `go test -count=1 -timeout 900s ./internal/harness/...` (`daemon/`) | 0 | 9 packages `ok` |
| CI self-test suites (structure extraction, record cleanliness, citation existence, record sanitizer) | `python3 scripts/ci/test-ci-structure-extraction.py`, `test-check-record-cleanliness.py`, `test-check-citation-existence.py`, `test-model-routing-record-sanitize.py` | `OK` each | 40 / 19 / 49 / 57 tests |
| record cleanliness over the change | `python3 scripts/ci/check-record-cleanliness.py --base 613999840a4e… --head <scratch>` | 0 | `5 changed record(s) pass the root-only check` |
| record sanitizer, root-only, per changed doc | `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only < <doc>` | 0 for 9, 1 for 3 | the three residuals are §7 rows |
| citation existence over the change | `python3 scripts/ci/check-citation-existence.py --base 613999840a4e… --head <scratch>` | 0 | `1 changed document(s) pass the path and symbol check` |
| clone gate over the change | `python3 scripts/ci/jscpd-changed.py --base 613999840a4e… --head <scratch>` | 0 | none new; the tolerated pairs are the receipts' pre-existing internal duplication, each row reported as holding no added content |
| clone gate self-test | `python3 scripts/ci/test-jscpd-changed.py` | `OK` | see the stage report |
| equality and whole-tree residual proof | `python3 proof_note_final.py` (§Appendix) | 0 | 28 proved rows (25 mapped == delivered, 1 declared paragraph replace, 2 insert-only); residual 0 / 0 / 0 |
| post-change censuses, whole tree | hostname: `git grep -I -i -c -F -e "<hostname value>" <scratch>` (1 = no match); relay: same with `<relay host value>`; path class: the §4 probe | 1 = no match each | hostname 0, relay 0, profile-name path 0; one bare profile mention (§7) |

Key-shape census over the delivered tree, counts only (`<revision>` is `<scratch>`; `-P` is PCRE2):

```sh
git grep -I -o -P -e 'sk-[A-Za-z0-9_-]{8,}' <revision> | wc -l
git grep -I -l -P -e 'sk-[A-Za-z0-9_-]{8,}' <revision> | wc -l
git grep -I -o -P -e '(?<![A-Za-z0-9_-])sk-[A-Za-z0-9]{16,}(?![A-Za-z0-9])' <revision> | wc -l
```

The loose token shape (`sk-` plus eight or more token characters) is 51 occurrences in 33 files,
all inside `task-`/`disk-`/`umask-` words (43) or the deliberate scratch dummy tokens of the
redaction tests (8); the anchored key shape (`sk-` plus sixteen or more alphanumerics,
boundary-delimited) is 0; the configured relay credential probes 0. None is a credential.

## 7. Residuals, exemptions and superseded statements

- Git history keeps every pre-change byte; history is not rewritten. This record is the present-tree
  census.
- The two statements this change falsified are corrected in place with one-sentence pointers:
  `daemon/internal/harness/testdata/opencode/2.4.1/README.md` (`:80-81` and `:222-223`, pointer at `:82-83` and `:226-227`, sha256
  `5581c088801f2f8e1051a480f9d9c3678bfad592ab5f2ba6c2ba7bbb144a06c9`) and
  `docs/evidence/rf-acc-opencode-reeval-2026-11-25.md` (`:468`, pointer at `:472-473`, sha256
  `36e735b81938b12b0d751dd44230f443d591b82c890d765e0e08e362eede7847`).
- Frozen records — the three archived goals and the three earlier evidence records — are edited by
  the substitution only; their bytes change because the ruled classes include them, and nothing
  else in them moves.
- Three changed documents still carry pre-existing absolute paths that belong to no ruled class;
  the sites are content-located by line and their files by sha256.
  W1/W5 forbid further bytes there, and this change's substituted lines introduce none:
  `docs/goals/archive/0006j-omp-native-capability.md:35` and
  `docs/goals/archive/0006k-omo-native-capability.md:38` carry the Linux surface's root-user home
  executable path (sha256 `762fee977e68fa13e919127eb97953c091eeb66c5c7c3c774f0dee22d5c9699d` and
  `03c0888fa244abb2ec72d9443d1fe9665a668513caa49821b374c8792869ec05`);
  `docs/runbooks/control-plane-and-daemon.md:216,:351,:354` carry the state directory under the
  var root and, twice, the scratch build-output path under the tmp root (sha256
  `fc9226eb42473216956790c06c4ad5982e26ff5f426147b8ccac96ce62baebac`).
- One bare account-name mention remains, not a path and outside the three ruled classes:
  `docs/evidence/rf-w3-08-unexport-state-surface-2026-11-21.md:287` (sha256
  `77e493bd8e0b54d7cc114d0ebf02efd386e03cee0f1a77bc769be7cecefe38eb`).

## 8. PR #291 round-1 review corrections (2026-11-26)

One non-substitution edit beyond the record text: `daemon/internal/harness/omo/native_test.go`'s
Windows pin, a redacted token path after the widening, is re-expressed home-relative — the process
user's home directory (required non-nil) joined with `.local/bin/omo.exe` — so the opt-in
`TestNativeAcceptance` pin resolves again; the size and digest pins that carry the identity proof
are byte-unchanged and the fixture-replay pin is untouched. The `docs/TODO.md` entry and the §7
bullet that tracked this gap are closed, and the §5 row carries the new delivered sha256. The
other round-1 corrections are in place: §3's exemption set covers every self-hit and names the PR
base, the head and the fix-round tree; §6's key-shape counts carry their exact commands; the 2.3.0
README proof is content-aware; and the appendix listing's sha256 was re-derived, updating both
citations (the §4 line and the appendix header). No redacted artifact changed; the §5 digest chain
stays as delivered.

## Appendix — proof tooling (verbatim; sha256 68caef0c710503aa830022775beca9352b1b10574a20c83585bee00197add756)

```python
#!/usr/bin/env python3
"""§4 proof for the rf-opencode-230-hostname-redact fix pass: mapped==delivered per
file plus the whole-tree residual census; the recovered values are never printed."""
import hashlib
import json
import re
import subprocess
import difflib

BASE = "613999840a4e4bc1d1169a37d3775b1814671015"
PRE = "c9606af4c3db5bab5de3db7bac22b75b4d038fe0"

FROM_BASE = {
    "daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json",
}
SUBSTITUTED = [
    "daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14-r1.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/provider-discovery.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/probe-windows.json",
    "daemon/internal/harness/testdata/opencode/2.3.0/README.md",
    "daemon/internal/harness/testdata/omo/5.1.69/linux.json",
    "daemon/internal/harness/testdata/omo/5.1.69/windows.json",
    "daemon/internal/harness/testdata/omo/5.1.69/adapter-windows.json",
    "daemon/internal/harness/testdata/omo/5.1.69/README.md",
    "daemon/internal/harness/omo/native_test.go",
    "daemon/internal/harness/omo/transcript_test.go",
    "daemon/internal/harness/testdata/claude/2.1.323/native-repository-turn-windows-2026-11-13.md",
    "daemon/internal/harness/testdata/codex/0.168.0/native-windows-2026-11-13.md",
    "daemon/internal/harness/testdata/omp/18.20.0/fixture-manifest.json",
    "daemon/internal/harness/testdata/opencode/1.18.30/native-synthetic-tool-current-run-2026-09-13.md",
    "daemon/internal/harness/testdata/pi/1.5.2/native-promotion-windows-2026-11-14.md",
    "docs/goals/archive/0006g-opencode-support-decision.md",
    "docs/goals/archive/0006j-omp-native-capability.md",
    "docs/goals/archive/0006k-omo-native-capability.md",
    "docs/plans/2026-11-14-0006f-pi-native-capability-plan.md",
    "docs/plans/2026-11-21-goal-command-stale-race-plan.md",
    "docs/runbooks/control-plane-and-daemon.md",
    "docs/evidence/rf-acc-pi-durability-matrices-2026-11-24.md",
    "docs/evidence/rf-acc-witness-unresolved-exit-2026-11-25.md",
    "docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md",
]
# the five files whose values must stay absolute for the product parser
ROOT_KEEP = {
    "daemon/internal/harness/testdata/omo/5.1.69/windows.json",
    "daemon/internal/harness/testdata/omo/5.1.69/adapter-windows.json",
    "daemon/internal/harness/testdata/omo/5.1.69/README.md",
    "daemon/internal/harness/omo/native_test.go",
    "daemon/internal/harness/omo/transcript_test.go",
}


def git(*args):
    return subprocess.run(["git", *args], capture_output=True, check=True).stdout


def sha(data):
    return hashlib.sha256(data).hexdigest()


receipt = "daemon/internal/harness/testdata/opencode/2.3.0/linux-2026-11-14.json"
windows = "daemon/internal/harness/testdata/opencode/2.3.0/windows-2026-11-14.json"
base_windows = git("show", f"{BASE}:{windows}")
base_receipt = git("show", f"{BASE}:{receipt}")
HOSTNAME = re.search(rb'\\"hostname\\":\\"([^\\"]+)\\"', base_windows).group(1)
RELAY = re.search(rb'"base_url"\s*:\s*"https://([^/"]+)/v1"', base_windows).group(1)
RELAY_LABEL = RELAY.split(b".")[0]
recorded = re.search(rb"pathlib\.Path\('(/[^']+)'\)", base_receipt).group(1)  # the recorded WSL blob path
segments = recorded.split(b"/")            # b"", mount, letter, account-home, profile, ...
PROFILE = segments[4]
MOUNT_DIR = b"/" + segments[1] + b"/"      # the recorded mount directory, built at run time
CLASS = re.compile(rb"(?i)(?:" + MOUNT_DIR + rb"[a-z]|[a-z]:)?[\\/]{1,4}[Uu]sers[\\/]{1,4}"
                   + re.escape(PROFILE))
ROOT_KEEP_CLASS = re.compile(rb"(?i)((?:" + MOUNT_DIR + rb"[a-z]|[a-z]:)[\\/]{1,4})[Uu]sers[\\/]{1,4}"
                             + re.escape(PROFILE))


def map_bytes(old, root_keep=False):
    new = old.replace(HOSTNAME, b"<HOSTNAME>")
    new = new.replace(RELAY, b"<RELAY_HOST>")
    new = new.replace(RELAY_LABEL, b"<RELAY_HOST>")
    if root_keep:
        return ROOT_KEEP_CLASS.sub(rb"\1<HOST-PATH>", new)
    return CLASS.sub(b"<HOST-PATH>", new)


def walk(node, fn):
    if isinstance(node, dict):
        return {k: walk(v, fn) for k, v in node.items()}
    if isinstance(node, list):
        return [walk(v, fn) for v in node]
    if isinstance(node, str):
        return fn(node.encode()).decode()
    return node


# declared non-substitution code/record edits, each verified as a content-aware delta
PROSE = {
    "daemon/internal/harness/testdata/opencode/2.3.0/README.md": "readme-paragraph",
    "daemon/internal/harness/testdata/opencode/2.4.1/README.md": "insert-only",
    "docs/evidence/rf-acc-opencode-reeval-2026-11-25.md": "insert-only",
}
NATIVE_TEST = "daemon/internal/harness/omo/native_test.go"
# §8: the declared pin edit; the old block is matched exactly, its drive root never spelled out
PIN_OLD = re.compile(rb"\t\texpectedPath, expectedSize = `[A-Za-z]:/<HOST-PATH>/\.local/bin/omo\.exe`, 202352128\n")
TEST_EDIT_NEW = (b"\t\t// Home-relative: the pin must not carry the profile name (redaction ruling P1).\n"
                 b"\t\thome, err := os.UserHomeDir()\n"
                 b'\t\trequire(err == nil, "home_directory_lookup_failed")\n'
                 b'\t\texpectedPath, expectedSize = filepath.Join(home, ".local", "bin", "omo.exe"), 202352128\n')
# §4: the delivered side of the 2.3.0 README's one replaced block, verbatim
README_PARAGRAPH = [line.encode() for line in [
    "All three receipts are byte-redacted since 2026-11-25, widened 2026-11-26: the collecting machine's",
    "hostname value became `<HOSTNAME>`, the relay host became `<RELAY_HOST>` and the user-profile",
    "absolute paths became `<HOST-PATH>` — constant-token substitutions, nothing else changed. This",
    "supersedes the residual those receipts carried in `../2.4.1/README.md`; the census, the",
    "byte-equality proof and the old→new digests are in",
]]


def delta_kind(before, after):
    opcodes = difflib.SequenceMatcher(None, before.splitlines(), after.splitlines()).get_opcodes()
    kinds = sorted({op for op, *_ in opcodes if op != "equal"})
    return kinds


def one_replace_paragraph(before, after, expected):
    """The only delta is one replace block whose delivered side equals the declared paragraph."""
    ops = [op for op in difflib.SequenceMatcher(None, before.splitlines(),
                                                 after.splitlines()).get_opcodes() if op[0] != "equal"]
    assert len(ops) == 1 and ops[0][0] == "replace", f"{ops}"
    *_, j1, j2 = ops[0]
    assert after.splitlines()[j1:j2] == expected, f"paragraph mismatch: {j1}:{j2}"


rows = []
for rel in SUBSTITUTED:
    pre = git("show", f"{BASE if rel in FROM_BASE else PRE}:{rel}")
    disk = open(rel, "rb").read()
    root_keep = rel in ROOT_KEEP
    mapped = map_bytes(pre, root_keep)
    kind = PROSE.get(rel, "exact")
    if rel == NATIVE_TEST:
        kind = "root-kept + §8 test edit"
        old = PIN_OLD.findall(mapped)
        assert len(old) == 1, f"{rel}: pin block not found"
        mapped = mapped.replace(old[0], TEST_EDIT_NEW)
    if kind in ("exact", "root-kept + §8 test edit"):
        assert mapped == disk, f"mapping mismatch: {rel}"
    elif kind == "readme-paragraph":
        one_replace_paragraph(mapped, disk, README_PARAGRAPH)
    else:
        kinds = delta_kind(mapped, disk)
        assert kinds == ["insert"], f"{rel}: {kinds}"
    if rel.endswith(".json"):
        assert walk(json.loads(pre), lambda b: map_bytes(b, root_keep)) == json.loads(disk), rel
    count = len((ROOT_KEEP_CLASS if root_keep else CLASS).findall(pre))
    rows.append((rel, kind, count, sha(pre), sha(disk)))
for rel, kind, count, old, new in rows:
    print(f"{rel} | {kind} | {old} | {new} | class occurrences={count}")
print(f"proved changed files: {len(rows)}")

for rel in ["daemon/internal/harness/testdata/opencode/2.4.1/README.md",
            "docs/evidence/rf-acc-opencode-reeval-2026-11-25.md"]:
    pre = git("show", f"{PRE}:{rel}")
    disk = open(rel, "rb").read()
    kinds = delta_kind(pre, disk)
    assert kinds == ["insert"], f"{rel}: {kinds}"
    rows.append((rel, "insert-only", 0, sha(pre), sha(disk)))
    print(f"{rel} | insert-only | {sha(pre)} | {sha(disk)} | class occurrences=0")

totals = {"hostname": 0, "relay": 0, "profile path": 0, "bare profile mention": 0}
for rel in git("ls-files").decode().splitlines():
    data = open(rel, "rb").read()
    totals["hostname"] += len(re.compile(re.escape(HOSTNAME), re.I).findall(data))
    totals["relay"] += data.count(RELAY) + data.count(RELAY_LABEL)
    totals["profile path"] += len(CLASS.findall(data))
    totals["bare profile mention"] += len(
        re.compile(rb"(?i)(?<![A-Za-z0-9])" + re.escape(PROFILE) + rb"(?![A-Za-z0-9])").findall(data))
print("whole-tree residual:", totals)
```
