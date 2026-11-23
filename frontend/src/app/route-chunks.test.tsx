import type { ReactNode } from "react";
import { createMemoryHistory, createRootRoute, createRoute, lazyRouteComponent, Outlet, Router, RouterProvider } from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { installStaleChunkReload, loadRouteChunk } from "./route-chunks";

const moduleNotFoundMessage = "Failed to fetch dynamically imported module: http://localhost/portal/assets/chat-4f1c.js";
const staleChunkErrorText = /This route's code changed while the tab was open/;
const reload = vi.fn();

interface RouteChunk { LatePage: () => ReactNode }

function renderRoutes(start: string, load: () => Promise<RouteChunk | undefined>) {
  const root = createRootRoute({ component: () => <><p>shell</p><Outlet /></> });
  const first = createRoute({ getParentRoute: () => root, path: "/first", component: () => <p>first page</p> });
  const late = createRoute({ getParentRoute: () => root, path: "/late", component: lazyRouteComponent(loadRouteChunk(load, reload), "LatePage") });
  const history = createMemoryHistory({ initialEntries: [start] });
  const router = new Router({ routeTree: root.addChildren([first, late]), history });
  return { router, history, ...render(<RouterProvider router={router} />) };
}

function preloadError(message: string): VitePreloadErrorEvent {
  return Object.assign(new Event("vite:preloadError", { cancelable: true }), { payload: new Error(message) });
}

describe("route chunk loading", () => {
  beforeAll(() => { installStaleChunkReload(reload); });
  beforeEach(() => { sessionStorage.clear(); });

  it("keeps the shell and the current route mounted while the next route's chunk is still loading", async () => {
    const { promise, resolve } = Promise.withResolvers<RouteChunk>();
    const { router, history } = renderRoutes("/first", () => promise);
    await screen.findByText("first page");
    act(() => { history.push("/late"); });
    await waitFor(() => { expect(router.state.status).toBe("pending"); });
    expect(screen.getByText("shell")).toBeVisible();
    expect(screen.getByText("first page")).toBeVisible();
    expect(screen.queryByText("late page")).toBeNull();
    resolve({ LatePage: () => <p>late page</p> });
    expect(await screen.findByText("late page")).toBeVisible();
    expect(screen.queryByText("first page")).toBeNull();
  });

  it("holds the first paint of a cold lazy deep link until that route's chunk resolves", async () => {
    const { promise, resolve } = Promise.withResolvers<RouteChunk>();
    renderRoutes("/late", () => promise);
    expect(screen.queryByText("shell")).toBeNull();
    resolve({ LatePage: () => <p>late page</p> });
    expect(await screen.findByText("shell")).toBeVisible();
    expect(screen.getByText("late page")).toBeVisible();
  });

  it("reloads the page once and shows the route's error state when a chunk load fails", async () => {
    renderRoutes("/late", () => Promise.reject(new Error(moduleNotFoundMessage)));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText(staleChunkErrorText)).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem("portal:stale-chunk-reload")).not.toBeNull();
  });

  it("shows the route's error state without another reload once this build has reloaded", async () => {
    const first = renderRoutes("/late", () => Promise.reject(new Error(moduleNotFoundMessage)));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
    first.unmount();
    renderRoutes("/late", () => Promise.reject(new Error(moduleNotFoundMessage)));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText(staleChunkErrorText)).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("leaves a failing route chunk that is not a stale chunk to the error state without reloading", async () => {
    renderRoutes("/late", () => Promise.reject(new Error("chunk evaluation failed")));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText("chunk evaluation failed")).toBeVisible();
    expect(reload).not.toHaveBeenCalled();
    expect(sessionStorage.getItem("portal:stale-chunk-reload")).toBeNull();
  });

  it("claims one reload for a prevented vite:preloadError and leaves the repeat to the error state", () => {
    const first = preloadError(moduleNotFoundMessage);
    window.dispatchEvent(first);
    expect(first.defaultPrevented).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
    const second = preloadError(moduleNotFoundMessage);
    window.dispatchEvent(second);
    expect(second.defaultPrevented).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("shows the route's error state when a prevented preload left the chunk import empty", async () => {
    renderRoutes("/late", () => Promise.resolve(undefined));
    window.dispatchEvent(preloadError(moduleNotFoundMessage));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText(staleChunkErrorText)).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("reloads for this build even when a marker an older build left is on record", async () => {
    const olderBuildMarker = "http://localhost/portal/assets/index-OLDBUILD.js";
    sessionStorage.setItem("portal:stale-chunk-reload", olderBuildMarker);
    renderRoutes("/late", () => Promise.reject(new Error(moduleNotFoundMessage)));
    await waitFor(() => { expect(reload).toHaveBeenCalledTimes(1); });
    expect(sessionStorage.getItem("portal:stale-chunk-reload")).not.toBe(olderBuildMarker);
    expect(await screen.findByText(staleChunkErrorText)).toBeVisible();
  });

  it("shows the route's error state without reloading when the session storage is blocked", async () => {
    const storage = vi.spyOn(window, "sessionStorage", "get").mockImplementation(() => {
      throw new DOMException("The operation is insecure.", "SecurityError");
    });
    try {
      renderRoutes("/late", () => Promise.reject(new Error(moduleNotFoundMessage)));
      expect(await screen.findByText("Something went wrong!")).toBeVisible();
      expect(screen.getByText(staleChunkErrorText)).toBeVisible();
      expect(reload).not.toHaveBeenCalled();
      // The listener cannot claim the reload either, so it leaves the event to the
      // boundary's rejection and the same terminal error state.
      const event = preloadError(moduleNotFoundMessage);
      window.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(false);
      expect(reload).not.toHaveBeenCalled();
    } finally {
      storage.mockRestore();
    }
  });

  it("claims one reload for a Firefox module-not-found wording", async () => {
    renderRoutes("/late", () => Promise.reject(new Error("error loading dynamically imported module: http://localhost/portal/assets/chat-4f1c.js")));
    await waitFor(() => { expect(reload).toHaveBeenCalledTimes(1); });
    expect(await screen.findByText(staleChunkErrorText)).toBeVisible();
  });

  it("claims one reload for a Safari module-not-found wording", async () => {
    renderRoutes("/late", () => Promise.reject(new Error("Importing a module script failed.")));
    await waitFor(() => { expect(reload).toHaveBeenCalledTimes(1); });
    expect(await screen.findByText(staleChunkErrorText)).toBeVisible();
  });

  it("leaves a preload failure that is not a stale chunk unclaimed and unprevented", () => {
    const event = preloadError("Unable to preload CSS for /portal/assets/index-abc.css");
    window.dispatchEvent(event);
    expect(reload).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });
});
