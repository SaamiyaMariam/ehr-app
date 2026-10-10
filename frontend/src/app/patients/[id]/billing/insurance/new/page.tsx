"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import InsurancePolicyForm from "@/components/insurance-policy-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  InsurancePolicy,
  emptyInsurancePolicy,
} from "@/types/billing";

export default function NewInsurancePolicyPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  async function createPolicy(policy: InsurancePolicy) {
    const response = await apiFetch(
      `/api/patients/${params.id}/insurance-policies`,
      {
        method: "POST",
        body: JSON.stringify(policy),
      },
    );

    if (response.status === 401) {
      clearToken();
      router.replace("/login");
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error ||
          `Unable to create insurance policy (${response.status})`,
      );
    }

    router.push(`/patients/${params.id}/billing`);
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href={`/patients/${params.id}/billing`}
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Billing
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">
          Add Insurance Policy
        </h1>

        <p className="mt-1 text-sm text-slate-500">
          Only the payer is required. Missing details can be added later.
        </p>

        <div className="mt-6">
          <InsurancePolicyForm
            initialPolicy={emptyInsurancePolicy}
            submitLabel="Create Policy"
            onSubmit={createPolicy}
          />
        </div>
      </div>
    </main>
  );
}
