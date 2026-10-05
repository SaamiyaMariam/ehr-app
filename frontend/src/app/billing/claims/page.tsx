"use client";

import BillingNav from "@/components/billing-nav";
import ClaimList from "@/components/claim-list";

export default function ClaimsPage() {
  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Claims</h1>
        <p className="mt-1 text-sm text-slate-500">
          Create claims from a patient&apos;s billing page (Create Claim).
        </p>

        <div className="mt-6">
          <ClaimList />
        </div>
      </div>
    </main>
  );
}
