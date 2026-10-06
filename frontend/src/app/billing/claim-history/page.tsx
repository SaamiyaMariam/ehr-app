"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, inputClass, labelClass, primaryButtonClass, secondaryButtonClass, titleCase } from "@/lib/ui";
import { ClaimHistoryEvent, claimStatus, claimStatusLabels, formatTimestamp, submissionMethodLabels } from "@/types/claims";

type Item = ClaimHistoryEvent & {
  claim_number: string;
  patient_id: string;
  patient_name: string;
  payer_name: string;
  sequence: string;
  claim_status: string;
  submission_method: string;
};

type Paged = { items: Item[]; total: number; page: number; page_size: number };

const emptyFilters: Record<string, string> = {
  patient: "",
  claim_number: "",
  payer_id: "",
  clinician_id: "",
  status: "",
  sequence: "",
  event_type: "",
  event_from: "",
  event_to: "",
};

const eventTypes = [
  "created", "validated", "cms1500_generated", "cms1500_regenerated", "mailed", "submitted",
  "submitted_externally", "queued", "rejected", "reviewed", "resubmission_started", "paid", "cancelled",
];

export default function ClaimHistoryPage() {
  const [filters, setFilters] = useState(emptyFilters);
  const [applied, setApplied] = useState(emptyFilters);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Paged | null>(null);
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
      const query = new URLSearchParams({ page: String(page), page_size: "30" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/claim-history?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load claim history");
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

  function submit(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setApplied(filters);
  }

  function input(key: string, label: string, type = "text") {
    return (
      <div>
        <label htmlFor={`ch-${key}`} className={labelClass}>{label}</label>
        <input id={`ch-${key}`} type={type} className={inputClass} value={filters[key]} onChange={(e) => setFilters({ ...filters, [key]: e.target.value })} />
      </div>
    );
  }

  function select(key: string, label: string, options: [string, string][], all: string) {
    return (
      <div>
        <label htmlFor={`ch-${key}`} className={labelClass}>{label}</label>
        <select id={`ch-${key}`} className={inputClass} value={filters[key]} onChange={(e) => setFilters({ ...filters, [key]: e.target.value })}>
          <option value="">{all}</option>
          {options.map(([v, t]) => (
            <option key={v} value={v}>{t}</option>
          ))}
        </select>
      </div>
    );
  }

  const totalPages = result ? Math.max(1, Math.ceil(result.total / result.page_size)) : 1;

  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Claim History</h1>
        <p className="mt-1 text-sm text-slate-500">Every claim lifecycle event, newest first.</p>

        <form onSubmit={submit} className="mt-6 grid gap-4 rounded-xl border bg-white p-5 md:grid-cols-3 lg:grid-cols-5">
          {input("patient", "Patient")}
          {input("claim_number", "Claim number")}
          {select("payer_id", "Payer", payers.map((p) => [p.id, p.payer_name]), "All payers")}
          {select("clinician_id", "Clinician", clinicians.map((c) => [c.id, `${c.first_name} ${c.last_name}`]), "All clinicians")}
          {select("status", "Current claim status", Object.entries(claimStatusLabels).map(([k, v]) => [k, v.label]), "All statuses")}
          {select("sequence", "Sequence", [["primary", "Primary"], ["secondary", "Secondary"], ["tertiary", "Tertiary"], ["quaternary", "Quaternary"]], "All sequences")}
          {select("event_type", "Event", eventTypes.map((e) => [e, titleCase(e)]), "All events")}
          {input("event_from", "Event from", "date")}
          {input("event_to", "Event to", "date")}
          <div className="flex items-end gap-3">
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>Clear</button>
            <button className={primaryButtonClass}>Search</button>
          </div>
        </form>

        <div className="mt-4"><ErrorBox message={error} /></div>

        <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
          {!result ? (
            <div className="p-8 text-center text-slate-500">Loading claim history...</div>
          ) : result.items.length === 0 ? (
            <div className="p-8 text-center text-slate-500">No claim events match these filters.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-3">When</th>
                  <th className="px-3 py-3">Claim</th>
                  <th className="px-3 py-3">Event</th>
                  <th className="px-3 py-3">Details</th>
                  <th className="px-3 py-3">Claim status</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map((e) => {
                  const status = claimStatus(e.claim_status);
                  return (
                    <tr key={e.id} className="border-b align-top last:border-0">
                      <td className="whitespace-nowrap px-3 py-3">
                        {formatTimestamp(e.created_at)}
                        <div className="text-xs text-slate-500">{e.source}{e.created_by && ` · ${e.created_by}`}</div>
                      </td>
                      <td className="px-3 py-3">
                        <Link href={`/billing/claims/${e.claim_id}`} className="font-medium underline">{e.claim_number}</Link>
                        <div className="text-xs text-slate-500">
                          {e.patient_name} · {e.payer_name} · <span className="capitalize">{e.sequence}</span> · {submissionMethodLabels[e.submission_method]}
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <div className="font-medium">{titleCase(e.event_type)}</div>
                        {e.to_status && e.to_status !== e.from_status && (
                          <div className="text-xs text-slate-500">→ {claimStatus(e.to_status).label}</div>
                        )}
                      </td>
                      <td className="max-w-md px-3 py-3 text-slate-700">{e.message}</td>
                      <td className="px-3 py-3"><Badge tone={status.tone}>{status.label}</Badge></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {result && result.total > 0 && (
          <nav aria-label="Pagination" className="mt-4 flex items-center justify-between text-sm">
            <span className="text-slate-600">{result.total} event{result.total === 1 ? "" : "s"} · page {result.page} of {totalPages}</span>
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
