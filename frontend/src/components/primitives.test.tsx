import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { RefObject } from "react";
import { describe, expect, it } from "vitest";
import { ServerRefusal } from "../services/api";
import { Field, FieldErrors, StatusBadge } from "./primitives";
import { useDialogOpener } from "./use-dialog-opener";

const refusal = (fields: Record<string, readonly string[]> | null) =>
  new ServerRefusal({ status: 422, code: "validation_failed", message: "request contains invalid fields", fields });

function RestoreOnClick({ openerRef, fallbackRef, onRestored }: {
  openerRef?: RefObject<HTMLElement | null>;
  fallbackRef?: RefObject<HTMLElement | null>;
  onRestored?: (prevented: boolean) => void;
}) {
  const { restore } = useDialogOpener(openerRef, fallbackRef);
  return <button type="button" onClick={() => {
    // Cancelable, like Radix's own close-auto-focus event: the restore cancels it
    // only when it has a target to focus.
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
    expect(done).not.toHaveAttribute("data-status");
    const review = screen.getByText("in review");
    expect(review).toHaveClass("status-badge");
    expect(review).not.toHaveClass("status-complete");
    expect(review.querySelector("svg")).toBeNull();
    expect(review).not.toHaveAttribute("data-status");
  });

  it("names an unknown workspace status instead of rendering it bare", () => {
    render(<>
      <StatusBadge status="mystery" />
      <StatusBadge status="" />
      <StatusBadge status="constructor" />
    </>);
    expect(screen.getByText("Unknown status: mystery")).toBeVisible();
    expect(screen.getByText("Unknown status")).toBeVisible();
    expect(screen.getByText("Unknown status: constructor")).toBeVisible();
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
    const degraded = screen.getByText("Degraded");
    expect(degraded).toHaveClass("status-badge", "status-attention");
    expect(degraded).toHaveAttribute("data-status", "degraded");
    const synced = screen.getByText("Synced");
    expect(synced).toHaveClass("status-badge", "status-complete");
    expect(synced).toHaveAttribute("data-status", "synced");
    const failed = screen.getByText("Failed");
    expect(failed).toHaveClass("status-badge", "status-attention");
    expect(failed).toHaveAttribute("data-status", "failed");
    const offline = screen.getByText("Offline");
    expect(offline).toHaveClass("status-badge", "status-attention");
    expect(offline).toHaveAttribute("data-status", "offline");
  });

  it("names an unknown resource status with its scope, never as a known word", () => {
    render(<>
      <StatusBadge status="future_kind" scope="resource" />
      <StatusBadge status="synced" scope="resource" />
      <StatusBadge status="" scope="sync" />
      <StatusBadge status="healthy" scope="sync" />
      <StatusBadge status="constructor" scope="connection" />
      <StatusBadge status="constructor" scope="sync" />
    </>);
    const future = screen.getByText("Unknown resource status: future_kind");
    expect(future).toBeVisible();
    expect(future).toHaveAttribute("data-status", "future_kind");
    expect(screen.getByText("Unknown resource status: synced")).toHaveAttribute("data-status", "synced");
    expect(screen.getByText("Unknown sync status: healthy")).toHaveAttribute("data-status", "healthy");
    const unspecified = screen.getByText("Unknown sync status: unspecified");
    expect(unspecified).toBeVisible();
    expect(unspecified).toHaveAttribute("data-status", "");
    const connectionConstructor = screen.getByText("Unknown connection status: constructor");
    expect(connectionConstructor).toBeVisible();
    expect(connectionConstructor).toHaveAttribute("data-status", "constructor");
    expect(screen.getByText("Unknown sync status: constructor")).toHaveAttribute("data-status", "constructor");
  });
});

describe("FieldErrors", () => {
  it("joins the server's messages for the field into one alert", () => {
    render(<FieldErrors error={refusal({ name: ["must be present", "is too long"] })} field="name" />);
    const alert = screen.getByRole("alert");
    // Exact text, not substring matching: a prefix or a double-space join would pass.
    expect(alert.textContent).toBe("must be present is too long");
    expect(alert).toHaveClass("field-error");
  });

  it("renders nothing for another field, another error, or an empty message list", () => {
    const { container } = render(<>
      <FieldErrors error={refusal({ name: ["has already been taken"] })} field="key" />
      <FieldErrors error={new Error("network is unreachable")} field="name" />
      <FieldErrors error={{ fields: { name: ["looks like a refusal but is not"] } }} field="name" />
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

  it("prefers the opener over the fallback when both can take focus", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <button type="button">Update checkout service</button>
      <h2 ref={(node) => { fallbackRef.current = node; }} tabIndex={-1}>Project resources</h2>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    openerRef.current = screen.getByRole("button", { name: "Update checkout service" });
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("button", { name: "Update checkout service" })).toHaveFocus();
    expect(screen.getByRole("heading", { name: "Project resources" })).not.toHaveFocus();
    expect(prevented).toEqual([true]);
  });

  it("falls back to the caller's fallback control when the opener is disabled", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <button type="button" disabled>Update checkout service</button>
      <h2 ref={(node) => { fallbackRef.current = node; }} tabIndex={-1}>Project resources</h2>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    openerRef.current = screen.getByRole("button", { name: "Update checkout service" });
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("heading", { name: "Project resources" })).toHaveFocus();
    expect(prevented).toEqual([true]);
  });

  it("falls back to the caller's fallback control when the opener left the document", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: document.createElement("button") };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <h2 ref={(node) => { fallbackRef.current = node; }} tabIndex={-1}>Project resources</h2>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(screen.getByRole("heading", { name: "Project resources" })).toHaveFocus();
    expect(prevented).toEqual([true]);
  });

  it("leaves focus where it was when the opener cannot take it and no fallback is given", async () => {
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

  it("leaves focus where it was when the opener and the fallback are both disabled", async () => {
    const user = userEvent.setup();
    const openerRef: RefObject<HTMLElement | null> = { current: null };
    const fallbackRef: RefObject<HTMLElement | null> = { current: null };
    const prevented: boolean[] = [];
    render(<>
      <button type="button" disabled>Update checkout service</button>
      <button type="button" ref={(node) => { fallbackRef.current = node; }} disabled>Project resources</button>
      <RestoreOnClick openerRef={openerRef} fallbackRef={fallbackRef} onRestored={(value) => { prevented.push(value); }} />
    </>);
    openerRef.current = screen.getByRole("button", { name: "Update checkout service" });
    const close = screen.getByRole("button", { name: "Close dialog" });
    await user.click(close);
    expect(prevented).toEqual([false]);
    expect(close).toHaveFocus();
    expect(screen.getByRole("button", { name: "Project resources" })).not.toHaveFocus();
  });

  it("captures the active element at mount when the caller owns no ref", async () => {
    const user = userEvent.setup();
    render(<button type="button">Update checkout service</button>);
    const invoking = screen.getByRole("button", { name: "Update checkout service" });
    invoking.focus();
    render(<RestoreOnClick />);
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(invoking).toHaveFocus();
  });
});
