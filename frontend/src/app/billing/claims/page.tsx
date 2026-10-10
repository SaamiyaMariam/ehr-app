"use client";

import { useSearchParams } from "next/navigation";
import { Suspense } from "react";

import BillingNav from "@/components/billing-nav";
import ClaimList from "@/components/claim-list";
import { claimQueueLabels } from "@/types/reports";

const filterKeys = ["queue", "status", "payer_id", "clinician_id", "sequence", "submission_method", "patient", "claim_number", "from", "to"];

function Claims() {
  const params = useSearchParams();
  const initial: Record<string, string> = {};

  for (const key of filterKeys) {
    const value = params.get(key);
    if (value) initial[key] = value;
  }

  const queue = initial.queue ? claimQueueLabels[initial.queue] : "";

  return (
    <>
      <h1 className="text-2xl font-semibold text-slate-900">{queue || "Claims"}</h1>
      <p className="mt-1 text-sm text-slate-500">
        {queue ? "Showing this work queue. Clear the work queue filter to see every claim." : "Create claims from a patient's billing page (Create Claim)."}
      </p>

      <div className="mt-6">
        <ClaimList key={JSON.stringify(initial)} initial={initial} />
      </div>
    </>
  );
}

export default function ClaimsPage() {
  return (
    <main className="flex-1 bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl px-6 py-8">
        <Suspense fallback={<p className="text-sm text-slate-500">Loading...</p>}>
          <Claims />
        </Suspense>
      </div>
    </main>
  );
}
