import { Field } from "./ui";

// Limits are edited as strings so a field can be empty while typing.
export type LimitsForm = {
  memory_mb: string;
  swap_mb: string;
  cpu_pct: string;
  cpuset: string;
  pids_limit: string;
  io_weight: string;
  disk_mb: string;
  backup_limit: string;
};

export type LimitsValue = {
  memory_mb: number;
  swap_mb: number;
  cpu_pct: number;
  cpuset: string;
  pids_limit: number;
  io_weight: number;
  disk_mb: number;
  backup_limit: number;
};

export const defaultLimitsForm: LimitsForm = {
  memory_mb: "1024", swap_mb: "0", cpu_pct: "0", cpuset: "", pids_limit: "1024", io_weight: "0", disk_mb: "0", backup_limit: "20",
};

export const limitsToForm = (l: LimitsValue): LimitsForm => ({
  memory_mb: String(l.memory_mb), swap_mb: String(l.swap_mb), cpu_pct: String(l.cpu_pct), cpuset: l.cpuset,
  pids_limit: String(l.pids_limit), io_weight: String(l.io_weight), disk_mb: String(l.disk_mb), backup_limit: String(l.backup_limit),
});

const n = (s: string, fallback: number) => (s.trim() === "" ? fallback : Number(s));

export const formToLimits = (f: LimitsForm): LimitsValue => ({
  memory_mb: n(f.memory_mb, 0), swap_mb: n(f.swap_mb, 0), cpu_pct: n(f.cpu_pct, 0), cpuset: f.cpuset.trim(),
  pids_limit: n(f.pids_limit, 1024), io_weight: n(f.io_weight, 0), disk_mb: n(f.disk_mb, 0), backup_limit: n(f.backup_limit, 20),
});

/** One input per limit. Zero generally means "unlimited" or "default"; the hints say which. */
export function LimitsFields({ value, onChange }: { value: LimitsForm; onChange: (v: LimitsForm) => void }) {
  const set = (k: keyof LimitsForm) => (e: React.ChangeEvent<HTMLInputElement>) => onChange({ ...value, [k]: e.target.value });
  const num = (k: keyof LimitsForm, label: string, hint: string, min: number, max: number, required = false) => (
    <Field label={label} hint={hint} key={k}>
      {(id) => <input id={id} className="input" type="number" min={min} max={max} required={required} value={value[k]} onChange={set(k)} />}
    </Field>
  );
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {num("memory_mb", "Memory (MB)", "Hard limit. The server is killed above it.", 64, 1048576, true)}
      {num("swap_mb", "Swap (MB)", "On top of memory. 0 = none, -1 = unlimited.", -1, 1048576)}
      {num("cpu_pct", "CPU limit (%)", "100 = one core. 0 = unlimited.", 0, 6400)}
      <Field label="CPU cores" hint="Pin to specific cores, e.g. 0-3 or 0,2. Empty = any.">
        {(id) => <input id={id} className="input font-mono" pattern="[0-9]+([\-,][0-9]+)*|" value={value.cpuset} onChange={set("cpuset")} />}
      </Field>
      {num("disk_mb", "Disk (MB)", "Quota enforced by the node. 0 = unlimited.", 0, 104857600)}
      {num("pids_limit", "Max processes", "Processes and threads. Stops fork bombs.", 16, 65536)}
      {num("io_weight", "Disk IO priority", "10-1000, relative to other servers. 0 = default.", 0, 1000)}
      {num("backup_limit", "Backups to keep", "1-100.", 1, 100)}
    </div>
  );
}
