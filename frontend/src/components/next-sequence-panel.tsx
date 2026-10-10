"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, cardClass, inputClass, labelClass, money, primaryButtonClass } from "@/lib/ui";
import { Claim, NextSequenceEval, claimStatus } from "@/types/claims";

// Statuses where an earlier payer may already have adjudicated the claim.
const adjudicationStatuses = ["submitted", "sent", "resubmitted", "paper_generated", "externally_submitted", "paid"];

function label(sequence: string) {
  return sequence ? sequence[0].toUpperCase() + sequence.slice(1) : "";
}

// "Secondary insurance" on a claim: shows whether the remaining insurance
// responsibility can be billed to the next payer and lets the biller create
// that claim. Nothing is created or sent automatically; the server decides
// the sequence, policy, services and amounts.
export default function NextSequencePanel({ claim, onMessage }: { claim: Claim; onMessage: (message: string) => void }) {
  const router = useRouter();
  const [evaluation, setEvaluation] = useState<NextSequenceEval | null>(null);
  const [policyId, setPolicyId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const relevant = adjudicationStatuses.includes(claim.status);

  useEffect(() => {
    if (!relevant) return;

    let stale = false;

    async function load() {
      const query = policyId ? `?insurance_policy_id=${policyId}` : "";
      const response = await apiFetch(`/api/claims/${claim.id}/next-sequence${query}`);
      const data = await response.json();

      if (stale) return;

      if (!response.ok) {
        setError(data.error || "Unable to evaluate the next insurance");
        return;
      }

      setError("");
      setEvaluation(data);
    }

    load();

    return () => {
      stale = true;
    };
  }, [claim.id, claim.status, claim.updated_at, policyId, relevant]);

  async function create() {
    setBusy(true);
    setError("");

    try {
      const response = await apiFetch(`/api/claims/${claim.id}/next-sequence`, {
        method: "POST",
        body: JSON.stringify(policyId ? { insurance_policy_id: policyId } : {}),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to create the claim");
      }

      onMessage(`${label(data.sequence)} claim ${data.claim_number} created.`);
      router.push("/billing/claims");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to create the claim");
      setBusy(false);
    }
  }

  if (!relevant || !evaluation) return null;

  const next = evaluation.next_sequence;
  // The last sequence has nothing to show.
  if (!next) return null;

  const existing = evaluation.existing_claims[0];

  return (
    <section className={cardClass} aria-labelledby="next-insurance-heading" data-testid="next-insurance">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="next-insurance-heading" className="text-lg font-semibold text-slate-900">{label(next)} Insurance</h2>
        <Badge tone={evaluation.can_create ? "green" : "slate"}>
          {evaluation.can_create ? `Eligible for ${label(next)} Claim` : existing ? `${label(next)} claim exists` : "Not eligible"}
        </Badge>
      </div>

      {evaluation.can_create && evaluation.policy ? (
        <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-3">
          <div>
            <dt className="text-slate-500">Policy</dt>
            <dd className="font-medium" data-testid="next-policy">
              {evaluation.policy.payer_name}
              <span className="block text-xs font-normal text-slate-500">Member {evaluation.policy.member_id || "—"}{evaluation.policy.plan_name && ` · ${evaluation.policy.plan_name}`}</span>
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">Eligible amount</dt>
            <dd className="font-medium" data-testid="next-eligible">{money(evaluation.eligible_amount)}</dd>
          </div>
          <div>
            <dt className="text-slate-500">Status</dt>
            <dd className="font-medium">Eligible</dd>
          </div>
        </dl>
      ) : (
        <p className="mt-3 text-sm text-slate-700" data-testid="next-reason">{evaluation.reason}</p>
      )}

      {evaluation.needs_policy_choice && (
        <div className="mt-4 max-w-md">
          <label htmlFor="next-policy-choice" className={labelClass}>Which {next} policy should be billed?</label>
          <select id="next-policy-choice" className={inputClass} value={policyId} onChange={(e) => setPolicyId(e.target.value)}>
            <option value="">Choose a policy</option>
            {evaluation.candidate_policies.map((p) => (
              <option key={p.id} value={p.id}>{p.payer_name} · member {p.member_id}</option>
            ))}
          </select>
        </div>
      )}

      {existing && (
        <p className="mt-3 text-sm">
          <Link href={`/billing/claims/${existing.id}`} className="underline">{existing.claim_number}</Link>{" "}
          <Badge tone={claimStatus(existing.status).tone}>{claimStatus(existing.status).label}</Badge>
        </p>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-3">
        <button type="button" disabled={busy || !evaluation.can_create} onClick={create} className={primaryButtonClass}>
          {busy ? "Creating..." : `Create ${label(next)} Claim`}
        </button>
        {evaluation.can_create && evaluation.lines.some((l) => !l.eligible) && (
          <span className="text-xs text-slate-500">Services that are not eligible are left off the claim.</span>
        )}
      </div>

      <div className="mt-3"><ErrorBox message={error} /></div>
    </section>
  );
}
