#!/usr/bin/env python3

import os
import subprocess

from audited_paths import AUDITED_PATHS

# --no-renames keeps renames and deletions deterministic: a path moved
# across the documentation boundary still reports both sides.
paths = subprocess.check_output(["git", "diff", "--name-only", "--no-renames", "-z", "HEAD^1", "HEAD"]).split(b"\0")
run_heavy = any(path and not (path.startswith(b"docs/") or path.endswith(b".md")) for path in paths)
run_records = any(
    path.startswith(b"docs/evaluations/")
    or path.startswith(b"docs/evidence/")
    or path == b"scripts/ci/model-routing-record-sanitize.py"
    or path == b"scripts/ci/check-record-cleanliness.py"
    or path == b"scripts/ci/revision_scope.py"
    for path in paths
)
run_citations = any(
    any(path == entry.encode() or path.startswith(entry.encode() + b"/") for entry in AUDITED_PATHS)
    or path == b"scripts/ci/check-citation-existence.py"
    or path == b"scripts/ci/test-check-citation-existence.py"
    or path == b"scripts/ci/revision_scope.py"
    or path == b"scripts/ci/audited_paths.py"
    for path in paths
)
with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
    output.write(f"run-heavy={'true' if run_heavy else 'false'}\n")
    output.write(f"run-records={'true' if run_records else 'false'}\n")
    output.write(f"run-citations={'true' if run_citations else 'false'}\n")
