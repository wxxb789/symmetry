import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RefObject } from "react";
import { describe, expect, it } from "vitest";
import { ServerRefusal } from "../services/api";
import { Field, FieldErrors, StatusBadge } from "./primitives";
import { useDialogOpener } from "./use-dialog-opener";

const refusal = (fields: Record<string, readonly string[]> | null) =>
  new ServerRefusal({ status: 422, code: "validation_failed", message: "request contains invalid fields", fields });

/** The smallest caller of the opener hook: a close control that runs the restore and reports whether Radix's default was prevented. */
function RestoreOnClick({ openerRef, fallbackRef, onRestored }: {
  openerRef?: RefObject<HTMLElement | null>;
  fallbackRef?: RefObject<HTMLElement | null>;
  onRestored?: (prevented: boolean) => void;
}) {
  const { restore } = useDialogOpener(openerRef, fallbackRef);
  return <button type="button" onClick={() => {
    // Cancelable, like Radix's own close-auto-focus event: the restore cancels
    // it only when it has a target to focus.
    const event = new FocusEvent("focus", { cancelable: true });
    restore(event);
    if (onRestored) onRestored(event.defaultPrevented);
  }}>Close dialog</button>;
}

describe("StatusBadge", () => {
  it("renders the workspace states, marking only done complete and with the check icon", () => {
    render(<>
      <StatusBadge status="done" />
      <StatusBadge status="in_review" />
    </>);
    const done = screen.getByText("done");
    expect(done).toHaveClass("status-badge", "status-complete");
    expect(done.querySelector("svg")).not.toBeNull();
    const review = screen.getByText("in review");
    expect(review).toHaveClass("status-badge");
    expect(review).not.toHaveClass("status-complete");
    expect(review.querySelector("svg")).toBeNull();
  });

  it("names an unknown workspace status instead of rendering it bare", () => {
    render(<>
      <StatusBadge status="mystery" />
      <StatusBadge status="" />
    </>);
    expect(screen.getByText("Unknown status: mystery")).toBeVisible();
    expect(screen.getByText("Unknown status")).toBeVisible();
  });

  it("renders the served resource scopes with their tone and their status attribute", () => {
    render(<>
      <StatusBadge status="healthy" scope="resource" />
      <StatusBadge status="degraded" scope="resource" />
      <StatusBadge status="synced" scope="sync" />
      <StatusBadge status="failed" scope="sync" />
      <StatusBadge status="offline" scope="connection" />
    </>);
    const healthy = screen.getByText("Healthy");
    expect(healthy).toHaveClass("status-badge", "status-complete");
    expect(healthy).toHaveAttribute("data-status", "healthy");
    expect(screen.getByText("Degraded")).toHaveClass("status-badge", "status-attention");
    expect(screen.getByText("Synced")).toHaveClass("status-badge", "status-complete");
    expect(screen.getByText("Failed")).toHaveClass("status-badge", "status-attention");
    expect(screen.getByText("Offline")).toHaveClass("status-badge", "status-attention");
  });

  it("names an unknown resource status with its scope, never as a known word", () => {
    render(<>
      <StatusBadge status="future_kind" scope="resource" />
      <StatusBadge status="" scope="sync" />
      <StatusBadge status="constructor" scope="connection" />
    </>);
    expect(screen.getByText("Unknown resource status: future_kind")).toHaveAttribute("data-status", "future_kind");
    expect(screen.getByText("Unknown sync status: unspecified")).toBeVisible();
    // `constructor` is a prototype member, not a status this surface knows.
    expect(screen.getByText("Unknown connection status: constructor")).toBeVisible();
  });
});

describe("FieldErrors", () => {
  it("joins the server's messages for the field into one alert", () => {
    render(<FieldErrors error={refusal({ name: ["must be present", "is too long"] })} field="name" />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("must be present is too long");
    expect(alert).toHaveClass("field-error");
  });

  it("renders nothing for another field, another error, or an empty message list", () => {
    const { container } = render(<>
      <FieldErrors error={refusal({ name: ["has already been taken"] })} field="key" />
      <FieldErrors error={new Error("network is unreachable")} field="name" />
      <FieldErrors error={refusal({ name: [] })} field="name" />
      <FieldErrors error={refusal(null)} field="name" />
    </>);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("Field", () => {
  it("renders the label, the control and the field's messages as the form's own children", () => {
    const { container } = render(<form className="planning-form">
      <Field id="project-name" label="Name" error={refusal({ name: ["has already been taken"] })} field="name">
        <input id="project-name" name="name" defaultValue="Duplicate" />
      </Field>
    </form>);
    // `.planning-form` is a CSS grid over its direct children: the row must add
    // no element between the form and the label/control/message it renders.
    const rows = Array.from(container.querySelector("form")?.children ?? []);
    expect(rows.map((child) => child.tagName)).toEqual(["LABEL", "INPUT", "P"]);
    expect(screen.getByLabelText("Name")).toHaveValue("Duplicate");
    expect(screen.getByRole("alert")).toHaveTextContent("has already been taken");
  });
});

describe("useDialogOpener", () => {
  it("focuses the connected, enabled opener and clears it before restoring", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <button type="button">Update checkout service</button>
      <RestoreOnClick openerRef={openerRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    const opener = screen.getByRole("button", { name: "Update checkout service" });
    openerRef.current = opener;
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(openerRef.current).toBeNull();
    expect(prevented).toEqual([true]);
    expect(opener).toHaveFocus();
  });

  it("falls back to the caller's fallback control when the opener is disabled", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    render(<>
      <button type="button" disabled>Update checkout service</button>
      <h2 ref={(node) => { fallbackRef.current = node; }} tabIndex={-1}>Project resources</h2>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} />
    </>);
    openerRef.current = screen.getByRole("button", { name: "Update checkout service" });
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("heading", { name: "Project resources" })).toHaveFocus();
  });

  it("falls back to the caller's fallback control when the opener left the document", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: document.createElement("button") };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    render(<>
      <h2 ref={(node) => { fallbackRef.current = node; }} tabIndex={-1}>Project resources</h2>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} />
    </>);
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("heading", { name: "Project resources" })).toHaveFocus();
  });

  it("leaves focus where it was when neither the opener nor the fallback can take it", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <button type="button" disabled>Update checkout service</button>
      <RestoreOnClick openerRef={openerRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    openerRef.current = screen.getByRole("button", { name: "Update checkout service" });
    const close = screen.getByRole("button", { name: "Close dialog" });
    await user.click(close);
    expect(prevented).toEqual([false]);
    expect(close).toHaveFocus();
  });

  it("captures the active element at mount when the caller owns no ref", async () => {
    const user = userEvent.setup();
    render(<button type="button">Update checkout service</button>);
    const invoking = screen.getByRole("button", { name: "Update checkout service" });
    invoking.focus();
    // Mounted while the invoking control holds focus: the capture belongs to the
    // mount, not to the close.
    render(<RestoreOnClick />);
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(invoking).toHaveFocus();
  });
});
