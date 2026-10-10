"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/billing", label: "Billing" },
  { href: "/billing/claims", label: "Claims" },
  { href: "/billing/claim-history", label: "Claim History" },
  { href: "/billing/payments", label: "Payments" },
  { href: "/billing/insurance-payments", label: "Insurance Payments" },
  { href: "/billing/statements", label: "Statements" },
  { href: "/billing/reports/insurance-aging", label: "Insurance Aging" },
  { href: "/billing/reports/patient-aging", label: "Patient Aging" },
  { href: "/billing/reports/collections", label: "Collections" },
];

// Navigation below the app header for practice-wide billing pages.
export default function BillingNav() {
  const pathname = usePathname();

  return (
    <div className="border-b bg-white">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-6 py-4">
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
    </div>
  );
}
