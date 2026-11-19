// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BackupsTab } from "./BackupsTab";
import { FakeApi, renderWithClient } from "../test/harness";
import { server } from "../test/fixtures";
import type { components } from "../api/client";

type Backup = components["schemas"]["Backup"];
const srv = server({ name: "Survival SMP" });

function backup(over: Partial<Backup> & { name: string }): Backup {
  return {
    uuid: `b-${over.name}`,
    server_uuid: srv.uuid,
    status: "ok",
    size: 2048,
    sha256: "a".repeat(64),
    ignore: "",
    locked: false,
    created_at: 1_700_000_000,
    completed_at: 1_700_000_100,
    restore_status: "",
    restored_at: 0,
    ...over,
  } as Backup;
}

function setup(list: Backup[]) {
  const api = new FakeApi({
    [`GET /api/servers/${srv.uuid}`]: srv,
    [`GET /api/servers/${srv.uuid}/backups`]: list,
  }).install();
  renderWithClient(<BackupsTab uuid={srv.uuid} />);
  return api;
}

describe("BackupsTab restores", () => {
  it("starts a restore and shows it running in the list, without claiming it is done", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const list = [backup({ name: "nightly" })];
    const api = setup(list);
    api.on({
      [`POST /api/servers/${srv.uuid}/backups/b-nightly/restore`]: () => {
        list[0] = backup({ name: "nightly", restore_status: "running" });
        return new Response(null, { status: 202 });
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: "Restore nightly" }));
    expect(await screen.findByText("Restoring")).toBeInTheDocument();
    expect(screen.queryByText(/restored\./i)).not.toBeInTheDocument();
    // One restore at a time.
    expect(screen.getByRole("button", { name: "Restore nightly" })).toBeDisabled();
    expect(api.callsTo(`POST /api/servers/${srv.uuid}/backups/b-nightly/restore`).map((c) => c.body)).toEqual([{ truncate: false }]);
  });

  it("shows why a restore failed", async () => {
    setup([backup({ name: "big", restore_status: "failed", restore_error: "restoring this backup would exceed the server's disk limit" })]);
    expect(await screen.findByText(/Restore failed: restoring this backup would exceed/)).toBeInTheDocument();
  });

  it("shows when a restore finished", async () => {
    setup([backup({ name: "ok", restore_status: "ok", restored_at: 1_700_000_500 })]);
    expect(await screen.findByText(/^Restored /)).toBeInTheDocument();
  });
});

describe("BackupsTab downloads", () => {
  it("links finished backups for people who may download them, and marks S3 ones", async () => {
    srv.permissions = [...srv.permissions, "backups.download"];
    setup([backup({ name: "nightly", disk: "s3" }), backup({ name: "running", status: "running" })]);
    expect(await screen.findByRole("link", { name: "Download nightly" })).toHaveAttribute(
      "href",
      `/api/servers/${srv.uuid}/backups/b-nightly/download`,
    );
    expect(screen.queryByRole("link", { name: "Download running" })).toBeNull();
    expect(screen.getByText("S3")).toBeInTheDocument();
    srv.permissions = srv.permissions.filter((p) => p !== "backups.download");
  });
  it("offers no download without the permission", async () => {
    setup([backup({ name: "nightly" })]);
    await screen.findByText("nightly");
    expect(screen.queryByRole("link", { name: "Download nightly" })).toBeNull();
  });
});
