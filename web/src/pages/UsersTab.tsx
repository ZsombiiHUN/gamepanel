import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { api, call } from "../api/client";
import { ErrorBox, Field } from "../components/ui";

function PermPicker({ all, value, onChange }: { all: { id: string; description: string }[]; value: string[]; onChange: (v: string[]) => void }) {
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  return (
    <fieldset className="grid gap-1 sm:grid-cols-2">
      <legend className="sr-only">Permissions</legend>
      {all.map((p) => (
        <label key={p.id} className="flex items-start gap-2 text-sm text-zinc-300">
          <input type="checkbox" className="mt-1" checked={value.includes(p.id)} onChange={() => toggle(p.id)} />
          <span><code>{p.id}</code> <span className="text-zinc-500">{p.description}</span></span>
        </label>
      ))}
    </fieldset>
  );
}

export function UsersTab({ uuid }: { uuid: string }) {
  const qc = useQueryClient();
  const key = ["subusers", uuid];
  const perms = useQuery({ queryKey: ["permissions"], queryFn: () => call(api.GET("/permissions")) });
  const list = useQuery({ queryKey: key, queryFn: () => call(api.GET("/servers/{uuid}/users", { params: { path: { uuid } } })) });
  const refresh = () => qc.invalidateQueries({ queryKey: key });

  const [email, setEmail] = useState("");
  const [picked, setPicked] = useState<string[]>(["console.read"]);
  const [editing, setEditing] = useState<{ id: number; perms: string[] } | null>(null);

  const add = useMutation({
    mutationFn: () => call(api.POST("/servers/{uuid}/users", { params: { path: { uuid } }, body: { email, permissions: picked } })),
    onSuccess: () => {
      setEmail("");
      return refresh();
    },
  });
  const save = useMutation({
    mutationFn: (v: { id: number; perms: string[] }) =>
      call(api.PUT("/servers/{uuid}/users/{user_id}", { params: { path: { uuid, user_id: v.id } }, body: { permissions: v.perms } })),
    onSuccess: () => {
      setEditing(null);
      return refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => call(api.DELETE("/servers/{uuid}/users/{user_id}", { params: { path: { uuid, user_id: id } } })),
    onSuccess: refresh,
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    add.mutate();
  };
  const all = perms.data ?? [];

  return (
    <div className="space-y-4">
      <div className="card space-y-3" aria-labelledby="us-list">
        <h2 id="us-list" className="text-lg font-medium">People with access</h2>
        <p className="text-sm text-zinc-400">Subusers can only do what you tick. They never see the server&apos;s environment variables, and cannot manage access or delete the server.</p>
        <ErrorBox error={list.error ?? remove.error ?? save.error} />
        {list.data?.length === 0 && <p className="text-sm text-zinc-400">Nobody else has access.</p>}
        {list.data?.map((u) => (
          <div key={u.user_id} className="rounded-md border border-zinc-800 p-3 text-sm">
            <div className="flex items-center gap-2">
              <strong>{u.email}</strong>
              <span className="ml-auto flex gap-2">
                <button type="button" className="btn" onClick={() => setEditing(editing?.id === u.user_id ? null : { id: u.user_id, perms: u.permissions })}>
                  {editing?.id === u.user_id ? "Cancel" : "Edit"}
                </button>
                <button type="button" className="btn btn-danger" aria-label={`Remove ${u.email}`} onClick={() => window.confirm(`Remove ${u.email}'s access?`) && remove.mutate(u.user_id)}>
                  <Trash2 className="h-4 w-4" aria-hidden />
                </button>
              </span>
            </div>
            {editing?.id === u.user_id ? (
              <div className="mt-2 space-y-2">
                <PermPicker all={all} value={editing.perms} onChange={(v) => setEditing({ id: u.user_id, perms: v })} />
                <button type="button" className="btn btn-primary" disabled={save.isPending || editing.perms.length === 0} onClick={() => save.mutate(editing)}>
                  {save.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Save
                </button>
              </div>
            ) : (
              <p className="mt-1 text-zinc-400">{u.permissions.join(", ")}</p>
            )}
          </div>
        ))}
      </div>

      <form onSubmit={submit} className="card space-y-3" aria-labelledby="us-new">
        <h2 id="us-new" className="text-lg font-medium">Give someone access</h2>
        <Field label="Their account email" hint="They need an existing account on this panel.">
          {(id) => <input id={id} className="input" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />}
        </Field>
        <PermPicker all={all} value={picked} onChange={setPicked} />
        <ErrorBox error={add.error} />
        <button type="submit" className="btn btn-primary" disabled={add.isPending || picked.length === 0}>
          {add.isPending && <Loader2 className="h-4 w-4 animate-spin" aria-hidden />} Add
        </button>
      </form>
    </div>
  );
}
