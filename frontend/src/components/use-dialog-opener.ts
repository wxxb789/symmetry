import { useRef, type RefObject } from "react";
import { restoreFocus } from "./focus";

// Radix restores focus on close only to a `Dialog.Trigger`, and these dialogs have
// none: the opener is captured before Radix moves focus in, and cleared on restore.
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
