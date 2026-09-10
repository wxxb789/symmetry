# pi 0.85.1 RPC synthetic fixtures

These fixtures model the installed `pi --mode rpc` documentation only. They do
not prove authenticated native execution or advertise a supported adapter.

- `lifecycle.jsonl` demonstrates a correlated `get_state`, an accepted prompt,
  retry-shaped `agent_end` events, and the only final native boundary:
  `agent_settled`.
- `version.txt` records the observed local CLI version.
