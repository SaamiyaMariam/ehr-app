"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import PriorAuthorizationForm from "@/components/prior-authorization-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  PriorAuthorization,
  priorAuthorizationWarnings,
} from "@/types/billing";

export default function PriorAuthorizationPage() {
  const params = useParams<{
    id: string;
    policyId: string;
    authorizationId: string;
  }>();
  const router = useRouter();

  const policyPath = `/patients/${params.id}/billing/insurance/${params.policyId}`;

  const [authorization, setAuthorization] =
    useState<PriorAuthorization | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadAuthorization() {
      try {
        const response = await apiFetch(
          `/api/prior-authorizations/${params.authorizationId}`,
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
              `Unable to load prior authorization (${response.status})`,
          );
        }

        // Guard against an authorization ID pasted under the wrong policy URL.
        if (data.insurance_policy_id !== params.policyId) {
          throw new Error(
            "Prior authorization not found for this insurance policy",
          );
        }

        setAuthorization(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load prior authorization",
        );
      } finally {
        setLoading(false);
      }
    }

    if (params.authorizationId) {
      loadAuthorization();
    }
  }, [params.authorizationId, params.policyId, router]);

  async function updateAuthorization(updated: PriorAuthorization) {
    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/prior-authorizations/${params.authorizationId}`,
      {
        method: "PUT",
        body: JSON.stringify(updated),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error ||
          `Unable to update prior authorization (${response.status})`,
      );
    }

    setAuthorization(data);
    router.push(policyPath);
  }

  async function toggleStatus() {
    if (!authorization) {
      return;
    }

    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/prior-authorizations/${params.authorizationId}/status`,
      {
        method: "PATCH",
        body: JSON.stringify({
          is_active: !authorization.is_active,
        }),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      setError(
        data.error || "Unable to update prior authorization status",
      );
      return;
    }

    setAuthorization({
      ...authorization,
      is_active: data.is_active,
    });

    setMessage(
      data.is_active
        ? "Prior authorization enabled."
        : "Prior authorization disabled.",
    );
  }

  const warnings = authorization
    ? priorAuthorizationWarnings(authorization)
    : [];

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href={policyPath}
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Insurance Policy
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {loading && <p>Loading prior authorization...</p>}

        {error && (
          <div className="mb-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {authorization && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">
                  Prior Authorization {authorization.authorization_code}
                </h1>

                <p className="mt-1 flex flex-wrap items-center gap-2 text-sm">
                  <span
                    className={
                      authorization.is_active
                        ? "font-medium text-green-700"
                        : "font-medium text-slate-600"
                    }
                  >
                    {authorization.is_active ? "Active" : "Disabled"}
                  </span>

                  {warnings.map((warning) => (
                    <span
                      key={warning}
                      className="rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-medium text-amber-800"
                    >
                      {warning}
                    </span>
                  ))}
                </p>
              </div>

              <button
                type="button"
                onClick={toggleStatus}
                className="rounded-lg border bg-white px-4 py-2 text-sm font-medium"
              >
                {authorization.is_active
                  ? "Disable Authorization"
                  : "Enable Authorization"}
              </button>
            </div>

            {message && (
              <div className="mt-5 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                {message}
              </div>
            )}

            <div className="mt-6">
              <PriorAuthorizationForm
                key={authorization.id}
                initialAuthorization={authorization}
                submitLabel="Save Changes"
                onSubmit={updateAuthorization}
              />
            </div>
          </>
        )}
      </div>
    </main>
  );
}
