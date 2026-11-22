import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import "./index.css";
import "./i18n";
import { createAppRouter } from "./router";
import { setUnauthorizedHandler } from "./api/client";
import { queryClient } from "./queryClient";

const router = createAppRouter(queryClient);

setUnauthorizedHandler(() => {
  queryClient.clear();
  if (router.state.location.pathname !== "/login") void router.navigate({ to: "/login" });
});

const root = document.getElementById("root");
if (!root) throw new Error("index.html has no #root element");
createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
