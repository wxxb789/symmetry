import type { ReactNode } from "react";
import { createMemoryHistory, createRootRoute, createRoute, lazyRouteComponent, Outlet, Router, RouterProvider } from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { installStaleChunkReload, loadRouteChunk } from "./route-chunks";

const staleChunkMessage = "Failed to fetch dynamically imported module: http://localhost/portal/assets/chat-4f1c.js";
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
    renderRoutes("/late", () => Promise.reject(new Error(staleChunkMessage)));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText(staleChunkErrorText)).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
    expect(sessionStorage.getItem("portal:stale-chunk-reload")).not.toBeNull();
  });

  it("shows the route's error state without another reload once this build has reloaded", async () => {
    const first = renderRoutes("/late", () => Promise.reject(new Error(staleChunkMessage)));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
    first.unmount();
    renderRoutes("/late", () => Promise.reject(new Error(staleChunkMessage)));
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
    const first = preloadError(staleChunkMessage);
    window.dispatchEvent(first);
    expect(first.defaultPrevented).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
    const second = preloadError(staleChunkMessage);
    window.dispatchEvent(second);
    expect(second.defaultPrevented).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("shows the route's error state when a prevented preload left the chunk import empty", async () => {
    renderRoutes("/late", () => Promise.resolve(undefined));
    window.dispatchEvent(preloadError(staleChunkMessage));
    expect(await screen.findByText("Something went wrong!")).toBeVisible();
    expect(screen.getByText(staleChunkErrorText)).toBeVisible();
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
