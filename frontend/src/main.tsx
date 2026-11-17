import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createApplication } from "./app/application";
import { legacyDestination } from "./app/legacy";
import { createAppRouter } from "./app/router";
import "./styles.css";

// The one-time legacy hash repair runs before the router mounts, on the same
// document: one history replacement, then the router renders the destination.
const legacy = /^\/portal\/?$/.test(window.location.pathname)
  ? legacyDestination(window.location.hash, window.location.search)
  : null;
if (legacy) window.history.replaceState(null, "", legacy);

const application = createApplication();
const router = createAppRouter(application);
const element = document.getElementById("root");
if (!element) throw new Error("Workspace mount element is missing");
createRoot(element).render(
  <StrictMode>
    <QueryClientProvider client={application.queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
