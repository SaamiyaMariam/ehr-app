"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import PriorAuthorizationForm from "@/components/prior-authorization-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  PriorAuthorization,
  emptyPriorAuthorization,
} from "@/types/billing";

export default function NewPriorAuthorizationPage() {
  const params = useParams<{ id: string; policyId: string }>();
  const router = useRouter();

  const policyPath = `/patients/${params.id}/billing/insurance/${params.policyId}`;

  async function createAuthorization(authorization: PriorAuthorization) {
    const response = await apiFetch(
      `/api/insurance-policies/${params.policyId}/prior-authorizations`,
      {
        method: "POST",
        body: JSON.stringify(authorization),
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
          `Unable to create prior authorization (${response.status})`,
      );
    }

    router.push(policyPath);
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href={policyPath}
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Insurance Policy
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">
          Add Prior Authorization
        </h1>

        <p className="mt-1 text-sm text-slate-500">
          Dates and use counts are optional and can be added later.
        </p>

        <div className="mt-6">
          <PriorAuthorizationForm
            initialAuthorization={emptyPriorAuthorization}
            submitLabel="Create Prior Authorization"
            onSubmit={createAuthorization}
          />
        </div>
      </div>
    </main>
  );
}
