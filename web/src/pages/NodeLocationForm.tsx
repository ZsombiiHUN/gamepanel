import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, call, type components } from "../api/client";
import { invalidates, keys, locationsQuery } from "../api/queries";
import { ErrorBox, Field } from "../components/ui";

type Location = components["schemas"]["Location"];

/** "eu-west (Frankfurt)", or just the short code. */
export function locationLabel(l: Location | undefined) {
  if (!l) return undefined;
  return l.long ? `${l.short} (${l.long})` : l.short;
}

/**
 * Which location a node is in. Shops sell servers in a location (the Pterodactyl
 * application API's deploy.locations, location_ids on POST /servers) and the panel
 * picks a node there. A new location can be made right here.
 */
export function NodeLocationForm({ node }: { node: { id: number; location_id: number } }) {
  const { t } = useTranslation();
  const locations = useQuery(locationsQuery);
  const [location, setLocation] = useState(node.location_id);
  const [draft, setDraft] = useState({ short: "", long: "" });

  const save = useMutation({
    mutationFn: () => call(api.PUT("/nodes/{id}/location", { params: { path: { id: node.id } }, body: { location_id: location } })),
    meta: invalidates(keys.nodes, keys.locations),
  });
  const add = useMutation({
    mutationFn: () => call(api.POST("/locations", { body: { short: draft.short.trim(), long: draft.long.trim() } })),
    meta: invalidates(keys.locations),
    onSuccess: (l) => {
      setDraft({ short: "", long: "" });
      setLocation(l.id);
    },
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save.mutate();
      }}
      className="mt-5 space-y-3 border-t border-line pt-4"
    >
      <Field label={t("nodesPage.location")} hint={t("nodesPage.locationHint")}>
        {(id) => (
          <select id={id} className="input sm:max-w-md" value={location} onChange={(e) => setLocation(Number(e.target.value))}>
            <option value={0}>{t("nodesPage.noLocation")}</option>
            {(locations.data ?? []).map((l) => (
              <option key={l.id} value={l.id}>
                {locationLabel(l)}
              </option>
            ))}
          </select>
        )}
      </Field>
      <fieldset className="grid gap-3 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
        <legend className="sr-only">{t("nodesPage.newLocation")}</legend>
        <Field label={t("nodesPage.newLocationCode")}>
          {(id) => (
            <input
              id={id}
              className="input font-mono"
              maxLength={60}
              value={draft.short}
              onChange={(e) => setDraft({ ...draft, short: e.target.value })}
            />
          )}
        </Field>
        <Field label={t("nodesPage.newLocationDescription")}>
          {(id) => (
            <input
              id={id}
              className="input"
              maxLength={191}
              value={draft.long}
              onChange={(e) => setDraft({ ...draft, long: e.target.value })}
            />
          )}
        </Field>
        <button type="button" className="btn btn-ghost" disabled={!draft.short.trim() || add.isPending} onClick={() => add.mutate()}>
          <Plus className="h-4 w-4" aria-hidden /> {t("nodesPage.addLocation")}
        </button>
      </fieldset>
      <ErrorBox error={add.error ?? save.error} />
      <div className="flex items-center gap-3">
        <button type="submit" className="btn btn-primary" disabled={save.isPending}>
          {t("nodesPage.saveLocation")}
        </button>
        {save.isSuccess && (
          <span role="status" className="text-sm text-good-text">
            {t("nodesPage.saved")}
          </span>
        )}
      </div>
    </form>
  );
}
