import { describe, expect, it, vi } from "vitest";
import { createApi, Forbidden, Network, UnexpectedResponse, Validation } from "./api";

const workspace = {
  selected_project_id: null,
  projects: [{ id: "11111111-1111-4111-8111-111111111111", key: "DEMO", name: "Demo", description: null, status: "active", work_items: [] }],
  health: { status: "unknown" },
};

describe("read boundary", () => {
  it("decodes real workspace fields and sends URL selection without another attempt", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify(workspace)));
    const api = createApi(fetcher);
    const result = await api.workspace("project /?", new AbortController().signal);
    expect(result.projects[0]?.name).toBe("Demo");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe("/portal/api/workspace?project_id=project+%2F%3F");
  });

  it("rejects malformed success payloads instead of rendering trusted-looking records", async () => {
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response('{"projects":[]}')));
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(UnexpectedResponse);
  });

  it("rejects invalid UTF-8 bytes in a 200 workspace response instead of accepting U+FFFD", async () => {
    const body = new Uint8Array([
      ...new TextEncoder().encode('{"selected_project_id":null,"projects":[{"id":"11111111-1111-4111-8111-111111111111","key":"DEMO","name":"Demo'),
      0xff,
      ...new TextEncoder().encode('","description":null,"status":"active","work_items":[]}],"health":{"status":"unknown"}}'),
    ]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const failure = await api.workspace(null, new AbortController().signal).then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).not.toBeInstanceOf(Network);
  });

  it("keeps a valid non-ASCII project name byte-exact through decoding", async () => {
    const name = "プロジェクト Ω—λ";
    const body = new Uint8Array([
      ...new TextEncoder().encode('{"selected_project_id":null,"projects":[{"id":"11111111-1111-4111-8111-111111111111","key":"DEMO","name":"'),
      ...new TextEncoder().encode(name),
      ...new TextEncoder().encode('","description":null,"status":"active","work_items":[]}],"health":{"status":"unknown"}}'),
    ]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const result = await api.workspace(null, new AbortController().signal);
    expect(result.projects[0]?.name).toBe(name);
  });

  it("applies the same strict UTF-8 rejection to the session bootstrap path", async () => {
    const body = new Uint8Array([...new TextEncoder().encode('{"csrf_token":"to'), 0xff, ...new TextEncoder().encode('ken"}')]);
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const failure = await api.session(new AbortController().signal).then(() => null, (error: unknown) => error);
    expect(failure).toBeInstanceOf(UnexpectedResponse);
    expect(failure).not.toBeInstanceOf(Network);
  });

  it("keeps workspace reads usable when existing resource metadata contains fractional numbers", async () => {
    const body = JSON.stringify({
      ...workspace,
      projects: workspace.projects.map((project) => ({
        ...project,
        resources: [{ metadata: { ratio: 0.5 } }],
      })),
    });
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response(body)));
    const result = await api.workspace(null, new AbortController().signal);
    expect(result.projects[0]?.name).toBe("Demo");
  });

  it.each([
    { status: 403, error: Forbidden },
    { status: 422, error: Validation },
  ])("returns a typed non-transient error for HTTP $status", async ({ status, error }) => {
    const api = createApi(vi.fn<typeof fetch>().mockResolvedValue(new Response("{}", { status })));
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(error);
  });

  it("leaves network retry to the query owner", async () => {
    const fetcher = vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline"));
    const api = createApi(fetcher);
    await expect(api.workspace(null, new AbortController().signal)).rejects.toBeInstanceOf(Network);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("aborts the underlying request when the consuming query is cancelled", async () => {
    let requestSignal: AbortSignal | null | undefined;
    const fetcher = vi.fn<typeof fetch>().mockImplementation((_input, init) => {
      requestSignal = init?.signal;
      const { promise, reject } = Promise.withResolvers<Response>();
      requestSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      return promise;
    });
    const controller = new AbortController();
    const pending = createApi(fetcher).workspace(null, controller.signal);
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    controller.abort();
    await expect(pending).rejects.toBeDefined();
    expect(requestSignal?.aborted).toBe(true);
  });

  it("requests session bootstrap without a browser HTTP cache", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{"csrf_token":"fresh-token"}'));
    const session = await createApi(fetcher).session(new AbortController().signal);
    expect(session.csrf_token).toBe("fresh-token");
    expect(fetcher.mock.calls[0]?.[1]?.cache).toBe("no-store");
  });
});
