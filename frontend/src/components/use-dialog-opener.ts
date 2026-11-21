import { useRef, type RefObject } from "react";
import { restoreFocus } from "./focus";

/**
 * The opener capture and close-time restore the shared dialog frame and the
 * planning editors use. Radix restores focus on close only to a
 * `Dialog.Trigger`, and none of these dialogs has one: the invoking control is
 * captured before Radix moves keyboard focus inside, and focused again when the
 * dialog closes (the disabled-control rule lives in `restoreFocus`).
 *
 * A caller that already holds the invoking control passes its ref and the
 * fallback its surface keeps; a caller that does not gets `document.activeElement`
 * as of the first render, with no fallback. `restore` clears the opener before
 * delegating, so a close cannot re-target the previous dialog's control.
 */
export function useDialogOpener(openerRef?: RefObject<HTMLElement | null>, fallbackFocus?: RefObject<HTMLElement | null>) {
  const capturedOpener = useRef<HTMLElement | null>(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  const opener = openerRef ?? capturedOpener;
  const restore = (event: Event) => {
    const target = opener.current;
    opener.current = null;
    restoreFocus(event, target, fallbackFocus?.current ?? null);
  };
  return { restore };
}
