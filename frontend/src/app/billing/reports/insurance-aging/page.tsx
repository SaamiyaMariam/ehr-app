"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import AgingTotalsCards from "@/components/aging-totals";
import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { claimStatus } from "@/types/claims";
import { AgingTotals, InsuranceAgingRow } from "@/types/reports";

type Report = { basis: string; totals: AgingTotals; items: InsuranceAgingRow[]; total: number; page: number; page_size: number };
type Filters = Record<string, string>;

const emptyFilters: Filters = { payer_id: "", clinician_id: "", sequence: "", patient: "", bucket: "" };

export default function InsuranceAgingPage() {
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [page, setPage] = useState(1);
  const [report, setReport] = useState<Report | null>(null);
  const [payers, setPayers] = useState<{ id: string; payer_name: string }[]>([]);
  const [clinicians, setClinicians] = useState<{ id: string; first_name: string; last_name: string }[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadOptions() {
      const [p, c] = await Promise.all([apiFetch("/api/payers"), apiFetch("/api/clinicians")]);
      if (p.ok) setPayers(await p.json());
      if (c.ok) setClinicians(await c.json());
    }

    loadOptions();
  }, []);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/billing/reports/insurance-aging?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load the report");
        return;
      }

      setError("");
      setReport(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [applied, page]);

  function submit(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setApplied(filters);
  }

  function pickBucket(bucket: string) {
    const next = { ...filters, bucket };
    setFilters(next);
    setApplied(next);
    setPage(1);
  }

  const totalPages = report ? Math.max(1, Math.ceil(report.total / report.page_size)) : 1;

  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Insurance Aging</h1>
          <p className="mt-1 text-sm text-slate-600">
            Insurance balances by age. Age is counted from the <strong>date of service</strong>. Each balance is
            attributed to the claim currently responsible for it (the highest claim sequence), or to the service&apos;s
            payer while it is unbilled.
          </p>
        </div>

        <form onSubmit={submit} className={`${cardClass} grid gap-4 md:grid-cols-5`} aria-label="Filters">
          <div>
            <label htmlFor="ia-payer" className={labelClass}>Payer</label>
            <select id="ia-payer" className={inputClass} value={filters.payer_id} onChange={(e) => setFilters({ ...filters, payer_id: e.target.value })}>
              <option value="">All payers</option>
              {payers.map((p) => (<option key={p.id} value={p.id}>{p.payer_name}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="ia-clinician" className={labelClass}>Clinician</label>
            <select id="ia-clinician" className={inputClass} value={filters.clinician_id} onChange={(e) => setFilters({ ...filters, clinician_id: e.target.value })}>
              <option value="">All clinicians</option>
              {clinicians.map((c) => (<option key={c.id} value={c.id}>{c.first_name} {c.last_name}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="ia-sequence" className={labelClass}>Claim sequence</label>
            <select id="ia-sequence" className={inputClass} value={filters.sequence} onChange={(e) => setFilters({ ...filters, sequence: e.target.value })}>
              <option value="">All</option>
              <option value="unbilled">Not yet billed</option>
              <option value="primary">Primary</option>
              <option value="secondary">Secondary</option>
              <option value="tertiary">Tertiary</option>
              <option value="quaternary">Quaternary</option>
            </select>
          </div>
          <div>
            <label htmlFor="ia-patient" className={labelClass}>Patient name</label>
            <input id="ia-patient" className={inputClass} value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
          </div>
          <div className="flex items-end gap-3">
            <button className={primaryButtonClass}>Run</button>
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>Clear</button>
          </div>
        </form>

        <ErrorBox message={error} />

        <AgingTotalsCards totals={report?.totals ?? null} active={applied.bucket} onSelect={pickBucket} />

        <div className="overflow-x-auto rounded-xl border bg-white">
          {!report ? (
            <div className="p-6 text-center text-slate-500">Loading report...</div>
          ) : report.items.length === 0 ? (
            <div className="p-6 text-center text-slate-500">No insurance balances match these filters.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">Service date</th>
                  <th className="px-3 py-2 text-right">Age (days)</th>
                  <th className="px-3 py-2">Patient</th>
                  <th className="px-3 py-2">Service</th>
                  <th className="px-3 py-2">Payer</th>
                  <th className="px-3 py-2">Claim</th>
                  <th className="px-3 py-2 text-right">Insurance balance</th>
                </tr>
              </thead>
              <tbody>
                {report.items.map((row) => {
                  const status = row.claim_status ? claimStatus(row.claim_status) : null;

                  return (
                    <tr key={row.charge_id} className="border-b last:border-0" data-testid="aging-row">
                      <td className="whitespace-nowrap px-3 py-2">
                        <Link href={`/patients/${row.patient_id}/billing/charges/${row.charge_id}`} className="underline">{row.date_of_service}</Link>
                      </td>
                      <td className="px-3 py-2 text-right">{row.age_days}</td>
                      <td className="px-3 py-2"><Link href={`/patients/${row.patient_id}/billing`} className="underline">{row.patient_name}</Link></td>
                      <td className="px-3 py-2">{row.service_code}</td>
                      <td className="px-3 py-2">{row.payer_name}</td>
                      <td className="px-3 py-2">
                        {row.claim_id ? (
                          <>
                            <Link href={`/billing/claims/${row.claim_id}`} className="underline">{row.claim_number}</Link>
                            <span className="ml-1 text-xs capitalize text-slate-500">{row.claim_sequence}</span>
                            {status && <div><Badge tone={status.tone}>{status.label}</Badge></div>}
                          </>
                        ) : (
                          <Badge tone="amber">Not billed</Badge>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-right font-medium">{money(row.insurance_balance)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {report && report.total > 0 && (
          <nav aria-label="Pagination" className="flex items-center justify-between text-sm">
            <span className="text-slate-600">{report.total} service{report.total === 1 ? "" : "s"} · page {report.page} of {totalPages}</span>
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
