"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  PriorAuthorization,
  priorAuthorizationWarnings,
  usageSettingLabel,
} from "@/types/billing";

type Props = {
  patientId: string;
  policyId: string;
  policyActive: boolean;
  // Bumped by the parent when the policy status changes (cascade).
  refreshKey: number;
};

function formatCount(value: number | null) {
  return value === null ? "—" : value;
}

export default function PriorAuthorizationsSection({
  patientId,
  policyId,
  policyActive,
  refreshKey,
}: Props) {
  const [authorizations, setAuthorizations] = useState<PriorAuthorization[]>(
    [],
  );
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadAuthorizations() {
      try {
        const response = await apiFetch(
          `/api/insurance-policies/${policyId}/prior-authorizations`,
        );

        const data = await response.json();

        if (!response.ok) {
          throw new Error(
            data.error || "Unable to load prior authorizations",
          );
        }

        setAuthorizations(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load prior authorizations",
        );
      } finally {
        setLoading(false);
      }
    }

    loadAuthorizations();
  }, [policyId, refreshKey]);

  async function toggleStatus(authorization: PriorAuthorization) {
    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/prior-authorizations/${authorization.id}/status`,
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

    setAuthorizations((current) =>
      current.map((item) =>
        item.id === authorization.id
          ? { ...item, is_active: data.is_active }
          : item,
      ),
    );

    setMessage(
      data.is_active
        ? "Prior authorization enabled."
        : "Prior authorization disabled.",
    );
  }

  const basePath = `/patients/${patientId}/billing/insurance/${policyId}/prior-authorizations`;

  return (
    <section className="mt-10">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold text-slate-900">
          Prior Authorizations
        </h2>

        {policyActive ? (
          <Link
            href={`${basePath}/new`}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
          >
            Add Prior Authorization
          </Link>
        ) : (
          <span className="text-sm text-slate-500">
            Enable the policy to add authorizations
          </span>
        )}
      </div>

      {message && (
        <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700">
          {message}
        </div>
      )}

      {error && (
        <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
        {loading ? (
          <div className="p-8 text-center text-slate-500">
            Loading prior authorizations...
          </div>
        ) : authorizations.length === 0 ? (
          <div className="p-8 text-center text-slate-500">
            No prior authorizations yet.
          </div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-4 py-3">Authorization</th>
                <th className="px-4 py-3">Service Codes</th>
                <th className="px-4 py-3">Start / Expiration</th>
                <th className="px-4 py-3">Remaining / Allowed</th>
                <th className="px-4 py-3">Usage Setting</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3"></th>
              </tr>
            </thead>

            <tbody>
              {authorizations.map((authorization) => {
                const warnings = priorAuthorizationWarnings(authorization);

                return (
                  <tr
                    key={authorization.id}
                    className={`border-b last:border-0 ${
                      authorization.is_active
                        ? ""
                        : "bg-slate-50 text-slate-500"
                    }`}
                  >
                    <td className="px-4 py-4 font-medium">
                      {authorization.authorization_code}
                    </td>

                    <td className="px-4 py-4">
                      {authorization.applies_to_any_service_code
                        ? "Any"
                        : authorization.service_codes
                            ?.map((code) => code.code)
                            .join(", ") || "—"}
                    </td>

                    <td className="whitespace-nowrap px-4 py-4">
                      <div>{authorization.start_date || "—"}</div>
                      <div className="text-slate-500">
                        {authorization.expiration_date || "—"}
                      </div>
                    </td>

                    <td className="whitespace-nowrap px-4 py-4">
                      {formatCount(authorization.uses_remaining)} /{" "}
                      {formatCount(authorization.uses_allowed)}
                    </td>

                    <td className="whitespace-nowrap px-4 py-4">
                      {usageSettingLabel(authorization.usage_setting)}
                    </td>

                    <td className="px-4 py-4">
                      <div className="flex flex-col items-start gap-1">
                        <span
                          className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                            authorization.is_active
                              ? "bg-green-100 text-green-800"
                              : "bg-slate-200 text-slate-600"
                          }`}
                        >
                          {authorization.is_active ? "Active" : "Disabled"}
                        </span>

                        {warnings.map((warning) => (
                          <span
                            key={warning}
                            className="whitespace-nowrap rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-medium text-amber-800"
                          >
                            {warning}
                          </span>
                        ))}
                      </div>
                    </td>

                    <td className="whitespace-nowrap px-4 py-4 text-right">
                      <Link
                        href={`${basePath}/${authorization.id}`}
                        className="font-medium text-slate-900 underline"
                      >
                        View / Edit
                      </Link>

                      <button
                        type="button"
                        onClick={() => toggleStatus(authorization)}
                        className="ml-4 font-medium text-slate-900 underline"
                      >
                        {authorization.is_active ? "Disable" : "Enable"}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
