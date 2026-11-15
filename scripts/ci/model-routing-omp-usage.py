#!/usr/bin/env python3
"""Extract allowlisted routing and usage metadata from one OMP session JSONL file.

This is the usage-extraction step of the repository's auditable acceptance
workflow. The emitted row is one phase-ledger row: stage, opaque session
reference, actually served model identities, prompt digests, token totals,
cost estimates with provenance, wall time and limitations.

Only allowlisted scalar metadata, counts and digests leave this tool. Message
text, prompts, tool payloads, configuration and credential values are never
exported, and the raw session file stays outside Git. Harness cost numbers are
the harness's own estimate, never provider billing: reported_cost is always
null and estimated_cost is only the sum of observed harness cost totals, or
null when any assistant record lacks one. Missing evidence is never zero.

Every instruction-bearing input is bound by a SHA-256 digest without exporting
its text: the session_init system prompt and task, user messages and
custom_message instructions. metadata.source_sha256 additionally binds the
whole private session file, so rows stay tied to their source even when an
instruction event type is not recognized for hashing.
"""

import argparse
import datetime
import hashlib
import json
import math
import pathlib
import re
import sys


# The six accountable phases: exploration, implementation, verification,
# review, repair and failed attempts (the CLI value is failed_attempt).
STAGES = ("exploration", "implementation", "verification", "review", "repair", "failed_attempt")

# Shape restriction, not secret detection: these patterns only keep emitted
# metadata and identifiers short, ASCII and path-free. A value that fits them
# can still be a secret, so callers must never pass credential, prompt or
# transcript values through these arguments.
SAFE_LABEL_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
SAFE_TOKEN_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:+/@~-]{0,127}$")

# Thinking-level suffixes a configured resolved model may carry as
# `provider/model:effort`. Only these known suffixes are stripped when the
# actually served model is compared with the configured identity.
THINKING_SUFFIXES = ("none", "off", "minimal", "low", "medium", "high", "xhigh", "max")

# Bounds keep a pathological session file from exhausting memory.
MAX_LINE_BYTES = 32 * 1024 * 1024
MAX_RECORDS = 1_000_000

COST_PROVENANCE = "observed_omp_session_harness_cost_total_sum_estimate_no_provider_billing_proof"

# Token fields summed from usage records; cached is derived from the two
# cache counters. Only the first (input, output, cached) decide the top-level
# token totals.
TOKEN_FIELDS = ("input", "output", "cacheRead", "cacheWrite", "cached",
                "totalTokens", "reasoningTokens")
SOURCE_TOKEN_FIELDS = ("input", "output", "cacheRead", "cacheWrite",
                       "totalTokens", "reasoningTokens")
COST_FIELDS = ("input", "output", "cacheRead", "cacheWrite", "total")
COMPLETE_TOKEN_FIELDS = ("input", "output", "cached")


class UsageRefusal(Exception):
    """A constant, nonsecret refusal message; session values are never included."""


def refuse(condition, message):
    if not condition:
        raise UsageRefusal(message)


def strict_json(text):
    """Parse one record, refusing duplicate keys and non-finite constants."""

    def object_pairs(pairs):
        value = {}
        for key, item in pairs:
            refuse(key not in value, "session record repeats a JSON key")
            value[key] = item
        return value

    def invalid_constant(_):
        raise UsageRefusal("session record contains a non-finite JSON number")

    return json.loads(text, object_pairs_hook=object_pairs, parse_constant=invalid_constant)


def safe_token(value):
    return isinstance(value, str) and SAFE_TOKEN_RE.match(value) is not None


def safe_label(value):
    return isinstance(value, str) and SAFE_LABEL_RE.match(value) is not None


def observed_number(value):
    refuse(not isinstance(value, bool) and isinstance(value, (int, float)),
           "session record has a non-numeric usage value")
    if isinstance(value, float):
        refuse(math.isfinite(value), "session record has a non-finite usage value")
    refuse(value >= 0, "session record has a negative usage value")
    return value


def content_digest(value):
    """Digest one prompt value; the value itself never leaves this function."""
    if isinstance(value, str):
        payload = value.encode("utf-8")
    elif isinstance(value, (list, dict)):
        try:
            payload = json.dumps(value, sort_keys=True, separators=(",", ":"),
                                 ensure_ascii=False, allow_nan=False).encode("utf-8")
        except (TypeError, ValueError):
            raise UsageRefusal("session prompt content is not canonicalizable")
    else:
        return None
    return "sha256:" + hashlib.sha256(payload).hexdigest()


def parsed_timestamp(value):
    if not isinstance(value, str):
        return None
    text = value.strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        return datetime.datetime.fromisoformat(text)
    except ValueError:
        return None


def decoded_record(raw, first):
    """Decode one raw record line; a leading byte-order mark is tolerated once."""
    try:
        return raw.decode("utf-8-sig" if first else "utf-8")
    except UnicodeDecodeError:
        raise UsageRefusal("session record stream is not valid UTF-8")


def resolved_model_identity(resolved_model):
    """Split `provider/model[:thinking]` into its comparable identity parts."""
    provider = None
    model = resolved_model
    head, separator, tail = model.partition("/")
    if separator and head and tail:
        provider, model = head, tail
    base, separator, suffix = model.rpartition(":")
    if separator and base and suffix in THINKING_SUFFIXES:
        model = base
    return {"provider": provider, "model": model}


def session_init_metadata(record):
    values = {}
    for source, target in (("agent", "agent"), ("modelRole", "model_role"),
                           ("resolvedModel", "resolved_model")):
        value = record.get(source)
        if value is None:
            continue
        refuse(safe_token(value), "session_init record has an unsafe value")
        values[target] = value
    if "readOnly" in record:
        value = record["readOnly"]
        refuse(isinstance(value, bool), "session_init record has an unsafe readOnly value")
        values["read_only"] = value
    return values


def extract_usage(path, stage, credential_route, provenance, price_version):
    limitations = []

    def note(code):
        if code not in limitations:
            limitations.append(code)

    session_id = None
    session_ids = set()
    session_start = None
    session_records = 0
    session_inits = []
    prompt_hashes = []
    user_messages = 0
    custom_messages = 0
    model_changes = []
    thinking_level_changes = []
    fallback_records = []
    entry_types = {}
    entry_ids = set()
    record_times = []
    served = {}
    assistant_messages = 0
    assistant_with_usage = 0
    assistant_with_cost_total = 0
    assistant_without_model = 0
    sums = {}
    counts = {}
    complete = {field: True for field in COMPLETE_TOKEN_FIELDS}
    cost_complete = True
    records = 0
    source_hash = hashlib.sha256()

    first_chunk = True
    with open(path, "rb") as stream:
        while True:
            raw = stream.readline(MAX_LINE_BYTES + 1)
            if not raw:
                break
            source_hash.update(raw)
            refuse(len(raw) <= MAX_LINE_BYTES, "session record exceeds the bounded line size")
            line = decoded_record(raw, first_chunk)
            first_chunk = False
            if not line.strip():
                continue
            records += 1
            refuse(records <= MAX_RECORDS, "session file exceeds the bounded record count")
            try:
                record = strict_json(line)
            except json.JSONDecodeError:
                raise UsageRefusal("session record is not valid JSON")
            refuse(isinstance(record, dict), "session record is not a JSON object")
            record_type = record.get("type")
            refuse(safe_token(record_type), "session record has a missing or unsafe type")
            entry_types[record_type] = entry_types.get(record_type, 0) + 1
            entry_id = record.get("id")
            if isinstance(entry_id, str) and entry_id:
                # Digesting keeps the guard bounded for any id shape; the id itself is
                # never exported, so an unsafe-looking value stays tolerated but opaque.
                digest = hashlib.sha256(entry_id.encode("utf-8", "surrogatepass")).hexdigest()
                refuse(digest not in entry_ids,
                       "session record stream repeats an entry id; a duplicated or merged export "
                       "cannot be counted once")
                entry_ids.add(digest)

            timestamp = parsed_timestamp(record.get("timestamp"))
            if timestamp is None and record.get("timestamp") is not None:
                note("unrecognized_timestamp")
            elif timestamp is not None:
                record_times.append(timestamp)

            if record_type == "session":
                session_records += 1
                if isinstance(record.get("id"), str) and record["id"]:
                    session_ids.add(record["id"])
                    if session_id is None:
                        session_id = record["id"]
                if session_start is None and timestamp is not None:
                    session_start = timestamp
            elif record_type == "session_init":
                init_metadata = session_init_metadata(record)
                if not init_metadata:
                    note("session_init_fields_absent")
                if session_inits:
                    note("session_init_repeated")
                else:
                    prompts = (("systemPrompt", "system_prompt", "system_prompt_absent"),
                               ("task", "task", "task_prompt_absent"))
                    for key, kind, absent in prompts:
                        digest = content_digest(record.get(key))
                        if digest is None:
                            note(absent)
                        else:
                            prompt_hashes.append({"kind": kind, "sha256": digest})
                session_inits.append(init_metadata)
            elif record_type == "model_change":
                item = {"record": records - 1}
                for source, target in (("model", "model"), ("provider", "provider"),
                                       ("api", "api"), ("resolvedModel", "resolved_model")):
                    value = record.get(source)
                    if value is None:
                        continue
                    refuse(safe_token(value), "session model change has an unsafe value")
                    item[target] = value
                if len(item) > 1:
                    model_changes.append(item)
                else:
                    note("model_change_without_model")
            elif "thinking" in record_type and ("level" in record_type or "change" in record_type):
                level = None
                for key in ("level", "thinkingLevel", "thinking_level"):
                    value = record.get(key)
                    if value is None:
                        continue
                    refuse(
                        safe_token(value) or (isinstance(value, int)
                                              and not isinstance(value, bool) and value >= 0),
                        "session thinking change has an unsafe level",
                    )
                    level = value
                    break
                if level is None:
                    note("thinking_level_change_unrecognized")
                else:
                    thinking_level_changes.append({"record": records - 1, "level": level})
            elif "fallback" in record_type:
                item = {"record": records - 1}
                for source, target in (("model", "model"), ("provider", "provider"),
                                       ("api", "api"), ("resolvedModel", "resolved_model")):
                    value = record.get(source)
                    if value is None:
                        continue
                    refuse(safe_token(value), "session fallback record has an unsafe identity")
                    item[target] = value
                if len(item) > 1:
                    fallback_records.append(item)
                else:
                    note("fallback_record_unrecognized")
            elif record_type == "message":
                payload = record.get("message")
                refuse(isinstance(payload, dict), "session message record has no message object")
                role = payload.get("role")
                refuse(safe_token(role), "session message record has a missing or unsafe role")
                if role == "assistant":
                    assistant_messages += 1
                    model = payload.get("model")
                    provider = payload.get("provider")
                    api = payload.get("api")
                    for value in (model, provider, api):
                        if value is not None:
                            refuse(safe_token(value),
                                   "session assistant record has an unsafe model identity")
                    if model is None:
                        assistant_without_model += 1
                        note("assistant_model_identity_missing")
                    else:
                        identity = (model, provider, api)
                        served[identity] = served.get(identity, 0) + 1

                    usage = payload.get("usage")
                    refuse(usage is None or isinstance(usage, dict),
                           "session assistant usage is not a JSON object")
                    observed = {}
                    if isinstance(usage, dict):
                        assistant_with_usage += 1
                        for field in SOURCE_TOKEN_FIELDS:
                            if field in usage:
                                observed[field] = observed_number(usage[field])
                        if "cacheRead" in observed and "cacheWrite" in observed:
                            observed["cached"] = observed["cacheRead"] + observed["cacheWrite"]
                        cost = usage.get("cost")
                        refuse(cost is None or isinstance(cost, dict),
                               "session assistant cost is not a JSON object")
                        if isinstance(cost, dict):
                            for field in COST_FIELDS:
                                if field in cost:
                                    observed["cost." + field] = observed_number(cost[field])
                        if any(field not in observed for field in SOURCE_TOKEN_FIELDS):
                            note("assistant_usage_fields_missing")
                    else:
                        note("assistant_usage_missing")
                    if "cost.total" in observed:
                        assistant_with_cost_total += 1
                    else:
                        cost_complete = False
                        note("assistant_cost_missing")
                    for field in COMPLETE_TOKEN_FIELDS:
                        if field not in observed:
                            complete[field] = False
                    for field, value in observed.items():
                        sums[field] = sums.get(field, 0) + value
                        counts[field] = counts.get(field, 0) + 1
                elif role == "user":
                    user_messages += 1
                    content = payload.get("content")
                    if content is None:
                        content = payload.get("text")
                    digest = content_digest(content)
                    if digest is None:
                        note("user_message_content_absent")
                    else:
                        prompt_hashes.append({"kind": "user_message",
                                              "index": user_messages - 1, "sha256": digest})
            elif record_type == "custom_message":
                custom_messages += 1
                digest = content_digest(record.get("content"))
                if digest is None:
                    note("custom_message_content_absent")
                else:
                    prompt_hashes.append({"kind": "custom_message",
                                          "index": custom_messages - 1, "sha256": digest})

    if assistant_messages == 0:
        note("no_assistant_messages")
    if not session_inits:
        note("session_init_absent")
    if session_records == 0:
        note("session_entry_absent")
    refuse(len(session_ids) <= 1,
           "session file contains more than one session id; import one private session file per usage row")

    if isinstance(session_id, str) and session_id:
        if (safe_token(session_id) and "/" not in session_id and "\\" not in session_id
                and re.match(r"^[A-Za-z]:", session_id) is None):
            session_ref = session_id
            session_ref_source = "observed_id"
        else:
            note("session_id_hashed")
            session_ref = "sha256:" + hashlib.sha256(
                session_id.encode("utf-8", "surrogatepass")).hexdigest()
            session_ref_source = "hashed_id"
    else:
        note("session_id_absent")
        try:
            reference = str(pathlib.Path(path).resolve())
        except OSError:
            reference = str(path)
        session_ref = "sha256:" + hashlib.sha256(
            reference.encode("utf-8", "surrogatepass")).hexdigest()
        session_ref_source = "hashed_path"

    wall_time = None
    time_values = list(record_times) + ([session_start] if session_start is not None else [])
    if not record_times or len({value.tzinfo is not None for value in time_values}) > 1:
        note("wall_time_unavailable")
    else:
        last = max(record_times)
        if session_start is not None and session_start > last:
            note("wall_time_inconsistent")
        elif session_start is not None:
            wall_time = (last - session_start).total_seconds()
        elif len(record_times) >= 2:
            wall_time = (last - min(record_times)).total_seconds()
        else:
            note("wall_time_unavailable")

    tokens = {
        "input": sums.get("input") if assistant_messages > 0 and complete["input"] else None,
        "output": sums.get("output") if assistant_messages > 0 and complete["output"] else None,
        "cached": sums.get("cached") if assistant_messages > 0 and complete["cached"] else None,
    }
    estimated_cost = sums.get("cost.total") if assistant_messages > 0 and cost_complete else None
    if price_version is None:
        note("price_version_absent")
    elif estimated_cost is None:
        note("price_version_without_observed_cost")
    note("provider_billing_not_observed")

    first_init = session_inits[0] if session_inits else None
    resolved_model = first_init.get("resolved_model") if first_init else None
    served_models = [
        {"model": key[0], "provider": key[1], "api": key[2], "assistant_messages": count}
        for key, count in sorted(served.items(), key=lambda item: tuple(part or "" for part in item[0]))
    ]
    resolved_identity = resolved_model_identity(resolved_model) if resolved_model else None
    if resolved_identity is None or not served_models:
        served_model_differs = None
    else:
        configured_provider = resolved_identity["provider"]
        configured_model = resolved_identity["model"]
        served_model_differs = any(
            item["model"] != configured_model
            or (configured_provider is not None and item["provider"] is not None
                and item["provider"] != configured_provider)
            for item in served_models
        )

    metadata = {
        "source_format": "omp-session-jsonl",
        "source_sha256": "sha256:" + source_hash.hexdigest(),
        "credential_route": credential_route,
        "routing_provenance": provenance,
        "accounting_complete": False,
        "session_ref_source": session_ref_source,
        "records": records,
        "session_records": session_records,
        "session_init_records": len(session_inits),
        "session_init": first_init,
        "resolved_model_identity": resolved_identity,
        "model_changes": model_changes,
        "thinking_level_changes": thinking_level_changes,
        "fallback_records": fallback_records,
        "entry_types": dict(sorted(entry_types.items())),
        "assistant_messages": assistant_messages,
        "assistant_messages_with_usage": assistant_with_usage,
        "assistant_messages_with_cost_total": assistant_with_cost_total,
        "assistant_messages_without_model_identity": assistant_without_model,
        "user_messages": user_messages,
        "custom_messages": custom_messages,
        "usage_includes_all_assistant_attempts": True,
        "served_model_differs_from_resolved_model": served_model_differs,
    }

    known_subtotals = {
        "assistant_messages": assistant_messages,
        "assistant_messages_with_usage": assistant_with_usage,
        "assistant_messages_with_cost_total": assistant_with_cost_total,
    }
    for field in TOKEN_FIELDS:
        if counts.get(field):
            known_subtotals[field] = {"sum": sums[field], "messages": counts[field]}
    cost_subtotals = {}
    for field in COST_FIELDS:
        name = "cost." + field
        if counts.get(name):
            cost_subtotals[field] = {"sum": sums[name], "messages": counts[name]}
    if cost_subtotals:
        known_subtotals["cost"] = cost_subtotals

    return {
        "stage": stage,
        "session_ref": session_ref,
        "non_model_reason": None,
        "model_ids": served_models,
        "prompt_hashes": prompt_hashes,
        "tokens": tokens,
        "reported_cost": None,
        "estimated_cost": estimated_cost,
        "cost_provenance": COST_PROVENANCE if estimated_cost is not None else None,
        "price_version": price_version,
        "wall_time": wall_time,
        "metadata": metadata,
        "limitations": limitations,
        "known_subtotals": known_subtotals,
    }


def label_argument(value):
    if not safe_label(value):
        raise argparse.ArgumentTypeError("must be a short safe metadata label")
    return value


def token_argument(value):
    if not safe_token(value):
        raise argparse.ArgumentTypeError("must be a short safe token")
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Extract allowlisted routing and usage metadata from one OMP session JSONL "
                    "file; never exports prompts, text, tool payloads, configuration or "
                    "credential values.",
    )
    parser.add_argument("--session", required=True, metavar="PATH",
                        help="OMP session JSONL file to inspect (never copied into the output)")
    parser.add_argument("--stage", required=True, choices=STAGES,
                        help="accountable phase this session belongs to")
    parser.add_argument("--credential-route", required=True, type=label_argument, metavar="LABEL",
                        help="safe metadata label of the credential route used, never a secret")
    parser.add_argument("--provenance", required=True, type=token_argument, metavar="TOKEN",
                        help="safe routing provenance token for the observed session")
    parser.add_argument("--price-version", type=token_argument, default=None, metavar="TOKEN",
                        help="operator-supplied price-version provenance for harness cost estimates")
    arguments = parser.parse_args(argv)
    try:
        row = extract_usage(arguments.session, arguments.stage, arguments.credential_route,
                            arguments.provenance, arguments.price_version)
    except UsageRefusal as refusal:
        print(f"refused: {refusal}", file=sys.stderr)
        return 1
    except (OSError, UnicodeError):
        print("refused: could not read the session file", file=sys.stderr)
        return 1
    print(json.dumps(row, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
