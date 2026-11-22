import { useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Box, ImageUp, Loader2 } from "lucide-react";
import { api, call } from "../../api/client";
import { dirQuery, invalidates, keys, minecraftStatusQuery } from "../../api/queries";
import { ErrorBox, FormDialog, InfoList, Section, Spinner } from "../../components/ui";
import { parseProperties, setProperties, worldFolders } from "../../lib/minecraft";
import { sendCommand, useServerFile, useWriteFile, type Can } from "./shared";
import { Trans, useTranslation } from "react-i18next";
import i18n from "../../i18n";

export function WorldSection({ uuid, running, stopped, can }: { uuid: string; running: boolean; stopped: boolean; can: Can }) {
  const { t } = useTranslation();
  const file = useServerFile(uuid, "/server.properties");
  const root = useQuery(dirQuery(uuid, "/"));
  const write = useWriteFile(uuid);
  const props = parseProperties(file.data ?? "");
  const level = props.get("level-name") || "world";
  const present = new Set((root.data ?? []).filter((f) => f.dir).map((f) => f.name));
  const folders = worldFolders(level).filter((f) => present.has(f));
  const [reset, setReset] = useState(false);
  const [seed, setSeed] = useState("");
  const [archived, setArchived] = useState<string | null>(null);

  const doReset = useMutation({
    mutationFn: async () => {
      if (folders.length)
        await call(api.POST("/servers/{uuid}/files/delete", { params: { path: { uuid } }, body: { paths: folders.map((f) => "/" + f) } }));
      if (file.data != null && seed.trim() !== (props.get("level-seed") ?? ""))
        await write.mutateAsync({ path: "/server.properties", content: setProperties(file.data, { "level-seed": seed.trim() }) });
    },
    meta: invalidates(keys.files(uuid)),
    onSuccess: () => setReset(false),
  });
  const archive = useMutation({
    mutationFn: async () => {
      if (running && can("console.send")) {
        // Write everything in memory to disk first, so the archive is complete.
        await sendCommand(uuid, "save-all flush");
        await new Promise((r) => setTimeout(r, 2500));
      }
      return call(
        api.POST("/servers/{uuid}/files/compress", {
          params: { path: { uuid } },
          body: { root: "/", paths: folders.map((f) => "/" + f), format: "zip" },
        }),
      );
    },
    meta: invalidates(keys.files(uuid)),
    onSuccess: (r) => setArchived(r.path),
  });

  return (
    <Section
      title={t("worldSection.world")}
      desc={t("worldSection.theWorldTheServerLoads")}
      footer={
        <>
          <span>{stopped ? t("worldSection.resettingDeletesTheWorldFor") : t("worldSection.stopTheServerToReset")}</span>
          <div className="flex gap-2">
            <button type="button" className="btn" disabled={folders.length === 0 || archive.isPending} onClick={() => archive.mutate()}>
              {archive.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} {t("worldSection.archiveAsZip")}
            </button>
            <button
              type="button"
              className="btn btn-danger"
              disabled={!stopped || folders.length === 0}
              onClick={() => {
                setSeed(props.get("level-seed") ?? "");
                setReset(true);
              }}
            >
              {t("worldSection.resetWorld")}
            </button>
          </div>
        </>
      }
    >
      {file.isPending || root.isPending ? (
        <Spinner />
      ) : (
        <InfoList
          rows={[
            { label: t("common.name"), value: level },
            { label: t("worldSection.seed"), value: props.get("level-seed") || t("worldSection.random") },
            { label: t("common.type"), value: props.get("level-type") || "minecraft:normal" },
            { label: t("worldSection.folders"), value: folders.length ? folders.join(", ") : t("worldSection.notCreated") },
          ]}
        />
      )}
      {archived && (
        <p role="status" className="mt-3 text-[13px] text-good-text">
          {t("worldSection.savedAs", { name: archived })}{" "}
          <Link className="link" to="/servers/$uuid" params={{ uuid }} search={{ tab: "files" }}>
            {t("worldSection.downloadItFromFiles")}
          </Link>
          .
        </p>
      )}
      {archive.error ? <ErrorBox error={archive.error} /> : null}
      <FormDialog
        open={reset}
        title={t("worldSection.resetTheWorld")}
        submitLabel={t("worldSection.deleteAndReset")}
        danger
        pending={doReset.isPending}
        error={doReset.error}
        onClose={() => setReset(false)}
        onSubmit={() => doReset.mutate()}
      >
        <p>
          <Trans
            i18nKey="worldSection.resetBody"
            values={{ folders: folders.join(", ") }}
            components={{ code: <span className="font-mono text-zinc-100" /> }}
          />
        </p>
        <div>
          <label htmlFor="mc-seed" className="label">
            {t("worldSection.seedForTheNewWorld")}
          </label>
          <input
            id="mc-seed"
            className="input font-mono"
            placeholder={t("worldSection.leaveEmptyForARandom")}
            maxLength={64}
            value={seed}
            onChange={(e) => setSeed(e.target.value)}
          />
        </div>
      </FormDialog>
    </Section>
  );
}

// ---- Icon ----------------------------------------------------------------------------------

/** Scales an image to the 64×64 PNG the game wants, cropping it to a square from the middle. */
async function toIcon(file: File): Promise<Blob> {
  const bmp = await createImageBitmap(file);
  const side = Math.min(bmp.width, bmp.height);
  const c = document.createElement("canvas");
  c.width = c.height = 64;
  const g = c.getContext("2d");
  if (!g) throw new Error(i18n.t("worldSection.cannotResize"));
  g.imageSmoothingQuality = "high";
  g.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, 64, 64);
  return new Promise((ok, bad) => c.toBlob((b) => (b ? ok(b) : bad(new Error(i18n.t("worldSection.cannotConvert")))), "image/png"));
}

export function IconSection({ uuid, running }: { uuid: string; running: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const status = qc.getQueryData(minecraftStatusQuery(uuid, running).queryKey);
  const upload = useMutation({
    mutationFn: async (f: File) => {
      if (f.size > 20 << 20) throw new Error(t("worldSection.tooBig"));
      const png = await toIcon(f);
      const res = await fetch(`/api/servers/${uuid}/files/upload?path=${encodeURIComponent("/server-icon.png")}`, {
        method: "PUT",
        body: png,
        credentials: "same-origin",
      });
      if (!res.ok) throw new Error(t("worldSection.uploadFailed", { status: res.status }));
      return URL.createObjectURL(png);
    },
    meta: invalidates(keys.files(uuid)),
    onSuccess: (url) => setPreview(url),
  });
  const shown = preview ?? status?.favicon ?? null;
  return (
    <Section
      title={t("worldSection.serverIcon")}
      desc={t("worldSection.thePictureNextToYour")}
      footer={
        <>
          <span>
            {preview
              ? running
                ? t("worldSection.savedRestartTheServerTo")
                : t("worldSection.savedItShowsFromThe")
              : t("worldSection.pngJpegWebpOrGif")}
          </span>
          <button type="button" className="btn" disabled={upload.isPending} onClick={() => input.current?.click()}>
            {upload.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <ImageUp className="h-4 w-4" aria-hidden />}
            {t("worldSection.uploadAnImage")}
          </button>
        </>
      }
    >
      <div className="flex items-center gap-4">
        {shown ? (
          <img
            src={shown}
            alt={t("worldSection.serverIcon")}
            width={64}
            height={64}
            className="h-16 w-16 rounded-[4px] border border-line [image-rendering:pixelated]"
          />
        ) : (
          <span
            className="grid h-16 w-16 place-content-center rounded-[4px] border border-dashed border-line-strong text-zinc-600"
            aria-hidden
          >
            <Box className="h-7 w-7" />
          </span>
        )}
        <p className="text-[13px] text-zinc-500">{shown ? t("worldSection.currentIcon") : t("worldSection.theIconShowsHereOnce")}</p>
      </div>
      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="sr-only"
        aria-label={t("worldSection.serverIconImage")}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) upload.mutate(f);
          e.target.value = "";
        }}
      />
      {upload.error ? (
        <div className="mt-3">
          <ErrorBox error={upload.error} />
        </div>
      ) : null}
    </Section>
  );
}
