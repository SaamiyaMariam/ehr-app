"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import ConfirmDialog from "@/components/confirm-dialog";
import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, SuccessBox, linkButtonClass, money, primaryButtonClass } from "@/lib/ui";
import { Charge, chargeStatusLabels } from "@/types/charges";
import { billingMethodLabel } from "@/types/rates";

type Props = {
  patientId: string;
  // Notifies parents (e.g. balance summary) that money totals changed.
  onChanged?: () => void;
};

export default function TransactionsSection({ patientId, onChanged }: Props) {
  const [charges, setCharges] = useState<Charge[]>([]);
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);
  const [voiding, setVoiding] = useState<Charge | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patients/${patientId}/billing-transactions`);
      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load billing transactions");
      } else {
        setCharges(data);
      }

      setLoading(false);
    }

    load();
  }, [patientId, reloadKey]);

  async function voidCharge(reason: string) {
    if (!voiding) return;

    const response = await apiFetch(`/api/charges/${voiding.id}/void`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to void charge");
    }

    setMessage(`Service on ${voiding.date_of_service} voided.`);
    setReloadKey((k) => k + 1);
    onChanged?.();
  }

  return (
    <section className="mt-8">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold text-slate-900">Transactions</h2>
        <Link href={`/patients/${patientId}/billing/charges/new`} className={primaryButtonClass}>
          Add Billable Service
        </Link>
      </div>

      <div className="mt-4 space-y-3">
        <SuccessBox message={message} />
        <ErrorBox message={error} />
      </div>

      <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
        {loading ? (
          <div className="p-8 text-center text-slate-500">Loading transactions...</div>
        ) : charges.length === 0 ? (
          <div className="p-8 text-center text-slate-500">No billable services yet.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-3 py-3">Date</th>
                <th className="px-3 py-3">Service</th>
                <th className="px-3 py-3">Method</th>
                <th className="px-3 py-3 text-right">Charge</th>
                <th className="px-3 py-3 text-right">Patient / Insurance</th>
                <th className="px-3 py-3 text-right">Balance</th>
                <th className="px-3 py-3">Status</th>
                <th className="px-3 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {charges.map((charge) => {
                const status = chargeStatusLabels[charge.display_status] ?? {
                  label: charge.display_status,
                  tone: "slate" as const,
                };

                return (
                  <tr
                    key={charge.id}
                    className={`border-b align-top last:border-0 ${charge.status === "voided" ? "bg-slate-50 text-slate-500" : ""}`}
                  >
                    <td className="whitespace-nowrap px-3 py-3">{charge.date_of_service}</td>
                    <td className="px-3 py-3">
                      <div className="font-medium">
                        {charge.service_code}
                        {charge.units > 1 && ` × ${charge.units}`}
                      </div>
                      <div className="text-xs text-slate-500">{charge.clinician_name}</div>
                    </td>
                    <td className="px-3 py-3 text-xs">
                      {billingMethodLabel(charge.billing_method)}
                      {charge.payer_name && <div className="text-slate-500">{charge.payer_name}</div>}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">
                      {money(charge.total_charge)}
                      <div className="text-xs text-slate-500">{money(charge.rate_per_unit)}/unit</div>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">
                      {money(charge.balances.patient_responsibility)} / {money(charge.balances.insurance_responsibility)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right font-medium">
                      {money(charge.balances.total_balance)}
                    </td>
                    <td className="px-3 py-3">
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-right">
                      <Link
                        href={`/patients/${patientId}/billing/charges/${charge.id}`}
                        className={linkButtonClass}
                      >
                        View / Edit
                      </Link>
                      {charge.status !== "voided" && !charge.lock_reason && (
                        <button
                          type="button"
                          onClick={() => setVoiding(charge)}
                          className={`ml-3 ${linkButtonClass}`}
                        >
                          Void
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      <ConfirmDialog
        open={voiding !== null}
        title="Void billable service?"
        description={
          voiding
            ? `${voiding.service_code} on ${voiding.date_of_service} (${money(voiding.total_charge)}) will be voided. Voided services stay in history but no longer count toward balances.`
            : ""
        }
        confirmLabel="Void Service"
        reasonLabel="Void reason"
        onConfirm={voidCharge}
        onClose={() => setVoiding(null)}
      />
    </section>
  );
}
