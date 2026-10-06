"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import ChargeForm from "@/components/charge-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ChargeInput, emptyChargeInput } from "@/types/charges";

export default function NewChargePage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const today = new Date().toLocaleDateString("en-CA");

  async function createCharge(charge: ChargeInput) {
    const response = await apiFetch(`/api/patients/${params.id}/charges`, {
      method: "POST",
      body: JSON.stringify(charge),
    });

    if (response.status === 401) {
      clearToken();
      router.replace("/login");
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || `Unable to create billable service (${response.status})`);
    }

    router.push(`/patients/${params.id}/billing`);
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

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Add Billable Service</h1>
        <p className="mt-1 text-sm text-slate-500">
          The rate is resolved and saved when the service is created; later rate changes do not
          affect it.
        </p>

        <div className="mt-6">
          <ChargeForm
            patientId={params.id}
            initialCharge={emptyChargeInput(today)}
            submitLabel="Create Billable Service"
            onSubmit={createCharge}
          />
        </div>
      </div>
    </main>
  );
}
