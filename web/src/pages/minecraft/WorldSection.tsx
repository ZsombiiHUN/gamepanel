import { useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Box, ImageUp, Loader2 } from "lucide-react";
import { api, call } from "../../api/client";
import { dirQuery, invalidates, keys, minecraftStatusQuery } from "../../api/queries";
import { ErrorBox, FormDialog, InfoList, Section, Spinner } from "../../components/ui";
import { parseProperties, setProperties, worldFolders } from "../../lib/minecraft";
import { sendCommand, useServerFile, useWriteFile, type Can } from "./shared";

export function WorldSection({ uuid, running, stopped, can }: { uuid: string; running: boolean; stopped: boolean; can: Can }) {
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
      title="World"
      desc="The world the server loads, from level-name in its settings."
      footer={
        <>
          <span>{stopped ? "Resetting deletes the world for good." : "Stop the server to reset its world."}</span>
          <div className="flex gap-2">
            <button type="button" className="btn" disabled={folders.length === 0 || archive.isPending} onClick={() => archive.mutate()}>
              {archive.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Archive as .zip
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
              Reset world
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
            { label: "Name", value: level },
            { label: "Seed", value: props.get("level-seed") || "random" },
            { label: "Type", value: props.get("level-type") || "minecraft:normal" },
            { label: "Folders", value: folders.length ? folders.join(", ") : "not created yet" },
          ]}
        />
      )}
      {archived && (
        <p role="status" className="mt-3 text-[13px] text-good-text">
          Saved as {archived}.{" "}
          <Link className="link" to="/servers/$uuid" params={{ uuid }} search={{ tab: "files" }}>
            Download it from Files
          </Link>
          .
        </p>
      )}
      {archive.error ? <ErrorBox error={archive.error} /> : null}
      <FormDialog
        open={reset}
        title="Reset the world?"
        submitLabel="Delete and reset"
        danger
        pending={doReset.isPending}
        error={doReset.error}
        onClose={() => setReset(false)}
        onSubmit={() => doReset.mutate()}
      >
        <p>
          This deletes <span className="font-mono text-zinc-100">{folders.join(", ")}</span>. The server makes a new world when it next
          starts. Backups are not touched.
        </p>
        <div>
          <label htmlFor="mc-seed" className="label">
            Seed for the new world
          </label>
          <input
            id="mc-seed"
            className="input font-mono"
            placeholder="Leave empty for a random seed"
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
  if (!g) throw new Error("This browser cannot resize images.");
  g.imageSmoothingQuality = "high";
  g.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, 64, 64);
  return new Promise((ok, bad) => c.toBlob((b) => (b ? ok(b) : bad(new Error("The image could not be converted."))), "image/png"));
}

export function IconSection({ uuid, running }: { uuid: string; running: boolean }) {
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const status = qc.getQueryData(minecraftStatusQuery(uuid, running).queryKey);
  const upload = useMutation({
    mutationFn: async (f: File) => {
      if (f.size > 20 << 20) throw new Error("Pick an image under 20 MB.");
      const png = await toIcon(f);
      const res = await fetch(`/api/servers/${uuid}/files/upload?path=${encodeURIComponent("/server-icon.png")}`, {
        method: "PUT",
        body: png,
        credentials: "same-origin",
      });
      if (!res.ok) throw new Error(`The upload failed (${res.status}).`);
      return URL.createObjectURL(png);
    },
    meta: invalidates(keys.files(uuid)),
    onSuccess: (url) => setPreview(url),
  });
  const shown = preview ?? status?.favicon ?? null;
  return (
    <Section
      title="Server icon"
      desc="The picture next to your server in the multiplayer list. Any image works: it is cropped to a square and scaled to 64×64."
      footer={
        <>
          <span>
            {preview
              ? running
                ? "Saved. Restart the server to show it."
                : "Saved. It shows from the next start."
              : "PNG, JPEG, WebP or GIF."}
          </span>
          <button type="button" className="btn" disabled={upload.isPending} onClick={() => input.current?.click()}>
            {upload.isPending ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : <ImageUp className="h-4 w-4" aria-hidden />}
            Upload an image
          </button>
        </>
      }
    >
      <div className="flex items-center gap-4">
        {shown ? (
          <img
            src={shown}
            alt="Server icon"
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
        <p className="text-[13px] text-zinc-500">{shown ? "Current icon." : "The icon shows here once the server reports it."}</p>
      </div>
      <input
        ref={input}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        className="sr-only"
        aria-label="Server icon image"
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
