"use client";

import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  ErrorBox,
  SuccessBox,
  cardClass,
  inputClass,
  money,
  primaryButtonClass,
} from "@/lib/ui";
import { PatientCashRate } from "@/types/rates";
import { ServiceCode } from "@/types/service-code";

export default function CashRatesSection({ patientId }: { patientId: string }) {
  const [serviceCodes, setServiceCodes] = useState<ServiceCode[]>([]);
  // service_code_id → custom cash rate ("" = use standard rate)
  const [rates, setRates] = useState<Record<string, string>>({});
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const [codesResponse, ratesResponse] = await Promise.all([
        apiFetch("/api/service-codes"),
        apiFetch(`/api/patients/${patientId}/cash-rates`),
      ]);

      if (!codesResponse.ok || !ratesResponse.ok) {
        setError("Unable to load cash rates");
        return;
      }

      const current: PatientCashRate[] = await ratesResponse.json();

      setServiceCodes(await codesResponse.json());
      setRates(Object.fromEntries(current.map((r) => [r.service_code_id, r.rate])));
      setLoaded(true);
    }

    load();
  }, [patientId]);

  async function save() {
    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch(`/api/patients/${patientId}/cash-rates`, {
        method: "PUT",
        body: JSON.stringify({
          rates: Object.entries(rates)
            .filter(([, rate]) => rate.trim() !== "")
            .map(([service_code_id, rate]) => ({ service_code_id, rate: rate.trim() })),
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to save cash rates");
      }

      setRates(
        Object.fromEntries(
          (data as PatientCashRate[]).map((r) => [r.service_code_id, r.rate]),
        ),
      );
      setMessage("Patient cash rates saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save cash rates");
    } finally {
      setSaving(false);
    }
  }

  const rows = serviceCodes.filter((code) => code.is_active || rates[code.id ?? ""]);

  return (
    <section className={`${cardClass} mt-8`}>
      <h2 className="text-lg font-semibold text-slate-900">Patient Cash Rates</h2>
      <p className="mt-1 text-sm text-slate-500">
        Custom rates for direct (self-pay) billing. Leave blank to use the standard rate.
        Insurance billing never uses these rates.
      </p>

      {!loaded ? (
        <p className="mt-4 text-sm text-slate-500">Loading cash rates...</p>
      ) : rows.length === 0 ? (
        <p className="mt-4 text-sm text-slate-500">No active service codes.</p>
      ) : (
        <div className="mt-4 overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-4 py-2">Code</th>
                <th className="px-4 py-2">Description</th>
                <th className="px-4 py-2">Standard Rate</th>
                <th className="px-4 py-2">Cash Rate</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((code) => (
                <tr key={code.id} className="border-b last:border-0">
                  <td className="px-4 py-2 font-medium">{code.code}</td>
                  <td className="px-4 py-2">{code.description}</td>
                  <td className="px-4 py-2">{money(code.standard_rate)}</td>
                  <td className="px-4 py-2">
                    <input
                      aria-label={`Cash rate for ${code.code}`}
                      type="number"
                      min="0"
                      step="0.01"
                      inputMode="decimal"
                      className={`${inputClass} max-w-36`}
                      value={rates[code.id ?? ""] ?? ""}
                      onChange={(e) =>
                        setRates((current) => ({ ...current, [code.id ?? ""]: e.target.value }))
                      }
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="mt-4 space-y-3">
        <SuccessBox message={message} />
        <ErrorBox message={error} />
      </div>

      <div className="mt-4 flex justify-end">
        <button
          type="button"
          onClick={save}
          disabled={saving || !loaded}
          className={primaryButtonClass}
        >
          {saving ? "Saving..." : "Save Cash Rates"}
        </button>
      </div>
    </section>
  );
}
