"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { ErrorBox, inputClass, labelClass, primaryButtonClass, secondaryButtonClass } from "@/lib/ui";

export type DialogField = {
  name: string;
  label: string;
  type?: "text" | "date" | "textarea" | "select" | "number";
  required?: boolean;
  options?: { value: string; label: string }[];
  defaultValue?: string;
  maxLength?: number;
  help?: string;
  // Only shown when another field has a given value.
  showWhen?: { field: string; values: string[] };
};

type Props = {
  open: boolean;
  title: string;
  description?: string;
  fields: DialogField[];
  confirmLabel: string;
  onSubmit: (values: Record<string, string>) => Promise<void>;
  onClose: () => void;
};

// Accessible modal form for workflow actions that need a few inputs.
export default function FormDialog({ open, title, description, fields, confirmLabel, onSubmit, onClose }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const initial = () => Object.fromEntries(fields.map((f) => [f.name, f.defaultValue ?? ""]));
  const [values, setValues] = useState<Record<string, string>>(initial);
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

  const visible = fields.filter((f) => !f.showWhen || f.showWhen.values.includes(values[f.showWhen.field] ?? ""));

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");

    for (const f of visible) {
      if (f.required && !(values[f.name] ?? "").trim()) {
        setError(`${f.label} is required.`);
        return;
      }
    }

    setBusy(true);

    try {
      await onSubmit(Object.fromEntries(visible.map((f) => [f.name, (values[f.name] ?? "").trim()])));
      setValues(initial());
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
      aria-labelledby="form-dialog-title"
      className="w-full max-w-lg rounded-xl p-0 backdrop:bg-slate-900/40"
    >
      <form onSubmit={submit} className="space-y-4 p-6">
        <h2 id="form-dialog-title" className="text-lg font-semibold text-slate-900">{title}</h2>
        {description && <p className="text-sm text-slate-600">{description}</p>}

        {visible.map((f) => {
          const id = `fd-${f.name}`;
          const common = {
            id,
            className: inputClass,
            value: values[f.name] ?? "",
            maxLength: f.maxLength,
          };

          return (
            <div key={f.name}>
              <label htmlFor={id} className={labelClass}>
                {f.label}{f.required ? " *" : ""}
              </label>
              {f.type === "textarea" ? (
                <textarea {...common} rows={3} onChange={(e) => setValues({ ...values, [f.name]: e.target.value })} />
              ) : f.type === "select" ? (
                <select {...common} onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}>
                  {f.options?.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              ) : (
                <input
                  {...common}
                  type={f.type ?? "text"}
                  step={f.type === "number" ? "0.01" : undefined}
                  onChange={(e) => setValues({ ...values, [f.name]: e.target.value })}
                />
              )}
              {f.help && <p className="mt-1 text-xs text-slate-500">{f.help}</p>}
            </div>
          );
        })}

        <ErrorBox message={error} />

        <div className="flex justify-end gap-3">
          <button type="button" onClick={onClose} className={secondaryButtonClass}>Cancel</button>
          <button disabled={busy} className={primaryButtonClass}>{busy ? "Working..." : confirmLabel}</button>
        </div>
      </form>
    </dialog>
  );
}
