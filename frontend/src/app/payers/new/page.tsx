"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";

import PayerForm from "@/components/payer-form";
import { apiFetch } from "@/lib/api";
import { emptyPayer, Payer } from "@/types/payer";

export default function NewPayerPage() {
  const router = useRouter();

  async function createPayer(payer: Payer) {
    const response = await apiFetch("/api/payers", {
      method: "POST",
      body: JSON.stringify(payer),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to create payer");
    }

    if (!data.id) {
      throw new Error("Payer created but server returned no ID");
    }

    router.push("/payers");
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href="/payers">
            ← Back to Payers
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold">
          Add Payer
        </h1>

        <div className="mt-6">
          <PayerForm
            initialPayer={emptyPayer}
            submitLabel="Create Payer"
            onSubmit={createPayer}
          />
        </div>
      </div>
    </main>
  );
}
