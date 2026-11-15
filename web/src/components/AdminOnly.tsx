import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { meQuery } from "../api/queries";
import { Empty } from "./ui";

export function AdminOnly({ children }: { children: ReactNode }) {
  const me = useQuery(meQuery);
  if (!me.data) return null;
  if (!me.data.is_admin)
    return (
      <div role="alert" className="mx-auto max-w-md pt-10">
        <Empty icon={<Lock className="h-5 w-5" aria-hidden />} title="Administrators only">You need administrator access to view this page.</Empty>
      </div>
    );
  return <>{children}</>;
}
