import importlib.util
import json
import math
import os
import subprocess
import sys

# Inherited values would redirect `-C <repository>` to another repository or config scope;
# GIT_CONFIG pins stay because callers pin configuration through them.
GIT_REPOSITORY_VARIABLES = (
    "GIT_DIR",
    "GIT_WORK_TREE",
    "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY",
    "GIT_COMMON_DIR",
    "GIT_ALTERNATE_OBJECT_DIRECTORIES",
)
GIT_CONFIG_REDIRECT_VARIABLE = "GIT_CONFIG"
GIT_CONFIG_PINNED_VARIABLES = ("GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM")


class GateImportRefused(Exception):
    pass


class StrictJsonRefused(Exception):
    pass


def load_gate(path, name):
    specification = importlib.util.spec_from_file_location(name, path)
    if specification is None or specification.loader is None:
        raise GateImportRefused(f"{path} is not an importable module")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def run_gate(script, *arguments, cwd=None, env=None, timeout=None, encoding=None):
    return subprocess.run(
        [sys.executable, str(script), *arguments],
        capture_output=True,
        text=encoding is None,
        encoding=encoding,
        check=False,
        cwd=cwd,
        env=env,
        timeout=timeout,
    )


def git_environment():
    environment = dict(os.environ)
    for name in list(environment):
        if name.startswith(GIT_CONFIG_REDIRECT_VARIABLE) and name not in GIT_CONFIG_PINNED_VARIABLES:
            environment.pop(name)
    for name in GIT_REPOSITORY_VARIABLES:
        environment.pop(name, None)
    return environment


def git(repository, *arguments, input=None):
    try:
        return subprocess.run(
            ["git", "-C", str(repository), *arguments],
            input=b"" if input is None else input,
            capture_output=True,
            check=True,
            env=git_environment(),
        )
    except subprocess.CalledProcessError as refusal:
        if refusal.stderr:
            sys.stderr.write(refusal.stderr.decode("utf-8", "replace"))
        raise


def git_text(repository, *arguments):
    return git(repository, *arguments).stdout.decode("ascii").strip()


def write_bytes(path, data, *, mode=None):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    if mode is not None:
        os.chmod(path, mode)
    return data


def write_json(path, value, *, sort_keys=False, mode=None):
    document = json.dumps(value, indent=2, sort_keys=sort_keys) + "\n"
    return write_bytes(path, document.encode("utf-8"), mode=mode)


def strict_json(text, source):
    def object_pairs(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise StrictJsonRefused(f"{source} repeats the JSON key {key!r}")
            value[key] = item
        return value

    def parse_float(number):
        value = float(number)
        if not math.isfinite(value):
            raise StrictJsonRefused(f"{source} contains a nonfinite number")
        return value

    def parse_constant(name):
        raise StrictJsonRefused(f"{source} contains the non-JSON constant {name}")

    try:
        return json.loads(
            text,
            object_pairs_hook=object_pairs,
            parse_float=parse_float,
            parse_constant=parse_constant,
        )
    except json.JSONDecodeError as error:
        raise StrictJsonRefused(f"{source} is not valid JSON: {error}") from error
