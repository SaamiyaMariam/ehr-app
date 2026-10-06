"use client";

import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import {
  EntryLine,
  OutstandingLine,
  centsToMoney,
  entryLineFrom,
  lineAmounts,
  otherAdjustmentOptions,
  remainingAfter,
} from "@/types/insurance-payments";

type Paged = { items: OutstandingLine[]; total: number };

const emptyFilters = { patient: "", claim_number: "", from: "", to: "" };

// Finds claim lines the payer still owes on and lets the biller adjudicate
// them. Every figure shown here is a helper; the backend validates and
// computes the real balances.
export default function RemittanceLines({
  payerId,
  lines,
  onChange,
  disabled,
}: {
  payerId: string;
  lines: EntryLine[];
  onChange: (lines: EntryLine[]) => void;
  disabled?: boolean;
}) {
  const [filters, setFilters] = useState(emptyFilters);
  const [applied, setApplied] = useState(emptyFilters);
  const [result, setResult] = useState<Paged | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!payerId) return;

    let stale = false;

    async function load() {
      const query = new URLSearchParams({ payer_id: payerId, page_size: "100" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/billing/outstanding-insurance?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load outstanding claims");
        return;
      }

      setError("");
      setResult(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [payerId, applied]);

  function search() {
    setApplied(filters);
  }

  const chosen = new Set(lines.map((l) => l.claim_line_id));

  function update(index: number, patch: Partial<EntryLine>) {
    onChange(lines.map((l, i) => (i === index ? { ...l, ...patch } : l)));
  }

  function numberField(index: number, line: EntryLine, field: keyof EntryLine, label: string, id: string) {
    return (
      <div>
        <label htmlFor={id} className={`${labelClass} text-xs`}>{label}</label>
        <input
          id={id}
          inputMode="decimal"
          disabled={disabled}
          className={inputClass}
          value={line[field] as string}
          onChange={(e) => update(index, { [field]: e.target.value })}
        />
      </div>
    );
  }

  const items = (result?.items ?? []).filter((i) => i.payer_id === payerId);

  return (
    <div className="space-y-6">
      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Find Claims to Post Against</h2>
        <p className="mt-1 text-sm text-slate-600">
          Submitted claims for this payer that still carry an insurance balance.
        </p>

        {!payerId ? (
          <p className="mt-4 text-sm text-slate-500">Choose a payer first.</p>
        ) : (
          <>
            {/* Not a <form>: this component renders inside the payment form. */}
            <div
              role="search"
              aria-label="Find claims"
              className="mt-4 grid gap-3 md:grid-cols-5"
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  search();
                }
              }}
            >
              <div>
                <label htmlFor="out-patient" className={labelClass}>Patient name</label>
                <input id="out-patient" className={inputClass} value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
              </div>
              <div>
                <label htmlFor="out-claim" className={labelClass}>Claim number</label>
                <input id="out-claim" className={inputClass} value={filters.claim_number} onChange={(e) => setFilters({ ...filters, claim_number: e.target.value })} />
              </div>
              <div>
                <label htmlFor="out-from" className={labelClass}>Service from</label>
                <input id="out-from" type="date" className={inputClass} value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
              </div>
              <div>
                <label htmlFor="out-to" className={labelClass}>Service to</label>
                <input id="out-to" type="date" className={inputClass} value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
              </div>
              <div className="flex items-end gap-2">
                <button type="button" className={primaryButtonClass} onClick={search}>Find</button>
                <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); }}>Clear</button>
              </div>
            </div>

            <div className="mt-3"><ErrorBox message={error} /></div>

            <div className="mt-3 overflow-x-auto rounded-lg border">
              {!result ? (
                <div className="p-4 text-center text-sm text-slate-500">Loading claims...</div>
              ) : items.length === 0 ? (
                <div className="p-4 text-center text-sm text-slate-500">No outstanding claim lines for this payer.</div>
              ) : (
                <table className="w-full text-left text-sm">
                  <thead className="border-b bg-slate-50">
                    <tr>
                      <th className="px-3 py-2">Claim</th>
                      <th className="px-3 py-2">Patient</th>
                      <th className="px-3 py-2">Service date</th>
                      <th className="px-3 py-2">Service</th>
                      <th className="px-3 py-2 text-right">Billed</th>
                      <th className="px-3 py-2 text-right">Ins. balance</th>
                      <th className="px-3 py-2"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((l) => (
                      <tr key={l.claim_line_id} className="border-b last:border-0">
                        <td className="whitespace-nowrap px-3 py-2">
                          {l.claim_number} <span className="text-xs capitalize text-slate-500">{l.sequence}</span>
                          {l.adjudicated && <div><Badge tone="amber">Adjudicated</Badge></div>}
                        </td>
                        <td className="px-3 py-2">{l.patient_name}</td>
                        <td className="whitespace-nowrap px-3 py-2">{l.date_of_service}</td>
                        <td className="px-3 py-2">{l.service_code}</td>
                        <td className="whitespace-nowrap px-3 py-2 text-right">{money(l.billed)}</td>
                        <td className="whitespace-nowrap px-3 py-2 text-right">{money(l.insurance_balance)}</td>
                        <td className="px-3 py-2 text-right">
                          <button
                            type="button"
                            disabled={disabled || chosen.has(l.claim_line_id)}
                            className={secondaryButtonClass}
                            aria-label={`Add ${l.claim_number} line ${l.line_number}`}
                            onClick={() => onChange([...lines, entryLineFrom(l)])}
                          >
                            {chosen.has(l.claim_line_id) ? "Added" : "Add"}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </>
        )}
      </section>

      <section>
        <h2 className="text-lg font-semibold text-slate-900">Remittance Lines</h2>

        {lines.length === 0 && (
          <p className="mt-2 text-sm text-slate-500">No lines yet. Add claim lines above to allocate this payment.</p>
        )}

        <div className="mt-3 space-y-4">
          {lines.map((line, index) => {
            const amounts = lineAmounts(line);
            const remaining = remainingAfter(line);
            const key = line.claim_line_id;

            return (
              <div key={key} className={cardClass} data-testid={`remit-line-${index}`}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="text-sm">
                    <div className="font-medium text-slate-900">
                      {line.claim_number} · {line.patient_name}
                    </div>
                    <div className="text-slate-600">
                      {line.date_of_service} · {line.service_code} · Billed {money(line.billed)} · Insurance balance {money(line.insurance_balance)}
                    </div>
                  </div>
                  <button type="button" disabled={disabled} className="text-sm font-medium text-slate-700 underline" onClick={() => onChange(lines.filter((_, i) => i !== index))}>
                    Remove
                  </button>
                </div>

                <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {numberField(index, line, "paid", "Paid by insurance", `paid-${key}`)}
                  {numberField(index, line, "allowed", "Allowed amount", `allowed-${key}`)}
                  {numberField(index, line, "contractual", "Contractual write-off", `contractual-${key}`)}
                  <div className="grid grid-cols-2 gap-2">
                    {numberField(index, line, "other", "Other adjustment", `other-${key}`)}
                    <div>
                      <label htmlFor={`other-type-${key}`} className={`${labelClass} text-xs`}>Type</label>
                      <select id={`other-type-${key}`} disabled={disabled} className={inputClass} value={line.other_type} onChange={(e) => update(index, { other_type: e.target.value })}>
                        {otherAdjustmentOptions.map((o) => (<option key={o.value} value={o.value}>{o.label}</option>))}
                      </select>
                    </div>
                  </div>
                  {numberField(index, line, "deductible", "Deductible → patient", `deductible-${key}`)}
                  {numberField(index, line, "copay", "Copay → patient", `copay-${key}`)}
                  {numberField(index, line, "coinsurance", "Coinsurance → patient", `coinsurance-${key}`)}
                  {numberField(index, line, "noncovered", "Non-covered → patient", `noncovered-${key}`)}
                </div>

                <div className="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm">
                  <label className="flex items-center gap-2">
                    <input type="checkbox" disabled={disabled} checked={line.final} onChange={(e) => update(index, { final: e.target.checked })} />
                    Payer is finished with this line (final adjudication)
                  </label>
                  <div className={amounts.invalid || remaining < 0 ? "font-medium text-red-700" : "text-slate-700"} role="status">
                    {amounts.invalid
                      ? "Enter amounts like 12.34"
                      : remaining < 0
                        ? `Exceeds the insurance balance by ${centsToMoney(-remaining)}`
                        : `Insurance balance after posting: ${centsToMoney(remaining)}`}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}
