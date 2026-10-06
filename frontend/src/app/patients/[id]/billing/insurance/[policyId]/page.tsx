"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import InsurancePolicyForm from "@/components/insurance-policy-form";
import PriorAuthorizationsSection from "@/components/prior-authorizations-section";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { InsurancePolicy, priorityLabel } from "@/types/billing";

export default function InsurancePolicyPage() {
  const params = useParams<{ id: string; policyId: string }>();
  const router = useRouter();

  const [policy, setPolicy] = useState<InsurancePolicy | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [authorizationsRefreshKey, setAuthorizationsRefreshKey] = useState(0);

  useEffect(() => {
    async function loadPolicy() {
      try {
        const response = await apiFetch(
          `/api/insurance-policies/${params.policyId}`,
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
              `Unable to load insurance policy (${response.status})`,
          );
        }

        // Guard against a policy ID pasted under the wrong patient URL.
        if (data.patient_id !== params.id) {
          throw new Error("Insurance policy not found for this patient");
        }

        setPolicy(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load insurance policy",
        );
      } finally {
        setLoading(false);
      }
    }

    if (params.policyId) {
      loadPolicy();
    }
  }, [params.id, params.policyId, router]);

  async function updatePolicy(updatedPolicy: InsurancePolicy) {
    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/insurance-policies/${params.policyId}`,
      {
        method: "PUT",
        body: JSON.stringify(updatedPolicy),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error ||
          `Unable to update insurance policy (${response.status})`,
      );
    }

    setPolicy(data);
    setMessage("Insurance policy updated successfully.");
  }

  async function toggleStatus() {
    if (!policy) {
      return;
    }

    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/insurance-policies/${params.policyId}/status`,
      {
        method: "PATCH",
        body: JSON.stringify({
          is_active: !policy.is_active,
        }),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update insurance policy status");
      return;
    }

    setPolicy({
      ...policy,
      is_active: data.is_active,
    });

    // Disabling the policy also disables its prior authorizations.
    setAuthorizationsRefreshKey((current) => current + 1);

    const cascaded = data.prior_authorizations_disabled ?? 0;

    setMessage(
      data.is_active
        ? "Insurance policy enabled. Its prior authorizations stay disabled until re-enabled individually."
        : cascaded > 0
          ? `Insurance policy disabled. ${cascaded} prior authorization${cascaded === 1 ? " was" : "s were"} also disabled.`
          : "Insurance policy disabled.",
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href={`/patients/${params.id}/billing`}
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Billing
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {loading && <p>Loading insurance policy...</p>}

        {error && (
          <div className="mb-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {policy && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">
                  {policy.payer_name}
                </h1>

                <p className="mt-1 text-sm text-slate-500">
                  {priorityLabel(policy.priority)} insurance ·{" "}
                  <span
                    className={
                      policy.is_active
                        ? "font-medium text-green-700"
                        : "font-medium text-slate-600"
                    }
                  >
                    {policy.is_active ? "Active" : "Disabled"}
                  </span>
                </p>
              </div>

              <button
                type="button"
                onClick={toggleStatus}
                className="rounded-lg border bg-white px-4 py-2 text-sm font-medium"
              >
                {policy.is_active ? "Disable Policy" : "Enable Policy"}
              </button>
            </div>

            {message && (
              <div className="mt-5 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                {message}
              </div>
            )}

            <div className="mt-6">
              <InsurancePolicyForm
                key={policy.id}
                initialPolicy={policy}
                submitLabel="Save Changes"
                onSubmit={updatePolicy}
              />
            </div>

            <PriorAuthorizationsSection
              patientId={params.id}
              policyId={params.policyId}
              policyActive={policy.is_active}
              refreshKey={authorizationsRefreshKey}
            />
          </>
        )}
      </div>
    </main>
  );
}
