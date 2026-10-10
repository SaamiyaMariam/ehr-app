"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import TransactionSearch from "@/components/transaction-search";
import { apiFetch } from "@/lib/api";
import { ErrorBox, money } from "@/lib/ui";
import { BillingDashboard } from "@/types/reports";

type Card = { key: string; label: string; value: string; href: string; hint?: string; tone?: "alert" | "normal" };

const filterKeys = [
  "patient", "patient_id", "clinician_id", "payer_id", "service_code_id", "from", "to", "billing_method",
  "status", "claim_status", "has_patient_balance", "has_insurance_balance", "balance",
];

function DashboardCards() {
  const [data, setData] = useState<BillingDashboard | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch("/api/billing/dashboard");
      const body = await response.json();

      if (!response.ok) {
        setError(body.error || "Unable to load the dashboard");
        return;
      }

      setData(body);
    }

    load();
  }, []);

  if (error) return <ErrorBox message={error} />;
  if (!data) return <p className="text-sm text-slate-500">Loading dashboard...</p>;

  const c = data.claims;

  const claimCards: Card[] = [
    { key: "pending", label: "Pending Claims", value: String(c.pending), href: "/billing/claims?queue=pending", hint: "Not yet sent", tone: c.pending > 0 ? "alert" : "normal" },
    { key: "validation_errors", label: "Validation Errors", value: String(c.validation_errors), href: "/billing/claims?queue=validation_errors", hint: "Need correcting", tone: c.validation_errors > 0 ? "alert" : "normal" },
    { key: "rejected", label: "Rejected Claims", value: String(c.rejected), href: "/billing/claims?queue=rejected", hint: "Review and resubmit", tone: c.rejected > 0 ? "alert" : "normal" },
    { key: "paper_pending", label: "Pending Paper Claims", value: String(c.paper_pending), href: "/billing/claims?queue=paper_pending", hint: `${c.paper_generated} generated, not yet marked mailed` },
    { key: "external_pending", label: "Pending External Claims", value: String(c.external_pending), href: "/billing/claims?queue=external_pending", hint: "Submit outside this app, then mark submitted" },
    {
      key: "electronic_ready",
      label: data.clearinghouse_configured ? "Prepared Electronic Claims" : "Electronic Claims, No Clearinghouse",
      value: String(c.electronic_ready),
      href: "/billing/claims?queue=electronic_ready",
      hint: data.clearinghouse_configured ? "Ready to transmit" : "Prepared but cannot be sent: no clearinghouse is configured",
    },
  ];

  const moneyCards: Card[] = [
    { key: "patient_balance", label: "Outstanding Patient Balance", value: money(data.patient_balance), href: "/billing/reports/patient-aging", hint: `${data.patients_with_balance} patient${data.patients_with_balance === 1 ? "" : "s"} · aging report` },
    { key: "insurance_balance", label: "Outstanding Insurance Balance", value: money(data.insurance_balance), href: "/billing/reports/insurance-aging", hint: "Aging report" },
    { key: "credit", label: "Unallocated Patient Credits", value: money(data.unallocated_patient_credit), href: "/billing/payments", hint: "Patient payments" },
    { key: "unbilled", label: "Unbilled Insurance Services", value: String(data.unbilled_services.count), href: "/billing?claim_status=none&has_insurance_balance=true", hint: `${money(data.unbilled_services.amount)} not on any claim` },
    { key: "unapplied", label: "Unapplied Insurance Payments", value: String(data.unapplied_insurance_payments.count), href: "/billing/insurance-payments", hint: `${money(data.unapplied_insurance_payments.amount)} not yet allocated` },
  ];

  const render = (cards: Card[]) =>
    cards.map((card) => (
      <Link
        key={card.key}
        href={card.href}
        data-testid={`dash-${card.key}`}
        className={`rounded-xl border bg-white p-4 hover:border-slate-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-slate-500 ${card.tone === "alert" ? "border-amber-300" : ""}`}
      >
        <div className="text-sm text-slate-500">{card.label}</div>
        <div className="mt-1 text-2xl font-semibold text-slate-900" data-testid={`dash-${card.key}-value`}>{card.value}</div>
        {card.hint && <div className="mt-1 text-xs text-slate-500">{card.hint}</div>}
      </Link>
    ));

  return (
    <div className="space-y-6">
      <section aria-labelledby="claims-needing-attention">
        <h2 id="claims-needing-attention" className="text-lg font-semibold text-slate-900">Claims needing attention</h2>
        <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{render(claimCards)}</div>
      </section>

      <section aria-labelledby="money-heading">
        <h2 id="money-heading" className="text-lg font-semibold text-slate-900">Balances and payments</h2>
        <div className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{render(moneyCards)}</div>
      </section>
    </div>
  );
}

function Transactions() {
  const params = useSearchParams();
  const initial: Record<string, string> = {};

  for (const key of filterKeys) {
    const value = params.get(key);
    if (value) initial[key] = value;
  }

  // Remount when the link's filters change so the form and results follow.
  return <TransactionSearch key={JSON.stringify(initial)} initial={initial} />;
}

export default function BillingPage() {
  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-10 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Billing</h1>
          <p className="mt-1 text-sm text-slate-600">Work queues, balances and every billable service. Each card opens the matching list.</p>
        </div>

        <DashboardCards />

        <section aria-labelledby="transactions-heading">
          <h2 id="transactions-heading" className="mb-3 text-lg font-semibold text-slate-900">Billing Transactions</h2>
          <Suspense fallback={<p className="text-sm text-slate-500">Loading...</p>}>
            <Transactions />
          </Suspense>
        </section>
      </div>
    </main>
  );
}
