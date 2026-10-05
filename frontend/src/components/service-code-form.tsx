"use client";

import { FormEvent, useState } from "react";

import { ServiceCode } from "@/types/service-code";

type Props = {
  initialServiceCode: ServiceCode;
  submitLabel: string;
  onSubmit: (serviceCode: ServiceCode) => Promise<void>;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

export default function ServiceCodeForm({
  initialServiceCode,
  submitLabel,
  onSubmit,
}: Props) {
  const [serviceCode, setServiceCode] =
    useState<ServiceCode>(initialServiceCode);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  function update<K extends keyof ServiceCode>(
    field: K,
    value: ServiceCode[K],
  ) {
    setServiceCode((current) => ({
      ...current,
      [field]: value,
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");
    setSaving(true);

    try {
      await onSubmit(serviceCode);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Unable to save service code",
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Service Code
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Service code *</label>
            <input
              required
              maxLength={50}
              className={inputClass}
              value={serviceCode.code}
              onChange={(e) => update("code", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Standard rate</label>
            <input
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              className={inputClass}
              value={serviceCode.standard_rate}
              onChange={(e) => update("standard_rate", e.target.value)}
            />
          </div>

          <div className="md:col-span-2">
            <label className={labelClass}>Description *</label>
            <input
              required
              className={inputClass}
              value={serviceCode.description}
              onChange={(e) => update("description", e.target.value)}
            />
          </div>

          <label className="flex items-center gap-3 text-sm md:col-span-2">
            <input
              type="checkbox"
              checked={serviceCode.is_add_on}
              onChange={(e) => {
                const isAddOn = e.target.checked;

                setServiceCode((current) => ({
                  ...current,
                  is_add_on: isAddOn,
                  // Multiple units only apply to add-on codes.
                  allow_multiple_units: isAddOn
                    ? current.allow_multiple_units
                    : false,
                }));
              }}
            />
            This is an add-on code
          </label>

          {serviceCode.is_add_on && (
            <label className="ml-7 flex items-center gap-3 text-sm md:col-span-2">
              <input
                type="checkbox"
                checked={serviceCode.allow_multiple_units}
                onChange={(e) =>
                  update("allow_multiple_units", e.target.checked)
                }
              />
              Allow multiple units
            </label>
          )}
        </div>
      </section>

      {error && (
        <div className="rounded-lg bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="flex justify-end">
        <button
          disabled={saving}
          className="rounded-lg bg-slate-900 px-5 py-2.5 text-sm font-medium text-white disabled:opacity-50"
        >
          {saving ? "Saving..." : submitLabel}
        </button>
      </div>
    </form>
  );
}
