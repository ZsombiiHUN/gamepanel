import { describe, expect, it } from "vitest";
import { describeUsage, gb, type Limits } from "./usage";

const MB = 1024 ** 2;
const GB = 1024 ** 3;
const limits: Limits = { memoryMb: 4096, diskMb: 10240, cpuPct: 200 };
const reading = (cpu: number, memGb: number, diskGb: number) => ({ cpu_pct: cpu, mem_bytes: memGb * GB, disk_bytes: diskGb * GB });

describe("gb", () => {
  it("shows one decimal below 10 and whole numbers from 10", () => {
    expect([gb(0.5), gb(9.94), gb(10), gb(12.6)]).toEqual(["0.5", "9.9", "10", "13"]);
  });
});

describe("describeUsage", () => {
  it("shows dashes and 'Not running' when nothing is reported", () => {
    const [cpu, mem, disk] = describeUsage(null, null, limits, []);
    expect([cpu.value, mem.value, disk.value]).toEqual(["-", "-", "-"]);
    expect(mem.note).toBe("Not running");
  });

  it("reports CPU against the limit in cores", () => {
    const [cpu] = describeUsage(reading(150, 1, 1), reading(150, 1, 1), limits, [150]);
    expect(cpu).toMatchObject({ value: "150%", unit: "of 200% limit", note: "About 1.5 of 2 cores in use" });
  });

  it("describes an unlimited CPU as a share of one core", () => {
    const [cpu] = describeUsage(reading(40, 1, 1), null, { ...limits, cpuPct: 0 }, []);
    expect(cpu).toMatchObject({ unit: "no limit", note: "Share of one core" });
  });

  it("grows the CPU axis with the readings in steps of 20, starting at 20", () => {
    expect(describeUsage(null, null, limits, [])[0].max).toBe(20);
    expect(describeUsage(null, null, limits, [10])[0].max).toBe(40);
    expect(describeUsage(null, null, limits, [95])[0].max).toBe(120);
    expect(describeUsage(null, null, limits, [95])[0].ticks.map((t) => t.label)).toEqual(["0", "60%", "120%"]);
  });

  it("warns on memory at 85% of the limit, and not before", () => {
    const calm = describeUsage(reading(1, 3.3, 1), null, limits, [])[1];
    const hot = describeUsage(reading(1, 3.5, 1), null, limits, [])[1];
    expect([calm.noteWarn, hot.noteWarn]).toEqual([false, true]);
    expect(hot).toMatchObject({ value: "3.5", unit: "GB of 4.0 GB", note: "88% used" });
    expect(hot.color).not.toBe(calm.color);
  });

  it("measures a small server against its own memory limit, in MB", () => {
    const small = { ...limits, memoryMb: 128 };
    const calm = describeUsage({ cpu_pct: 0, mem_bytes: 100 * MB, disk_bytes: 0 }, null, small, [])[1];
    const hot = describeUsage({ cpu_pct: 0, mem_bytes: 110 * MB, disk_bytes: 0 }, null, small, [])[1];
    expect(hot).toMatchObject({ value: "110", unit: "MB of 128 MB", note: "86% used", max: 128, limit: 128, per: MB, noteWarn: true });
    expect(hot.ticks.map((t) => t.label)).toEqual(["0", "64 MB", "128 MB"]);
    expect(calm.noteWarn).toBe(false);
  });

  it("switches to GB at one gigabyte", () => {
    expect(describeUsage(null, null, { ...limits, memoryMb: 1023 }, [])[1].unit).toBe("MB of 1023 MB");
    expect(describeUsage(null, null, { ...limits, memoryMb: 1024 }, [])[1]).toMatchObject({ unit: "GB of 1.0 GB", per: GB });
  });

  it("shows a small disk quota in MB", () => {
    const disk = describeUsage(null, { cpu_pct: 0, mem_bytes: 0, disk_bytes: 30 * MB }, { ...limits, diskMb: 100 }, [])[2];
    expect(disk).toMatchObject({ value: "30", unit: "MB of 100 MB", note: "30% used", max: 100, limit: 100 });
    expect(disk.ticks.map((t) => t.label)).toEqual(["0", "50 MB", "100 MB"]);
  });

  it("warns that storage is almost full and says the server stops", () => {
    const disk = describeUsage(null, reading(0, 0, 9), limits, [])[2];
    expect(disk.noteWarn).toBe(true);
    expect(disk.note).toBe("Almost full. The server stops at its limit.");
  });

  it("scales the storage axis to the data when there is no quota", () => {
    const disk = describeUsage(null, reading(0, 0, 4), { ...limits, diskMb: 0 }, [])[2];
    expect(disk).toMatchObject({ unit: "GB, no limit", note: "No quota set", max: 5, limit: undefined, noteWarn: false });
  });

  it("draws the limit line only when there is a limit", () => {
    const [, mem, disk] = describeUsage(null, reading(0, 0, 1), limits, []);
    expect([mem.limit, disk.limit]).toEqual([4, 10]);
  });
});
