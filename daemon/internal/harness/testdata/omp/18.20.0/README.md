# OMP 18.20.0 native observations

These are sanitized observations from the pinned OMP executables, not Pi
fixtures and not capability-promotion certificates. `fixture-manifest.json`
records each platform, kernel, distribution, executable path, size, SHA-256,
source stream digest and sanitized fixture digest.

- `*-configured.jsonl`: startup `ready`, successful v2 negotiation, idle
  session identity and explicit rejection of protocol version 999.
- `*-live-protocol.jsonl`: a real prompt to the configured Tencent LiteLLM
  relay using the initial, incorrect environment credential, refused with
  HTTP 401, then `prompt_result.status=error`,
  `session_settled` and clean EOF. No repository artifact was produced.
- `*-cancel.jsonl`: abort after a prompt acknowledgement, matching
  `prompt_result.status=aborted`, `session_settled`, correlated
  `abort_and_restore_queue` response and clean EOF. No running repository tool
  was observed; this is not repository-tool cancellation evidence.
- `*-retained-identity.jsonl`: a second process reopens the failed turn's exact
  native session. No new prompt or successful retained-work continuation was
  attempted in this observation.

Native paths and IDs are replaced consistently with placeholders. `get_state`
retains only session identity and quiescence fields. Provider diagnostics that
included an upstream-masked credential fragment are replaced with a plain HTTP
401 classification; no credential value is present. Exact machine-local paths
remain only in the session-local receipts.

The catalog was generated from the operator's configured relay URL with
`apiKey: TENCENT_GLM_API_KEY`; the initial value stayed in inherited process
environments. The later operator correction identifies a different, working
virtual key in the harness configuration. A non-generating authenticated relay
metadata read accepted that key and advertised nonzero rates; zero permitted
spend therefore prevented positive generation acceptance. The historical 401
fixtures do not show that the corrected credential is unavailable. No dummy key
or loopback gateway was used. All OMP executable operations remain
unsupported/unverified.
