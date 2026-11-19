# GamePanel web

React 19 + Vite 8 + TypeScript SPA (TanStack Router/Query, Tailwind v4, xterm.js, CodeMirror 6).

```sh
pnpm install
pnpm gen:api     # regenerate src/api/schema.d.ts from openapi.json
pnpm dev         # http://localhost:5173, proxies /api (incl. websockets) to 127.0.0.1:8080
pnpm typecheck   # tsc --noEmit
pnpm test        # vitest: unit and component tests (jsdom, fake API), about 10 s
pnpm e2e         # builds, then Playwright against a real panel with a fake daemon
pnpm build       # type-check, then build into ../internal/web/dist (embedded by the Go binary)
```

TypeScript is pinned to 6.0.x because openapi-typescript 7 does not work with TypeScript 7.

## Checks

`pnpm lint` runs ESLint with the React hooks rules (they catch effects that copy state, refs read while rendering, impure render code) plus project rules: query keys and cache invalidation are only written in `api/queries.ts` and `queryClient.ts`. `pnpm typecheck` covers the app and the e2e specs.

## Tests

| Command     | What it covers                                                                                                                                                                                                                                                                                                                                |
| ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pnpm test` | Pure logic in `src/lib` and `src/hooks` (search, ranking, sorting, file crawl, usage charts, URL state), routing (sign-in gate, admin pages, state in the address) and whole pages rendered in jsdom against a fake API (`src/test/harness.tsx`): filtering, empty states, keyboard shortcuts, the quick switcher, audit paging, file search. |
| `pnpm e2e`  | A real browser (Chromium) against `internal/e2e/testpanel`: sign-in, searching, console, files, audit, creating servers, a regular user's permissions, accessibility (axe), and layout at 1440x900, 1366x768 and 390x844.                                                                                                                     |

First time: `pnpm e2e:install` downloads the browser. Playwright is pinned to 1.58.2; set `PW_CHROMIUM` to use a browser you already have.

Layout (sticky console bar, pinned sidebar, sideways scroll) can only be tested in a real browser, so it lives in
`e2e/layout.spec.ts`. jsdom has no layout engine; component tests check structure and behaviour, not pixels.

`internal/e2e/testpanel` is a test-only command: panel, in-process fake daemon, an admin, a regular user
(who may read the console and files of one server), and 26 servers. Each e2e test that changes things makes
its own servers through `panel.createServer()`, which are deleted afterwards, so the shared ones stay as seeded.
Reports and traces from failed runs are in `e2e/.report` and `e2e/.results`.
