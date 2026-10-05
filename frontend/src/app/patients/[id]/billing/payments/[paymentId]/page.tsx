"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import ConfirmDialog from "@/components/confirm-dialog";
import FormDialog from "@/components/form-dialog";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Badge, ErrorBox, SuccessBox, cardClass, linkButtonClass, money, secondaryButtonClass } from "@/lib/ui";
import { formatTimestamp } from "@/types/claims";
import { PatientPayment, newIdempotencyKey, paymentMethodLabel, paymentMethodOptions } from "@/types/payments";

export default function PatientPaymentPage() {
  const params = useParams<{ id: string; paymentId: string }>();
  const router = useRouter();

  const [payment, setPayment] = useState<PatientPayment | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [dialog, setDialog] = useState<"void" | "refund" | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patient-payments/${params.paymentId}`);

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await response.json();

      if (!response.ok || data.patient_id !== params.id) {
        setError(data.error || "Payment not found for this patient");
        return;
      }

      setPayment(data);
    }

    load();
  }, [params.id, params.paymentId, router, reloadKey]);

  async function post(path: string, body: object, success: string, throwErrors = false) {
    setMessage("");
    setError("");
    setBusy(true);

    try {
      const response = await apiFetch(path, { method: "POST", body: JSON.stringify(body) });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Action failed");
      }

      setMessage(success);
      setReloadKey((k) => k + 1);
    } catch (err) {
      if (throwErrors) throw err;
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  const posted = payment?.status === "posted";
  const hasCredit = payment ? payment.unallocated !== "0.00" : false;

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">← Back to Billing</Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <ErrorBox message={error} />
        <SuccessBox message={message} />

        {payment && (
          <>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">Payment of {money(payment.amount)}</h1>
                <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-600">
                  {payment.patient_name} · {payment.payment_date} · {paymentMethodLabel(payment.method)}
                  {payment.check_number && ` #${payment.check_number}`}
                  <Badge tone={posted ? "green" : "slate"}>{posted ? "Posted" : "Voided"}</Badge>
                </p>
              </div>
              {posted && (
                <div className="flex gap-3">
                  {hasCredit && (
                    <button type="button" className={secondaryButtonClass} onClick={() => setDialog("refund")}>Refund Credit</button>
                  )}
                  <button type="button" className={secondaryButtonClass} onClick={() => setDialog("void")}>Void Payment</button>
                </div>
              )}
            </div>

            <section className={`${cardClass} grid gap-4 text-sm sm:grid-cols-4`}>
              <div><div className="text-slate-500">Amount</div><div className="text-lg font-semibold">{money(payment.amount)}</div></div>
              <div><div className="text-slate-500">Applied</div><div className="text-lg font-semibold">{money(payment.allocated)}</div></div>
              <div><div className="text-slate-500">Refunded</div><div className="text-lg font-semibold">{money(payment.refunded)}</div></div>
              <div><div className="text-slate-500">Unapplied credit</div><div className="text-lg font-semibold">{money(payment.unallocated)}</div></div>
              {payment.reference_number && <div className="sm:col-span-2">Reference: {payment.reference_number}</div>}
              {payment.notes && <div className="sm:col-span-4">Notes: {payment.notes}</div>}
              {!posted && <div className="text-red-800 sm:col-span-4">Voided: {payment.void_reason}</div>}
              <div className="text-xs text-slate-500 sm:col-span-4">Entered {formatTimestamp(payment.created_at)} by {payment.created_by}</div>
            </section>

            <section>
              <div className="flex items-center justify-between gap-4">
                <h2 className="text-lg font-semibold text-slate-900">Applied To</h2>
                {posted && hasCredit && (
                  <button type="button" disabled={busy} className={secondaryButtonClass}
                    onClick={() => post(`/api/patient-payments/${payment.id}/allocations`, { auto_allocate: true }, "Credit applied to the oldest open balances.")}>
                    Apply Credit to Oldest Balances
                  </button>
                )}
              </div>
              <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
                {payment.allocations?.length === 0 ? (
                  <div className="p-6 text-center text-slate-500">Not applied to any services.</div>
                ) : (
                  <table className="w-full text-left text-sm">
                    <thead className="border-b bg-slate-50">
                      <tr>
                        <th className="px-3 py-2">Service date</th>
                        <th className="px-3 py-2">Service</th>
                        <th className="px-3 py-2 text-right">Amount</th>
                        <th className="px-3 py-2">Status</th>
                        <th className="px-3 py-2"></th>
                      </tr>
                    </thead>
                    <tbody>
                      {payment.allocations?.map((a) => (
                        <tr key={a.id} className={`border-b last:border-0 ${a.status === "voided" ? "text-slate-500" : ""}`}>
                          <td className="px-3 py-2">
                            <Link href={`/patients/${params.id}/billing/charges/${a.charge_id}`} className="underline">{a.date_of_service}</Link>
                          </td>
                          <td className="px-3 py-2">{a.service_code}</td>
                          <td className="whitespace-nowrap px-3 py-2 text-right">{money(a.amount)}</td>
                          <td className="px-3 py-2"><Badge tone={a.status === "active" ? "green" : "slate"}>{a.status === "active" ? "Applied" : "Unapplied"}</Badge></td>
                          <td className="px-3 py-2 text-right">
                            {posted && a.status === "active" && (
                              <button type="button" disabled={busy} className={linkButtonClass}
                                onClick={() => post(`/api/patient-payment-allocations/${a.id}/void`, {}, "Allocation removed; the amount is back in credit.")}>
                                Unapply
                              </button>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </section>

            {(payment.refunds?.length ?? 0) > 0 && (
              <section>
                <h2 className="text-lg font-semibold text-slate-900">Refunds</h2>
                <ul className="mt-3 space-y-2 text-sm">
                  {payment.refunds?.map((f) => (
                    <li key={f.id} className={`${cardClass} py-3`}>
                      {money(f.amount)} on {f.refund_date} by {paymentMethodLabel(f.method)}
                      {f.reference_number && ` (${f.reference_number})`} — {f.reason}
                    </li>
                  ))}
                </ul>
              </section>
            )}
          </>
        )}
      </div>

      <ConfirmDialog
        open={dialog === "void"}
        title="Void payment?"
        description="All of this payment's allocations are reversed and the services' balances restored. The payment stays in history as voided."
        confirmLabel="Void Payment"
        reasonLabel="Void reason"
        onConfirm={(reason) => post(`/api/patient-payments/${params.paymentId}/void`, { reason }, "Payment voided.", true)}
        onClose={() => setDialog(null)}
      />

      {dialog === "refund" && payment && (
        <FormDialog
          open
          title="Refund credit"
          description={`Up to ${money(payment.unallocated)} of unapplied credit can be refunded. To refund money applied to a service, unapply it first.`}
          confirmLabel="Record Refund"
          fields={[
            { name: "amount", label: "Refund amount", type: "number", required: true, defaultValue: payment.unallocated },
            { name: "refund_date", label: "Refund date", type: "date", required: true, defaultValue: new Date().toLocaleDateString("en-CA") },
            { name: "method", label: "Refund method", type: "select", required: true, defaultValue: payment.method, options: paymentMethodOptions },
            { name: "reference_number", label: "Reference", maxLength: 100 },
            { name: "reason", label: "Reason", type: "textarea", required: true, maxLength: 500 },
          ]}
          onSubmit={(v) => post(`/api/patient-payments/${params.paymentId}/refunds`, { ...v, idempotency_key: newIdempotencyKey() }, "Refund recorded.", true)}
          onClose={() => setDialog(null)}
        />
      )}
    </main>
  );
}
