"use client";

import { Claim } from "@/types/claims";

type Props = {
  claim: Claim;
  onChanged: () => void;
  onMessage: (message: string) => void;
  onError: (message: string) => void;
};

// Submission workflow actions (paper / external / electronic). Extended by
// the CMS-1500 and submission modules.
export default function ClaimActions({ claim }: Props) {
  if (claim.status === "validation_error") {
    return (
      <p className="mt-3 text-sm text-slate-600">
        Fix the validation errors (update the patient, policy, payer or billing profiles), then
        use Refresh Claim Data.
      </p>
    );
  }

  return null;
}
