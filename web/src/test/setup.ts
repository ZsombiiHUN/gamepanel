import { afterEach } from "vitest";
import "@testing-library/jest-dom/vitest";

// Component tests run in jsdom; pure-logic tests stay in node and skip this.
if (typeof document !== "undefined") {
  const { cleanup, configure } = await import("@testing-library/react");
  // A cold start compiles lazily; give async queries room so a slow first run is not a failure.
  configure({ asyncUtilTimeout: 4000 });
  afterEach(() => cleanup());

  // jsdom does not implement <dialog> modality.
  const proto = HTMLDialogElement.prototype;
  proto.showModal ??= function (this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  proto.close ??= function (this: HTMLDialogElement) {
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  };
  Element.prototype.scrollIntoView ??= () => {};
  window.scrollTo = (() => {}) as typeof window.scrollTo;
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  window.matchMedia ??= ((q: string) => ({
    matches: false,
    media: q,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    onchange: null,
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
}

// The API client captures `fetch` and `Request` when its module loads, so install
// delegating versions up front; tests swap the implementation with `setFetch`.
type FetchFn = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
let impl: FetchFn = async (input) => {
  throw new Error(`Unexpected network request: ${String(input instanceof Request ? input.url : input)}`);
};
export function setFetch(fn: FetchFn) {
  impl = fn;
}
if (typeof document !== "undefined") {
  const Orig = globalThis.Request;
  // Node's Request rejects relative URLs; the app calls "/api/...".
  globalThis.Request = class extends Orig {
    constructor(input: RequestInfo | URL, init?: RequestInit) {
      super(typeof input === "string" && input.startsWith("/") ? "http://localhost" + input : input, init);
    }
  } as typeof Request;
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => impl(input, init)) as typeof fetch;
}
