"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import ServiceCodeForm from "@/components/service-code-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ServiceCode } from "@/types/service-code";

export default function ServiceCodePage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [serviceCode, setServiceCode] = useState<ServiceCode | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadServiceCode() {
      try {
        const response = await apiFetch(`/api/service-codes/${params.id}`);

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        const data = await response.json();

        if (!response.ok) {
          throw new Error(
            data.error || `Unable to load service code (${response.status})`,
          );
        }

        setServiceCode(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load service code",
        );
      } finally {
        setLoading(false);
      }
    }

    if (params.id) {
      loadServiceCode();
    }
  }, [params.id, router]);

  async function updateServiceCode(updated: ServiceCode) {
    setMessage("");
    setError("");

    const response = await apiFetch(`/api/service-codes/${params.id}`, {
      method: "PUT",
      body: JSON.stringify(updated),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error || `Unable to update service code (${response.status})`,
      );
    }

    setServiceCode(data);
    setMessage("Service code updated successfully.");
  }

  async function toggleStatus() {
    if (!serviceCode) {
      return;
    }

    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/service-codes/${params.id}/status`,
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

    setServiceCode({
      ...serviceCode,
      is_active: data.is_active,
    });

    setMessage(
      data.is_active
        ? "Service code enabled."
        : "Service code disabled.",
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href="/settings/service-codes"
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Service Codes
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {loading && <p>Loading service code...</p>}

        {error && (
          <div className="mb-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {serviceCode && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">
                  {serviceCode.code}
                </h1>

                <p className="mt-1 text-sm text-slate-500">
                  {serviceCode.is_active ? "Enabled" : "Disabled"}
                </p>
              </div>

              <button
                type="button"
                onClick={toggleStatus}
                className="rounded-lg border bg-white px-4 py-2 text-sm font-medium"
              >
                {serviceCode.is_active
                  ? "Disable Service Code"
                  : "Enable Service Code"}
              </button>
            </div>

            {message && (
              <div className="mt-5 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                {message}
              </div>
            )}

            <div className="mt-6">
              <ServiceCodeForm
                key={serviceCode.id}
                initialServiceCode={serviceCode}
                submitLabel="Save Changes"
                onSubmit={updateServiceCode}
              />
            </div>
          </>
        )}
      </div>
    </main>
  );
}
