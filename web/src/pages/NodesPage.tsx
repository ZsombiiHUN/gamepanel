import { useState, type SubmitEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { HardDrive, Loader2, Plus, Settings2, Trash2, X } from "lucide-react";
import { api, call, type components } from "../api/client";
import { invalidates, keys, nodesQuery, serversQuery } from "../api/queries";
import { ConfirmDialog, CopyButton, Empty, ErrorBox, Field, Meter, PageHeader, Pill, Section, ServerIcon, Spinner } from "../components/ui";
import { useTranslation } from "react-i18next";

type NodeView = components["schemas"]["NodeView"];

export function NodesPage() {
  const { t } = useTranslation();
  const nodes = useQuery(nodesQuery);
  const servers = useQuery(serversQuery);
  const byNode = new Map<number, { uuid: string; name: string }[]>();
  for (const sv of servers.data ?? []) byNode.set(sv.node_id, [...(byNode.get(sv.node_id) ?? []), sv]);
  const [name, setName] = useState("");
  const [start, setStart] = useState(25565);
  const [end, setEnd] = useState(25665);
  const [capMem, setCapMem] = useState("0");
  const [capDisk, setCapDisk] = useState("0");
  const [capOver, setCapOver] = useState("0");
  const [editing, setEditing] = useState<{ id: number; mem: string; disk: string; over: string; host: string; port: string } | null>(null);
  const [adding, setAdding] = useState(false);
  const [created, setCreated] = useState<{ name: string; token: string } | null>(null);
  const [toDelete, setToDelete] = useState<NodeView | null>(null);

  const create = useMutation({
    mutationFn: () =>
      call(
        api.POST("/nodes", {
          body: {
            name,
            port_start: start,
            port_end: end,
            memory_mb: Number(capMem) || 0,
            disk_mb: Number(capDisk) || 0,
            overallocate_pct: Number(capOver) || 0,
          },
        }),
      ),
    meta: invalidates(keys.nodes),
    onSuccess: (r) => {
      setCreated({ name: r.node.name, token: r.token });
      setName("");
      setAdding(false);
    },
  });
  const saveCap = useMutation({
    mutationFn: (v: { id: number; mem: string; disk: string; over: string }) =>
      call(
        api.PUT("/nodes/{id}/capacity", {
          params: { path: { id: v.id } },
          body: { memory_mb: Number(v.mem) || 0, disk_mb: Number(v.disk) || 0, overallocate_pct: Number(v.over) || 0 },
        }),
      ),
    meta: invalidates(keys.nodes),
    onSuccess: () => {
      setEditing(null);
    },
  });
  const saveSftp = useMutation({
    mutationFn: (v: { id: number; host: string; port: string }) =>
      call(api.PUT("/nodes/{id}/sftp", { params: { path: { id: v.id } }, body: { host: v.host.trim(), port: Number(v.port) || 0 } })),
    meta: invalidates(keys.nodes),
  });
  const removeUnknown = useMutation({
    mutationFn: (v: { id: number; uuid: string }) => call(api.DELETE("/nodes/{id}/unknown/{uuid}", { params: { path: v } })),
    meta: invalidates(keys.nodes),
  });
  const del = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/nodes/{id}", { params: { path: { id } } })),
    meta: invalidates(keys.nodes),
    onSuccess: () => {
      setToDelete(null);
    },
  });

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    setCreated(null);
    create.mutate();
  };
  const envLine = created ? `GAMEPANEL_NODE_TOKEN=${created.token}` : "";

  const pct = (used: number, cap: number) => (cap > 0 ? (used / cap) * 100 : null);
  const fmt = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(mb % 1024 ? 1 : 0)} GB` : `${mb} MB`);

  return (
    <div>
      <PageHeader
        title={t("nodesPage.nodes")}
        sub={t("nodesPage.machinesThatRunYourGame")}
        actions={
          <button type="button" className="btn btn-primary" aria-expanded={adding} onClick={() => setAdding(!adding)}>
            {adding ? <X className="h-4 w-4" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />}{" "}
            {adding ? t("common.cancel") : t("nodesPage.addNode")}
          </button>
        }
      />

      <div className="space-y-5">
        {created && (
          <section role="alert" aria-labelledby="token-h" className="rounded-xl border border-warn/30 bg-warn-bg p-5">
            <h2 id="token-h" className="text-[15px] font-semibold text-warn-text">
              {t("nodesPage.created", { name: created.name })}
            </h2>
            <p className="mt-1 text-sm text-warn-text">{t("nodesPage.itIsShownOnlyOnce")}</p>
            <pre
              tabIndex={0}
              role="region"
              aria-label={t("nodesPage.daemonEnvironment")}
              className="mt-3 overflow-x-auto rounded-lg border border-line bg-well p-3 font-mono text-[13px] text-good-text select-all"
            >
              <code>{envLine}</code>
            </pre>
            <div className="mt-3 flex flex-wrap gap-2">
              <CopyButton text={envLine} label={t("nodesPage.copyEnvLine")} />
              <CopyButton text={created.token} label={t("nodesPage.copyTokenOnly")} />
              <button type="button" className="btn btn-ghost" onClick={() => setCreated(null)}>
                {t("nodesPage.dismiss")}
              </button>
            </div>
          </section>
        )}

        {adding && (
          <form onSubmit={submit}>
            <Section
              title={t("nodesPage.addANode")}
              desc={t("nodesPage.creatingANodeGivesYou")}
              footer={
                <>
                  <span>{t("nodesPage.serversOnThisNodeGet")}</span>
                  <button type="submit" className="btn btn-primary" disabled={create.isPending}>
                    {create.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <Plus className="h-4 w-4" aria-hidden />}{" "}
                    {t("nodesPage.createNode")}
                  </button>
                </>
              }
            >
              <div className="grid gap-4 sm:grid-cols-3">
                <div className="sm:col-span-3 sm:max-w-md">
                  <Field label={t("common.name")}>
                    {(id) => (
                      <input
                        id={id}
                        className="input"
                        required
                        autoFocus
                        maxLength={64}
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                      />
                    )}
                  </Field>
                </div>
                <Field label={t("nodesPage.portRangeStart")}>
                  {(id) => (
                    <input
                      id={id}
                      className="input"
                      type="number"
                      min={1024}
                      max={65535}
                      required
                      value={start}
                      onChange={(e) => setStart(Number(e.target.value))}
                    />
                  )}
                </Field>
                <Field label={t("nodesPage.portRangeEnd")}>
                  {(id) => (
                    <input
                      id={id}
                      className="input"
                      type="number"
                      min={1024}
                      max={65535}
                      required
                      value={end}
                      onChange={(e) => setEnd(Number(e.target.value))}
                    />
                  )}
                </Field>
                <span className="hidden sm:block" />
                <Field label={t("nodesPage.memoryCapacityMb")} hint={t("nodesPage.0NotTracked")}>
                  {(id) => (
                    <input id={id} className="input" type="number" min={0} value={capMem} onChange={(e) => setCapMem(e.target.value)} />
                  )}
                </Field>
                <Field label={t("nodesPage.diskCapacityMb")} hint={t("nodesPage.0NotTracked")}>
                  {(id) => (
                    <input id={id} className="input" type="number" min={0} value={capDisk} onChange={(e) => setCapDisk(e.target.value)} />
                  )}
                </Field>
                <Field label={t("nodesPage.overAllocate")} hint={t("nodesPage.limitsMayAddUpTo")}>
                  {(id) => (
                    <input
                      id={id}
                      className="input"
                      type="number"
                      min={0}
                      max={1000}
                      value={capOver}
                      onChange={(e) => setCapOver(e.target.value)}
                    />
                  )}
                </Field>
              </div>
              <div className="mt-3">
                <ErrorBox error={create.error} />
              </div>
            </Section>
          </form>
        )}

        {nodes.isPending && <Spinner />}
        <ErrorBox error={nodes.error} />
        {nodes.data?.length === 0 && !adding && (
          <Empty icon={<HardDrive className="h-5 w-5" aria-hidden />} title={t("nodesPage.noNodesYet")}>
            {t("nodesPage.addANodeThenStart")}
          </Empty>
        )}

        <ul className="grid gap-3 lg:grid-cols-2">
          {nodes.data?.map((n) => {
            const open = editing?.id === n.id;
            return (
              <li key={n.id} className={`flex flex-col rounded-xl border bg-zinc-900 p-4 ${n.online ? "border-line" : "border-bad/30"}`}>
                <div className="flex items-start gap-3">
                  <div className="grid h-9 w-9 shrink-0 place-content-center rounded-lg border border-line bg-thead text-zinc-400">
                    <HardDrive className="h-4 w-4" aria-hidden />
                  </div>
                  <div className="min-w-0 flex-1">
                    <h2 className="flex items-center gap-2.5 truncate text-[14.5px] font-semibold text-zinc-100">
                      <span className="truncate">
                        {n.name} <span className="font-normal text-zinc-500">#{n.id}</span>
                      </span>
                      <Pill tone={n.online ? "good" : "bad"}>
                        <span className={`h-1.5 w-1.5 rounded-full ${n.online ? "bg-good" : "bg-bad"}`} aria-hidden />
                        {n.online ? t("common.online") : t("common.offline")}
                        <span className="sr-only"> {t("nodesPage.daemon")}</span>
                      </Pill>
                    </h2>
                    <div className="mt-0.5 flex flex-wrap items-center gap-x-2 font-mono text-[12px] text-zinc-500">
                      <span className="font-mono">{t("nodesPage.ports", { from: n.port_start, to: n.port_end })}</span>
                      <span aria-hidden>·</span>
                      <span title={t("nodesPage.whereSftpClientsConnect")}>
                        {t("nodesPage.sftp")}{" "}
                        <span className="font-mono">
                          {n.sftp_host || t("nodesPage.panelHost")}
                          {n.sftp_port ? t("nodesPage.sftpPort", { sftp_port: n.sftp_port }) : ""}
                        </span>
                      </span>
                    </div>
                  </div>
                  <button
                    type="button"
                    className="btn btn-ghost !min-h-8 !px-2"
                    aria-expanded={open}
                    aria-label={t("nodesPage.settingsOfName", { name: n.name })}
                    title={t("nodesPage.capacityAndSftpAddress")}
                    onClick={() => {
                      saveCap.reset();
                      saveSftp.reset();
                      setEditing(
                        open
                          ? null
                          : {
                              id: n.id,
                              mem: String(n.memory_mb),
                              disk: String(n.disk_mb),
                              over: String(n.overallocate_pct),
                              host: n.sftp_host,
                              port: n.sftp_port ? String(n.sftp_port) : "",
                            },
                      );
                    }}
                  >
                    <Settings2 className="h-4 w-4" aria-hidden />
                  </button>
                  <button
                    type="button"
                    className="btn btn-ghost !min-h-8 !px-2 hover:[&_svg]:text-bad-text"
                    onClick={() => setToDelete(n)}
                    aria-label={t("nodesPage.deleteNodeName", { name: n.name })}
                  >
                    <Trash2 className="h-4 w-4" aria-hidden />
                  </button>
                </div>
                <div className="mt-4 space-y-3">
                  <Meter
                    label={t("nodesPage.memoryAllocated")}
                    text={
                      n.memory_mb
                        ? `${fmt(n.allocated_memory_mb)} / ${fmt(n.memory_mb)}`
                        : t("nodesPage.noCap", { used: fmt(n.allocated_memory_mb) })
                    }
                    pct={pct(n.allocated_memory_mb, n.memory_mb)}
                  />
                  <Meter
                    label={t("nodesPage.diskAllocated")}
                    text={
                      n.disk_mb
                        ? `${fmt(n.allocated_disk_mb)} / ${fmt(n.disk_mb)}`
                        : t("nodesPage.noCap", { used: fmt(n.allocated_disk_mb) })
                    }
                    pct={pct(n.allocated_disk_mb, n.disk_mb)}
                  />
                </div>
                <div className="mt-4 flex items-center gap-2 border-t border-line pt-3 text-[12.5px] text-zinc-500">
                  {(byNode.get(n.id) ?? []).slice(0, 6).map((sv) => (
                    <span key={sv.uuid} title={sv.name}>
                      <ServerIcon name={sv.name} size={20} />
                    </span>
                  ))}
                  <span className={(byNode.get(n.id)?.length ?? 0) > 0 ? "ml-1" : undefined}>
                    {(() => {
                      const c = byNode.get(n.id)?.length ?? 0;
                      return c === 0 ? t("nodesPage.noServers") : t("appShell.servers", { count: c });
                    })()}
                  </span>
                </div>
                {n.unknown_servers.length > 0 && (
                  <div role="status" className="mt-5 rounded-xl border border-warn/30 bg-warn-bg p-4 text-sm text-warn-text">
                    <p className="font-medium">{t("nodesPage.unknownServers", { count: n.unknown_servers.length })}</p>
                    <p className="mt-1 text-warn-text">{t("nodesPage.leftBehindByADelete")}</p>
                    <ul className="mt-3 space-y-2">
                      {n.unknown_servers.map((uuid) => (
                        <li key={uuid} className="flex items-center justify-between gap-3">
                          <span className="truncate font-mono text-xs">{uuid}</span>
                          <button
                            type="button"
                            className="btn btn-danger !py-1 text-xs"
                            disabled={removeUnknown.isPending || !n.online}
                            title={n.online ? t("nodesPage.deleteItsContainerAndFiles") : t("nodesPage.theNodeIsOffline")}
                            onClick={() =>
                              window.confirm(t("nodesPage.confirmRemoveUnknown", { uuid, node: n.name })) &&
                              removeUnknown.mutate({ id: n.id, uuid })
                            }
                          >
                            {t("common.remove")}
                          </button>
                        </li>
                      ))}
                    </ul>
                    {removeUnknown.error && (
                      <div className="mt-3">
                        <ErrorBox error={removeUnknown.error} />
                      </div>
                    )}
                  </div>
                )}
                {open && (
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      saveCap.mutate(editing);
                    }}
                    className="mt-5 space-y-3 border-t border-line pt-4"
                  >
                    <div className="grid gap-3 sm:grid-cols-3">
                      <Field label={t("nodesPage.memoryMb")}>
                        {(id) => (
                          <input
                            id={id}
                            className="input"
                            type="number"
                            min={0}
                            value={editing.mem}
                            onChange={(e) => setEditing({ ...editing, mem: e.target.value })}
                          />
                        )}
                      </Field>
                      <Field label={t("nodesPage.diskMb")}>
                        {(id) => (
                          <input
                            id={id}
                            className="input"
                            type="number"
                            min={0}
                            value={editing.disk}
                            onChange={(e) => setEditing({ ...editing, disk: e.target.value })}
                          />
                        )}
                      </Field>
                      <Field label={t("nodesPage.overAllocate")}>
                        {(id) => (
                          <input
                            id={id}
                            className="input"
                            type="number"
                            min={0}
                            max={1000}
                            value={editing.over}
                            onChange={(e) => setEditing({ ...editing, over: e.target.value })}
                          />
                        )}
                      </Field>
                    </div>
                    <p className="text-xs text-zinc-500">{t("nodesPage.existingServersAreNotChanged")}</p>
                    <ErrorBox error={saveCap.error} />
                    <button type="submit" className="btn btn-primary" disabled={saveCap.isPending}>
                      {t("nodesPage.saveCapacity")}
                    </button>
                  </form>
                )}
                {open && (
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      saveSftp.mutate(editing);
                    }}
                    className="mt-5 space-y-3 border-t border-line pt-4"
                  >
                    <div className="grid gap-3 sm:grid-cols-3">
                      <div className="sm:col-span-2">
                        <Field label={t("nodesPage.sftpHost")} hint={t("nodesPage.hostNameOrIpAddress")}>
                          {(id) => (
                            <input
                              id={id}
                              className="input font-mono"
                              placeholder={t("nodesPage.node1ExampleCom")}
                              maxLength={253}
                              value={editing.host}
                              onChange={(e) => setEditing({ ...editing, host: e.target.value })}
                            />
                          )}
                        </Field>
                      </div>
                      <Field label={t("nodesPage.sftpPort2")} hint={t("nodesPage.emptyWhatTheDaemonListens")}>
                        {(id) => (
                          <input
                            id={id}
                            className="input"
                            type="number"
                            min={0}
                            max={65535}
                            placeholder="2022"
                            value={editing.port}
                            onChange={(e) => setEditing({ ...editing, port: e.target.value })}
                          />
                        )}
                      </Field>
                    </div>
                    <ErrorBox error={saveSftp.error} />
                    <div className="flex items-center gap-3">
                      <button type="submit" className="btn btn-primary" disabled={saveSftp.isPending}>
                        {t("nodesPage.saveSftpAddress")}
                      </button>
                      {saveSftp.isSuccess && (
                        <span role="status" className="text-sm text-good-text">
                          {t("nodesPage.saved")}
                        </span>
                      )}
                    </div>
                  </form>
                )}
              </li>
            );
          })}
        </ul>
      </div>

      <ConfirmDialog
        open={!!toDelete}
        title={t("nodesPage.confirmDelete", { name: toDelete?.name ?? "" })}
        body={t("nodesPage.theNodeSTokenStops")}
        confirmLabel={t("nodesPage.deleteNode")}
        pending={del.isPending}
        error={del.error}
        onConfirm={() => toDelete && del.mutate(toDelete.id)}
        onCancel={() => {
          setToDelete(null);
          del.reset();
        }}
      />
    </div>
  );
}
