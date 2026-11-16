import { describe, expect, it } from "vitest";
import { describeAction, relativeTime } from "./activity";

describe("activity", () => {
  it("describes actions in plain words", () => {
    expect(describeAction("file.upload")).toBe("uploaded a file");
    expect(describeAction("server.power", "restart")).toBe("restarted the server");
    expect(describeAction("something.new")).toBe("something.new");
  });
  it("says how long ago", () => {
    const now = Date.UTC(2026, 0, 10, 12);
    const at = (sec: number) => now / 1000 - sec;
    expect(relativeTime(at(10), now)).toBe("just now");
    expect(relativeTime(at(60), now)).toBe("1 minute ago");
    expect(relativeTime(at(3 * 3600), now)).toBe("3 hours ago");
    expect(relativeTime(at(26 * 3600), now)).toBe("yesterday");
  });
});
