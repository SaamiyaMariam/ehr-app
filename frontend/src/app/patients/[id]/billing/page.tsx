"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";

import BalanceSummary from "@/components/balance-summary";
import CashRatesSection from "@/components/cash-rates-section";
import DiagnosesSection from "@/components/diagnoses-section";
import PatientClaimsSection from "@/components/patient-claims-section";
import PaymentsSection from "@/components/payments-section";
import TransactionsSection from "@/components/transactions-section";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  InsurancePolicyListItem,
  priorityLabel,
} from "@/types/billing";
import { Patient } from "@/types/patient";

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

function formatCoverage(start: string, end: string) {
  if (!start && !end) {
    return "—";
  }

  return `${start || "?"} → ${end || "ongoing"}`;
}

export default function PatientBillingPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [patient, setPatient] = useState<Patient | null>(null);
  const [billingComments, setBillingComments] = useState("");
  const [policies, setPolicies] = useState<InsurancePolicyListItem[]>([]);

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [savingSettings, setSavingSettings] = useState(false);
  const [settingsMessage, setSettingsMessage] = useState("");
  const [settingsError, setSettingsError] = useState("");

  const [policyMessage, setPolicyMessage] = useState("");
  const [policyError, setPolicyError] = useState("");

  useEffect(() => {
    async function loadBilling() {
      try {
        const [patientResponse, settingsResponse, policiesResponse] =
          await Promise.all([
            apiFetch(`/api/patients/${params.id}`),
            apiFetch(`/api/patients/${params.id}/billing-settings`),
            apiFetch(`/api/patients/${params.id}/insurance-policies`),
          ]);

        if (
          patientResponse.status === 401 ||
          settingsResponse.status === 401 ||
          policiesResponse.status === 401
        ) {
          clearToken();
          router.replace("/login");
          return;
        }

        const [patientData, settingsData, policiesData] =
          await Promise.all([
            patientResponse.json(),
            settingsResponse.json(),
            policiesResponse.json(),
          ]);

        if (!patientResponse.ok) {
          throw new Error(
            patientData.error ||
              `Unable to load patient (${patientResponse.status})`,
          );
        }

        if (!settingsResponse.ok) {
          throw new Error(
            settingsData.error ||
              `Unable to load billing settings (${settingsResponse.status})`,
          );
        }

        if (!policiesResponse.ok) {
          throw new Error(
            policiesData.error ||
              `Unable to load insurance policies (${policiesResponse.status})`,
          );
        }

        setPatient(patientData);
        setBillingComments(settingsData.billing_comments);
        setPolicies(policiesData);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load patient billing",
        );
      } finally {
        setLoading(false);
      }
    }

    if (params.id) {
      loadBilling();
    }
  }, [params.id, router]);

  async function saveSettings(event: FormEvent) {
    event.preventDefault();

    setSettingsMessage("");
    setSettingsError("");
    setSavingSettings(true);

    try {
      const response = await apiFetch(
        `/api/patients/${params.id}/billing-settings`,
        {
          method: "PUT",
          body: JSON.stringify({
            billing_comments: billingComments,
          }),
        },
      );

      const data = await response.json();

      if (!response.ok) {
        throw new Error(
          data.error ||
            `Unable to save billing settings (${response.status})`,
        );
      }

      setBillingComments(data.billing_comments);
      setSettingsMessage("Billing settings saved.");
    } catch (err) {
      setSettingsError(
        err instanceof Error
          ? err.message
          : "Unable to save billing settings",
      );
    } finally {
      setSavingSettings(false);
    }
  }

  async function togglePolicyStatus(policy: InsurancePolicyListItem) {
    setPolicyMessage("");
    setPolicyError("");

    const response = await apiFetch(
      `/api/insurance-policies/${policy.id}/status`,
      {
        method: "PATCH",
        body: JSON.stringify({
          is_active: !policy.is_active,
        }),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      setPolicyError(
        data.error || "Unable to update insurance policy status",
      );
      return;
    }

    setPolicies((current) =>
      current.map((item) =>
        item.id === policy.id
          ? { ...item, is_active: data.is_active }
          : item,
      ),
    );

    setPolicyMessage(
      data.is_active
        ? "Insurance policy enabled."
        : "Insurance policy disabled.",
    );
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href={`/patients/${params.id}`}
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Patient
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {loading && <p>Loading billing...</p>}

        {error && (
          <div className="rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {patient && (
          <>
            <div className="mb-6">
              <h1 className="text-2xl font-semibold text-slate-900">
                {patient.first_name} {patient.last_name}
              </h1>

              <p className="mt-1 text-sm text-slate-500">
                Billing settings &amp; insurance
              </p>

              <nav aria-label="Patient billing" className="mt-3 flex flex-wrap gap-4 text-sm">
                <Link href={`/patients/${params.id}/billing/statements`} className="font-medium underline">
                  Statements
                </Link>
                <Link href={`/patients/${params.id}/billing/superbills`} className="font-medium underline">
                  Superbills
                </Link>
              </nav>
            </div>

            <BalanceSummary patientId={params.id} />

            <form
              onSubmit={saveSettings}
              className="rounded-xl border bg-white p-6"
            >
              <h2 className="text-lg font-semibold text-slate-900">
                Billing Settings
              </h2>

              <label className="mb-1 mt-5 block text-sm font-medium text-slate-700">
                Billing comments
              </label>
              <textarea
                rows={4}
                className={inputClass}
                value={billingComments}
                onChange={(e) => setBillingComments(e.target.value)}
              />

              {settingsMessage && (
                <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                  {settingsMessage}
                </div>
              )}

              {settingsError && (
                <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">
                  {settingsError}
                </div>
              )}

              <div className="mt-4 flex justify-end">
                <button
                  disabled={savingSettings}
                  className="rounded-lg bg-slate-900 px-5 py-2.5 text-sm font-medium text-white disabled:opacity-50"
                >
                  {savingSettings ? "Saving..." : "Save Billing Settings"}
                </button>
              </div>
            </form>

            <section className="mt-8">
              <div className="flex items-center justify-between gap-4">
                <h2 className="text-lg font-semibold text-slate-900">
                  Insurance Policies
                </h2>

                <Link
                  href={`/patients/${params.id}/billing/insurance/new`}
                  className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
                >
                  Add Insurance Policy
                </Link>
              </div>

              {policyMessage && (
                <div className="mt-4 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                  {policyMessage}
                </div>
              )}

              {policyError && (
                <div className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">
                  {policyError}
                </div>
              )}

              <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
                {policies.length === 0 ? (
                  <div className="p-8 text-center text-slate-500">
                    No insurance policies yet.
                  </div>
                ) : (
                  <table className="w-full text-left text-sm">
                    <thead className="border-b bg-slate-50">
                      <tr>
                        <th className="px-5 py-3">Payer</th>
                        <th className="px-5 py-3">Priority</th>
                        <th className="px-5 py-3">Member ID</th>
                        <th className="px-5 py-3">Plan</th>
                        <th className="px-5 py-3">Coverage</th>
                        <th className="px-5 py-3">Status</th>
                        <th className="px-5 py-3"></th>
                      </tr>
                    </thead>

                    <tbody>
                      {policies.map((policy) => (
                        <tr
                          key={policy.id}
                          className={`border-b last:border-0 ${
                            policy.is_active ? "" : "bg-slate-50 text-slate-500"
                          }`}
                        >
                          <td className="px-5 py-4 font-medium">
                            {policy.payer_name}
                          </td>

                          <td className="px-5 py-4">
                            {priorityLabel(policy.priority)}
                          </td>

                          <td className="px-5 py-4">
                            {policy.member_id || "—"}
                          </td>

                          <td className="px-5 py-4">
                            {policy.plan_name || "—"}
                          </td>

                          <td className="whitespace-nowrap px-5 py-4">
                            {formatCoverage(
                              policy.coverage_start,
                              policy.coverage_end,
                            )}
                          </td>

                          <td className="px-5 py-4">
                            <span
                              className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                                policy.is_active
                                  ? "bg-green-100 text-green-800"
                                  : "bg-slate-200 text-slate-600"
                              }`}
                            >
                              {policy.is_active ? "Active" : "Disabled"}
                            </span>
                          </td>

                          <td className="whitespace-nowrap px-5 py-4 text-right">
                            <Link
                              href={`/patients/${params.id}/billing/insurance/${policy.id}`}
                              className="font-medium text-slate-900 underline"
                            >
                              View / Edit
                            </Link>

                            <button
                              type="button"
                              onClick={() => togglePolicyStatus(policy)}
                              className="ml-4 font-medium text-slate-900 underline"
                            >
                              {policy.is_active ? "Disable" : "Enable"}
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            </section>

            <TransactionsSection patientId={params.id} />

            <div id="patient-payments" className="scroll-mt-4">
              <PaymentsSection patientId={params.id} />
            </div>

            <PatientClaimsSection patientId={params.id} />

            <DiagnosesSection patientId={params.id} />

            <CashRatesSection patientId={params.id} />
          </>
        )}
      </div>
    </main>
  );
}
