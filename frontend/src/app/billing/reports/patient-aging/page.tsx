"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import AgingTotalsCards from "@/components/aging-totals";
import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { ErrorBox, SuccessBox, cardClass, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { AgingTotals } from "@/types/reports";
import { PatientAgingRow } from "@/types/statements";

type Report = { basis: string; totals: AgingTotals; items: PatientAgingRow[]; total: number; page: number; page_size: number };
type Filters = Record<string, string>;

const emptyFilters: Filters = { clinician_id: "", patient: "", min_balance: "", bucket: "" };

export default function PatientAgingPage() {
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [page, setPage] = useState(1);
  const [report, setReport] = useState<Report | null>(null);
  const [clinicians, setClinicians] = useState<{ id: string; first_name: string; last_name: string }[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadOptions() {
      const response = await apiFetch("/api/clinicians");
      if (response.ok) setClinicians(await response.json());
    }

    loadOptions();
  }, []);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/billing/reports/patient-aging?${query}`);
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

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  }

  async function generate() {
    setBusy(true);
    setError("");
    setMessage("");

    try {
      const response = await apiFetch("/api/billing/statements/batch", {
        method: "POST",
        body: JSON.stringify({ patient_ids: [...selected] }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to generate statements");
      }

      setSelected(new Set());
      setMessage(`${data.created.length} statement${data.created.length === 1 ? "" : "s"} generated. Find them under Billing → Statements.`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to generate statements");
    } finally {
      setBusy(false);
    }
  }

  const totalPages = report ? Math.max(1, Math.ceil(report.total / report.page_size)) : 1;
  const pageIds = report?.items.map((r) => r.patient_id) ?? [];
  const allOnPage = pageIds.length > 0 && pageIds.every((id) => selected.has(id));

  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Patient Aging</h1>
          <p className="mt-1 text-sm text-slate-600">
            Balances patients owe, by age from the <strong>date of service</strong>. Insurance balances are not included.
          </p>
        </div>

        <form onSubmit={submit} className={`${cardClass} grid gap-4 md:grid-cols-4`} aria-label="Filters">
          <div>
            <label htmlFor="pa-patient" className={labelClass}>Patient name</label>
            <input id="pa-patient" className={inputClass} value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
          </div>
          <div>
            <label htmlFor="pa-clinician" className={labelClass}>Clinician</label>
            <select id="pa-clinician" className={inputClass} value={filters.clinician_id} onChange={(e) => setFilters({ ...filters, clinician_id: e.target.value })}>
              <option value="">All clinicians</option>
              {clinicians.map((c) => (<option key={c.id} value={c.id}>{c.first_name} {c.last_name}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="pa-min" className={labelClass}>Minimum balance</label>
            <input id="pa-min" inputMode="decimal" className={inputClass} placeholder="0.00" value={filters.min_balance} onChange={(e) => setFilters({ ...filters, min_balance: e.target.value })} />
          </div>
          <div className="flex items-end gap-3">
            <button className={primaryButtonClass}>Run</button>
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>Clear</button>
          </div>
        </form>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        <AgingTotalsCards totals={report?.totals ?? null} active={applied.bucket} onSelect={pickBucket} />

        <div className="overflow-x-auto rounded-xl border bg-white">
          {!report ? (
            <div className="p-6 text-center text-slate-500">Loading report...</div>
          ) : report.items.length === 0 ? (
            <div className="p-6 text-center text-slate-500">No patient balances match these filters.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">
                    <input
                      type="checkbox"
                      aria-label="Select all on this page"
                      checked={allOnPage}
                      onChange={() => {
                        const next = new Set(selected);
                        pageIds.forEach((id) => (allOnPage ? next.delete(id) : next.add(id)));
                        setSelected(next);
                      }}
                    />
                  </th>
                  <th className="px-3 py-2">Patient</th>
                  <th className="px-3 py-2 text-right">0–30</th>
                  <th className="px-3 py-2 text-right">31–60</th>
                  <th className="px-3 py-2 text-right">61–90</th>
                  <th className="px-3 py-2 text-right">91+</th>
                  <th className="px-3 py-2 text-right">Balance</th>
                  <th className="px-3 py-2">Oldest service</th>
                  <th className="px-3 py-2">Last statement</th>
                  <th className="px-3 py-2"></th>
                </tr>
              </thead>
              <tbody>
                {report.items.map((row) => (
                  <tr key={row.patient_id} className="border-b last:border-0" data-testid="aging-row">
                    <td className="px-3 py-2">
                      <input type="checkbox" aria-label={`Select ${row.patient_name}`} checked={selected.has(row.patient_id)} onChange={() => toggle(row.patient_id)} />
                    </td>
                    <td className="px-3 py-2"><Link href={`/patients/${row.patient_id}/billing`} className="underline">{row.patient_name}</Link></td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(row.bucket_0_30)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(row.bucket_31_60)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(row.bucket_61_90)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(row.bucket_91_plus)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right font-medium">{money(row.total)}</td>
                    <td className="whitespace-nowrap px-3 py-2">{row.oldest_service_date}</td>
                    <td className="whitespace-nowrap px-3 py-2">{row.last_statement_date || "Never"}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">
                      <Link href={`/billing?patient_id=${row.patient_id}&has_patient_balance=true`} className="underline">Services</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {report && report.total > 0 && (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <nav aria-label="Pagination" className="flex items-center gap-4 text-sm">
              <span className="text-slate-600">{report.total} patient{report.total === 1 ? "" : "s"} · page {report.page} of {totalPages}</span>
              <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
              <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
            </nav>
            <div className="flex items-center gap-3 text-sm">
              <span role="status">{selected.size} selected</span>
              <button type="button" disabled={busy || selected.size === 0} className={primaryButtonClass} onClick={generate}>
                {busy ? "Generating..." : "Generate Statements for Selected"}
              </button>
            </div>
          </div>
        )}
      </div>
    </main>
  );
}
