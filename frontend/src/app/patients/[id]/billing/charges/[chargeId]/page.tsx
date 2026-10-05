"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import ChargeForm from "@/components/charge-form";
import ChargeLedger from "@/components/charge-ledger";
import ConfirmDialog from "@/components/confirm-dialog";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Badge, ErrorBox, SuccessBox, cardClass, money, secondaryButtonClass } from "@/lib/ui";
import { Charge, ChargeInput, chargeStatusLabels, chargeToInput } from "@/types/charges";
import { billingMethodLabel, rateSourceLabels } from "@/types/rates";

export default function ChargePage() {
  const params = useParams<{ id: string; chargeId: string }>();
  const router = useRouter();

  const [charge, setCharge] = useState<Charge | null>(null);
  const [loading, setLoading] = useState(true);
  const [voidOpen, setVoidOpen] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      try {
        const response = await apiFetch(`/api/charges/${params.chargeId}`);

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load billable service");
        }

        if (data.patient_id !== params.id) {
          throw new Error("Billable service not found for this patient");
        }

        setCharge(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unable to load billable service");
      } finally {
        setLoading(false);
      }
    }

    load();
  }, [params.id, params.chargeId, router]);

  async function updateCharge(input: ChargeInput) {
    setMessage("");

    const response = await apiFetch(`/api/charges/${params.chargeId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to update billable service");
    }

    setCharge(data);
    setMessage("Billable service updated successfully.");
  }

  async function voidCharge(reason: string) {
    const response = await apiFetch(`/api/charges/${params.chargeId}/void`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to void billable service");
    }

    setCharge(data);
    setMessage("Billable service voided.");
  }

  const status = charge ? chargeStatusLabels[charge.display_status] : null;

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">
            ← Back to Billing
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl space-y-5 px-6 py-8">
        {loading && <p>Loading billable service...</p>}
        <ErrorBox message={error} />

        {charge && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">
                  {charge.service_code} on {charge.date_of_service}
                </h1>
                <p className="mt-1 flex items-center gap-2 text-sm text-slate-500">
                  {charge.patient_name}
                  {status && <Badge tone={status.tone}>{status.label}</Badge>}
                </p>
              </div>

              {charge.status !== "voided" && !charge.lock_reason && (
                <button type="button" onClick={() => setVoidOpen(true)} className={secondaryButtonClass}>
                  Void Service
                </button>
              )}
            </div>

            <SuccessBox message={message} />

            <section className={cardClass}>
              <h2 className="text-lg font-semibold text-slate-900">Amounts</h2>
              <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-4">
                <div>
                  <dt className="text-slate-500">Rate (snapshot)</dt>
                  <dd className="font-medium">
                    {money(charge.rate_per_unit)} × {charge.units}
                    <span className="block text-xs font-normal text-slate-500">
                      {rateSourceLabels[charge.rate_source] ?? charge.rate_source}
                    </span>
                  </dd>
                </div>
                <div>
                  <dt className="text-slate-500">Total charge</dt>
                  <dd className="font-medium">{money(charge.total_charge)}</dd>
                </div>
                <div>
                  <dt className="text-slate-500">Patient balance</dt>
                  <dd className="font-medium">{money(charge.balances.patient_balance)}</dd>
                </div>
                <div>
                  <dt className="text-slate-500">Insurance balance</dt>
                  <dd className="font-medium">{money(charge.balances.insurance_balance)}</dd>
                </div>
              </dl>
              <p className="mt-3 text-xs text-slate-500">{billingMethodLabel(charge.billing_method)}</p>
              {charge.status === "voided" && (
                <p className="mt-3 text-sm text-slate-600">
                  Voided {charge.voided_at.slice(0, 10)}: {charge.void_reason}
                </p>
              )}
            </section>

            <ChargeLedger
              patientId={params.id}
              chargeId={params.chargeId}
              voided={charge.status === "voided"}
              selfPay={charge.billing_method === "direct"}
              onCharge={setCharge}
            />

            {charge.status !== "voided" && (
              <ChargeForm
                key={`${charge.id}-${charge.lock_reason}`}
                patientId={params.id}
                initialCharge={chargeToInput(charge)}
                submitLabel="Save Changes"
                lockReason={charge.lock_reason}
                onSubmit={updateCharge}
              />
            )}
          </>
        )}
      </div>

      <ConfirmDialog
        open={voidOpen}
        title="Void billable service?"
        description="The service stays in history but no longer counts toward balances. This cannot be undone."
        confirmLabel="Void Service"
        reasonLabel="Void reason"
        onConfirm={voidCharge}
        onClose={() => setVoidOpen(false)}
      />
    </main>
  );
}
