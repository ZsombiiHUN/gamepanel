// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FakeApi, renderApp } from "../test/harness";

const node = (over: Record<string, unknown> = {}) => ({
  id: 1,
  name: "fra-1",
  port_start: 25565,
  port_end: 25665,
  memory_mb: 0,
  disk_mb: 0,
  overallocate_pct: 0,
  allocated_memory_mb: 0,
  allocated_disk_mb: 0,
  sftp_host: "",
  sftp_port: 0,
  location_id: 0,
  online: true,
  unknown_servers: [],
  version: "",
  version_mismatch: false,
  ...over,
});

describe("Node locations", () => {
  it("shows a node's location and moves it to another, made on the spot", async () => {
    let locations = [{ id: 1, short: "eu", long: "Frankfurt", nodes: 1, created_at: 0 }];
    const api = new FakeApi({
      "GET /api/servers": [],
      "GET /api/nodes": [node({ location_id: 1 })],
      "GET /api/locations": () => locations,
      "POST /api/locations": (c) => {
        const l = { id: 2, nodes: 0, created_at: 0, ...(c.body as { short: string; long: string }) };
        locations = [...locations, l];
        return l;
      },
      "PUT /api/nodes/1/location": undefined,
    });
    renderApp(api, "/nodes");
    expect(await screen.findByText("in eu (Frankfurt)")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Settings of fra-1" }));
    const select = screen.getByRole("combobox", { name: "Location" });
    expect(select).toHaveValue("1");
    await userEvent.type(screen.getByRole("textbox", { name: "Short code, e.g. eu-west" }), "us-east");
    await userEvent.type(screen.getByRole("textbox", { name: "Description, e.g. Frankfurt" }), "Virginia");
    await userEvent.click(screen.getByRole("button", { name: "Add location" }));
    await waitFor(() => expect(select).toHaveValue("2"));
    expect(api.callsTo("POST /api/locations")[0]?.body).toEqual({ short: "us-east", long: "Virginia" });

    await userEvent.click(screen.getByRole("button", { name: "Save location" }));
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(api.callsTo("PUT /api/nodes/1/location")[0]?.body).toEqual({ location_id: 2 });
  });

  it("can take a node out of any location", async () => {
    const api = new FakeApi({
      "GET /api/servers": [],
      "GET /api/nodes": [node({ location_id: 1 })],
      "GET /api/locations": [{ id: 1, short: "eu", long: "", nodes: 1, created_at: 0 }],
      "PUT /api/nodes/1/location": undefined,
    });
    renderApp(api, "/nodes");
    await userEvent.click(await screen.findByRole("button", { name: "Settings of fra-1" }));
    await userEvent.selectOptions(screen.getByRole("combobox", { name: "Location" }), "No location");
    await userEvent.click(screen.getByRole("button", { name: "Save location" }));
    await waitFor(() => expect(api.callsTo("PUT /api/nodes/1/location")[0]?.body).toEqual({ location_id: 0 }));
  });
});

describe("Node versions", () => {
  it("shows each node's daemon version and warns when it differs from the panel's", async () => {
    const api = new FakeApi({
      "GET /api/servers": [],
      "GET /api/nodes": [
        node({ version: "0.3.0" }),
        node({ id: 2, name: "old-1", version: "0.2.1", version_mismatch: true }),
        node({ id: 3, name: "off-1", online: false }),
      ],
      "GET /api/locations": [],
    });
    renderApp(api, "/nodes");
    expect(await screen.findByText("Daemon 0.3.0")).toBeInTheDocument();
    expect(screen.getByText("Daemon 0.2.1")).toBeInTheDocument();
    const warnings = screen.getAllByText(/runs a different version/);
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toHaveTextContent("(0.2.1)");
  });
});
