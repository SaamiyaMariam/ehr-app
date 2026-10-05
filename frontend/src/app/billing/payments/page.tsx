"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { PatientPayment, paymentMethodLabel, paymentMethodOptions } from "@/types/payments";

type Paged = { items: PatientPayment[]; total: number; page: number; page_size: number };
type PatientOption = { id: string; first_name: string; last_name: string; date_of_birth: string };

const emptyFilters: Record<string, string> = { patient: "", from: "", to: "", method: "", status: "" };

export default function PaymentsPage() {
  const router = useRouter();

  const [patients, setPatients] = useState<PatientOption[]>([]);
  const [patientId, setPatientId] = useState("");
  const [filters, setFilters] = useState(emptyFilters);
  const [applied, setApplied] = useState(emptyFilters);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Paged | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPatients() {
      const response = await apiFetch("/api/patients");
      if (response.ok) setPatients(await response.json());
    }

    loadPatients();
  }, []);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/patient-payments?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load payments");
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
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Payments</h1>

        <section className={cardClass}>
          <h2 className="text-lg font-semibold text-slate-900">Enter Patient Payment</h2>
          <div className="mt-4 flex flex-wrap items-end gap-3">
            <div className="min-w-64 flex-1">
              <label htmlFor="pay-patient" className={labelClass}>Patient</label>
              <select id="pay-patient" className={inputClass} value={patientId} onChange={(e) => setPatientId(e.target.value)}>
                <option value="">Select patient</option>
                {patients.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.last_name}, {p.first_name}{p.date_of_birth ? ` (${p.date_of_birth})` : ""}
                  </option>
                ))}
              </select>
            </div>
            <button
              type="button"
              disabled={!patientId}
              className={primaryButtonClass}
              onClick={() => router.push(`/patients/${patientId}/billing/payments/new`)}
            >
              Enter Patient Payment
            </button>
          </div>
        </section>

        <section>
          <h2 className="text-lg font-semibold text-slate-900">Patient Payment History</h2>
          <form onSubmit={search} className="mt-3 grid gap-4 rounded-xl border bg-white p-5 md:grid-cols-5">
            <div>
              <label htmlFor="pf-patient" className={labelClass}>Patient name</label>
              <input id="pf-patient" className={inputClass} value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
            </div>
            <div>
              <label htmlFor="pf-from" className={labelClass}>From</label>
              <input id="pf-from" type="date" className={inputClass} value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
            </div>
            <div>
              <label htmlFor="pf-to" className={labelClass}>To</label>
              <input id="pf-to" type="date" className={inputClass} value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
            </div>
            <div>
              <label htmlFor="pf-method" className={labelClass}>Method</label>
              <select id="pf-method" className={inputClass} value={filters.method} onChange={(e) => setFilters({ ...filters, method: e.target.value })}>
                <option value="">All methods</option>
                {paymentMethodOptions.map((o) => (<option key={o.value} value={o.value}>{o.label}</option>))}
              </select>
            </div>
            <div>
              <label htmlFor="pf-status" className={labelClass}>Status</label>
              <select id="pf-status" className={inputClass} value={filters.status} onChange={(e) => setFilters({ ...filters, status: e.target.value })}>
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

          <div className="mt-3"><ErrorBox message={error} /></div>

          <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
            {!result ? (
              <div className="p-6 text-center text-slate-500">Loading payments...</div>
            ) : result.items.length === 0 ? (
              <div className="p-6 text-center text-slate-500">No payments match these filters.</div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-3 py-2">Date</th>
                    <th className="px-3 py-2">Patient</th>
                    <th className="px-3 py-2">Method</th>
                    <th className="px-3 py-2 text-right">Amount</th>
                    <th className="px-3 py-2 text-right">Credit</th>
                    <th className="px-3 py-2">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((p) => (
                    <tr key={p.id} className="border-b last:border-0">
                      <td className="whitespace-nowrap px-3 py-2">
                        <Link href={`/patients/${p.patient_id}/billing/payments/${p.id}`} className="underline">{p.payment_date}</Link>
                      </td>
                      <td className="px-3 py-2">{p.patient_name}</td>
                      <td className="px-3 py-2">{paymentMethodLabel(p.method)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.amount)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.unallocated)}</td>
                      <td className="px-3 py-2"><Badge tone={p.status === "posted" ? "green" : "slate"}>{p.status === "posted" ? "Posted" : "Voided"}</Badge></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          {result && result.total > 0 && (
            <nav aria-label="Pagination" className="mt-3 flex items-center justify-between text-sm">
              <span className="text-slate-600">{result.total} payment{result.total === 1 ? "" : "s"} · page {result.page} of {totalPages}</span>
              <div className="flex gap-2">
                <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
                <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
              </div>
            </nav>
          )}
        </section>
      </div>
    </main>
  );
}
