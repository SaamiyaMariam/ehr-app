"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { ErrorBox, inputClass, labelClass, secondaryButtonClass } from "@/lib/ui";

type Props = {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  // When set, a (required) reason text box is shown.
  reasonLabel?: string;
  danger?: boolean;
  onConfirm: (reason: string) => Promise<void>;
  onClose: () => void;
};

// Accessible confirmation for dangerous actions (void, refund, submit…).
export default function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  reasonLabel,
  danger = true,
  onConfirm,
  onClose,
}: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open && !dialog.open) {
      dialog.showModal();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");

    if (reasonLabel && !reason.trim()) {
      setError(`${reasonLabel} is required.`);
      return;
    }

    setBusy(true);

    try {
      await onConfirm(reason.trim());
      setReason("");
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={dialogRef}
      onClose={onClose}
      aria-labelledby="confirm-dialog-title"
      className="w-full max-w-md rounded-xl p-0 backdrop:bg-slate-900/40"
    >
      <form onSubmit={handleSubmit} className="space-y-4 p-6">
        <h2 id="confirm-dialog-title" className="text-lg font-semibold text-slate-900">
          {title}
        </h2>
        <p className="text-sm text-slate-600">{description}</p>

        {reasonLabel && (
          <div>
            <label htmlFor="confirm-dialog-reason" className={labelClass}>
              {reasonLabel}
            </label>
            <textarea
              id="confirm-dialog-reason"
              rows={3}
              maxLength={500}
              className={inputClass}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </div>
        )}

        <ErrorBox message={error} />

        <div className="flex justify-end gap-3">
          <button type="button" onClick={onClose} className={secondaryButtonClass}>
            Cancel
          </button>
          <button
            disabled={busy}
            className={`rounded-lg px-4 py-2 text-sm font-medium text-white disabled:opacity-50 ${
              danger ? "bg-red-700" : "bg-slate-900"
            }`}
          >
            {busy ? "Working..." : confirmLabel}
          </button>
        </div>
      </form>
    </dialog>
  );
}
