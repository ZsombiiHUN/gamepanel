import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { meQuery } from "../api/queries";

export function AdminOnly({ children }: { children: ReactNode }) {
  const me = useQuery(meQuery);
  if (!me.data) return null;
  if (!me.data.is_admin) return <p role="alert" className="card text-sm text-zinc-300">You need administrator access to view this page.</p>;
  return <>{children}</>;
}
