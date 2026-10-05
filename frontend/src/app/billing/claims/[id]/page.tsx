"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useState } from "react";

import BillingNav from "@/components/billing-nav";
import ClaimActions from "@/components/claim-actions";
import ConfirmDialog from "@/components/confirm-dialog";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  Badge,
  ErrorBox,
  SuccessBox,
  cardClass,
  inputClass,
  money,
  primaryButtonClass,
  secondaryButtonClass,
  titleCase,
} from "@/lib/ui";
import { Claim, claimStatus, editableClaimStatuses, formatTimestamp, submissionMethodLabels } from "@/types/claims";

function Address({ a }: { a: { address_1: string; address_2: string; city: string; state: string; zip: string } }) {
  if (!a.address_1) return <span className="text-red-700">Missing address</span>;
  return (
    <>
      {a.address_1}
      {a.address_2 && `, ${a.address_2}`}
      <br />
      {a.city}, {a.state} {a.zip}
    </>
  );
}

export default function ClaimPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [claim, setClaim] = useState<Claim | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [busy, setBusy] = useState(false);
  const [comment, setComment] = useState("");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const reload = useCallback(() => setReloadKey((k) => k + 1), []);

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/claims/${params.id}`);

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load claim");
        return;
      }

      setClaim(data);
    }

    load();
  }, [params.id, router, reloadKey]);

  async function action(path: string, body: object, success: string) {
    setMessage("");
    setError("");
    setBusy(true);

    try {
      const response = await apiFetch(`/api/claims/${params.id}${path}`, {
        method: path === "" ? "PUT" : "POST",
        body: JSON.stringify(body),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Action failed");
      }

      setMessage(success);
      reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  async function addComment(event: FormEvent) {
    event.preventDefault();
    if (!comment.trim()) return;
    await action("/comments", { comment }, "Comment added.");
    setComment("");
  }

  async function cancelClaim(reason: string) {
    const response = await apiFetch(`/api/claims/${params.id}/cancel`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    });
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to cancel claim");
    }

    setMessage("Claim cancelled. Its services can be billed again.");
    reload();
  }

  const status = claim ? claimStatus(claim.status) : null;
  const editable = claim ? editableClaimStatuses.includes(claim.status) : false;
  const s = claim?.snapshot;

  return (
    <main className="min-h-screen bg-slate-100">
      <BillingNav />

      <div className="mx-auto max-w-6xl space-y-6 px-6 py-8">
        <Link href="/billing/claims" className="text-sm font-medium text-slate-600">
          ← Back to Claims
        </Link>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        {claim && status && s && (
          <>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">Claim {claim.claim_number}</h1>
                <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-600">
                  <Link href={`/patients/${claim.patient_id}/billing`} className="underline">{claim.patient_name}</Link>
                  · {claim.payer_name} · <span className="capitalize">{claim.sequence}</span> ·{" "}
                  {submissionMethodLabels[claim.submission_method]}
                  <Badge tone={status.tone}>{status.label}</Badge>
                  {claim.resubmission_type !== "new" && (
                    <Badge tone="amber">{titleCase(claim.resubmission_type)} resubmission</Badge>
                  )}
                </p>
              </div>
              <div className="text-right">
                <div className="text-sm text-slate-500">Total billed</div>
                <div className="text-2xl font-semibold">{money(claim.total_billed)}</div>
              </div>
            </div>

            {claim.validation && (claim.validation.errors.length > 0 || claim.validation.warnings.length > 0) && (
              <section className={cardClass} aria-labelledby="validation-heading">
                <h2 id="validation-heading" className="text-lg font-semibold text-slate-900">
                  Validation
                  <span className="ml-2 text-xs font-normal text-slate-500">{formatTimestamp(claim.validated_at)}</span>
                </h2>
                {claim.validation.errors.length > 0 && (
                  <ul className="mt-3 list-inside list-disc space-y-1 text-sm text-red-800">
                    {claim.validation.errors.map((e) => (
                      <li key={e}><strong>Error:</strong> {e}</li>
                    ))}
                  </ul>
                )}
                {claim.validation.warnings.length > 0 && (
                  <ul className="mt-3 list-inside list-disc space-y-1 text-sm text-amber-900">
                    {claim.validation.warnings.map((w) => (
                      <li key={w}><strong>Warning:</strong> {w}</li>
                    ))}
                  </ul>
                )}
                <p className="mt-3 text-xs text-slate-500">
                  Checks completeness of the data this practice controls. It is not payer-specific
                  or X12 compliance validation; a payer or clearinghouse may still reject the claim.
                </p>
              </section>
            )}

            <section className={cardClass}>
              <h2 className="text-lg font-semibold text-slate-900">Actions</h2>
              <div className="mt-4 flex flex-wrap gap-3">
                {editable && (
                  <>
                    <button type="button" disabled={busy} onClick={() => action("/validate", {}, "Claim validated.")} className={secondaryButtonClass}>
                      Validate
                    </button>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() =>
                        action("", { refresh_snapshot: true, resubmission_type: claim.resubmission_type, payer_claim_control_number: claim.payer_claim_control_number }, "Claim data refreshed from current records.")
                      }
                      className={secondaryButtonClass}
                    >
                      Refresh Claim Data
                    </button>
                    {!claim.submitted_at && (
                      <button type="button" disabled={busy} onClick={() => setCancelOpen(true)} className={secondaryButtonClass}>
                        Cancel Claim
                      </button>
                    )}
                  </>
                )}
              </div>
              <ClaimActions claim={claim} onChanged={reload} onMessage={setMessage} onError={setError} />
            </section>

            <section className="grid gap-4 md:grid-cols-2">
              <div className={cardClass}>
                <h2 className="font-semibold text-slate-900">Patient</h2>
                <p className="mt-2 text-sm">
                  {s.patient.first_name} {s.patient.last_name} · DOB {s.patient.date_of_birth || "—"} · Sex {s.patient.sex || "—"}
                  <br />
                  <Address a={s.patient.address} />
                </p>
              </div>
              <div className={cardClass}>
                <h2 className="font-semibold text-slate-900">Insured</h2>
                <p className="mt-2 text-sm">
                  {s.insured.first_name} {s.insured.last_name} ({s.insured.relationship || "relationship missing"}) · Member {s.insured.member_id || "—"}
                  {s.insured.policy_group && ` · Group ${s.insured.policy_group}`}
                  <br />
                  <Address a={s.insured.address} />
                </p>
              </div>
              <div className={cardClass}>
                <h2 className="font-semibold text-slate-900">Payer</h2>
                <p className="mt-2 text-sm">
                  {s.payer.name} · Payer ID {s.payer.payer_id || "—"} · {titleCase(s.payer.insurance_type || "type not set")}
                  <br />
                  <Address a={s.payer.address} />
                </p>
              </div>
              <div className={cardClass}>
                <h2 className="font-semibold text-slate-900">Billing Provider</h2>
                <p className="mt-2 text-sm">
                  {s.practice.name || <span className="text-red-700">Practice name missing</span>} · NPI {s.practice.npi || "—"} · {s.practice.tax_id_type.toUpperCase()} {s.practice.tax_id ? `•••••${s.practice.tax_id.slice(-4)}` : "—"}
                  <br />
                  <Address a={s.practice.address} />
                </p>
              </div>
              {(s.other_insurance ?? []).length > 0 && (
                <div className={`${cardClass} md:col-span-2`}>
                  <h2 className="font-semibold text-slate-900">Other Insurance</h2>
                  {(s.other_insurance ?? []).map((o) => (
                    <p key={o.claim_number} className="mt-2 text-sm capitalize">
                      {o.sequence}: {o.payer_name} · Member {o.member_id} · Claim {o.claim_number} · Paid {money(o.amount_paid)}
                    </p>
                  ))}
                </div>
              )}
              <p className="text-xs text-slate-500 md:col-span-2">Data captured {formatTimestamp(s.captured_at)}.</p>
            </section>

            <section>
              <h2 className="text-lg font-semibold text-slate-900">Service Lines</h2>
              <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
                <table className="w-full text-left text-sm">
                  <thead className="border-b bg-slate-50">
                    <tr>
                      <th className="px-3 py-2">#</th>
                      <th className="px-3 py-2">Date</th>
                      <th className="px-3 py-2">Service</th>
                      <th className="px-3 py-2">Dx</th>
                      <th className="px-3 py-2">Rendering</th>
                      <th className="px-3 py-2">Auth</th>
                      <th className="px-3 py-2 text-right">Billed</th>
                      <th className="px-3 py-2 text-right">Ins. paid</th>
                    </tr>
                  </thead>
                  <tbody>
                    {claim.lines?.map((l) => (
                      <tr key={l.id} className="border-b align-top last:border-0">
                        <td className="px-3 py-2">{l.line_number}</td>
                        <td className="whitespace-nowrap px-3 py-2">{l.date_of_service}</td>
                        <td className="px-3 py-2">
                          {l.service_code}
                          {l.modifiers.length > 0 && <span className="text-slate-500"> {l.modifiers.join(" ")}</span>}
                          {l.units > 1 && ` × ${l.units}`}
                          <div className="text-xs text-slate-500">POS {l.place_of_service}</div>
                        </td>
                        <td className="px-3 py-2">{l.diagnosis_pointers || <span className="text-red-700">none</span>}</td>
                        <td className="px-3 py-2">
                          {l.rendering_name}
                          <div className="text-xs text-slate-500">NPI {l.rendering_npi || "missing"}</div>
                        </td>
                        <td className="px-3 py-2">{l.prior_authorization_code || "—"}</td>
                        <td className="whitespace-nowrap px-3 py-2 text-right">{money(l.line_total)}</td>
                        <td className="whitespace-nowrap px-3 py-2 text-right">
                          {money(l.insurance_paid)}
                          {l.adjudicated && <div className="text-xs text-green-700">Adjudicated</div>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="mt-2 text-sm text-slate-600">
                Diagnoses:{" "}
                {claim.diagnoses?.map((d) => `${d.letter}. ${d.icd10_code} ${d.description}`).join(" · ")}
              </p>
            </section>

            <section className="grid gap-6 md:grid-cols-2">
              <div className={cardClass}>
                <h2 className="text-lg font-semibold text-slate-900">History</h2>
                <ol className="mt-4 space-y-3 border-l pl-4 text-sm">
                  {claim.history?.map((h) => (
                    <li key={h.id}>
                      <div className="font-medium">
                        {titleCase(h.event_type)}
                        {h.to_status && h.to_status !== h.from_status && (
                          <span className="font-normal text-slate-500">
                            {" "}→ {claimStatus(h.to_status).label}
                          </span>
                        )}
                      </div>
                      {h.message && <div className="text-slate-700">{h.message}</div>}
                      <div className="text-xs text-slate-500">
                        {formatTimestamp(h.created_at)} · {h.source}
                        {h.created_by && ` · ${h.created_by}`}
                      </div>
                    </li>
                  ))}
                </ol>
              </div>

              <div className={cardClass}>
                <h2 className="text-lg font-semibold text-slate-900">Biller Comments</h2>
                {claim.comments?.length === 0 && <p className="mt-3 text-sm text-slate-500">No comments yet.</p>}
                <ul className="mt-3 space-y-3 text-sm">
                  {claim.comments?.map((c) => (
                    <li key={c.id} className="rounded-lg bg-slate-50 p-3">
                      <div className="whitespace-pre-wrap">{c.comment}</div>
                      <div className="mt-1 text-xs text-slate-500">{c.author} · {formatTimestamp(c.created_at)}</div>
                    </li>
                  ))}
                </ul>
                <form onSubmit={addComment} className="mt-4 space-y-2">
                  <label htmlFor="claim-comment" className="sr-only">Comment</label>
                  <textarea id="claim-comment" rows={2} maxLength={2000} className={inputClass} placeholder="Add a comment" value={comment} onChange={(e) => setComment(e.target.value)} />
                  <div className="flex justify-end">
                    <button disabled={busy || !comment.trim()} className={primaryButtonClass}>Add Comment</button>
                  </div>
                </form>
              </div>
            </section>
          </>
        )}
      </div>

      <ConfirmDialog
        open={cancelOpen}
        title="Cancel claim?"
        description="The claim is voided and its services become available for a new claim. Use this only for claims that were never sent."
        confirmLabel="Cancel Claim"
        reasonLabel="Reason"
        onConfirm={cancelClaim}
        onClose={() => setCancelOpen(false)}
      />
    </main>
  );
}
