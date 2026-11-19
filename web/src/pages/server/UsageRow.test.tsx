// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, renderWithClient } from "../../test/harness";
import { UsageRow } from "./UsageRow";

const UUID = "11111111-2222-4333-8444-555555555555";
const limits = { memoryMb: 1024, diskMb: 2048, cpuPct: 200 };

describe("usage history", () => {
  it("asks for the chosen range and shows peaks", async () => {
    const api = new FakeApi({
      [`GET /api/servers/${UUID}/metrics`]: {
        step: 600,
        points: [
          { at: 1, cpu_avg: 20, cpu_max: 80, mem_avg: 100 << 20, mem_max: 300 << 20, disk: 1 << 30, running: true },
          { at: 2, cpu_avg: 0, cpu_max: 0, mem_avg: 0, mem_max: 0, disk: 1 << 30, running: false },
        ],
      },
    }).install();
    renderWithClient(<UsageRow uuid={UUID} running={null} latest={null} limits={limits} />);
    expect(screen.getByText(/Live readings/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "24 h" }));
    await waitFor(() => expect(api.callsTo(`GET /api/servers/${UUID}/metrics`)[0]?.query.get("range")).toBe("24h"));
    expect(await screen.findByText(/Peaks: CPU 80%/)).toBeInTheDocument();
    expect(screen.getByText(/Averages over 10 min/)).toBeInTheDocument();
    expect(screen.getAllByText("24 h ago")).toHaveLength(3);
  });
});
