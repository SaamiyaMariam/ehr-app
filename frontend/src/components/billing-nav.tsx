"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/billing", label: "Billing" },
  { href: "/billing/claims", label: "Claims" },
];

// Shared header for practice-wide billing pages.
export default function BillingNav() {
  const pathname = usePathname();

  return (
    <header className="border-b bg-white">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-6 py-4">
        <Link href="/dashboard" className="font-semibold">
          EHR
        </Link>
        <nav aria-label="Billing" className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
          {links.map((link) => {
            const active = pathname === link.href;

            return (
              <Link
                key={link.href}
                href={link.href}
                aria-current={active ? "page" : undefined}
                className={active ? "font-semibold text-slate-900 underline" : "text-slate-600 hover:text-slate-900"}
              >
                {link.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </header>
  );
}
