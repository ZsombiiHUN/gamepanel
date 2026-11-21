import { type ReactElement } from "react";
import { render } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createQueryClient } from "../queryClient";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { createAppRouter } from "../router";
import { admin } from "./fixtures";
import { setFetch } from "./setup";

export interface Call {
  method: string;
  path: string;
  query: URLSearchParams;
  body: unknown;
}
type Reply = ((c: Call) => unknown) | object | string | number | boolean | null | undefined;

/**
 * A stand-in for the panel API. Routes are "METHOD /api/path" and reply with JSON
 * (or a function returning JSON or a Response). Unknown routes fail the test loudly.
 */
export class FakeApi {
  calls: Call[] = [];
  private routes = new Map<string, Reply>();

  constructor(routes: Record<string, Reply> = {}) {
    this.on({ "GET /api/auth/me": admin, ...routes });
  }
  on(routes: Record<string, Reply>) {
    for (const [k, v] of Object.entries(routes)) this.routes.set(k, v);
    return this;
  }
  callsTo(key: string) {
    const [method, path] = key.split(" ");
    return this.calls.filter((c) => c.method === method && c.path === path);
  }
  install() {
    setFetch(async (input, init) => {
      const req = input instanceof Request ? input : new Request(input, init);
      const url = new URL(req.url);
      const text = req.method === "GET" || req.method === "HEAD" ? "" : await req.text();
      const call: Call = { method: req.method, path: url.pathname, query: url.searchParams, body: text ? JSON.parse(text) : undefined };
      this.calls.push(call);
      const key = `${call.method} ${call.path}`;
      if (!this.routes.has(key)) throw new Error(`FakeApi: no route for ${key}`);
      const reply = this.routes.get(key);
      const out = typeof reply === "function" ? await (reply as (c: Call) => unknown)(call) : reply;
      if (out instanceof Response) return out;
      if (out === undefined) return new Response(null, { status: 204 });
      return new Response(JSON.stringify(out), { status: 200, headers: { "content-type": "application/json" } });
    });
    return this;
  }
}

export function problem(status: number, detail: string) {
  return new Response(JSON.stringify({ title: "Error", status, detail }), {
    status,
    headers: { "content-type": "application/problem+json" },
  });
}

function newQueryClient() {
  const client = createQueryClient();
  client.setDefaultOptions({ queries: { ...client.getDefaultOptions().queries, retry: false, refetchInterval: false } });
  return client;
}

/** Render the whole app (router, shell and pages) at `path`, with `api` standing in for the panel. */
export function renderApp(api: FakeApi, path = "/") {
  api.install();
  const history = createMemoryHistory({ initialEntries: [path] });
  const client = newQueryClient();
  const router = createAppRouter(client, history);
  const utils = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...utils, router, client };
}

export function renderWithClient(ui: ReactElement) {
  const client = newQueryClient();
  return { client, ...render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>) };
}
