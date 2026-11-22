import { Field } from "./ui";
import { useTranslation } from "react-i18next";

// Limits are edited as strings so a field can be empty while typing.
export interface LimitsForm {
  memory_mb: string;
  swap_mb: string;
  cpu_pct: string;
  cpuset: string;
  pids_limit: string;
  io_weight: string;
  disk_mb: string;
  backup_limit: string;
  port_limit: string;
  database_limit: string;
  bandwidth_mbit: string;
}

export interface LimitsValue {
  memory_mb: number;
  swap_mb: number;
  cpu_pct: number;
  cpuset: string;
  pids_limit: number;
  io_weight: number;
  disk_mb: number;
  backup_limit: number;
  port_limit?: number;
  database_limit?: number;
  bandwidth_mbit?: number;
}

export const defaultLimitsForm: LimitsForm = {
  memory_mb: "1024",
  swap_mb: "0",
  cpu_pct: "0",
  cpuset: "",
  pids_limit: "1024",
  io_weight: "0",
  disk_mb: "0",
  backup_limit: "20",
  port_limit: "0",
  database_limit: "0",
  bandwidth_mbit: "0",
};

export const limitsToForm = (l: LimitsValue): LimitsForm => ({
  memory_mb: String(l.memory_mb),
  swap_mb: String(l.swap_mb),
  cpu_pct: String(l.cpu_pct),
  cpuset: l.cpuset,
  pids_limit: String(l.pids_limit),
  io_weight: String(l.io_weight),
  disk_mb: String(l.disk_mb),
  backup_limit: String(l.backup_limit),
  port_limit: String(l.port_limit ?? 0),
  database_limit: String(l.database_limit ?? 0),
  bandwidth_mbit: String(l.bandwidth_mbit ?? 0),
});

const n = (s: string, fallback: number) => (s.trim() === "" ? fallback : Number(s));

export const formToLimits = (f: LimitsForm): LimitsValue => ({
  memory_mb: n(f.memory_mb, 0),
  swap_mb: n(f.swap_mb, 0),
  cpu_pct: n(f.cpu_pct, 0),
  cpuset: f.cpuset.trim(),
  pids_limit: n(f.pids_limit, 1024),
  io_weight: n(f.io_weight, 0),
  disk_mb: n(f.disk_mb, 0),
  backup_limit: n(f.backup_limit, 20),
  port_limit: n(f.port_limit, 0),
  database_limit: n(f.database_limit, 0),
  bandwidth_mbit: n(f.bandwidth_mbit, 0),
});

/** One input per limit. Zero generally means "unlimited" or "default"; the hints say which. */
type NumField = Exclude<keyof LimitsForm, "cpuset">;

export function LimitsFields({ value, onChange }: { value: LimitsForm; onChange: (v: LimitsForm) => void }) {
  const { t } = useTranslation();
  const set = (k: keyof LimitsForm) => (e: React.ChangeEvent<HTMLInputElement>) => onChange({ ...value, [k]: e.target.value });
  const num = (k: NumField, min: number, max: number, required = false) => (
    <Field label={t(`limitsFields.fields.${k}.label`)} hint={t(`limitsFields.fields.${k}.hint`)} key={k}>
      {(id) => <input id={id} className="input" type="number" min={min} max={max} required={required} value={value[k]} onChange={set(k)} />}
    </Field>
  );
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {num("memory_mb", 64, 1048576, true)}
      {num("swap_mb", -1, 1048576)}
      {num("cpu_pct", 0, 6400)}
      <Field label={t("limitsFields.cpuCores")} hint={t("limitsFields.pinToSpecificCoresE")}>
        {(id) => (
          <input id={id} className="input font-mono" pattern="[0-9]+([\-,][0-9]+)*|" value={value.cpuset} onChange={set("cpuset")} />
        )}
      </Field>
      {num("disk_mb", 0, 104857600)}
      {num("pids_limit", 16, 65536)}
      {num("io_weight", 0, 1000)}
      {num("backup_limit", 1, 100)}
      {num("bandwidth_mbit", 0, 100000)}
      {num("database_limit", 0, 100)}
      {num("port_limit", 0, 100)}
    </div>
  );
}
