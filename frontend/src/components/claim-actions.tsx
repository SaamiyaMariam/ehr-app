"use client";

import { useState } from "react";

import FormDialog, { DialogField } from "@/components/form-dialog";
import { apiFetch } from "@/lib/api";
import { downloadFile } from "@/lib/download";
import { linkButtonClass, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";
import { Claim, formatTimestamp } from "@/types/claims";

type Props = {
  claim: Claim;
  onChanged: () => void;
  onMessage: (message: string) => void;
  onError: (message: string) => void;
};

type DialogKey = "mail" | "external" | "reject" | "review" | "resubmit" | null;

const today = () => new Date().toLocaleDateString("en-CA");

const sentStatuses = ["submitted", "sent", "resubmitted", "externally_submitted", "pending_submission"];
const resubmittableStatuses = ["submitted", "sent", "resubmitted", "externally_submitted", "paper_generated", "rejected_new", "rejected", "paid"];

// Submission workflow actions for paper, external and electronic claims.
export default function ClaimActions({ claim, onChanged, onMessage, onError }: Props) {
  const [busy, setBusy] = useState(false);
  const [dialog, setDialog] = useState<DialogKey>(null);

  async function post(path: string, body: object, success: string, throwErrors = false) {
    onMessage("");
    onError("");
    setBusy(true);

    try {
      const response = await apiFetch(`/api/claims/${claim.id}${path}`, {
        method: "POST",
        body: JSON.stringify(body),
      });
      const data = await response.json();

      if (!response.ok) {
        const details = data.validation?.errors?.length ? ` ${data.validation.errors.join(" ")}` : "";
        throw new Error((data.error || "Action failed") + details);
      }

      onMessage(success);
      onChanged();
    } catch (err) {
      if (throwErrors) throw err;
      onError(err instanceof Error ? err.message : "Action failed");
      onChanged();
    } finally {
      setBusy(false);
    }
  }

  async function download(version?: number) {
    onError("");

    try {
      await downloadFile(`/api/claims/${claim.id}/cms1500${version ? `?version=${version}` : ""}`, `${claim.claim_number}-cms1500.pdf`);
    } catch (err) {
      onError(err instanceof Error ? err.message : "Download failed");
    }
  }

  async function previewPayload() {
    onError("");

    try {
      await downloadFile(`/api/claims/${claim.id}/electronic-payload`, `${claim.claim_number}-payload.json`);
    } catch (err) {
      onError(err instanceof Error ? err.message : "Download failed");
    }
  }

  const documents = claim.documents ?? [];
  const editable = ["draft", "validation_error", "ready"].includes(claim.status);
  const method = claim.submission_method;

  const dialogs: Record<Exclude<DialogKey, null>, { title: string; description: string; confirm: string; fields: DialogField[]; submit: (v: Record<string, string>) => Promise<void> }> = {
    mail: {
      title: "Mark claim as mailed",
      description: "Record that the printed CMS-1500 was mailed to the payer.",
      confirm: "Mark Mailed",
      fields: [
        { name: "submitted_on", label: "Mailed on", type: "date", required: true, defaultValue: today() },
        { name: "comment", label: "Comment", type: "textarea", maxLength: 2000 },
      ],
      submit: (v) => post("/mark-mailed", v, "Claim marked as mailed.", true),
    },
    external: {
      title: "Mark submitted externally",
      description: "Confirm this claim was submitted outside this application (e.g. a payer portal). Nothing is transmitted from here.",
      confirm: "Mark Submitted",
      fields: [
        { name: "submitted_on", label: "Submitted on", type: "date", required: true, defaultValue: today() },
        { name: "reference", label: "Reference (optional)", maxLength: 100, help: "e.g. the confirmation number shown by the payer portal" },
        { name: "comment", label: "Comment", type: "textarea", maxLength: 2000 },
      ],
      submit: (v) => post("/mark-external", v, "Claim recorded as submitted externally.", true),
    },
    reject: {
      title: "Record rejection",
      description: "Record a rejection received from the payer or clearinghouse.",
      confirm: "Record Rejection",
      fields: [
        { name: "reason", label: "Rejection reason", type: "textarea", required: true, maxLength: 2000 },
        { name: "rejected_on", label: "Rejected on", type: "date", defaultValue: today() },
      ],
      submit: (v) => post("/record-rejection", v, "Rejection recorded.", true),
    },
    review: {
      title: "Mark rejection reviewed",
      description: "Mark that a biller reviewed this rejection.",
      confirm: "Mark Reviewed",
      fields: [{ name: "comment", label: "Comment", type: "textarea", maxLength: 2000 }],
      submit: (v) => post("/mark-rejection-reviewed", v, "Rejection marked as reviewed.", true),
    },
    resubmit: {
      title: "Start resubmission",
      description: "Reopen this claim for correction. Amended and void claims must reference the payer's original claim control number.",
      confirm: "Start Resubmission",
      fields: [
        {
          name: "resubmission_type", label: "Resubmission type", type: "select", required: true, defaultValue: claim.submitted_at ? "amended" : "new",
          options: [
            { value: "new", label: "New claim" },
            { value: "amended", label: "Amended / replacement claim (7)" },
            { value: "void", label: "Void claim (8)" },
          ],
        },
        {
          name: "payer_claim_control_number", label: "Payer claim control number", maxLength: 50, required: true,
          showWhen: { field: "resubmission_type", values: ["amended", "void"] }, defaultValue: claim.payer_claim_control_number,
        },
      ],
      submit: (v) => post("/start-resubmission", v, "Claim reopened for resubmission. Refresh its data, then submit again.", true),
    },
  };

  const active = dialog ? dialogs[dialog] : null;

  return (
    <div className="mt-4 space-y-4">
      {claim.status === "validation_error" && (
        <p className="text-sm text-slate-600">
          Fix the validation errors (update the patient, policy, payer or billing profiles), then use
          Refresh Claim Data.
        </p>
      )}

      <div className="flex flex-wrap items-center gap-3">
        {method === "paper" && claim.status !== "voided" && (
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              post("/cms1500", {}, editable ? "CMS-1500 generated. Print and mail it, then mark the claim as mailed." : "A new copy of the CMS-1500 was generated.")
            }
            className={editable ? primaryButtonClass : secondaryButtonClass}
          >
            {editable ? "Generate CMS-1500" : "Generate New Copy"}
          </button>
        )}
        {documents.length > 0 && (
          <button type="button" onClick={() => download()} className={secondaryButtonClass}>Download CMS-1500</button>
        )}
        {method === "paper" && claim.status === "paper_generated" && (
          <button type="button" disabled={busy} onClick={() => setDialog("mail")} className={primaryButtonClass}>Mark as Mailed</button>
        )}

        {method === "external" && editable && (
          <button type="button" disabled={busy} onClick={() => setDialog("external")} className={primaryButtonClass}>Mark Submitted Externally</button>
        )}

        {method === "electronic" && editable && (
          <>
            <button type="button" disabled={busy} onClick={() => post("/submit-electronic", {}, "Claim submitted electronically.")} className={primaryButtonClass}>
              Submit Electronically
            </button>
            <button type="button" onClick={previewPayload} className={secondaryButtonClass}>Download Payload Preview</button>
          </>
        )}

        {sentStatuses.includes(claim.status) && (
          <button type="button" disabled={busy} onClick={() => setDialog("reject")} className={secondaryButtonClass}>Record Rejection</button>
        )}
        {claim.status === "rejected_new" && (
          <button type="button" disabled={busy} onClick={() => setDialog("review")} className={primaryButtonClass}>Mark Rejection Reviewed</button>
        )}
        {resubmittableStatuses.includes(claim.status) && (
          <button type="button" disabled={busy} onClick={() => setDialog("resubmit")} className={secondaryButtonClass}>Start Resubmission</button>
        )}
      </div>

      {method === "electronic" && editable && (
        <p className="text-xs text-slate-500">
          Electronic submission requires a configured clearinghouse integration. Without one, the
          claim is validated and prepared but nothing is sent.
        </p>
      )}

      {documents.length > 0 && (
        <div className="text-sm">
          <h3 className="font-medium text-slate-900">Generated documents</h3>
          <ul className="mt-2 space-y-1">
            {documents.map((d) => (
              <li key={d.id}>
                <button type="button" onClick={() => download(d.version)} className={linkButtonClass}>
                  CMS-1500 v{d.version}
                </button>{" "}
                <span className="text-slate-500">
                  · frequency {d.frequency_code} · {d.page_count} page(s) · {formatTimestamp(d.created_at)} · {d.generated_by}
                </span>
              </li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-slate-500">
            CMS-1500-compatible field layout; not the official red OCR form. Downloading a copy does
            not change the claim or use prior authorizations.
          </p>
        </div>
      )}

      {active && (
        <FormDialog
          key={dialog}
          open
          title={active.title}
          description={active.description}
          fields={active.fields}
          confirmLabel={active.confirm}
          onSubmit={active.submit}
          onClose={() => setDialog(null)}
        />
      )}
    </div>
  );
}
