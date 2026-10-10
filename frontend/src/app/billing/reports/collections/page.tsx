"use client";

import { FormEvent, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass } from "@/lib/ui";
import { CollectionsLine, CollectionsReport } from "@/types/reports";

function monthStart() {
  const d = new Date();
  return new Date(d.getFullYear(), d.getMonth(), 1).toLocaleDateString("en-CA");
}

function Stat({ line, id, hint }: { line: CollectionsLine; id: string; hint?: string }) {
  return (
    <div className={cardClass} data-testid={id}>
      <div className="text-sm text-slate-500">{line.label}</div>
      <div className="mt-1 text-2xl font-semibold text-slate-900" data-testid={`${id}-amount`}>{money(line.amount)}</div>
      <div className="mt-1 text-xs text-slate-500">{line.count} item{line.count === 1 ? "" : "s"}{hint ? ` · ${hint}` : ""}</div>
    </div>
  );
}

function Breakdown({ title, lines }: { title: string; lines: CollectionsLine[] }) {
  return (
    <div className="overflow-hidden rounded-xl border bg-white">
      <h3 className="border-b bg-slate-50 px-4 py-2 font-semibold text-slate-900">{title}</h3>
      {lines.length === 0 ? (
        <p className="p-4 text-sm text-slate-500">Nothing in this period.</p>
      ) : (
        <table className="w-full text-left text-sm">
          <tbody>
            {lines.map((l) => (
              <tr key={l.label} className="border-b last:border-0">
                <td className="px-4 py-2">{l.label}</td>
                <td className="px-4 py-2 text-right text-slate-500">{l.count}</td>
                <td className="px-4 py-2 text-right font-medium">{money(l.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

export default function CollectionsPage() {
  const [from, setFrom] = useState(monthStart);
  const [to, setTo] = useState(() => new Date().toLocaleDateString("en-CA"));
  const [applied, setApplied] = useState({ from: monthStart(), to: new Date().toLocaleDateString("en-CA") });
  const [report, setReport] = useState<CollectionsReport | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams(applied);
      const response = await apiFetch(`/api/billing/reports/collections?${query}`);
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
  }, [applied]);

  function submit(event: FormEvent) {
    event.preventDefault();
    setApplied({ from, to });
  }

  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Collections Summary</h1>
          <p className="mt-1 text-sm text-slate-600">
            Money received, separate from what was billed. Charges are billed amounts, not revenue; adjustments and
            write-offs are not payments.
          </p>
        </div>

        <form onSubmit={submit} className={`${cardClass} flex flex-wrap items-end gap-4`} aria-label="Date range">
          <div>
            <label htmlFor="col-from" className={labelClass}>From</label>
            <input id="col-from" type="date" required className={inputClass} value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
          <div>
            <label htmlFor="col-to" className={labelClass}>To</label>
            <input id="col-to" type="date" required className={inputClass} value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
          <button className={primaryButtonClass}>Run</button>
        </form>

        <ErrorBox message={error} />

        {report && (
          <>
            <section aria-labelledby="collected-heading">
              <h2 id="collected-heading" className="text-lg font-semibold text-slate-900">Collections</h2>
              <div className="mt-3 grid gap-4 md:grid-cols-4">
                <Stat line={report.patient_payments} id="col-patient" hint="by payment date" />
                <Stat line={report.insurance_payments} id="col-insurance" hint="by payment date" />
                <Stat line={report.refunds} id="col-refunds" hint="subtracted" />
                <div className={`${cardClass} border-slate-900`} data-testid="col-total">
                  <div className="text-sm text-slate-500">Total collections</div>
                  <div className="mt-1 text-2xl font-semibold text-slate-900" data-testid="col-total-amount">{money(report.total_collections)}</div>
                  <div className="mt-1 text-xs text-slate-500">Patient + insurance − refunds</div>
                </div>
              </div>
            </section>

            <section aria-labelledby="billed-heading">
              <h2 id="billed-heading" className="text-lg font-semibold text-slate-900">Charges and adjustments (not collections)</h2>
              <div className="mt-3 grid gap-4 md:grid-cols-3">
                <Stat line={report.charges_created} id="col-charges" hint="billed, by entry date" />
                <Stat line={report.writeoffs} id="col-writeoffs" hint="by date recorded" />
                <Stat line={report.adjustments} id="col-adjustments" hint="payer / manual" />
              </div>
            </section>

            <section className="grid gap-4 md:grid-cols-2">
              <Breakdown title="Insurance payments by payer" lines={report.insurance_by_payer} />
              <Breakdown title="Patient payments by method" lines={report.patient_by_method} />
            </section>

            <details className={cardClass}>
              <summary className="cursor-pointer text-sm font-medium text-slate-900">How these numbers are dated</summary>
              <ul className="mt-3 list-inside list-disc space-y-1 text-sm text-slate-600">
                {Object.entries(report.definitions).map(([k, v]) => (<li key={k}>{v}</li>))}
              </ul>
            </details>
          </>
        )}
      </div>
    </main>
  );
}
