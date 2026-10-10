"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import { apiFetch } from "@/lib/api";
import { downloadFile, openPdf } from "@/lib/download";
import { Badge, ErrorBox, SuccessBox, cardClass, inputClass, labelClass, linkButtonClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { PatientAgingRow, PatientStatement, agingBucketOptions, statementPeriod } from "@/types/statements";

type Paged<T> = { items: T[]; total: number; page: number; page_size: number };
type Option = { id: string; label: string };

const emptyFilters: Record<string, string> = { patient: "", clinician_id: "", min_balance: "", bucket: "" };

export default function BatchStatementsPage() {
  const [clinicians, setClinicians] = useState<Option[]>([]);
  const [filters, setFilters] = useState(emptyFilters);
  const [applied, setApplied] = useState(emptyFilters);
  const [page, setPage] = useState(1);
  const [candidates, setCandidates] = useState<Paged<PatientAgingRow> | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<PatientStatement[]>([]);
  const [skipped, setSkipped] = useState<{ patient_id: string; reason: string }[]>([]);
  const [recent, setRecent] = useState<Paged<PatientStatement> | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadClinicians() {
      const response = await apiFetch("/api/clinicians");
      if (response.ok) {
        setClinicians((await response.json()).map((x: { id: string; first_name: string; last_name: string }) => ({ id: x.id, label: `${x.first_name} ${x.last_name}` })));
      }
    }

    loadClinicians();
  }, []);

  useEffect(() => {
    let stale = false;

    async function load() {
      const query = new URLSearchParams({ page: String(page), page_size: "25" });
      Object.entries(applied).forEach(([k, v]) => v && query.set(k, v));

      const response = await apiFetch(`/api/billing/statement-candidates?${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to load patients");
        return;
      }

      setError("");
      setCandidates(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [applied, page, reloadKey]);

  useEffect(() => {
    let stale = false;

    async function load() {
      const response = await apiFetch("/api/billing/statements?page_size=10");
      const data = await response.json();

      if (!stale && response.ok) setRecent(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [reloadKey]);

  function toggle(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  }

  const pageIds = candidates?.items.map((c) => c.patient_id) ?? [];
  const allOnPage = pageIds.length > 0 && pageIds.every((id) => selected.has(id));

  function togglePage() {
    const next = new Set(selected);
    pageIds.forEach((id) => (allOnPage ? next.delete(id) : next.add(id)));
    setSelected(next);
  }

  async function generate() {
    setBusy(true);
    setError("");
    setMessage("");

    try {
      const response = await apiFetch("/api/billing/statements/batch", {
        method: "POST",
        body: JSON.stringify({ patient_ids: [...selected], comment }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to generate statements");
      }

      setCreated(data.created);
      setSkipped(data.skipped);
      setSelected(new Set());
      setMessage(`${data.created.length} statement${data.created.length === 1 ? "" : "s"} generated${data.skipped.length ? `, ${data.skipped.length} skipped` : ""}.`);
      setReloadKey((k) => k + 1);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to generate statements");
    } finally {
      setBusy(false);
    }
  }

  async function pdf(path: string, action: "open" | "download", name = "statements.pdf") {
    setError("");
    try {
      if (action === "open") await openPdf(path);
      else await downloadFile(path, name);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to open the PDF");
    }
  }

  const totalPages = candidates ? Math.max(1, Math.ceil(candidates.total / candidates.page_size)) : 1;
  const combinedPath = `/api/billing/statements/combined-pdf?ids=${created.map((s) => s.id).join(",")}`;

  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Patient Statements</h1>
          <p className="mt-1 text-sm text-slate-600">
            Patients with an open patient balance. Insurance-only balances are not listed. Each selected patient
            gets their own statement record.
          </p>
        </div>

        <form
          onSubmit={(e) => { e.preventDefault(); setPage(1); setApplied(filters); }}
          className={`${cardClass} grid gap-4 md:grid-cols-5`}
          aria-label="Filter patients"
        >
          <div>
            <label htmlFor="bs-patient" className={labelClass}>Patient name</label>
            <input id="bs-patient" className={inputClass} value={filters.patient} onChange={(e) => setFilters({ ...filters, patient: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bs-clinician" className={labelClass}>Clinician</label>
            <select id="bs-clinician" className={inputClass} value={filters.clinician_id} onChange={(e) => setFilters({ ...filters, clinician_id: e.target.value })}>
              <option value="">All clinicians</option>
              {clinicians.map((c) => (<option key={c.id} value={c.id}>{c.label}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="bs-min" className={labelClass}>Minimum balance</label>
            <input id="bs-min" inputMode="decimal" className={inputClass} placeholder="0.00" value={filters.min_balance} onChange={(e) => setFilters({ ...filters, min_balance: e.target.value })} />
          </div>
          <div>
            <label htmlFor="bs-bucket" className={labelClass}>Aging bucket</label>
            <select id="bs-bucket" className={inputClass} value={filters.bucket} onChange={(e) => setFilters({ ...filters, bucket: e.target.value })}>
              <option value="">Any age</option>
              {agingBucketOptions.map((o) => (<option key={o.value} value={o.value}>{o.label}</option>))}
            </select>
          </div>
          <div className="flex items-end gap-3">
            <button className={primaryButtonClass}>Search</button>
            <button type="button" className={secondaryButtonClass} onClick={() => { setFilters(emptyFilters); setApplied(emptyFilters); setPage(1); }}>Clear</button>
          </div>
        </form>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        <div className="overflow-x-auto rounded-xl border bg-white">
          {!candidates ? (
            <div className="p-6 text-center text-slate-500">Loading patients...</div>
          ) : candidates.items.length === 0 ? (
            <div className="p-6 text-center text-slate-500">No patients with an open patient balance match these filters.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">
                    <input type="checkbox" aria-label="Select all on this page" checked={allOnPage} onChange={togglePage} />
                  </th>
                  <th className="px-3 py-2">Patient</th>
                  <th className="px-3 py-2 text-right">0–30</th>
                  <th className="px-3 py-2 text-right">31–60</th>
                  <th className="px-3 py-2 text-right">61–90</th>
                  <th className="px-3 py-2 text-right">91+</th>
                  <th className="px-3 py-2 text-right">Balance</th>
                  <th className="px-3 py-2 text-right">Credit</th>
                  <th className="px-3 py-2">Last statement</th>
                </tr>
              </thead>
              <tbody>
                {candidates.items.map((c) => (
                  <tr key={c.patient_id} className="border-b last:border-0" data-testid="candidate-row">
                    <td className="px-3 py-2">
                      <input type="checkbox" aria-label={`Select ${c.patient_name}`} checked={selected.has(c.patient_id)} onChange={() => toggle(c.patient_id)} />
                    </td>
                    <td className="px-3 py-2">
                      <Link href={`/patients/${c.patient_id}/billing/statements`} className="underline">{c.patient_name}</Link>
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.bucket_0_30)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.bucket_31_60)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.bucket_61_90)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.bucket_91_plus)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right font-medium">{money(c.total)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.unallocated_credit)}</td>
                    <td className="whitespace-nowrap px-3 py-2">{c.last_statement_date || "Never"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {candidates && candidates.total > 0 && (
          <nav aria-label="Pagination" className="flex items-center justify-between text-sm">
            <span className="text-slate-600">{candidates.total} patient{candidates.total === 1 ? "" : "s"} · page {candidates.page} of {totalPages}</span>
            <div className="flex gap-2">
              <button type="button" className={secondaryButtonClass} disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>Previous</button>
              <button type="button" className={secondaryButtonClass} disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>Next</button>
            </div>
          </nav>
        )}

        <section className={`${cardClass} space-y-3`}>
          <h2 className="text-lg font-semibold text-slate-900">Generate statements</h2>
          <div>
            <label htmlFor="bs-comment" className={labelClass}>Message on each statement (optional)</label>
            <textarea id="bs-comment" rows={2} maxLength={500} className={inputClass} value={comment} onChange={(e) => setComment(e.target.value)} />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-sm text-slate-600" role="status">{selected.size} patient{selected.size === 1 ? "" : "s"} selected</span>
            <button type="button" disabled={busy || selected.size === 0} className={primaryButtonClass} onClick={generate}>
              {busy ? "Generating..." : "Generate Open Balance Statements"}
            </button>
          </div>
        </section>

        {created.length > 0 && (
          <section className={cardClass} aria-labelledby="batch-result">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 id="batch-result" className="text-lg font-semibold text-slate-900">Generated statements</h2>
              <div className="flex gap-3">
                <button type="button" className={secondaryButtonClass} onClick={() => pdf(combinedPath, "open")}>Open Combined PDF</button>
                <button type="button" className={secondaryButtonClass} onClick={() => pdf(combinedPath, "download", "statements-batch.pdf")}>Download Combined PDF</button>
              </div>
            </div>
            <ul className="mt-3 divide-y text-sm" data-testid="batch-created">
              {created.map((s) => (
                <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                  <span>{s.statement_number} · {s.patient_name} · balance {money(s.balance_due)}</span>
                  <button type="button" className={linkButtonClass} onClick={() => pdf(`/api/statements/${s.id}/pdf`, "open")}>View PDF</button>
                </li>
              ))}
            </ul>
            {skipped.length > 0 && (
              <p className="mt-3 text-sm text-amber-900">{skipped.length} patient{skipped.length === 1 ? " was" : "s were"} skipped: {skipped[0].reason}.</p>
            )}
          </section>
        )}

        <section>
          <h2 className="text-lg font-semibold text-slate-900">Recent statements</h2>
          <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
            {!recent ? (
              <div className="p-6 text-center text-slate-500">Loading...</div>
            ) : recent.items.length === 0 ? (
              <div className="p-6 text-center text-slate-500">No statements yet.</div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-3 py-2">Statement</th>
                    <th className="px-3 py-2">Patient</th>
                    <th className="px-3 py-2">Date</th>
                    <th className="px-3 py-2">Type</th>
                    <th className="px-3 py-2">Period</th>
                    <th className="px-3 py-2 text-right">Balance</th>
                  </tr>
                </thead>
                <tbody>
                  {recent.items.map((s) => (
                    <tr key={s.id} className="border-b last:border-0">
                      <td className="whitespace-nowrap px-3 py-2">{s.statement_number}</td>
                      <td className="px-3 py-2"><Link href={`/patients/${s.patient_id}/billing/statements`} className="underline">{s.patient_name}</Link></td>
                      <td className="whitespace-nowrap px-3 py-2">{s.statement_date}</td>
                      <td className="px-3 py-2"><Badge tone={s.statement_type === "open_balance" ? "blue" : "slate"}>{s.statement_type === "open_balance" ? "Open balance" : "Date range"}</Badge></td>
                      <td className="whitespace-nowrap px-3 py-2">{statementPeriod(s)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(s.balance_due)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>
      </div>
    </main>
  );
}
