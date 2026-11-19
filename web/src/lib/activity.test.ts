import { describe, expect, it } from "vitest";
import { activityKind, dayLabel, describeAction, groupByDay, relativeTime } from "./activity";

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

describe("activity grouping", () => {
  it("sorts actions into kinds", () => {
    expect(activityKind("server.power")).toBe("power");
    expect(activityKind("server.command")).toBe("console");
    expect(activityKind("file.write")).toBe("files");
    expect(activityKind("subuser.add")).toBe("access");
    expect(activityKind("server.rename")).toBe("server");
  });
  it("groups entries by day", () => {
    const now = new Date(2026, 0, 10, 12).getTime();
    const at = (h: number) => now / 1000 - h * 3600;
    const g = groupByDay([{ at: at(1) }, { at: at(2) }, { at: at(20) }, { at: at(60) }], now);
    expect(g.map((x) => [x.label, x.rows.length])).toEqual([
      ["Today", 2],
      ["Yesterday", 1],
      [dayLabel(at(60), now), 1],
    ]);
    expect(dayLabel(at(60), now)).not.toMatch(/Today|Yesterday/);
  });
});
