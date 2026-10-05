"use client";

import { money } from "@/lib/ui";
import { AgingTotals, bucketKeys } from "@/types/reports";

// Bucket totals as buttons: choosing one narrows the report to that bucket.
export default function AgingTotalsCards({
  totals,
  active,
  onSelect,
}: {
  totals: AgingTotals | null;
  active: string;
  onSelect: (bucket: string) => void;
}) {
  return (
    <div className="grid gap-3 sm:grid-cols-3 lg:grid-cols-5" role="group" aria-label="Aging buckets">
      {bucketKeys.map((b) => (
        <button
          key={b.value}
          type="button"
          aria-pressed={active === b.value}
          data-testid={`bucket-${b.value}`}
          onClick={() => onSelect(active === b.value ? "" : b.value)}
          className={`rounded-xl border bg-white p-4 text-left hover:border-slate-400 ${active === b.value ? "border-slate-900 ring-1 ring-slate-900" : ""}`}
        >
          <div className="text-sm text-slate-500">{b.label}</div>
          <div className="mt-1 text-xl font-semibold text-slate-900">{totals ? money(totals[b.key]) : "…"}</div>
        </button>
      ))}
      <div className="rounded-xl border bg-slate-50 p-4" data-testid="bucket-total">
        <div className="text-sm text-slate-500">Total</div>
        <div className="mt-1 text-xl font-semibold text-slate-900">{totals ? money(totals.total) : "…"}</div>
      </div>
    </div>
  );
}
