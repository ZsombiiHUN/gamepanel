import type { ReactNode } from "react";
import { Gamepad2 } from "lucide-react";
import { ServerIcon } from "./ui";

/** The two-column frame of the sign-in, sign-up and password pages. */
export function AuthLayout({ title, sub, children }: { title: string; sub: ReactNode; children: ReactNode }) {
  return (
    <div className="grid min-h-screen lg:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
      <main className="flex items-center justify-center px-6 py-10">
        <div className="w-full max-w-sm space-y-6">
          <div className="flex items-center gap-2.5 text-zinc-100">
            <Gamepad2 className="h-5 w-5 text-indigo-300" aria-hidden />
            <span className="h-display text-lg">GamePanel</span>
          </div>
          <div>
            <h1 id="auth-title" className="h-display text-3xl">{title}</h1>
            {sub && <p className="mt-1.5 text-sm text-zinc-400">{sub}</p>}
          </div>
          {children}
        </div>
      </main>
      <aside aria-hidden className="relative hidden overflow-hidden border-l border-zinc-800 bg-side lg:block">
        <div className="absolute inset-0 grid place-content-center gap-5 p-12">
          <div className="grid grid-cols-4 gap-5">
            {ICONS.map((n) => (
              <div key={n} className="opacity-90"><ServerIcon name={n} size={84} /></div>
            ))}
          </div>
        </div>
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-[#0b0d0f] via-[#0b0d0f]/90 to-transparent p-12 pt-24">
          <p className="h-display max-w-md text-2xl leading-snug text-zinc-100">Game servers, without the panel you have to babysit.</p>
        </div>
      </aside>
    </div>
  );
}

const ICONS = ["survival-smp", "creative-build", "valheim-co-op", "rust-weekly", "ark-ascended", "terraria-hub", "cs2-comp", "factorio-main", "vanilla-1.21", "skyblock", "mini-games", "modded-pack"];

/** The token from a link's fragment (#token=...), which never reaches any server log. */
export function tokenFromHash(hash = window.location.hash) {
  return new URLSearchParams(hash.replace(/^#/, "")).get("token") ?? "";
}
