"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import ConfirmDialog from "@/components/confirm-dialog";
import RemittanceLines from "@/components/remittance-lines";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Badge, ErrorBox, SuccessBox, cardClass, money, primaryButtonClass, secondaryButtonClass, titleCase } from "@/lib/ui";
import { formatTimestamp } from "@/types/claims";
import {
  EntryLine,
  InsurancePayment,
  centsToMoney,
  insurancePaymentTypeLabel,
  lineAmounts,
  remainingAfter,
  toCents,
  toRemittanceLine,
  transferReasonLabels,
} from "@/types/insurance-payments";

export default function InsurancePaymentPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [payment, setPayment] = useState<InsurancePayment | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [voidOpen, setVoidOpen] = useState(false);
  const [addLines, setAddLines] = useState<EntryLine[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/insurance-payments/${params.id}`);

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Insurance payment not found");
        return;
      }

      setPayment(data);

      // Set by the entry page after posting; read once, outside render.
      if (new URLSearchParams(window.location.search).get("posted") === "1") {
        setMessage("Insurance payment posted. Open a claim to see its updated status.");
        window.history.replaceState(null, "", window.location.pathname);
      }
    }

    load();
  }, [params.id, router, reloadKey]);

  async function voidPayment(reason: string) {
    const response = await apiFetch(`/api/insurance-payments/${params.id}/void`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to void the payment");
    }

    setError("");
    setMessage("Insurance payment voided. Balances were restored and affected claims re-evaluated.");
    setReloadKey((k) => k + 1);
  }

  async function postMoreLines() {
    if (!addLines || addLines.length === 0) return;

    if (addLines.some((l) => lineAmounts(l).invalid || remainingAfter(l) < 0)) {
      setError("Fix the highlighted line amounts first.");
      return;
    }

    setBusy(true);
    setError("");

    try {
      const response = await apiFetch(`/api/insurance-payments/${params.id}/allocations`, {
        method: "POST",
        body: JSON.stringify({ lines: addLines.map(toRemittanceLine) }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to post the lines");
      }

      setMessage("Lines posted.");
      setAddLines(null);
      setReloadKey((k) => k + 1);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to post the lines");
    } finally {
      setBusy(false);
    }
  }

  const posted = payment?.status === "posted";
  const unallocatedCents = payment ? (toCents(payment.unallocated) ?? 0) : 0;
  const pendingPaid = (addLines ?? []).reduce((sum, l) => sum + lineAmounts(l).paid, 0);

  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-6xl space-y-6 px-6 py-8">
        <Link href="/billing/insurance-payments" className="text-sm font-medium text-slate-600">← Back to Insurance Payments</Link>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        {payment && (
          <>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">Insurance payment of {money(payment.amount)}</h1>
                <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-600">
                  {payment.payer_name} · {payment.payment_date} · {insurancePaymentTypeLabel(payment.payment_type)}
                  {payment.reference_number && ` #${payment.reference_number}`}
                  <Badge tone={posted ? "green" : "slate"}>{posted ? "Posted" : "Voided"}</Badge>
                </p>
              </div>
              {posted && (
                <div className="flex gap-3">
                  <button type="button" className={secondaryButtonClass} onClick={() => setAddLines([])}>Adjudicate More Lines</button>
                  <button type="button" className={secondaryButtonClass} onClick={() => setVoidOpen(true)}>Void Payment</button>
                </div>
              )}
            </div>

            <section className={`${cardClass} grid gap-4 text-sm sm:grid-cols-4`}>
              <div><div className="text-slate-500">Payment amount</div><div className="text-lg font-semibold">{money(payment.amount)}</div></div>
              <div><div className="text-slate-500">Allocated</div><div className="text-lg font-semibold">{money(payment.allocated)}</div></div>
              <div><div className="text-slate-500">Unallocated</div><div className="text-lg font-semibold">{money(payment.unallocated)}</div></div>
              <div><div className="text-slate-500">Adjusted</div><div className="text-lg font-semibold">{money(payment.adjusted)}</div></div>
              <div><div className="text-slate-500">Moved to patient</div><div className="text-lg font-semibold">{money(payment.transferred_to_patient)}</div></div>
              {payment.notes && <div className="sm:col-span-3">Notes: {payment.notes}</div>}
              {!posted && <div className="text-red-800 sm:col-span-4">Voided: {payment.void_reason}</div>}
              <div className="text-xs text-slate-500 sm:col-span-4">Entered {formatTimestamp(payment.created_at)} by {payment.created_by}</div>
            </section>

            <section>
              <h2 className="text-lg font-semibold text-slate-900">Adjudicated Lines</h2>
              <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
                {payment.allocations?.length === 0 ? (
                  <div className="p-6 text-center text-slate-500">No claim lines have been adjudicated on this payment.</div>
                ) : (
                  <table className="w-full text-left text-sm">
                    <thead className="border-b bg-slate-50">
                      <tr>
                        <th className="px-3 py-2">Claim</th>
                        <th className="px-3 py-2">Patient</th>
                        <th className="px-3 py-2">Service</th>
                        <th className="px-3 py-2 text-right">Billed</th>
                        <th className="px-3 py-2 text-right">Allowed</th>
                        <th className="px-3 py-2 text-right">Paid</th>
                        <th className="px-3 py-2">Adjustments / transfers</th>
                        <th className="px-3 py-2">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {payment.allocations?.map((a) => (
                        <tr key={a.id} className={`border-b align-top last:border-0 ${a.status === "voided" ? "text-slate-500" : ""}`}>
                          <td className="whitespace-nowrap px-3 py-2">
                            <Link href={`/billing/claims/${a.claim_id}`} className="underline">{a.claim_number}</Link>
                          </td>
                          <td className="px-3 py-2">
                            <Link href={`/patients/${a.patient_id}/billing`} className="underline">{a.patient_name}</Link>
                          </td>
                          <td className="px-3 py-2">{a.date_of_service} · {a.service_code}</td>
                          <td className="whitespace-nowrap px-3 py-2 text-right">{money(a.billed)}</td>
                          <td className="whitespace-nowrap px-3 py-2 text-right">{a.allowed_amount ? money(a.allowed_amount) : "—"}</td>
                          <td className="whitespace-nowrap px-3 py-2 text-right">{money(a.amount_paid)}</td>
                          <td className="px-3 py-2">
                            <ul className="space-y-0.5">
                              {a.adjustments.map((x) => (
                                <li key={x.id} className={x.status === "voided" ? "line-through" : ""}>{titleCase(x.type)}: {money(x.amount)}</li>
                              ))}
                              {a.transfers.map((x) => (
                                <li key={x.id} className={x.status === "voided" ? "line-through" : ""}>→ Patient ({transferReasonLabels[x.reason] ?? x.reason}): {money(x.amount)}</li>
                              ))}
                              {a.adjustments.length === 0 && a.transfers.length === 0 && <li className="text-slate-500">—</li>}
                            </ul>
                          </td>
                          <td className="px-3 py-2">
                            <Badge tone={a.status === "active" ? "green" : "slate"}>{a.status === "active" ? "Posted" : "Voided"}</Badge>
                            {a.status === "active" && <div className="mt-1 text-xs text-slate-500">{a.is_final ? "Final" : "Partial"}</div>}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </section>

            {addLines && (
              <section className="space-y-4">
                <div className={`${cardClass} text-sm`}>
                  Unallocated on this payment: <strong>{centsToMoney(unallocatedCents - pendingPaid)}</strong>
                </div>
                <RemittanceLines payerId={payment.payer_id} lines={addLines} onChange={setAddLines} disabled={busy} />
                <div className="flex justify-end gap-3">
                  <button type="button" className={secondaryButtonClass} onClick={() => setAddLines(null)}>Cancel</button>
                  <button type="button" disabled={busy || addLines.length === 0} className={primaryButtonClass} onClick={postMoreLines}>Post Lines</button>
                </div>
              </section>
            )}
          </>
        )}
      </div>

      <ConfirmDialog
        open={voidOpen}
        title="Void insurance payment?"
        description="Every allocation, adjustment and responsibility transfer posted by this payment is reversed and balances are restored. Affected claims are re-evaluated. The payment stays in history as voided."
        confirmLabel="Void Payment"
        reasonLabel="Void reason"
        onConfirm={voidPayment}
        onClose={() => setVoidOpen(false)}
      />
    </main>
  );
}
