"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  Badge,
  ErrorBox,
  SuccessBox,
  cardClass,
  inputClass,
  labelClass,
  linkButtonClass,
  primaryButtonClass,
} from "@/lib/ui";
import { Diagnosis } from "@/types/charges";

export default function DiagnosesSection({ patientId }: { patientId: string }) {
  const [diagnoses, setDiagnoses] = useState<Diagnosis[]>([]);
  const [code, setCode] = useState("");
  const [description, setDescription] = useState("");
  const [primary, setPrimary] = useState(false);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patients/${patientId}/diagnoses`);
      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load diagnoses");
        return;
      }

      setDiagnoses(data);
    }

    load();
  }, [patientId, reloadKey]);

  async function addDiagnosis(event: FormEvent) {
    event.preventDefault();
    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch(`/api/patients/${patientId}/diagnoses`, {
        method: "POST",
        body: JSON.stringify({ icd10_code: code, description, is_primary: primary }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to add diagnosis");
      }

      setCode("");
      setDescription("");
      setPrimary(false);
      setMessage(`Diagnosis ${data.icd10_code} added.`);
      setReloadKey((k) => k + 1);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to add diagnosis");
    } finally {
      setSaving(false);
    }
  }

  async function makePrimary(d: Diagnosis) {
    setMessage("");
    setError("");

    const response = await apiFetch(`/api/diagnoses/${d.id}`, {
      method: "PUT",
      body: JSON.stringify({ icd10_code: d.icd10_code, description: d.description, is_primary: true }),
    });
    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update diagnosis");
      return;
    }

    setMessage(`${d.icd10_code} is now the primary diagnosis.`);
    setReloadKey((k) => k + 1);
  }

  async function toggleActive(d: Diagnosis) {
    setMessage("");
    setError("");

    const response = await apiFetch(`/api/diagnoses/${d.id}/status`, {
      method: "PATCH",
      body: JSON.stringify({ is_active: !d.is_active }),
    });
    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update diagnosis");
      return;
    }

    setMessage(data.is_active ? `${d.icd10_code} enabled.` : `${d.icd10_code} disabled.`);
    setReloadKey((k) => k + 1);
  }

  return (
    <section className={`${cardClass} mt-8`}>
      <h2 className="text-lg font-semibold text-slate-900">Diagnoses</h2>
      <p className="mt-1 text-sm text-slate-500">
        ICD-10 codes available to billable services and claims.
      </p>

      <div className="mt-4 space-y-3">
        <SuccessBox message={message} />
        <ErrorBox message={error} />
      </div>

      {diagnoses.length === 0 ? (
        <p className="mt-4 text-sm text-slate-500">No diagnoses yet.</p>
      ) : (
        <div className="mt-4 overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-4 py-2">Code</th>
                <th className="px-4 py-2">Description</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {diagnoses.map((d) => (
                <tr key={d.id} className={`border-b last:border-0 ${d.is_active ? "" : "text-slate-500"}`}>
                  <td className="px-4 py-2 font-medium">{d.icd10_code}</td>
                  <td className="px-4 py-2">{d.description}</td>
                  <td className="space-x-1 px-4 py-2">
                    <Badge tone={d.is_active ? "green" : "slate"}>{d.is_active ? "Active" : "Disabled"}</Badge>
                    {d.is_primary && <Badge tone="blue">Primary</Badge>}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right">
                    {d.is_active && !d.is_primary && (
                      <button type="button" onClick={() => makePrimary(d)} className={linkButtonClass}>
                        Make primary
                      </button>
                    )}
                    <button type="button" onClick={() => toggleActive(d)} className={`ml-4 ${linkButtonClass}`}>
                      {d.is_active ? "Disable" : "Enable"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <form onSubmit={addDiagnosis} className="mt-6 grid gap-4 md:grid-cols-[10rem_1fr_auto] md:items-end">
        <div>
          <label htmlFor="dx-code" className={labelClass}>
            ICD-10 code
          </label>
          <input
            id="dx-code"
            required
            maxLength={8}
            placeholder="F41.1"
            className={inputClass}
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        </div>
        <div>
          <label htmlFor="dx-description" className={labelClass}>
            Description
          </label>
          <input
            id="dx-description"
            required
            maxLength={255}
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <button disabled={saving} className={primaryButtonClass}>
          {saving ? "Adding..." : "Add Diagnosis"}
        </button>
        <label className="flex items-center gap-2 text-sm md:col-span-3">
          <input type="checkbox" checked={primary} onChange={(e) => setPrimary(e.target.checked)} />
          Primary diagnosis
        </label>
      </form>
    </section>
  );
}
