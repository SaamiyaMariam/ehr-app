"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";

import ServiceCodeForm from "@/components/service-code-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ServiceCode, emptyServiceCode } from "@/types/service-code";

export default function NewServiceCodePage() {
  const router = useRouter();

  async function createServiceCode(serviceCode: ServiceCode) {
    const response = await apiFetch("/api/service-codes", {
      method: "POST",
      body: JSON.stringify(serviceCode),
    });

    if (response.status === 401) {
      clearToken();
      router.replace("/login");
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error || `Unable to create service code (${response.status})`,
      );
    }

    router.push("/settings/service-codes");
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href="/settings/service-codes"
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Service Codes
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">
          Add Service Code
        </h1>

        <div className="mt-6">
          <ServiceCodeForm
            initialServiceCode={emptyServiceCode}
            submitLabel="Create Service Code"
            onSubmit={createServiceCode}
          />
        </div>
      </div>
    </main>
  );
}
