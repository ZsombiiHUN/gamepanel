import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, ScrollText } from "lucide-react";
import { AUDIT_PAGE_SIZE, auditQuery } from "../api/queries";
import { SearchInput } from "../components/SearchInput";
import { Empty, ErrorBox, PageHeader, Pill, Spinner } from "../components/ui";
import { useSearchText } from "../hooks/useSearchText";
import { useTranslation } from "react-i18next";

export function AuditPage() {
  const { t } = useTranslation();
  // The search term and page live in the address, so a view can be shared and Back works.
  // Changing the term and returning to page one is a single navigation, so paging is never undone by a timer.
  const { q: term = "", page = 0 } = useSearch({ from: "/app/admin/audit" });
  const navigate = useNavigate({ from: "/audit" });
  const [filter, setFilter] = useSearchText(term, (q) => void navigate({ search: { q: q || undefined }, replace: true }));
  const goTo = (p: number) => void navigate({ search: (prev) => ({ ...prev, page: p || undefined }) });

  const q = useQuery(auditQuery(term, page));
  const rows = q.data ?? [];

  return (
    <div>
      <PageHeader
        title={t("auditPage.auditLog")}
        sub={t("auditPage.securityEventsAcrossThePanel")}
        actions={
          <SearchInput
            label={t("auditPage.searchTheAuditLog")}
            placeholder={t("auditPage.searchEventUserTargetIp")}
            value={filter}
            onChange={setFilter}
            className="w-72"
          />
        }
      />
      {q.isPending && <Spinner />}
      {q.error && <ErrorBox error={q.error} />}
      {q.data?.length === 0 && page === 0 && !term && (
        <Empty icon={<ScrollText className="h-5 w-5" aria-hidden />} title={t("auditPage.noEventsYet")}>
          {t("auditPage.signInsPowerActionsAnd")}
        </Empty>
      )}
      {q.data && (q.data.length > 0 || page > 0 || term) && (
        <div className="overflow-hidden rounded-xl border border-line bg-zinc-900">
          <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label={t("auditPage.auditEvents")}>
            <table className="tbl">
              <caption className="sr-only">{t("auditPage.securityEventsNewestFirst")}</caption>
              <thead>
                <tr>
                  <th scope="col">{t("auditPage.time")}</th>
                  <th scope="col">{t("auditPage.event")}</th>
                  <th scope="col">{t("auditPage.user")}</th>
                  <th scope="col">{t("auditPage.target")}</th>
                  <th scope="col">{t("auditPage.ip")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((e) => {
                  const bad = e.action.includes("fail") || e.action.includes("blocked");
                  return (
                    <tr key={e.id}>
                      <td className="whitespace-nowrap text-zinc-400">{new Date(e.at * 1000).toLocaleString()}</td>
                      <td>
                        {bad ? (
                          <Pill tone="warn">{e.action}</Pill>
                        ) : (
                          <span className="font-mono text-[12.5px] text-zinc-200">{e.action}</span>
                        )}
                      </td>
                      <td className="text-zinc-300">{e.actor_email}</td>
                      <td className="text-zinc-300">
                        {e.target}
                        {e.detail && <span className="text-zinc-500"> ({e.detail})</span>}
                      </td>
                      <td className="font-mono text-[13px] text-zinc-500">{e.ip}</td>
                    </tr>
                  );
                })}
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={5} className="!py-10 text-center text-zinc-500">
                      {term ? t("auditPage.noEventsMatchTerm", { term }) : t("auditPage.noMoreEvents")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between border-t border-line bg-thead px-4 py-2 text-[13px] text-zinc-500">
            <span aria-live="polite">
              {t("common.page", { page: page + 1 })}
              {q.data.length ? ` · ${t("auditPage.events", { count: q.data.length })}` : ""}
              {term ? ` ${t("auditPage.matchingTerm", { term })}` : ""}
            </span>
            <div className="flex gap-2">
              <button type="button" className="btn" disabled={page === 0} onClick={() => goTo(page - 1)}>
                <ChevronLeft className="h-4 w-4" aria-hidden /> {t("auditPage.newer")}
              </button>
              <button type="button" className="btn" disabled={q.data.length < AUDIT_PAGE_SIZE} onClick={() => goTo(page + 1)}>
                {t("auditPage.older")} <ChevronRight className="h-4 w-4" aria-hidden />
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
