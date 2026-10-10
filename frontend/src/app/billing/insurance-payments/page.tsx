"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { InsurancePayment, insurancePaymentTypeLabel } from "@/types/insurance-payments";

type Paged = { items: InsurancePayment[]; total: number; page: number; page_size: number };
type PayerOption = { id: string; payer_name: string };

const emptyFilters: Record<string, string> = { payer_id: "", from: "", to: "", status: "", reference: "" };

export default function InsurancePaymentsPage() {
  const [payers, setPayers] = useState<PayerOption[]>([]);
  const [filters, setFilters] = useState(emptyFilters);
  const [applied, setApplied] = useState(emptyFilters);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Paged | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPayers() {
      const response = await apiFetch("/api/payers");
      if (response.ok) setPayers(await response.json());
    }

    loadPayers();
  }, []);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/insurance-payments?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load insurance payments");
        return;
      }

      setError("");
      setResult(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [applied, page]);

  function search(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setApplied(filters);
  }

  const totalPages = result ? Math.max(1, Math.ceil(result.total / result.page_size)) : 1;

  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-2xl font-semibold text-slate-900">Insurance Payments</h1>
          <Link href="/billing/insurance-payments/new" className={primaryButtonClass}>Enter Insurance Payment</Link>
        </div>

        <form onSubmit={search} className={`${cardClass} grid gap-4 md:grid-cols-5`}>
          <div>
            <label htmlFor="if-payer" className={labelClass}>Payer</label>
            <select id="if-payer" className={inputClass} value={filters.payer_id} onChange={(e) => setFilters({ ...filters, payer_id: e.target.value })}>
              <option value="">All payers</option>
              {payers.map((p) => (<option key={p.id} value={p.id}>{p.payer_name}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="if-from" className={labelClass}>From</label>
            <input id="if-from" type="date" className={inputClass} value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
          </div>
          <div>
            <label htmlFor="if-to" className={labelClass}>To</label>
            <input id="if-to" type="date" className={inputClass} value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
          </div>
          <div>
            <label htmlFor="if-reference" className={labelClass}>Check / EFT number</label>
            <input id="if-reference" className={inputClass} value={filters.reference} onChange={(e) => setFilters({ ...filters, reference: e.target.value })} />
          </div>
          <div>
            <label htmlFor="if-status" className={labelClass}>Status</label>
            <select id="if-status" className={inputClass} value={filters.status} onChange={(e) => setFilters({ ...filters, status: e.target.value })}>
              <option value="">All</option>
              <option value="posted">Posted</option>
              <option value="voided">Voided</option>
            </select>
          </div>
          <div className="flex gap-3 md:col-span-5 md:justify-end">
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>Clear</button>
            <button className={primaryButtonClass}>Search</button>
          </div>
        </form>

        <ErrorBox message={error} />

        <div className="overflow-x-auto rounded-xl border bg-white">
          {!result ? (
            <div className="p-6 text-center text-slate-500">Loading insurance payments...</div>
          ) : result.items.length === 0 ? (
            <div className="p-6 text-center text-slate-500">No insurance payments match these filters.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Payer</th>
                  <th className="px-3 py-2">Type</th>
                  <th className="px-3 py-2">Reference</th>
                  <th className="px-3 py-2 text-right">Amount</th>
                  <th className="px-3 py-2 text-right">Allocated</th>
                  <th className="px-3 py-2 text-right">Adjusted</th>
                  <th className="px-3 py-2 text-right">To patient</th>
                  <th className="px-3 py-2">Status</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map((p) => (
                  <tr key={p.id} className="border-b last:border-0">
                    <td className="whitespace-nowrap px-3 py-2">
                      <Link href={`/billing/insurance-payments/${p.id}`} className="underline">{p.payment_date}</Link>
                    </td>
                    <td className="px-3 py-2">{p.payer_name}</td>
                    <td className="px-3 py-2">{insurancePaymentTypeLabel(p.payment_type)}</td>
                    <td className="px-3 py-2">{p.reference_number || "—"}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.amount)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.allocated)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.adjusted)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.transferred_to_patient)}</td>
                    <td className="px-3 py-2"><Badge tone={p.status === "posted" ? "green" : "slate"}>{p.status === "posted" ? "Posted" : "Voided"}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {result && result.total > 0 && (
          <nav aria-label="Pagination" className="flex items-center justify-between text-sm">
            <span className="text-slate-600">{result.total} payment{result.total === 1 ? "" : "s"} · page {result.page} of {totalPages}</span>
            <div className="flex gap-2">
              <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
              <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
            </div>
          </nav>
        )}
      </div>
    </main>
  );
}
