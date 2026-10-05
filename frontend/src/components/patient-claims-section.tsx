"use client";

import Link from "next/link";

import ClaimList from "@/components/claim-list";
import { primaryButtonClass } from "@/lib/ui";

export default function PatientClaimsSection({ patientId }: { patientId: string }) {
  return (
    <section className="mt-8">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold text-slate-900">Claims</h2>
        <Link href={`/patients/${patientId}/billing/claims/new`} className={primaryButtonClass}>
          Create Claim
        </Link>
      </div>

      <div className="mt-4">
        <ClaimList fixed={{ patient_id: patientId }} showFilters={false} />
      </div>
    </section>
  );
}
