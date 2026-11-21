import { useEffect, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { notifyUnauthorized } from "../api/client";
import { keys, refreshNow } from "../api/queries";
import { formatProblem } from "../lib/problem";
import type { DroppedFile } from "../lib/files";

export interface Upload {
  id: number;
  path: string;
  size: number;
  sent: number;
  status: "queued" | "uploading" | "done" | "failed" | "cancelled";
  error?: string;
}

/** Uploads run a few at a time; more would only compete for the same bandwidth. */
const PARALLEL = 3;

/**
 * A queue of uploads into one server. Each file is a single streamed request
 * (XHR, so the browser reports progress), written in place only once complete.
 */
class UploadQueue {
  private items: Upload[] = [];
  private waiting: { id: number; file: File; path: string }[] = [];
  private xhrs = new Map<number, XMLHttpRequest>();
  private listeners = new Set<() => void>();
  private seq = 0;

  constructor(
    private uuid: string,
    private onFinished: () => void,
  ) {}

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => void this.listeners.delete(fn);
  };
  snapshot = () => this.items;

  private patch(id: number, p: Partial<Upload>) {
    this.items = this.items.map((u) => (u.id === id ? { ...u, ...p } : u));
    for (const fn of this.listeners) fn();
  }

  add(dir: string, files: DroppedFile[]) {
    const base = dir === "/" ? "" : dir.replace(/\/+$/, "");
    const jobs = files.map((f) => ({ id: ++this.seq, file: f.file, path: `${base}/${f.rel}` }));
    // Finished rows from an earlier batch make way for the new one.
    this.items = [
      ...this.items.filter((u) => u.status === "uploading" || u.status === "queued"),
      ...jobs.map((j) => ({ id: j.id, path: j.path, size: j.file.size, sent: 0, status: "queued" as const })),
    ];
    this.waiting.push(...jobs);
    for (const fn of this.listeners) fn();
    this.pump();
  }

  cancel(id: number) {
    const x = this.xhrs.get(id);
    if (x) {
      x.abort();
      return;
    }
    this.waiting = this.waiting.filter((j) => j.id !== id);
    this.patch(id, { status: "cancelled" });
  }

  clear() {
    this.items = this.items.filter((u) => u.status === "uploading" || u.status === "queued");
    for (const fn of this.listeners) fn();
  }

  /** Abort everything (the page is going away). */
  stop() {
    this.waiting = [];
    for (const x of this.xhrs.values()) x.abort();
  }

  private pump() {
    while (this.xhrs.size < PARALLEL) {
      const job = this.waiting.shift();
      if (!job) break;
      const xhr = new XMLHttpRequest();
      this.xhrs.set(job.id, xhr);
      const finish = (p: Partial<Upload>) => {
        this.xhrs.delete(job.id);
        this.patch(job.id, p);
        this.onFinished();
        this.pump();
      };
      xhr.upload.onprogress = (e) => this.patch(job.id, { sent: e.loaded });
      xhr.onload = () => {
        if (xhr.status === 401) notifyUnauthorized();
        if (xhr.status >= 200 && xhr.status < 300) {
          finish({ status: "done", sent: job.file.size });
          return;
        }
        let body: unknown;
        try {
          body = JSON.parse(xhr.responseText);
        } catch {
          body = undefined;
        }
        finish({ status: "failed", error: formatProblem(body, xhr.status) });
      };
      xhr.onerror = () => finish({ status: "failed", error: "The upload was interrupted. Check your connection and try again." });
      xhr.onabort = () => finish({ status: "cancelled" });
      xhr.open("PUT", `/api/servers/${this.uuid}/files/upload?path=${encodeURIComponent(job.path)}`);
      xhr.withCredentials = true;
      this.patch(job.id, { status: "uploading" });
      xhr.send(job.file);
    }
  }
}

export function useUploads(uuid: string) {
  const qc = useQueryClient();
  const [queue] = useState(() => new UploadQueue(uuid, () => refreshNow(qc, keys.files(uuid))));
  useEffect(() => () => queue.stop(), [queue]);
  const items = useSyncExternalStore(queue.subscribe, queue.snapshot);
  return {
    items,
    add: (dir: string, files: DroppedFile[]) => queue.add(dir, files),
    cancel: (id: number) => queue.cancel(id),
    clear: () => queue.clear(),
  };
}
