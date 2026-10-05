"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import ConfirmDialog from "@/components/confirm-dialog";
import FormDialog from "@/components/form-dialog";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, SuccessBox, cardClass, linkButtonClass, money, secondaryButtonClass, titleCase } from "@/lib/ui";
import { ChargeBreakdown, LedgerEvent, ledgerEventLabels, signedAmount } from "@/types/balances";
import { Charge } from "@/types/charges";
import { otherAdjustmentOptions, transferReasonLabels } from "@/types/insurance-payments";

const adjustmentOptions = [
  { value: "contractual_writeoff", label: "Contractual write-off (insurance only)" },
  ...otherAdjustmentOptions,
];

const transferReasons = ["deductible", "copay", "coinsurance", "noncovered", "correction", "other"].map((value) => ({
  value,
  label: transferReasonLabels[value] ?? value,
}));

// "-0.00" reads badly; zero stays zero.
const negate = (value: string) => (/^0+(\.0+)?$/.test(value) ? value : `-${value}`);

function eventLink(patientId: string, e: LedgerEvent) {
  if (e.event_type === "patient_payment") return `/patients/${patientId}/billing/payments/${e.source_id}`;
  if (e.event_type === "insurance_payment") return `/billing/insurance-payments/${e.source_id}`;
  return null;
}

// Explains one charge's balances: where each side started, what moved, what
// was paid or adjusted, and every event behind it. Read from the backend
// balance engine; nothing here is computed in the browser.
export default function ChargeLedger({
  patientId,
  chargeId,
  voided,
  onCharge,
}: {
  patientId: string;
  chargeId: string;
  voided: boolean;
  onCharge: (charge: Charge) => void;
}) {
  const [data, setData] = useState<{ breakdown: ChargeBreakdown; events: LedgerEvent[] } | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [dialog, setDialog] = useState<"adjust" | "transfer" | null>(null);
  const [voiding, setVoiding] = useState<LedgerEvent | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let stale = false;

    async function load() {
      const response = await apiFetch(`/api/charges/${chargeId}/ledger`);
      const body = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(body.error || "Unable to load the ledger");
        return;
      }

      setData(body);
    }

    load();

    return () => {
      stale = true;
    };
  }, [chargeId, reloadKey]);

  async function send(path: string, body: object, success: string) {
    setMessage("");
    const response = await apiFetch(path, { method: "POST", body: JSON.stringify(body) });
    const result = await response.json();

    if (!response.ok) {
      throw new Error(result.error || "Action failed");
    }

    onCharge(result);
    setMessage(success);
    setReloadKey((k) => k + 1);
  }

  const b = data?.breakdown;

  const rows: { label: string; patient: string; insurance: string; strong?: boolean }[] = b
    ? [
        { label: "Responsibility at charge", patient: b.initial_patient_responsibility, insurance: b.initial_insurance_responsibility },
        { label: "Moved to patient", patient: b.transfers_to_patient, insurance: negate(b.transfers_to_patient) },
        { label: "Moved to insurance", patient: negate(b.transfers_to_insurance), insurance: b.transfers_to_insurance },
        { label: "Current responsibility", patient: b.patient_responsibility, insurance: b.insurance_responsibility, strong: true },
        { label: "Payments", patient: negate(b.patient_paid), insurance: negate(b.insurance_paid) },
        { label: "Adjustments / write-offs", patient: negate(b.patient_adjustments), insurance: negate(b.insurance_adjustments) },
        { label: "Balance", patient: b.patient_balance, insurance: b.insurance_balance, strong: true },
      ]
    : [];

  return (
    <section className={cardClass} aria-labelledby="charge-ledger-heading">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="charge-ledger-heading" className="text-lg font-semibold text-slate-900">How this balance is made up</h2>
        {!voided && (
          <div className="flex gap-3">
            <button type="button" className={secondaryButtonClass} onClick={() => setDialog("adjust")}>Post Adjustment</button>
            <button type="button" className={secondaryButtonClass} onClick={() => setDialog("transfer")}>Transfer Responsibility</button>
          </div>
        )}
      </div>

      <div className="mt-3 space-y-3">
        <ErrorBox message={error} />
        <SuccessBox message={message} />
      </div>

      {b && (
        <>
          <p className="mt-3 text-sm text-slate-600">Original charge: <strong>{money(b.original_charge)}</strong>. Total balance: <strong>{money(b.total_balance)}</strong>.</p>

          <div className="mt-3 overflow-x-auto rounded-lg border">
            <table className="w-full text-left text-sm" data-testid="breakdown">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">&nbsp;</th>
                  <th className="px-3 py-2 text-right">Patient</th>
                  <th className="px-3 py-2 text-right">Insurance</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.label} className={`border-b last:border-0 ${r.strong ? "bg-slate-50 font-semibold" : ""}`}>
                    <td className="px-3 py-2">{r.label}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(r.patient)}</td>
                    <td className="whitespace-nowrap px-3 py-2 text-right">{money(r.insurance)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <h3 className="mt-6 font-semibold text-slate-900">Events</h3>
          <div className="mt-2 overflow-x-auto rounded-lg border">
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-3 py-2">Date</th>
                  <th className="px-3 py-2">Event</th>
                  <th className="px-3 py-2">Party</th>
                  <th className="px-3 py-2 text-right">Amount</th>
                  <th className="px-3 py-2">Status</th>
                  <th className="px-3 py-2"></th>
                </tr>
              </thead>
              <tbody>
                {data?.events.map((e, i) => {
                  const link = eventLink(patientId, e);

                  return (
                    <tr key={`${e.source_id}-${e.event_type}-${e.party}-${i}`} className={`border-b last:border-0 ${e.status === "voided" ? "text-slate-500 line-through" : ""}`}>
                      <td className="whitespace-nowrap px-3 py-2">{e.occurred_on}</td>
                      <td className="px-3 py-2">
                        {link ? <Link href={link} className="underline">{ledgerEventLabels[e.event_type] ?? e.event_type}</Link> : (ledgerEventLabels[e.event_type] ?? e.event_type)}
                        {e.detail && <span className="text-xs text-slate-500"> · {titleCase(e.detail)}</span>}
                      </td>
                      <td className="px-3 py-2 capitalize">{e.party}</td>
                      <td className="whitespace-nowrap px-3 py-2 text-right font-medium">{signedAmount(e)}</td>
                      <td className="px-3 py-2"><Badge tone={e.status === "active" ? "green" : "slate"}>{e.status === "active" ? "Active" : "Voided"}</Badge></td>
                      <td className="px-3 py-2 text-right">
                        {e.voidable && (
                          <button type="button" className={linkButtonClass} onClick={() => setVoiding(e)}>Void</button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      )}

      {dialog === "adjust" && (
        <FormDialog
          open
          title="Post adjustment"
          description="Reduces the selected side's balance without money changing hands. Recorded permanently; reverse it by voiding."
          confirmLabel="Post Adjustment"
          fields={[
            { name: "party", label: "Applies to", type: "select", required: true, defaultValue: "insurance", options: [{ value: "insurance", label: "Insurance balance" }, { value: "patient", label: "Patient balance" }] },
            { name: "adjustment_type", label: "Type", type: "select", required: true, defaultValue: "payer_adjustment", options: adjustmentOptions },
            { name: "amount", label: "Amount", type: "number", required: true },
            { name: "reference", label: "Reference", maxLength: 100 },
            { name: "reason", label: "Reason", type: "textarea", required: true, maxLength: 500 },
          ]}
          onSubmit={(v) => send(`/api/charges/${chargeId}/adjustments`, v, "Adjustment posted.")}
          onClose={() => setDialog(null)}
        />
      )}

      {dialog === "transfer" && (
        <FormDialog
          open
          title="Transfer responsibility"
          description="Moves part of what one side owes to the other. The total owed does not change."
          confirmLabel="Transfer"
          fields={[
            { name: "from_party", label: "From", type: "select", required: true, defaultValue: "insurance", options: [{ value: "insurance", label: "Insurance" }, { value: "patient", label: "Patient" }] },
            { name: "to_party", label: "To", type: "select", required: true, defaultValue: "patient", options: [{ value: "patient", label: "Patient" }, { value: "insurance", label: "Insurance" }] },
            { name: "amount", label: "Amount", type: "number", required: true },
            { name: "reason", label: "Reason", type: "select", required: true, defaultValue: "noncovered", options: transferReasons },
            { name: "note", label: "Note", type: "textarea", maxLength: 500 },
          ]}
          onSubmit={(v) => send(`/api/charges/${chargeId}/transfers`, v, "Responsibility transferred.")}
          onClose={() => setDialog(null)}
        />
      )}

      <ConfirmDialog
        open={voiding !== null}
        title="Void this entry?"
        description="The entry stays in history as voided and the balance is restored."
        confirmLabel="Void"
        reasonLabel="Void reason"
        onConfirm={(reason) => {
          const target = voiding;
          if (!target) return Promise.resolve();

          const path = target.event_type === "adjustment"
            ? `/api/billing-adjustments/${target.source_id}/void`
            : `/api/responsibility-transfers/${target.source_id}/void`;

          return send(path, { reason }, "Entry voided.");
        }}
        onClose={() => setVoiding(null)}
      />
    </section>
  );
}
