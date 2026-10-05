"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { Claim, claimStatus, claimStatusLabels, submissionMethodLabels } from "@/types/claims";

type Paged = { items: Claim[]; total: number; page: number; page_size: number };
type Filters = Record<string, string>;

const emptyFilters: Filters = {
  patient: "",
  claim_number: "",
  payer_id: "",
  clinician_id: "",
  status: "",
  sequence: "",
  submission_method: "",
  from: "",
  to: "",
};

type Props = {
  // Fixed filters (e.g. a patient page shows only that patient's claims).
  fixed?: Filters;
  initial?: Filters;
  showFilters?: boolean;
};

export default function ClaimList({ fixed = {}, initial = {}, showFilters = true }: Props) {
  const [filters, setFilters] = useState<Filters>({ ...emptyFilters, ...initial });
  const [applied, setApplied] = useState<Filters>({ ...emptyFilters, ...initial });
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Paged | null>(null);
  const [payers, setPayers] = useState<{ id: string; payer_name: string }[]>([]);
  const [clinicians, setClinicians] = useState<{ id: string; first_name: string; last_name: string }[]>([]);
  const [error, setError] = useState("");
  const fixedKey = JSON.stringify(fixed);

  useEffect(() => {
    if (!showFilters) return;

    async function loadOptions() {
      const [p, c] = await Promise.all([apiFetch("/api/payers"), apiFetch("/api/clinicians")]);
      if (p.ok) setPayers(await p.json());
      if (c.ok) setClinicians(await c.json());
    }

    loadOptions();
  }, [showFilters]);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries({ ...applied, ...JSON.parse(fixedKey) }).forEach(
        ([key, value]) => value && query.set(key, String(value)),
      );

      const response = await apiFetch(`/api/claims?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load claims");
        return;
      }

      setError("");
      setResult(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [applied, page, fixedKey]);

  function submit(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setApplied(filters);
  }

  function field(key: string, label: string, type = "text") {
    return (
      <div>
        <label htmlFor={`cf-${key}`} className={labelClass}>{label}</label>
        <input id={`cf-${key}`} type={type} className={inputClass} value={filters[key]} onChange={(e) => setFilters({ ...filters, [key]: e.target.value })} />
      </div>
    );
  }

  function select(key: string, label: string, options: [string, string][], all: string) {
    return (
      <div>
        <label htmlFor={`cf-${key}`} className={labelClass}>{label}</label>
        <select id={`cf-${key}`} className={inputClass} value={filters[key]} onChange={(e) => setFilters({ ...filters, [key]: e.target.value })}>
          <option value="">{all}</option>
          {options.map(([value, text]) => (
            <option key={value} value={value}>{text}</option>
          ))}
        </select>
      </div>
    );
  }

  const totalPages = result ? Math.max(1, Math.ceil(result.total / result.page_size)) : 1;

  return (
    <section>
      {showFilters && (
        <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-white p-5 md:grid-cols-4">
          {field("patient", "Patient")}
          {field("claim_number", "Claim number")}
          {select("payer_id", "Payer", payers.map((p) => [p.id, p.payer_name]), "All payers")}
          {select("clinician_id", "Clinician", clinicians.map((c) => [c.id, `${c.first_name} ${c.last_name}`]), "All clinicians")}
          {select("status", "Status", Object.entries(claimStatusLabels).map(([k, v]) => [k, v.label]), "All statuses")}
          {select("sequence", "Sequence", [["primary", "Primary"], ["secondary", "Secondary"], ["tertiary", "Tertiary"], ["quaternary", "Quaternary"]], "All sequences")}
          {field("from", "Service from", "date")}
          {field("to", "Service to", "date")}
          <div className="flex gap-3 md:col-span-4 md:justify-end">
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>
              Clear
            </button>
            <button className={primaryButtonClass}>Search</button>
          </div>
        </form>
      )}

      <div className="mt-4"><ErrorBox message={error} /></div>

      <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
        {!result ? (
          <div className="p-8 text-center text-slate-500">Loading claims...</div>
        ) : result.items.length === 0 ? (
          <div className="p-8 text-center text-slate-500">No claims found.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-3 py-3">Claim</th>
                <th className="px-3 py-3">Patient</th>
                <th className="px-3 py-3">Payer</th>
                <th className="px-3 py-3">Service dates</th>
                <th className="px-3 py-3 text-right">Billed</th>
                <th className="px-3 py-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {result.items.map((c) => {
                const status = claimStatus(c.status);
                return (
                  <tr key={c.id} className="border-b align-top last:border-0">
                    <td className="whitespace-nowrap px-3 py-3">
                      <Link href={`/billing/claims/${c.id}`} className="font-medium underline">{c.claim_number}</Link>
                      <div className="text-xs capitalize text-slate-500">
                        {c.sequence} · {submissionMethodLabels[c.submission_method]}
                      </div>
                    </td>
                    <td className="px-3 py-3">{c.patient_name}</td>
                    <td className="px-3 py-3">{c.payer_name}</td>
                    <td className="whitespace-nowrap px-3 py-3">
                      {c.first_date_of_service}
                      {c.last_date_of_service !== c.first_date_of_service && ` – ${c.last_date_of_service}`}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">{money(c.total_billed)}</td>
                    <td className="px-3 py-3">
                      <Badge tone={status.tone}>{status.label}</Badge>
                      {c.validation && c.validation.errors.length > 0 && (
                        <div className="mt-1 text-xs text-red-700">{c.validation.errors.length} error(s)</div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {result && result.total > 0 && (
        <nav aria-label="Pagination" className="mt-4 flex items-center justify-between text-sm">
          <span className="text-slate-600">
            {result.total} claim{result.total === 1 ? "" : "s"} · page {result.page} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
            <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
          </div>
        </nav>
      )}
    </section>
  );
}
