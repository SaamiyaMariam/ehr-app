"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { apiFetch } from "@/lib/api";
import { clearToken, getToken } from "@/lib/auth";

export default function AppHeader() {
  const pathname = usePathname();
  const router = useRouter();
  const [username, setUsername] = useState("");
  const publicPage = pathname === "/" || pathname === "/login" || pathname === "/signup";

  useEffect(() => {
    if (publicPage) return;
    let cancelled = false;
    async function loadUser() {
      if (!getToken()) {
        router.replace("/login");
        return;
      }
      try {
        const response = await apiFetch("/api/auth/me");
        if (cancelled) return;
        if (response.status === 401) {
          clearToken();
          setUsername("");
          router.replace("/login");
          return;
        }
        if (response.ok) {
          const user = await response.json();
          if (!cancelled) setUsername(user.username);
        }
      } catch {
        // Keep Logout available if the API is temporarily unreachable.
      }
    }
    loadUser();
    return () => { cancelled = true; };
  }, [pathname, publicPage, router]);

  if (publicPage) return null;

  return (
    <header className="border-b border-slate-300 bg-white">
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-6 py-4">
        <Link href="/dashboard" className="shrink-0 font-semibold text-slate-900">EHR</Link>
        <div className="flex min-w-0 items-center gap-3 sm:gap-4">
          <span className="truncate text-sm text-slate-700" title={username}>{username}</span>
          <button
            type="button"
            className="shrink-0 rounded-lg border border-slate-300 bg-slate-100 px-3 py-2 text-sm font-medium text-slate-900 hover:bg-slate-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-700 focus-visible:ring-offset-2"
            onClick={() => {
              clearToken();
              setUsername("");
              router.replace("/login");
            }}
          >
            Logout
          </button>
        </div>
      </div>
    </header>
  );
}
