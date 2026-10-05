"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import RemittanceLines from "@/components/remittance-lines";
import { apiFetch } from "@/lib/api";
import { ErrorBox, cardClass, inputClass, labelClass, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import {
  EntryLine,
  centsToMoney,
  insurancePaymentTypeOptions,
  lineAmounts,
  newIdempotencyKey,
  remainingAfter,
  toCents,
  toRemittanceLine,
} from "@/types/insurance-payments";

type PayerOption = { id: string; payer_name: string; is_active: boolean };

export default function NewInsurancePaymentPage() {
  const router = useRouter();

  const [payers, setPayers] = useState<PayerOption[]>([]);
  const [payerId, setPayerId] = useState("");
  const [paymentDate, setPaymentDate] = useState(() => new Date().toLocaleDateString("en-CA"));
  const [amount, setAmount] = useState("");
  const [paymentType, setPaymentType] = useState("check");
  const [reference, setReference] = useState("");
  const [notes, setNotes] = useState("");
  const [lines, setLines] = useState<EntryLine[]>([]);
  const [idempotencyKey] = useState(newIdempotencyKey);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPayers() {
      const response = await apiFetch("/api/payers");
      if (response.ok) setPayers(await response.json());
    }

    loadPayers();
  }, []);

  // Display helpers; the server recomputes and validates everything.
  const paymentCents = toCents(amount);
  const allocated = lines.reduce((sum, l) => sum + lineAmounts(l).paid, 0);
  const unallocated = (paymentCents ?? 0) - allocated;
  const anyInvalid = lines.some((l) => lineAmounts(l).invalid || remainingAfter(l) < 0);

  function changePayer(id: string) {
    // Lines belong to one payer; switching starts the allocation over.
    setPayerId(id);
    setLines([]);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");

    if (!payerId) return setError("Select the payer that issued this payment.");
    if (paymentCents === null) return setError("Enter the payment amount like 125.00 (use 0 for a zero-dollar EOB).");
    if (anyInvalid) return setError("Fix the highlighted line amounts first.");
    if (paymentCents === 0 && lines.length === 0) return setError("A zero-dollar remittance needs at least one claim line to explain.");
    if (unallocated < 0) return setError(`Paid amounts exceed the payment by ${centsToMoney(-unallocated)}.`);

    setBusy(true);

    try {
      const response = await apiFetch("/api/insurance-payments", {
        method: "POST",
        body: JSON.stringify({
          payer_id: payerId,
          payment_date: paymentDate,
          amount: amount.trim(),
          payment_type: paymentType,
          reference_number: reference,
          notes,
          idempotency_key: idempotencyKey,
          lines: lines.map(toRemittanceLine),
        }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to post the insurance payment");
      }

      router.push(`/billing/insurance-payments/${data.payment.id}?posted=1`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to post the insurance payment");
      setBusy(false);
    }
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <form onSubmit={submit} className="mx-auto max-w-7xl space-y-6 px-6 py-8">
        <Link href="/billing/insurance-payments" className="text-sm font-medium text-slate-600">← Back to Insurance Payments</Link>

        <h1 className="text-2xl font-semibold text-slate-900">Enter Insurance Payment</h1>

        <section className={`${cardClass} grid gap-4 md:grid-cols-3`}>
          <div>
            <label htmlFor="ip-payer" className={labelClass}>Payer</label>
            <select id="ip-payer" className={inputClass} value={payerId} onChange={(e) => changePayer(e.target.value)}>
              <option value="">Select payer</option>
              {payers.map((p) => (
                <option key={p.id} value={p.id}>{p.payer_name}{p.is_active ? "" : " (disabled)"}</option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="ip-date" className={labelClass}>Payment date</label>
            <input id="ip-date" type="date" className={inputClass} value={paymentDate} onChange={(e) => setPaymentDate(e.target.value)} required />
          </div>
          <div>
            <label htmlFor="ip-amount" className={labelClass}>Payment amount</label>
            <input id="ip-amount" inputMode="decimal" className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="0.00" required />
            <p className="mt-1 text-xs text-slate-500">Enter 0.00 for a zero-dollar EOB.</p>
          </div>
          <div>
            <label htmlFor="ip-type" className={labelClass}>Payment type</label>
            <select id="ip-type" className={inputClass} value={paymentType} onChange={(e) => setPaymentType(e.target.value)}>
              {insurancePaymentTypeOptions.map((o) => (<option key={o.value} value={o.value}>{o.label}</option>))}
            </select>
          </div>
          <div>
            <label htmlFor="ip-reference" className={labelClass}>Check / EFT number</label>
            <input id="ip-reference" maxLength={100} className={inputClass} value={reference} onChange={(e) => setReference(e.target.value)} />
          </div>
          <div>
            <label htmlFor="ip-notes" className={labelClass}>Notes</label>
            <input id="ip-notes" maxLength={2000} className={inputClass} value={notes} onChange={(e) => setNotes(e.target.value)} />
          </div>
        </section>

        <section className={`${cardClass} grid gap-4 text-sm sm:grid-cols-3`} aria-label="Allocation summary">
          <div><div className="text-slate-500">Payment total</div><div className="text-lg font-semibold" data-testid="payment-total">{paymentCents === null ? "—" : centsToMoney(paymentCents)}</div></div>
          <div><div className="text-slate-500">Allocated to claim lines</div><div className="text-lg font-semibold" data-testid="allocated-total">{centsToMoney(allocated)}</div></div>
          <div>
            <div className="text-slate-500">Remaining unallocated</div>
            <div className={`text-lg font-semibold ${unallocated < 0 ? "text-red-700" : ""}`} data-testid="unallocated-total">{centsToMoney(unallocated)}</div>
          </div>
        </section>

        <RemittanceLines payerId={payerId} lines={lines} onChange={setLines} disabled={busy} />

        <ErrorBox message={error} />

        <div className="flex justify-end gap-3">
          <Link href="/billing/insurance-payments" className={secondaryButtonClass}>Cancel</Link>
          <button disabled={busy} className={primaryButtonClass}>{busy ? "Posting..." : "Post Insurance Payment"}</button>
        </div>
      </form>
    </main>
  );
}
