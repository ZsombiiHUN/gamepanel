import { useMemo, useState, type SubmitEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Check, Loader2 } from "lucide-react";
import { api, call } from "../api/client";
import { eggsQuery, invalidates, keys, nodesQuery } from "../api/queries";
import { ErrorBox, Field, InfoList, Meter, PageHeader, Section, ServerIcon, Spinner } from "../components/ui";
import { LimitsFields, defaultLimitsForm, formToLimits, type LimitsForm } from "../components/LimitsFields";
import { useTranslation } from "react-i18next";

export function CreateServerPage() {
  const { t } = useTranslation();
  const nodes = useQuery(nodesQuery);
  const eggs = useQuery(eggsQuery);
  const navigate = useNavigate();

  const [name, setName] = useState("");
  const [pickedNode, setNodeId] = useState("");
  const [pickedEgg, setEggId] = useState("");
  // With only one choice there is nothing to pick: use it.
  const only = (list?: { id: number }[]) => (list?.length === 1 ? String(list[0]?.id) : "");
  const nodeId = pickedNode || only(nodes.data);
  const eggId = pickedEgg || only(eggs.data);
  const [limits, setLimits] = useState<LimitsForm>(defaultLimitsForm);
  const [port, setPort] = useState("");
  // Variables start from the egg's defaults; what the admin typed is kept per egg, so picking another egg starts clean.
  const [typed, setTyped] = useState<{ egg: string; values: Record<string, string> }>({ egg: "", values: {} });

  const egg = useMemo(() => eggs.data?.find((e) => String(e.id) === eggId), [eggs.data, eggId]);

  const env = {
    ...Object.fromEntries((egg?.variables ?? []).map((v) => [v.env, v.default])),
    ...(typed.egg === eggId ? typed.values : {}),
  };

  const create = useMutation({
    mutationFn: () =>
      call(
        api.POST("/servers", {
          body: {
            name,
            node_id: Number(nodeId),
            egg_id: Number(eggId),
            ...formToLimits(limits),
            port: port === "" ? 0 : Number(port),
            env,
          },
        }),
      ),
    meta: invalidates(keys.servers),
    onSuccess: (s) => {
      void navigate({ to: "/servers/$uuid", params: { uuid: s.uuid }, search: { tab: "console" } });
    },
  });

  const submit = (e: SubmitEvent) => {
    e.preventDefault();
    create.mutate();
  };

  if (nodes.isPending || eggs.isPending) return <Spinner />;
  const loadErr = nodes.error ?? eggs.error;
  if (loadErr) return <ErrorBox error={loadErr} />;
  const nodeList = nodes.data ?? [];
  const eggList = eggs.data ?? [];

  const node = nodeList.find((n) => String(n.id) === nodeId);
  const mem = Number(limits.memory_mb) || 0;
  const disk = Number(limits.disk_mb) || 0;
  const gb = (mb: number) => (mb >= 1024 ? `${+(mb / 1024).toFixed(1)} GB` : `${mb} MB`);

  return (
    <form
      onSubmit={submit}
      aria-label={t("createServerPage.createServer")}
      className="xl:grid xl:grid-cols-[minmax(0,1fr)_300px] xl:items-start xl:gap-8"
    >
      <div className="min-w-0 max-w-[880px]">
        <PageHeader title={t("createServerPage.newServer")} sub={t("createServerPage.pickAGameAndA")} />
        <div className="space-y-5">
          <fieldset className="min-w-0">
            <legend className="mb-2.5 flex w-full items-baseline justify-between text-[13.5px] font-semibold">
              {t("createServerPage.game")}
            </legend>
            {eggList.length === 0 ? (
              <p className="text-[13px] text-zinc-500">{t("createServerPage.importAnEggFirst")}</p>
            ) : (
              <div className="grid gap-2.5 [grid-template-columns:repeat(auto-fill,minmax(min(100%,190px),1fr))]">
                {eggList.map((x) => (
                  <label
                    key={x.id}
                    className="relative flex cursor-pointer flex-col gap-2.5 rounded-xl border border-line bg-zinc-900 p-3.5 hover:border-line-strong has-[:checked]:border-zinc-400 has-[:checked]:bg-row-active has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-indigo-500"
                  >
                    <input
                      type="radio"
                      name="egg"
                      value={x.id}
                      required
                      checked={eggId === String(x.id)}
                      onChange={(e) => setEggId(e.target.value)}
                      className="peer absolute inset-0 cursor-pointer appearance-none rounded-xl opacity-0"
                      aria-describedby={`egg-${x.id}-d`}
                    />
                    <ServerIcon name={x.name} size={32} />
                    <span className="min-w-0">
                      <span className="block truncate text-[13.5px] font-semibold">{x.name}</span>
                      <span id={`egg-${x.id}-d`} className="block truncate font-mono text-[11.5px] text-zinc-500">
                        {x.docker_image.split("/").pop()}
                      </span>
                    </span>
                    <Check
                      className="absolute right-3 top-3 hidden h-[18px] w-[18px] rounded-full bg-zinc-100 p-[3px] text-zinc-950 peer-checked:block"
                      aria-hidden
                    />
                  </label>
                ))}
              </div>
            )}
            {egg?.description && <p className="mt-2 text-[12.5px] text-zinc-500">{egg.description}</p>}
          </fieldset>

          <Section title={t("createServerPage.details")} desc={t("createServerPage.theNameIsOnlyFor")}>
            <div className="grid gap-4 sm:grid-cols-2">
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
              <Field label={t("common.node")} hint={nodeList.length === 0 ? t("createServerPage.createANodeFirst") : undefined}>
                {(id) => (
                  <select id={id} className="input" required value={nodeId} onChange={(e) => setNodeId(e.target.value)}>
                    <option value="" disabled>
                      {t("createServerPage.selectANode")}
                    </option>
                    {nodeList.map((n) => (
                      <option key={n.id} value={n.id}>
                        {n.name}
                        {n.online ? "" : t("createServerPage.offline")}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
            </div>
          </Section>

          <Section title={t("createServerPage.resources")} desc={t("createServerPage.hardLimitsForThisServer")}>
            <LimitsFields value={limits} onChange={setLimits} />
          </Section>

          <Section title={t("createServerPage.network")}>
            <Field label={t("common.port")} hint={t("createServerPage.leaveEmptyToTakeThe")}>
              {(id) => (
                <input
                  id={id}
                  className="input w-40 font-mono"
                  type="number"
                  min={1}
                  max={65535}
                  placeholder={t("createServerPage.auto")}
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                />
              )}
            </Field>
          </Section>

          {egg && (egg.variables ?? []).length > 0 && (
            <Section
              title={t("createServerPage.environmentVariables")}
              desc={t("createServerPage.defaultsComeFromName", { name: egg.name })}
            >
              <div className="grid gap-4 sm:grid-cols-2">
                {(egg.variables ?? []).map((v) => (
                  <Field key={v.env} label={v.name} hint={v.description || v.env}>
                    {(id) => (
                      <input
                        id={id}
                        className="input font-mono text-[13px]"
                        value={env[v.env] ?? ""}
                        onChange={(e) => setTyped({ egg: eggId, values: { ...env, [v.env]: e.target.value } })}
                      />
                    )}
                  </Field>
                ))}
              </div>
            </Section>
          )}
        </div>
      </div>

      <aside aria-label={t("createServerPage.summary")} className="mt-6 xl:sticky xl:top-[76px] xl:mt-0">
        <div className="rounded-xl border border-line bg-zinc-900 p-4">
          <div className="flex items-center gap-3">
            <ServerIcon name={name || "new server"} size={40} />
            <div className="min-w-0">
              <p className="truncate text-[14.5px] font-semibold">{name || t("createServerPage.newServer")}</p>
              <p className="truncate text-[12.5px] text-zinc-500">{egg?.name ?? t("createServerPage.noGame")}</p>
            </div>
          </div>
          <div className="mt-4 border-t border-line pt-3">
            <InfoList
              rows={[
                { label: t("common.node"), value: node?.name ?? "-", plain: true },
                { label: t("common.port"), value: port || t("createServerPage.nextFree") },
                { label: t("common.memory"), value: mem ? gb(mem) : t("usage.noLimit") },
                { label: t("common.disk"), value: disk ? gb(disk) : t("usage.noLimit") },
                { label: t("createServerPage.backupsKept"), value: limits.backup_limit || "0" },
              ]}
            />
          </div>
          {node && (node.memory_mb > 0 || node.disk_mb > 0) && (
            <div className="mt-4 space-y-3 border-t border-line pt-3">
              <p className="text-[12.5px] text-zinc-500">{t("createServerPage.nodeAfter", { node: node.name })}</p>
              {node.memory_mb > 0 && (
                <Meter
                  label={t("common.memory")}
                  text={`${gb(node.allocated_memory_mb + mem)} / ${gb(node.memory_mb)}`}
                  pct={((node.allocated_memory_mb + mem) / node.memory_mb) * 100}
                />
              )}
              {node.disk_mb > 0 && (
                <Meter
                  label={t("common.disk")}
                  text={`${gb(node.allocated_disk_mb + disk)} / ${gb(node.disk_mb)}`}
                  pct={((node.allocated_disk_mb + disk) / node.disk_mb) * 100}
                />
              )}
            </div>
          )}
        </div>
        <div className="mt-3 space-y-2">
          <ErrorBox error={create.error} />
          <button type="submit" className="btn btn-primary w-full !min-h-10" disabled={create.isPending}>
            {create.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("createServerPage.createServer")}
          </button>
          <button type="button" className="btn btn-ghost w-full" onClick={() => void navigate({ to: "/" })}>
            {t("common.cancel")}
          </button>
        </div>
      </aside>
    </form>
  );
}
