// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { setUnauthorizedHandler } from "./client";
import { CLOSE_FORBIDDEN, CLOSE_NOT_FOUND, CLOSE_UNAUTHORIZED, useServerSocket } from "./ws";

/** Records every socket the hook opens; tests drive them by hand. */
class FakeSocket {
  static all: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(public url: string) {
    FakeSocket.all.push(this);
  }
  close() {}
}

/** The n-th socket the hook opened. */
const sock = (n: number) => {
  const s = FakeSocket.all[n];
  if (!s) throw new Error(`no socket #${n}`);
  return s;
};

const uuid = "00000000-0000-4000-8000-000000000001";
const handlers = { onConsole: () => {}, onReset: () => {} };

beforeEach(() => {
  FakeSocket.all = [];
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", FakeSocket);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  setUnauthorizedHandler(() => {});
});

describe("useServerSocket", () => {
  it("reconnects after the connection drops", () => {
    const { result } = renderHook(() => useServerSocket(uuid, handlers));
    act(() => sock(0).onopen?.());
    expect(result.current.status).toBe("open");
    act(() => sock(0).onclose?.({ code: 1006 }));
    expect(result.current.status).toBe("reconnecting");
    act(() => void vi.advanceTimersByTime(1000));
    expect(FakeSocket.all).toHaveLength(2);
  });

  it.each([
    ["no console permission", CLOSE_FORBIDDEN],
    ["server gone or access removed", CLOSE_NOT_FOUND],
  ])("stops for good when %s", (_, code) => {
    const { result } = renderHook(() => useServerSocket(uuid, handlers));
    act(() => sock(0).onclose?.({ code }));
    expect(result.current.status).toBe("closed");
    act(() => void vi.advanceTimersByTime(60_000));
    expect(FakeSocket.all).toHaveLength(1);
  });

  it("sends the user to sign in when the session ended", () => {
    const signedOut = vi.fn();
    setUnauthorizedHandler(signedOut);
    const { result } = renderHook(() => useServerSocket(uuid, handlers));
    act(() => sock(0).onclose?.({ code: CLOSE_UNAUTHORIZED }));
    expect(result.current.status).toBe("closed");
    expect(signedOut).toHaveBeenCalledOnce();
    act(() => void vi.advanceTimersByTime(60_000));
    expect(FakeSocket.all).toHaveLength(1);
  });

  it("passes on single lines and batches in order", () => {
    const seen: string[] = [];
    renderHook(() => useServerSocket(uuid, { ...handlers, onConsole: (l) => seen.push(l) }));
    act(() => sock(0).onopen?.());
    const send = (data: unknown) => act(() => sock(0).onmessage?.({ data: JSON.stringify({ type: "console", data }) }));
    send({ uuid, line: "one" });
    send({ uuid, line: "", lines: ["two", "three"] });
    send({ uuid, line: "" });
    expect(seen).toEqual(["one", "two", "three", ""]);
  });
});
