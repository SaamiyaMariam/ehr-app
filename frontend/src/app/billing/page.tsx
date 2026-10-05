"use client";

import BillingNav from "@/components/billing-nav";
import TransactionSearch from "@/components/transaction-search";

export default function BillingPage() {
  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Billing Transactions</h1>

        <div className="mt-6">
          <TransactionSearch />
        </div>
      </div>
    </main>
  );
}
