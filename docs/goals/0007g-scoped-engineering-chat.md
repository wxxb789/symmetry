# 0007g - Scoped, recoverable engineering conversation

Provide real workspace-, project-, work-, and run-scoped engineering
conversation that preserves each scope's draft, selection, scroll position, and
history. Questions, proposed commands, authorization, execution, and delivery
receipts remain distinct so conversation cannot silently mutate work.

Completion evidence includes browser and live-daemon tests for scope switching,
changed-action replay, late reads and writes, uncertain retry, history gaps,
unsafe output rendering, reconnect, and durable control receipts. Keyboard and
focus behavior remains usable throughout long conversations.

This goal renders no raw agent HTML and gives read-only conversation no implicit
worker authority. It depends on 0007a and 0007f, is one independently mergeable
PR, and must fit one agent run of at most 12 hours.
