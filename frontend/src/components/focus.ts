// Radix restores focus on close only to a Dialog.Trigger, and none of these
// dialogs has one: the invoking control is captured before Radix moves keyboard
// focus inside, and focused again when the dialog closes. That control can be
// disabled while its write is still pending — the dialog stays closable so the
// operator can keep working — and focusing a disabled control never moves
// focus, which would drop keyboard focus to the body. Those closes fall back to
// an element no pending write can disable.
export function restoreFocus(event: Event, opener: HTMLElement | null, fallback: HTMLElement | null) {
  const target = opener?.isConnected && !opener.matches(":disabled")
    ? opener
    : fallback?.isConnected && !fallback.matches(":disabled") ? fallback : null;
  if (!target) return;
  event.preventDefault();
  target.focus();
}
