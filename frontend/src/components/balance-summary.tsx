"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, money, secondaryButtonClass, titleCase } from "@/lib/ui";
import { LedgerEvent, PatientBalanceSummary, ledgerEventLabels, signedAmount } from "@/types/balances";

type Filter = "all" | "patient" | "insurance";

type Paged = { items: LedgerEvent[]; total: number; page: number; page_size: number };

function eventLink(patientId: string, e: LedgerEvent) {
  if (e.event_type === "patient_payment") return `/patients/${patientId}/billing/payments/${e.source_id}`;
  if (e.event_type === "insurance_payment") return `/billing/insurance-payments/${e.source_id}`;
  return `/patients/${patientId}/billing/charges/${e.charge_id}`;
}

// Balance cards plus the ledger that explains them. Every number comes from
// the backend balance engine; nothing is computed in the browser.
export default function BalanceSummary({ patientId }: { patientId: string }) {
  const [summary, setSummary] = useState<PatientBalanceSummary | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [showVoided, setShowVoided] = useState(false);
  const [page, setPage] = useState(1);
  const [ledger, setLedger] = useState<Paged | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let stale = false;

    async function load() {
      const response = await apiFetch(`/api/patients/${patientId}/billing-summary`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load balances");
        return;
      }

      setSummary(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [patientId]);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "15" });
      if (filter !== "all") query.set("party", filter);
      if (showVoided) query.set("include_voided", "true");

      const response = await apiFetch(`/api/patients/${patientId}/ledger?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load the ledger");
        return;
      }

      setLedger(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [patientId, filter, showVoided, page]);

  function show(next: Filter) {
    setFilter(next);
    setPage(1);
    document.getElementById("balance-ledger")?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  const cards: { key: string; label: string; value: string | undefined; hint: string; onClick: () => void; testId: string }[] = [
    { key: "patient", label: "Patient Balance", value: summary?.patient_balance, hint: "Owed by the patient", onClick: () => show("patient"), testId: "card-patient-balance" },
    { key: "insurance", label: "Insurance Balance", value: summary?.insurance_balance, hint: "Owed by insurers", onClick: () => show("insurance"), testId: "card-insurance-balance" },
    { key: "total", label: "Total Outstanding", value: summary?.total_outstanding, hint: "Patient + insurance", onClick: () => show("all"), testId: "card-total-outstanding" },
    {
      key: "credit",
      label: "Patient Credit",
      value: summary?.unallocated_patient_credit,
      hint: "Unapplied patient payments",
      onClick: () => document.getElementById("patient-payments")?.scrollIntoView({ behavior: "smooth", block: "start" }),
      testId: "card-patient-credit",
    },
  ];

  const totalPages = ledger ? Math.max(1, Math.ceil(ledger.total / ledger.page_size)) : 1;

  return (
    <section aria-labelledby="balances-heading" className="mb-8">
      <h2 id="balances-heading" className="sr-only">Balances</h2>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {cards.map((c) => (
          <button
            key={c.key}
            type="button"
            onClick={c.onClick}
            data-testid={c.testId}
            className="rounded-xl border bg-white p-4 text-left hover:border-slate-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-slate-500"
          >
            <div className="text-sm text-slate-500">{c.label}</div>
            <div className="mt-1 text-2xl font-semibold text-slate-900">{c.value === undefined ? "…" : money(c.value)}</div>
            <div className="mt-1 text-xs text-slate-500">{c.hint}</div>
          </button>
        ))}
      </div>

      <div className="mt-3"><ErrorBox message={error} /></div>

      <div id="balance-ledger" className={`${cardClass} mt-4 scroll-mt-4`}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h3 className="text-lg font-semibold text-slate-900">Ledger</h3>
          <div className="flex flex-wrap items-center gap-2 text-sm" role="group" aria-label="Ledger filter">
            {(["all", "patient", "insurance"] as Filter[]).map((f) => (
              <button
                key={f}
                type="button"
                aria-pressed={filter === f}
                onClick={() => { setFilter(f); setPage(1); }}
                className={filter === f ? "rounded-lg bg-slate-900 px-3 py-1.5 font-medium text-white" : "rounded-lg border bg-white px-3 py-1.5 font-medium text-slate-900"}
              >
                {f === "all" ? "All" : titleCase(f)}
              </button>
            ))}
            <label className="ml-2 flex items-center gap-2">
              <input type="checkbox" checked={showVoided} onChange={(e) => { setShowVoided(e.target.checked); setPage(1); }} />
              Show voided
            </label>
          </div>
        </div>
        <p className="mt-1 text-sm text-slate-600">
          Every change to what is owed. Amounts marked − lower a balance; + raise it.
        </p>

        <div className="mt-3 overflow-x-auto rounded-lg border">
          {!ledger ? (
            <div className="p-4 text-center text-sm text-slate-500">Loading ledger...</div>
          ) : ledger.items.length === 0 ? (
            <div className="p-4 text-center text-sm text-slate-500">No financial activity yet.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Service</th>
                  <th className="px-3 py-2">Event</th>
                  <th className="px-3 py-2">Party</th>
                  <th className="px-3 py-2 text-right">Amount</th>
                  <th className="px-3 py-2">Status</th>
                </tr>
              </thead>
              <tbody>
                {ledger.items.map((e, i) => (
                  <tr key={`${e.source_id}-${e.event_type}-${e.party}-${i}`} className={`border-b last:border-0 ${e.status === "voided" ? "text-slate-500 line-through" : ""}`}>
                    <td className="whitespace-nowrap px-3 py-2">{e.occurred_on}</td>
                    <td className="px-3 py-2">
                      <Link href={`/patients/${patientId}/billing/charges/${e.charge_id}`} className="underline">
                        {e.date_of_service} · {e.service_code}
                      </Link>
                    </td>
                    <td className="px-3 py-2">
                      <Link href={eventLink(patientId, e)} className="underline">{ledgerEventLabels[e.event_type] ?? e.event_type}</Link>
                      {e.detail && <span className="text-xs text-slate-500"> · {titleCase(e.detail)}</span>}
                    </td>
                    <td className="px-3 py-2 capitalize">{e.party}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right font-medium">{signedAmount(e)}</td>
                    <td className="px-3 py-2">
                      <Badge tone={e.status === "active" ? "green" : "slate"}>{e.status === "active" ? "Active" : "Voided"}</Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {ledger && ledger.total > ledger.page_size && (
          <nav aria-label="Ledger pagination" className="mt-3 flex items-center justify-between text-sm">
            <span className="text-slate-600">{ledger.total} events · page {ledger.page} of {totalPages}</span>
            <div className="flex gap-2">
              <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
              <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
            </div>
          </nav>
        )}
      </div>
    </section>
  );
}
