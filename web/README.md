# GamePanel web

React 19 + Vite 8 + TypeScript SPA (TanStack Router/Query, Tailwind v4, xterm.js, CodeMirror 6).

```sh
pnpm install
pnpm gen:api     # regenerate src/api/schema.d.ts from openapi.json
pnpm dev         # http://localhost:5173, proxies /api (incl. websockets) to 127.0.0.1:8080
pnpm typecheck   # tsc --noEmit
pnpm test        # vitest
pnpm build       # type-check, then build into ../internal/web/dist (embedded by the Go binary)
```

TypeScript is pinned to 6.0.x because openapi-typescript 7 does not work with TypeScript 7.
