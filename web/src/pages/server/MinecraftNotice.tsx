import { useMutation, useQuery } from "@tanstack/react-query";
import { Coffee, ExternalLink, FileCheck2, Loader2, X } from "lucide-react";
import { api, call } from "../../api/client";
import { invalidates, keys, startupQuery } from "../../api/queries";
import { ErrorBox } from "../../components/ui";
import { pickJavaImage, type Problem } from "../../lib/minecraft";

/**
 * A banner over the console when a Minecraft server stopped for a reason its owner can fix in one click:
 * the EULA is not accepted, or the image's Java is too old for this version.
 */
export function MinecraftNotice({
  uuid,
  problem,
  can,
  onDone,
}: {
  uuid: string;
  problem: Problem;
  can: (need?: string) => boolean;
  onDone: () => void;
}) {
  const startup = useQuery({ ...startupQuery(uuid), enabled: can("startup.read") });
  const start = () => call(api.POST("/servers/{uuid}/power", { params: { path: { uuid } }, body: { action: "start" } }));

  const eulaVar = startup.data?.variables.find((v) => v.env === "EULA" && v.editable);
  const acceptEula = useMutation({
    mutationFn: async () => {
      // Our eggs read the EULA variable; any egg's server reads eula.txt.
      if (eulaVar) await call(api.PUT("/servers/{uuid}/startup", { params: { path: { uuid } }, body: { env: { EULA: "true" } } }));
      else
        await call(
          api.PUT("/servers/{uuid}/files/content", { params: { path: { uuid } }, body: { path: "/eula.txt", content: "eula=true\n" } }),
        );
      if (can("power")) await start();
    },
    meta: invalidates(keys.startup(uuid), keys.server(uuid)),
    onSuccess: onDone,
  });

  const target = problem.kind === "java" && startup.data ? pickJavaImage(startup.data.images, problem.need) : null;
  const switchJava = useMutation({
    mutationFn: async (image: string) => {
      await call(api.PUT("/servers/{uuid}/startup", { params: { path: { uuid } }, body: { image } }));
      if (can("power")) await start();
    },
    meta: invalidates(keys.startup(uuid), keys.server(uuid)),
    onSuccess: onDone,
  });

  const canAccept = eulaVar ? can("startup.update") : can("files.write");
  const m = problem.kind === "eula" ? acceptEula : switchJava;

  return (
    <div role="alert" className="rounded-xl border border-warn/30 bg-warn-bg p-4 text-sm text-warn-text shadow-panel">
      <div className="flex items-start gap-3">
        {problem.kind === "eula" ? (
          <FileCheck2 className="mt-0.5 h-5 w-5 shrink-0" aria-hidden />
        ) : (
          <Coffee className="mt-0.5 h-5 w-5 shrink-0" aria-hidden />
        )}
        <div className="min-w-0 flex-1 space-y-2">
          {problem.kind === "eula" ? (
            <>
              <p className="font-medium text-zinc-100">Accept the Minecraft EULA to start this server</p>
              <p className="text-zinc-300">
                Mojang requires every server owner to agree to the Minecraft End User License Agreement.{" "}
                <a
                  className="link inline-flex items-center gap-1 underline"
                  href="https://aka.ms/MinecraftEULA"
                  target="_blank"
                  rel="noreferrer"
                >
                  Read the EULA <ExternalLink className="h-3 w-3" aria-hidden />
                </a>
              </p>
            </>
          ) : (
            <>
              <p className="font-medium text-zinc-100">
                This server needs Java {problem.need}
                {problem.have ? `, but the image has Java ${problem.have}` : " or newer"}
              </p>
              <p className="text-zinc-300">
                {target
                  ? `Switch the image to ${target.label} and start again.`
                  : "None of this egg's images has a new enough Java. Ask an admin for a newer image, or install an older Minecraft version."}
              </p>
            </>
          )}
          <div className="flex flex-wrap gap-2 pt-1">
            {problem.kind === "eula" && canAccept && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={acceptEula.isPending || startup.isPending}
                onClick={() => acceptEula.mutate()}
              >
                {acceptEula.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
                {can("power") ? "I accept, start the server" : "I accept"}
              </button>
            )}
            {problem.kind === "java" && target && can("startup.docker-image") && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={switchJava.isPending}
                onClick={() => switchJava.mutate(target.image)}
              >
                {switchJava.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />}
                Use {target.label}
                {can("power") ? " and start" : ""}
              </button>
            )}
            {((problem.kind === "eula" && !canAccept) || (problem.kind === "java" && target && !can("startup.docker-image"))) && (
              <p className="text-zinc-400">You do not have permission to change this; ask the server&apos;s owner.</p>
            )}
          </div>
          {m.error ? <ErrorBox error={m.error} /> : null}
        </div>
        <button type="button" className="rounded-md p-1 text-zinc-400 hover:text-zinc-100" aria-label="Dismiss" onClick={onDone}>
          <X className="h-4 w-4" aria-hidden />
        </button>
      </div>
    </div>
  );
}
