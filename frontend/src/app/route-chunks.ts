// Chrome, Firefox and Safari word a failed dynamic import differently; @tanstack/react-router's own
// module-missing guard (`lazyRouteComponent`, keyed `tanstack_router_reload:<message>`) matches the
// same three prefixes, and its one reload per message is what this module takes over.
const chunkLoadFailurePrefixes = [
  "Failed to fetch dynamically imported module",
  "error loading dynamically imported module",
  "Importing a module script failed",
];

// The entry chunk's own URL names this build: a deployment replaces it, so a marker an older build
// left behind never suppresses the reload this build needs.
const buildId = import.meta.url;
const staleChunkReloadKey = "portal:stale-chunk-reload";
const staleChunkMessage = "This route's code changed while the tab was open. Reload the page to load the current build.";

// jsdom's `Location.reload` is unforgeable, so the browser's own reload is an injected default: the
// unit test drives this seam, and the CI render gate covers the real one.
const reloadDocument = (): void => { window.location.reload(); };

function isChunkLoadFailure(error: unknown): boolean {
  const message = error instanceof Error ? error.message : "";
  return chunkLoadFailurePrefixes.some((prefix) => message.startsWith(prefix));
}

function claimReloadOnce(reload: () => void): boolean {
  try {
    if (window.sessionStorage.getItem(staleChunkReloadKey) === buildId) return false;
    window.sessionStorage.setItem(staleChunkReloadKey, buildId);
  } catch {
    // A storage-blocked context cannot record the claim, so a reload there would
    // repeat on every failure: the route's error state is the terminal state.
    return false;
  }
  reload();
  return true;
}

export function loadRouteChunk<Module>(
  importer: () => Promise<Module | undefined>,
  reload: () => void = reloadDocument,
): () => Promise<Module> {
  return async () => {
    let module: Module | undefined;
    try {
      module = await importer();
    } catch (error) {
      if (!isChunkLoadFailure(error)) throw error;
      claimReloadOnce(reload);
      throw new Error(staleChunkMessage, { cause: error });
    }
    // A prevented `vite:preloadError` resolves the import with nothing: the listener below already
    // claimed that failure's reload, and the route's error state is what remains.
    if (module === undefined) {
      claimReloadOnce(reload);
      throw new Error(staleChunkMessage);
    }
    return module;
  };
}

let installedOn: Window | null = null;

export function installStaleChunkReload(reload: () => void = reloadDocument): void {
  if (installedOn === window) return;
  installedOn = window;
  window.addEventListener("vite:preloadError", (event) => {
    if (!isChunkLoadFailure(event.payload)) return;
    if (claimReloadOnce(reload)) event.preventDefault();
  });
}
