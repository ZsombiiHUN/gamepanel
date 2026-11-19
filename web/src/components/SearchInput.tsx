import type { ReactNode } from "react";
import { Search } from "lucide-react";

/** The search box every list page uses: labelled, focusable with "/", cleared with Escape. */
export function SearchInput({
  label,
  placeholder,
  value,
  onChange,
  hint,
  className = "",
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  /** Shown at the right edge, e.g. the "/" key hint. */
  hint?: ReactNode;
  className?: string;
}) {
  return (
    <label
      className={`flex min-h-10 items-center gap-2 rounded-lg border border-zinc-700 bg-thead px-3 text-zinc-500 focus-within:border-indigo-500 ${className}`}
    >
      <Search className="h-4 w-4 shrink-0" aria-hidden />
      <span className="sr-only">{label}</span>
      <input
        data-page-search
        type="search"
        className="w-44 min-w-0 flex-1 bg-transparent text-sm text-zinc-100 outline-none placeholder:text-zinc-500 [&::-webkit-search-cancel-button]:hidden"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onChange("");
        }}
      />
      {hint}
    </label>
  );
}
