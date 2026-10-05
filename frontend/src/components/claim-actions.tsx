"use client";

import { useState } from "react";

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

// Submission workflow actions (paper / external / electronic).
export default function ClaimActions({ claim, onChanged, onMessage, onError }: Props) {
  const [busy, setBusy] = useState(false);

  async function post(path: string, body: object, success: string) {
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
      onError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  async function download(version?: number) {
    onError("");

    try {
      await downloadFile(
        `/api/claims/${claim.id}/cms1500${version ? `?version=${version}` : ""}`,
        `${claim.claim_number}-cms1500.pdf`,
      );
    } catch (err) {
      onError(err instanceof Error ? err.message : "Download failed");
    }
  }

  const documents = claim.documents ?? [];
  const isPaper = claim.submission_method === "paper";
  const firstPaperGeneration = isPaper && ["draft", "validation_error", "ready"].includes(claim.status);

  return (
    <div className="mt-4 space-y-4">
      {claim.status === "validation_error" && (
        <p className="text-sm text-slate-600">
          Fix the validation errors (update the patient, policy, payer or billing profiles), then
          use Refresh Claim Data.
        </p>
      )}

      {isPaper && claim.status !== "voided" && (
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              post(
                "/cms1500",
                {},
                firstPaperGeneration
                  ? "CMS-1500 generated. Print and mail it, then mark the claim as mailed."
                  : "A new copy of the CMS-1500 was generated.",
              )
            }
            className={firstPaperGeneration ? primaryButtonClass : secondaryButtonClass}
          >
            {firstPaperGeneration ? "Generate CMS-1500" : "Generate New Copy"}
          </button>
          {documents.length > 0 && (
            <button type="button" onClick={() => download()} className={secondaryButtonClass}>
              Download CMS-1500
            </button>
          )}
        </div>
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
                  · {d.page_count} page(s) · {formatTimestamp(d.created_at)} · {d.generated_by}
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
    </div>
  );
}
