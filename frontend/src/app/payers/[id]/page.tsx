"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import PayerForm from "@/components/payer-form";
import PayerRatesSection from "@/components/payer-rates-section";
import { apiFetch } from "@/lib/api";
import { Payer } from "@/types/payer";

export default function PayerPage() {
  const params = useParams<{ id: string }>();

  const [payer, setPayer] = useState<Payer | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPayer() {
      const response = await apiFetch(
        `/api/payers/${params.id}`
      );

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load payer");
        return;
      }

      setPayer(data);
    }

    loadPayer();
  }, [params.id]);

  async function updatePayer(updatedPayer: Payer) {
    setMessage("");

    const response = await apiFetch(
      `/api/payers/${params.id}`,
      {
        method: "PUT",
        body: JSON.stringify(updatedPayer),
      }
    );

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to update payer");
    }

    setPayer(data);
    setMessage("Payer updated successfully.");
  }

  async function toggleStatus() {
    if (!payer) {
      return;
    }

    setMessage("");

    const response = await apiFetch(
      `/api/payers/${params.id}/status`,
      {
        method: "PATCH",
        body: JSON.stringify({
          is_active: !payer.is_active,
        }),
      }
    );

    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update payer status");
      return;
    }

    setPayer({
      ...payer,
      is_active: data.is_active,
    });

    setMessage(
      data.is_active
        ? "Payer enabled successfully."
        : "Payer disabled successfully."
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href="/payers">
            ← Back to Payers
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {error && (
          <div className="mb-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {payer && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold">
                  {payer.payer_name}
                </h1>

                <p className="mt-1 text-sm text-slate-500">
                  {payer.is_active ? "Enabled" : "Disabled"}
                </p>
              </div>

              <button
                type="button"
                onClick={toggleStatus}
                className="rounded-lg border px-4 py-2 text-sm font-medium"
              >
                {payer.is_active
                  ? "Disable Payer"
                  : "Enable Payer"}
              </button>
            </div>

            {message && (
              <div className="mt-5 rounded-lg bg-green-50 p-3 text-green-700">
                {message}
              </div>
            )}

            <div className="mt-6">
              <PayerForm
                key={`${payer.id}-${payer.is_active}`}
                initialPayer={payer}
                submitLabel="Save Changes"
                onSubmit={updatePayer}
              />
            </div>

            <PayerRatesSection payerId={params.id} />
          </>
        )}
      </div>
    </main>
  );
}
