import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createApplication } from "./app/application";
import { legacyDestination } from "./app/legacy";
import { createAppRouter } from "./app/router";
import "./styles.css";

const legacy = /^\/portal\/?$/.test(window.location.pathname) ? legacyDestination(window.location.hash, window.location.search) : null;
if (legacy?.startsWith("/portal/classic")) {
  window.location.replace(legacy);
} else {
  if (legacy) window.history.replaceState(null, "", legacy);
  const application = createApplication();
  const router = createAppRouter(application);
  const element = document.getElementById("root");
  if (!element) throw new Error("Workspace mount element is missing");
  createRoot(element).render(<StrictMode><QueryClientProvider client={application.queryClient}><RouterProvider router={router} /></QueryClientProvider></StrictMode>);
}
