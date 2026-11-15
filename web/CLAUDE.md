# Frontend conventions

React 19, TanStack Query + Router, Tailwind 4. `pnpm lint && pnpm typecheck && pnpm test` must pass; `pnpm e2e` for anything visual or routed.

## Data
- Every query is a `queryOptions` factory in `src/api/queries.ts`, keyed through `keys.*`. Pages call `useQuery(xQuery(id))`. Never write a `queryKey` array or call `invalidateQueries` in a page (lint enforces it).
- A mutation says what it changes: `useMutation({ mutationFn, meta: invalidates(keys.x) })`. `queryClient.ts` refreshes it after success. Page-specific follow-up (clear a field, close a dialog) goes in `onSuccess`.
- Background work the server finishes later: `refreshLater(qc, key, ms)`.

## Routing and state
- Auth is `beforeLoad` on the `app` route; it puts `me` in route context. Pages never handle "not signed in". Admin-only pages live under the `admin` route.
- Anything a person would want to link to or reload belongs in the address as a validated search param (see `router.tsx`): filters, search text, page, folder, tab. Free-text boxes use `useSearchText` (local value, URL follows after a pause) so typing is never dropped.
- Loaders only *prefetch*. Pages keep their own loading and error states.

## Components
- A page file does one job. If it holds a list, a form, a dialog and a crawl, split it (`pages/server/` is the pattern). Over ~300 lines is a warning.
- Logic that can be a pure function goes in `src/lib` with a unit test (`lib/usage.ts`, `lib/servers.ts`). Components format and render.
- Do not copy props or query data into state with an effect. Derive it, key the component, or adjust state during render (`useRollingSamples`, `SettingsTab`). `useEffect` is for talking to the outside world: sockets, timers, DOM, subscriptions.
- Shared UI belongs in `components/` the second time it is needed (`SearchInput`, `Field`, `Section`), not the third.

## Tests
- Behaviour is tested through the real router with `renderApp(fakeApi, "/path")`. Render a bare component only when it takes no router state.
- Fakes behave like the server (a delete removes the file). Do not make the UI paper over a fake that does not.
- A bug found in the browser gets a regression test at the lowest level that can show it.
