"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  ErrorBox,
  cardClass,
  inputClass,
  labelClass,
  money,
  primaryButtonClass,
} from "@/lib/ui";
import { RateSchedule } from "@/types/rates";
import { ServiceCode } from "@/types/service-code";

type Props = {
  initialSchedule: RateSchedule;
  submitLabel: string;
  onSubmit: (schedule: RateSchedule) => Promise<void>;
};

export default function RateScheduleForm({
  initialSchedule,
  submitLabel,
  onSubmit,
}: Props) {
  const [name, setName] = useState(initialSchedule.name);
  const [useStandard, setUseStandard] = useState(
    initialSchedule.use_standard_practice_rates,
  );
  // service_code_id → custom rate text ("" = no custom rate)
  const [rates, setRates] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      initialSchedule.items.map((item) => [item.service_code_id, item.custom_rate]),
    ),
  );
  const [serviceCodes, setServiceCodes] = useState<ServiceCode[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadServiceCodes() {
      const response = await apiFetch("/api/service-codes");

      if (!response.ok) {
        setError("Unable to load service codes");
        return;
      }

      setServiceCodes(await response.json());
    }

    loadServiceCodes();
  }, []);

  const initialIds = new Set(initialSchedule.items.map((i) => i.service_code_id));
  const rows = serviceCodes.filter(
    (code) => code.is_active || initialIds.has(code.id ?? ""),
  );

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setSaving(true);

    try {
      await onSubmit({
        ...initialSchedule,
        name,
        use_standard_practice_rates: useStandard,
        items: Object.entries(rates)
          .filter(([, rate]) => rate.trim() !== "")
          .map(([service_code_id, custom_rate]) => ({
            service_code_id,
            custom_rate: custom_rate.trim(),
          })),
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save rate schedule");
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Rate Schedule</h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div className="md:col-span-2">
            <label htmlFor="schedule-name" className={labelClass}>
              Name *
            </label>
            <input
              id="schedule-name"
              required
              maxLength={150}
              className={inputClass}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <label className="flex items-center gap-3 text-sm md:col-span-2">
            <input
              type="checkbox"
              checked={useStandard}
              onChange={(e) => setUseStandard(e.target.checked)}
            />
            Use standard practice rates for every service
          </label>
        </div>
      </section>

      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Custom Rates</h2>
        <p className="mt-1 text-sm text-slate-500">
          Leave a rate blank to bill that service at its standard rate.
          {useStandard && " Custom rates are ignored while standard rates are in use."}
        </p>

        {rows.length === 0 ? (
          <p className="mt-4 text-sm text-slate-500">No active service codes.</p>
        ) : (
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-4 py-2">Code</th>
                  <th className="px-4 py-2">Description</th>
                  <th className="px-4 py-2">Standard Rate</th>
                  <th className="px-4 py-2">Custom Rate</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((code) => (
                  <tr key={code.id} className="border-b last:border-0">
                    <td className="px-4 py-2 font-medium">
                      {code.code}
                      {!code.is_active && (
                        <span className="ml-1 text-xs text-slate-500">(disabled)</span>
                      )}
                    </td>
                    <td className="px-4 py-2">{code.description}</td>
                    <td className="px-4 py-2">{money(code.standard_rate)}</td>
                    <td className="px-4 py-2">
                      <input
                        aria-label={`Custom rate for ${code.code}`}
                        type="number"
                        min="0"
                        step="0.01"
                        inputMode="decimal"
                        disabled={useStandard}
                        className={`${inputClass} max-w-36`}
                        value={rates[code.id ?? ""] ?? ""}
                        onChange={(e) =>
                          setRates((current) => ({
                            ...current,
                            [code.id ?? ""]: e.target.value,
                          }))
                        }
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <ErrorBox message={error} />

      <div className="flex justify-end">
        <button disabled={saving} className={primaryButtonClass}>
          {saving ? "Saving..." : submitLabel}
        </button>
      </div>
    </form>
  );
}
