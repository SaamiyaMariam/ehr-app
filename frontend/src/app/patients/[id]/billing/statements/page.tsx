"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import FormDialog from "@/components/form-dialog";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { downloadFile, openPdf } from "@/lib/download";
import { Badge, ErrorBox, SuccessBox, cardClass, linkButtonClass, money, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { formatTimestamp } from "@/types/claims";
import { PatientStatement, statementPeriod } from "@/types/statements";

export default function PatientStatementsPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [statements, setStatements] = useState<PatientStatement[] | null>(null);
  const [patientName, setPatientName] = useState("");
  const [dialog, setDialog] = useState<"open" | "range" | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    async function load() {
      const [list, patient] = await Promise.all([
        apiFetch(`/api/patients/${params.id}/statements`),
        apiFetch(`/api/patients/${params.id}`),
      ]);

      if (list.status === 401 || patient.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await list.json();

      if (!list.ok) {
        setError(data.error || "Unable to load statements");
        return;
      }

      setStatements(data);

      if (patient.ok) {
        const p = await patient.json();
        setPatientName(`${p.first_name} ${p.last_name}`);
      }
    }

    load();
  }, [params.id, router, reloadKey]);

  async function generate(body: Record<string, string>) {
    setMessage("");
    setError("");

    const response = await apiFetch(`/api/patients/${params.id}/statements`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to generate the statement");
    }

    setMessage(`Statement ${data.statement_number} generated.`);
    setReloadKey((k) => k + 1);
  }

  async function view(s: PatientStatement) {
    setError("");
    try {
      await openPdf(`/api/patients/${params.id}/statements/${s.id}/pdf`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to open the PDF");
    }
  }

  async function download(s: PatientStatement) {
    setError("");
    try {
      await downloadFile(`/api/patients/${params.id}/statements/${s.id}/pdf`, `${s.statement_number}.pdf`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to download the PDF");
    }
  }

  const today = new Date().toLocaleDateString("en-CA");

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">← Back to Billing</Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold text-slate-900">Statements</h1>
            {patientName && <p className="mt-1 text-sm text-slate-600">{patientName}</p>}
          </div>
          <div className="flex flex-wrap gap-3">
            <button type="button" className={primaryButtonClass} onClick={() => setDialog("open")}>Generate Open Balance Statement</button>
            <button type="button" className={secondaryButtonClass} onClick={() => setDialog("range")}>Generate Date Range Statement</button>
          </div>
        </div>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        <section className={cardClass}>
          <p className="text-sm text-slate-600">
            A statement is a permanent record of what the patient owed when it was generated. Payments posted
            later appear on the next statement; an old statement never changes. Only patient responsibility is
            shown, never insurance balances.
          </p>
        </section>

        <div className="overflow-x-auto rounded-xl border bg-white">
          {statements === null ? (
            <div className="p-6 text-center text-slate-500">Loading statements...</div>
          ) : statements.length === 0 ? (
            <div className="p-6 text-center text-slate-500">No statements have been generated for this patient.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">Statement</th>
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Type</th>
                  <th className="px-3 py-2">Period</th>
                  <th className="px-3 py-2 text-right">Balance at generation</th>
                  <th className="px-3 py-2 text-right">Credit</th>
                  <th className="px-3 py-2">Generated</th>
                  <th className="px-3 py-2"></th>
                </tr>
              </thead>
              <tbody>
                {statements.map((s) => (
                  <tr key={s.id} className="border-b last:border-0" data-testid="statement-row">
                    <td className="whitespace-nowrap px-3 py-2 font-medium">{s.statement_number}</td>
                    <td className="whitespace-nowrap px-3 py-2">{s.statement_date}</td>
                    <td className="px-3 py-2"><Badge tone={s.statement_type === "open_balance" ? "blue" : "slate"}>{s.statement_type === "open_balance" ? "Open balance" : "Date range"}</Badge></td>
                    <td className="whitespace-nowrap px-3 py-2">{statementPeriod(s)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right" data-testid="statement-balance">{money(s.balance_due)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(s.credit_on_account)}</td>
                    <td className="px-3 py-2 text-xs text-slate-500">{formatTimestamp(s.created_at)}{s.generated_by && ` · ${s.generated_by}`}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">
                      <button type="button" className={linkButtonClass} onClick={() => view(s)} aria-label={`Open PDF for ${s.statement_number}`}>View PDF</button>
                      <button type="button" className={`${linkButtonClass} ml-4`} onClick={() => download(s)} aria-label={`Download ${s.statement_number}`}>Download</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {dialog === "open" && (
        <FormDialog
          open
          title="Generate open balance statement"
          description="Lists every service with an open patient balance right now."
          confirmLabel="Generate"
          fields={[{ name: "comment", label: "Message on statement (optional)", type: "textarea", maxLength: 500 }]}
          onSubmit={(v) => generate({ statement_type: "open_balance", comment: v.comment })}
          onClose={() => setDialog(null)}
        />
      )}

      {dialog === "range" && (
        <FormDialog
          open
          title="Generate date range statement"
          description="Patient activity between the dates (charges by service date, payments by payment date, adjustments by the day they were recorded), with the balance as of the end date."
          confirmLabel="Generate"
          fields={[
            { name: "start_date", label: "Start date", type: "date", required: true },
            { name: "end_date", label: "End date", type: "date", required: true, defaultValue: today },
            { name: "comment", label: "Message on statement (optional)", type: "textarea", maxLength: 500 },
          ]}
          onSubmit={(v) => generate({ statement_type: "date_range", ...v })}
          onClose={() => setDialog(null)}
        />
      )}
    </main>
  );
}
