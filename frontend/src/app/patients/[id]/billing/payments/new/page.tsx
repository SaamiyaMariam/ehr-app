"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass } from "@/lib/ui";
import { Charge } from "@/types/charges";
import { newIdempotencyKey, paymentMethodOptions } from "@/types/payments";

export default function NewPatientPaymentPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  // One key per form instance: a double submit / retry cannot post twice.
  const [idempotencyKey] = useState(newIdempotencyKey);
  const [paymentDate, setPaymentDate] = useState(() => new Date().toLocaleDateString("en-CA"));
  const [amount, setAmount] = useState("");
  const [method, setMethod] = useState("cash");
  const [checkNumber, setCheckNumber] = useState("");
  const [reference, setReference] = useState("");
  const [notes, setNotes] = useState("");
  const [mode, setMode] = useState<"auto" | "manual" | "none">("auto");
  const [allocations, setAllocations] = useState<Record<string, string>>({});
  const [openCharges, setOpenCharges] = useState<Charge[] | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patients/${params.id}/billing-transactions`);

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data: Charge[] = await response.json();
      setOpenCharges(
        data
          .filter((c) => c.status === "active" && c.balances.patient_balance !== "0.00" && !c.balances.patient_balance.startsWith("-"))
          .sort((a, b) => a.date_of_service.localeCompare(b.date_of_service)),
      );
    }

    load();
  }, [params.id, router]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch(`/api/patients/${params.id}/payments`, {
        method: "POST",
        body: JSON.stringify({
          payment_date: paymentDate,
          amount,
          method,
          check_number: checkNumber,
          reference_number: reference,
          notes,
          idempotency_key: idempotencyKey,
          auto_allocate: mode === "auto",
          allocations:
            mode === "manual"
              ? Object.entries(allocations)
                  .filter(([, v]) => v.trim() !== "")
                  .map(([charge_id, value]) => ({ charge_id, amount: value.trim() }))
              : [],
        }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to post payment");
      }

      router.push("/billing/payments");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to post payment");
      setSaving(false);
    }
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">
            ← Back to Billing
          </Link>
        </div>
      </nav>

      <form onSubmit={submit} className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Enter Patient Payment</h1>

        <section className={cardClass}>
          <h2 className="text-lg font-semibold text-slate-900">Payment</h2>
          <div className="mt-5 grid gap-4 md:grid-cols-3">
            <div>
              <label htmlFor="pay-date" className={labelClass}>Payment date *</label>
              <input id="pay-date" type="date" required className={inputClass} value={paymentDate} onChange={(e) => setPaymentDate(e.target.value)} />
            </div>
            <div>
              <label htmlFor="pay-amount" className={labelClass}>Amount *</label>
              <input id="pay-amount" type="number" min="0.01" step="0.01" inputMode="decimal" required className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} />
            </div>
            <div>
              <label htmlFor="pay-method" className={labelClass}>Method *</label>
              <select id="pay-method" className={inputClass} value={method} onChange={(e) => setMethod(e.target.value)}>
                {paymentMethodOptions.map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
            </div>
            {method === "check" && (
              <div>
                <label htmlFor="pay-check" className={labelClass}>Check number *</label>
                <input id="pay-check" required maxLength={50} className={inputClass} value={checkNumber} onChange={(e) => setCheckNumber(e.target.value)} />
              </div>
            )}
            <div>
              <label htmlFor="pay-reference" className={labelClass}>Reference</label>
              <input id="pay-reference" maxLength={100} className={inputClass} value={reference} onChange={(e) => setReference(e.target.value)} />
              {method === "external_card" && (
                <p className="mt-1 text-xs text-slate-500">Processor transaction ID only. Never enter card numbers.</p>
              )}
            </div>
            <div className="md:col-span-3">
              <label htmlFor="pay-notes" className={labelClass}>Notes</label>
              <textarea id="pay-notes" rows={2} maxLength={2000} className={inputClass} value={notes} onChange={(e) => setNotes(e.target.value)} />
            </div>
          </div>
        </section>

        <section className={cardClass}>
          <h2 className="text-lg font-semibold text-slate-900">Apply Payment</h2>
          <fieldset className="mt-3 flex flex-wrap gap-6 text-sm">
            <legend className="sr-only">Allocation</legend>
            {[
              ["auto", "Allocate to oldest balances"],
              ["manual", "Choose amounts"],
              ["none", "Keep as credit"],
            ].map(([value, label]) => (
              <label key={value} className="flex items-center gap-2">
                <input type="radio" name="allocation-mode" checked={mode === value} onChange={() => setMode(value as typeof mode)} />
                {label}
              </label>
            ))}
          </fieldset>

          {mode === "manual" && (
            <div className="mt-4 overflow-x-auto">
              {openCharges === null ? (
                <p className="text-sm text-slate-500">Loading open balances...</p>
              ) : openCharges.length === 0 ? (
                <p className="text-sm text-slate-500">No open patient balances. The payment will be kept as credit.</p>
              ) : (
                <table className="w-full text-left text-sm">
                  <thead className="border-b bg-slate-50">
                    <tr>
                      <th className="px-3 py-2">Date</th>
                      <th className="px-3 py-2">Service</th>
                      <th className="px-3 py-2 text-right">Patient balance</th>
                      <th className="px-3 py-2">Apply</th>
                    </tr>
                  </thead>
                  <tbody>
                    {openCharges.map((c) => (
                      <tr key={c.id} className="border-b last:border-0">
                        <td className="whitespace-nowrap px-3 py-2">{c.date_of_service}</td>
                        <td className="px-3 py-2">{c.service_code}</td>
                        <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.balances.patient_balance)}</td>
                        <td className="px-3 py-2">
                          <input
                            aria-label={`Apply to ${c.service_code} on ${c.date_of_service}`}
                            type="number"
                            min="0"
                            step="0.01"
                            className={`${inputClass} max-w-32`}
                            value={allocations[c.id] ?? ""}
                            onChange={(e) => setAllocations({ ...allocations, [c.id]: e.target.value })}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
          <p className="mt-3 text-xs text-slate-500">
            Allocation is calculated by the server. Any amount not applied stays on the patient&apos;s
            account as credit.
          </p>
        </section>

        <ErrorBox message={error} />

        <div className="flex justify-end">
          <button disabled={saving} className={primaryButtonClass}>{saving ? "Posting..." : "Post Payment"}</button>
        </div>
      </form>
    </main>
  );
}
