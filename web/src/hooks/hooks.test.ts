// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useDebouncedValue } from "./useDebouncedValue";
import { useRollingSamples } from "./useRollingSamples";
import { useSearchText } from "./useSearchText";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useDebouncedValue", () => {
  it("follows the value only after it stops changing", () => {
    const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 200), { initialProps: { v: "a" } });
    rerender({ v: "ab" });
    act(() => void vi.advanceTimersByTime(150));
    rerender({ v: "abc" });
    act(() => void vi.advanceTimersByTime(150));
    expect(result.current).toBe("a");
    act(() => void vi.advanceTimersByTime(60));
    expect(result.current).toBe("abc");
  });
});

describe("useSearchText", () => {
  it("does not touch the address on mount", () => {
    const toUrl = vi.fn();
    renderHook(() => useSearchText("lobby", toUrl, 100));
    act(() => void vi.advanceTimersByTime(500));
    expect(toUrl).not.toHaveBeenCalled();
  });
  it("seeds the box from the address and keeps every keystroke locally", () => {
    const { result } = renderHook(() => useSearchText("lobby", vi.fn(), 100));
    expect(result.current[0]).toBe("lobby");
    act(() => result.current[1]("lobby 2"));
    expect(result.current[0]).toBe("lobby 2");
  });
  it("writes the trimmed text once typing pauses, only once", () => {
    const toUrl = vi.fn();
    const { result } = renderHook(() => useSearchText(undefined, toUrl, 100));
    act(() => result.current[1]("lo"));
    act(() => void vi.advanceTimersByTime(60));
    act(() => result.current[1]("lobby "));
    act(() => void vi.advanceTimersByTime(99));
    expect(toUrl).not.toHaveBeenCalled();
    act(() => void vi.advanceTimersByTime(2));
    expect(toUrl).toHaveBeenCalledExactlyOnceWith("lobby");
  });
  it("writes an empty string when the box is cleared", () => {
    const toUrl = vi.fn();
    const { result } = renderHook(() => useSearchText("lobby", toUrl, 100));
    act(() => result.current[1](""));
    act(() => void vi.advanceTimersByTime(100));
    expect(toUrl).toHaveBeenCalledExactlyOnceWith("");
  });
});

describe("useRollingSamples", () => {
  const pick = (s: { n: number }) => s.n;
  it("collects one value per new sample, ignoring a repeat of the same one", () => {
    const a = { n: 1 },
      b = { n: 2 };
    const { result, rerender } = renderHook(({ s }) => useRollingSamples(s, pick, 5), { initialProps: { s: a as { n: number } | null } });
    rerender({ s: a });
    rerender({ s: b });
    rerender({ s: b });
    expect(result.current).toEqual([1, 2]);
  });
  it("keeps only the newest `size` values", () => {
    const { result, rerender } = renderHook(({ s }) => useRollingSamples(s, pick, 3), {
      initialProps: { s: { n: 0 } as { n: number } | null },
    });
    for (let n = 1; n <= 5; n++) rerender({ s: { n } });
    expect(result.current).toEqual([3, 4, 5]);
  });
  it("skips gaps where nothing is reported", () => {
    const { result, rerender } = renderHook(({ s }) => useRollingSamples(s, pick, 5), {
      initialProps: { s: { n: 1 } as { n: number } | null },
    });
    rerender({ s: null });
    rerender({ s: { n: 2 } });
    expect(result.current).toEqual([1, 2]);
  });
});
