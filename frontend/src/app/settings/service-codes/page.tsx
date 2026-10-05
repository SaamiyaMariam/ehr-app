"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ServiceCode, formatRate } from "@/types/service-code";

export default function ServiceCodesPage() {
  const router = useRouter();

  const [serviceCodes, setServiceCodes] = useState<ServiceCode[]>([]);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadServiceCodes() {
      try {
        const response = await apiFetch("/api/service-codes");

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load service codes");
        }

        setServiceCodes(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load service codes",
        );
      } finally {
        setLoading(false);
      }
    }

    loadServiceCodes();
  }, [router]);

  async function toggleStatus(serviceCode: ServiceCode) {
    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/service-codes/${serviceCode.id}/status`,
      {
        method: "PATCH",
        body: JSON.stringify({
          is_active: !serviceCode.is_active,
        }),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update service code status");
      return;
    }

    setServiceCodes((current) =>
      current.map((item) =>
        item.id === serviceCode.id
          ? { ...item, is_active: data.is_active }
          : item,
      ),
    );

    setMessage(
      data.is_active
        ? `Service code ${serviceCode.code} enabled.`
        : `Service code ${serviceCode.code} disabled.`,
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-4">
          <Link href="/dashboard" className="font-semibold">
            EHR
          </Link>

          <Link
            href="/settings/service-codes/new"
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
          >
            Add Service Code
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">
          Service Codes
        </h1>

        {loading && (
          <p className="mt-6 text-slate-500">
            Loading service codes...
          </p>
        )}

        {message && (
          <div className="mt-6 rounded-lg bg-green-50 p-3 text-sm text-green-700">
            {message}
          </div>
        )}

        {error && (
          <div className="mt-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {!loading && (
          <div className="mt-6 overflow-x-auto rounded-xl border bg-white">
            {serviceCodes.length === 0 ? (
              <div className="p-8 text-center text-slate-500">
                No service codes yet.
              </div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-5 py-3">Code</th>
                    <th className="px-5 py-3">Description</th>
                    <th className="px-5 py-3">Add-on</th>
                    <th className="px-5 py-3">Standard Rate</th>
                    <th className="px-5 py-3">Status</th>
                    <th className="px-5 py-3"></th>
                  </tr>
                </thead>

                <tbody>
                  {serviceCodes.map((serviceCode) => (
                    <tr
                      key={serviceCode.id}
                      className={`border-b last:border-0 ${
                        serviceCode.is_active
                          ? ""
                          : "bg-slate-50 text-slate-500"
                      }`}
                    >
                      <td className="px-5 py-4 font-medium">
                        {serviceCode.code}
                      </td>

                      <td className="px-5 py-4">
                        {serviceCode.description}
                      </td>

                      <td className="px-5 py-4">
                        {serviceCode.is_add_on
                          ? serviceCode.allow_multiple_units
                            ? "Yes (multiple units)"
                            : "Yes"
                          : "No"}
                      </td>

                      <td className="px-5 py-4">
                        {formatRate(serviceCode.standard_rate)}
                      </td>

                      <td className="px-5 py-4">
                        <span
                          className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                            serviceCode.is_active
                              ? "bg-green-100 text-green-800"
                              : "bg-slate-200 text-slate-600"
                          }`}
                        >
                          {serviceCode.is_active ? "Enabled" : "Disabled"}
                        </span>
                      </td>

                      <td className="whitespace-nowrap px-5 py-4 text-right">
                        <Link
                          href={`/settings/service-codes/${serviceCode.id}`}
                          className="font-medium underline"
                        >
                          View / Edit
                        </Link>

                        <button
                          type="button"
                          onClick={() => toggleStatus(serviceCode)}
                          className="ml-4 font-medium underline"
                        >
                          {serviceCode.is_active ? "Disable" : "Enable"}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </main>
  );
}
