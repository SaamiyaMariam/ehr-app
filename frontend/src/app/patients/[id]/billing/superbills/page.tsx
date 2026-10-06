"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { downloadFile } from "@/lib/download";
import { ErrorBox, SuccessBox, linkButtonClass, money, primaryButtonClass } from "@/lib/ui";
import { Charge } from "@/types/charges";
import { formatTimestamp } from "@/types/claims";
import { billingMethodLabel } from "@/types/rates";

type Superbill = {
  id: string;
  total_charges: string;
  total_paid: string;
  service_count: number;
  first_date_of_service: string;
  last_date_of_service: string;
  generated_by: string;
  created_at: string;
};

export default function SuperbillsPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [charges, setCharges] = useState<Charge[] | null>(null);
  const [superbills, setSuperbills] = useState<Superbill[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [reloadKey, setReloadKey] = useState(0);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const [c, s] = await Promise.all([
        apiFetch(`/api/patients/${params.id}/superbill-charges`),
        apiFetch(`/api/patients/${params.id}/superbills`),
      ]);

      if (c.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      if (!c.ok || !s.ok) {
        setError("Unable to load superbills");
        return;
      }

      setCharges(await c.json());
      setSuperbills(await s.json());
    }

    load();
  }, [params.id, router, reloadKey]);

  async function generate() {
    setMessage("");
    setError("");
    setBusy(true);

    try {
      const response = await apiFetch(`/api/patients/${params.id}/superbills`, {
        method: "POST",
        body: JSON.stringify({ charge_ids: selected }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to generate superbill");
      }

      setSelected([]);
      setMessage(`Superbill generated for ${data.service_count} service(s).`);
      setReloadKey((k) => k + 1);
      await downloadFile(`/api/superbills/${data.id}/pdf`, "superbill.pdf");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to generate superbill");
    } finally {
      setBusy(false);
    }
  }

  async function download(id: string) {
    setError("");
    try {
      await downloadFile(`/api/superbills/${id}/pdf`, "superbill.pdf");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Download failed");
    }
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">
            ← Back to Billing
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Superbills</h1>
          <p className="mt-1 text-sm text-slate-500">
            Itemized statements of services the patient can submit to their insurance. Only
            direct (self-pay) and out-of-network services are eligible.
          </p>
        </div>

        <SuccessBox message={message} />
        <ErrorBox message={error} />

        <section>
          <h2 className="text-lg font-semibold text-slate-900">Eligible Services</h2>
          <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
            {charges === null ? (
              <div className="p-6 text-center text-slate-500">Loading services...</div>
            ) : charges.length === 0 ? (
              <div className="p-6 text-center text-slate-500">No direct or out-of-network services.</div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-3 py-2"><span className="sr-only">Select</span></th>
                    <th className="px-3 py-2">Date</th>
                    <th className="px-3 py-2">Service</th>
                    <th className="px-3 py-2">Method</th>
                    <th className="px-3 py-2 text-right">Fee</th>
                    <th className="px-3 py-2 text-right">Patient paid</th>
                  </tr>
                </thead>
                <tbody>
                  {charges.map((c) => (
                    <tr key={c.id} className="border-b last:border-0">
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          aria-label={`Include ${c.service_code} on ${c.date_of_service}`}
                          checked={selected.includes(c.id)}
                          onChange={(e) =>
                            setSelected((cur) => (e.target.checked ? [...cur, c.id] : cur.filter((x) => x !== c.id)))
                          }
                        />
                      </td>
                      <td className="whitespace-nowrap px-3 py-2">{c.date_of_service}</td>
                      <td className="px-3 py-2">{c.service_code} – {c.service_description}</td>
                      <td className="px-3 py-2 text-xs">{billingMethodLabel(c.billing_method)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.total_charge)}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.balances.patient_payments)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          <div className="mt-3 flex justify-end">
            <button type="button" onClick={generate} disabled={busy || selected.length === 0} className={primaryButtonClass}>
              {busy ? "Generating..." : `Generate Superbill (${selected.length})`}
            </button>
          </div>
        </section>

        <section>
          <h2 className="text-lg font-semibold text-slate-900">Generated Superbills</h2>
          <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
            {superbills.length === 0 ? (
              <div className="p-6 text-center text-slate-500">No superbills generated yet.</div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-3 py-2">Generated</th>
                    <th className="px-3 py-2">Service dates</th>
                    <th className="px-3 py-2 text-right">Services</th>
                    <th className="px-3 py-2 text-right">Fees</th>
                    <th className="px-3 py-2"></th>
                  </tr>
                </thead>
                <tbody>
                  {superbills.map((s) => (
                    <tr key={s.id} className="border-b last:border-0">
                      <td className="px-3 py-2">{formatTimestamp(s.created_at)}<div className="text-xs text-slate-500">{s.generated_by}</div></td>
                      <td className="whitespace-nowrap px-3 py-2">{s.first_date_of_service} – {s.last_date_of_service}</td>
                      <td className="px-3 py-2 text-right">{s.service_count}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right">{money(s.total_charges)}</td>
                      <td className="px-3 py-2 text-right">
                        <button type="button" onClick={() => download(s.id)} className={linkButtonClass}>Download PDF</button>
                      </td>
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
