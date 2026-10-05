"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { Charge, chargeStatusLabels } from "@/types/charges";
import { billingMethodLabel, billingMethodLabels } from "@/types/rates";

type Option = { id: string; label: string };

type Filters = Record<string, string>;

const emptyFilters: Filters = {
  patient: "",
  clinician_id: "",
  payer_id: "",
  service_code_id: "",
  from: "",
  to: "",
  billing_method: "",
  status: "",
};

type Paged = { items: Charge[]; total: number; page: number; page_size: number };

export default function TransactionSearch() {
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<Paged | null>(null);
  const [clinicians, setClinicians] = useState<Option[]>([]);
  const [payers, setPayers] = useState<Option[]>([]);
  const [codes, setCodes] = useState<Option[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadOptions() {
      const [c, p, s] = await Promise.all([apiFetch("/api/clinicians"), apiFetch("/api/payers"), apiFetch("/api/service-codes")]);

      if (c.ok) setClinicians((await c.json()).map((x: { id: string; first_name: string; last_name: string }) => ({ id: x.id, label: `${x.first_name} ${x.last_name}` })));
      if (p.ok) setPayers((await p.json()).map((x: { id: string; payer_name: string }) => ({ id: x.id, label: x.payer_name })));
      if (s.ok) setCodes((await s.json()).map((x: { id: string; code: string; description: string }) => ({ id: x.id, label: `${x.code} – ${x.description}` })));
    }

    loadOptions();
  }, []);

  useEffect(() => {
    // Ignore responses from superseded searches so a slow earlier request
    // can't overwrite newer results.
    let stale = false;

    async function search() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([key, value]) => value && query.set(key, value));

      const response = await apiFetch(`/api/billing/transactions?${query}`);
      const data = await response.json();

      if (stale) {
        return;
      }

      if (!response.ok) {
        setError(data.error || "Unable to search transactions");
        return;
      }

      setError("");
      setResult(data);
    }

    search();

    return () => {
      stale = true;
    };
  }, [applied, page]);

  function submit(event: FormEvent) {
    event.preventDefault();
    setPage(1);
    setApplied(filters);
  }

  function select(key: string, label: string, options: Option[], allLabel: string) {
    return (
      <div>
        <label htmlFor={`f-${key}`} className={labelClass}>{label}</label>
        <select id={`f-${key}`} className={inputClass} value={filters[key]} onChange={(e) => setFilters({ ...filters, [key]: e.target.value })}>
          <option value="">{allLabel}</option>
          {options.map((o) => (
            <option key={o.id} value={o.id}>{o.label}</option>
          ))}
        </select>
      </div>
    );
  }

  const totalPages = result ? Math.max(1, Math.ceil(result.total / result.page_size)) : 1;

  return (
    <section>
      <form onSubmit={submit} className="grid gap-4 rounded-xl border bg-white p-5 md:grid-cols-4">
        <div>
          <label htmlFor="f-patient" className={labelClass}>Patient</label>
          <input id="f-patient" className={inputClass} placeholder="Name" value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
        </div>
        {select("clinician_id", "Clinician", clinicians, "All clinicians")}
        {select("payer_id", "Payer", payers, "All payers")}
        {select("service_code_id", "Service", codes, "All services")}
        <div>
          <label htmlFor="f-from" className={labelClass}>From</label>
          <input id="f-from" type="date" className={inputClass} value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
        </div>
        <div>
          <label htmlFor="f-to" className={labelClass}>To</label>
          <input id="f-to" type="date" className={inputClass} value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
        </div>
        {select(
          "billing_method",
          "Billing method",
          [{ id: "insurance", label: "Any insurance" }, ...Object.entries(billingMethodLabels).map(([id, label]) => ({ id, label }))],
          "All methods",
        )}
        {select(
          "status",
          "Status",
          Object.entries(chargeStatusLabels).map(([id, s]) => ({ id, label: s.label })),
          "All statuses",
        )}
        <div className="flex gap-3 md:col-span-4 md:justify-end">
          <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>
            Clear
          </button>
          <button className={primaryButtonClass}>Search</button>
        </div>
      </form>

      <div className="mt-4">
        <ErrorBox message={error} />
      </div>

      <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
        {!result ? (
          <div className="p-8 text-center text-slate-500">Loading transactions...</div>
        ) : result.items.length === 0 ? (
          <div className="p-8 text-center text-slate-500">No billing transactions match these filters.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-3 py-3">Date</th>
                <th className="px-3 py-3">Patient</th>
                <th className="px-3 py-3">Service</th>
                <th className="px-3 py-3">Method</th>
                <th className="px-3 py-3 text-right">Charge</th>
                <th className="px-3 py-3 text-right">Patient Bal.</th>
                <th className="px-3 py-3 text-right">Ins. Bal.</th>
                <th className="px-3 py-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {result.items.map((c) => {
                const status = chargeStatusLabels[c.display_status] ?? { label: c.display_status, tone: "slate" as const };

                return (
                  <tr key={c.id} className="border-b align-top last:border-0">
                    <td className="whitespace-nowrap px-3 py-3">
                      <Link href={`/patients/${c.patient_id}/billing/charges/${c.id}`} className="underline">
                        {c.date_of_service}
                      </Link>
                    </td>
                    <td className="px-3 py-3">
                      <Link href={`/patients/${c.patient_id}/billing`} className="font-medium underline">
                        {c.patient_name}
                      </Link>
                    </td>
                    <td className="px-3 py-3">
                      {c.service_code}
                      <div className="text-xs text-slate-500">{c.clinician_name}</div>
                    </td>
                    <td className="px-3 py-3 text-xs">
                      {billingMethodLabel(c.billing_method)}
                      {c.payer_name && <div className="text-slate-500">{c.payer_name}</div>}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">{money(c.total_charge)}</td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">{money(c.balances.patient_balance)}</td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">{money(c.balances.insurance_balance)}</td>
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
          <span className="text-slate-600">
            {result.total} result{result.total === 1 ? "" : "s"} · page {result.page} of {totalPages}
          </span>
          <div className="flex gap-2">
            <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
              Previous
            </button>
            <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
              Next
            </button>
          </div>
        </nav>
      )}
    </section>
  );
}
