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
import { InsurancePolicyListItem, PriorAuthorization, priorityLabel } from "@/types/billing";
import { ChargeInput, Diagnosis, placeOfServiceOptions } from "@/types/charges";
import { RatePreview, billingMethodLabels, rateSourceLabels } from "@/types/rates";
import { ServiceCode } from "@/types/service-code";

type Props = {
  patientId: string;
  initialCharge: ChargeInput;
  submitLabel: string;
  // Non-empty when billing fields are locked (only notes can change).
  lockReason?: string;
  onSubmit: (charge: ChargeInput) => Promise<void>;
};

type Clinician = { id: string; first_name: string; last_name: string };

const insuranceMethods = Object.entries(billingMethodLabels).filter(([value]) => value !== "direct");

export default function ChargeForm({ patientId, initialCharge, submitLabel, lockReason = "", onSubmit }: Props) {
  const [charge, setCharge] = useState<ChargeInput>(initialCharge);
  const [clinicians, setClinicians] = useState<Clinician[]>([]);
  const [serviceCodes, setServiceCodes] = useState<ServiceCode[]>([]);
  const [policies, setPolicies] = useState<InsurancePolicyListItem[]>([]);
  const [diagnoses, setDiagnoses] = useState<Diagnosis[]>([]);
  const [authorizations, setAuthorizations] = useState<PriorAuthorization[]>([]);
  const [preview, setPreview] = useState<RatePreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const locked = lockReason !== "";

  useEffect(() => {
    async function load() {
      const [c, s, p, d] = await Promise.all([
        apiFetch("/api/clinicians"),
        apiFetch("/api/service-codes"),
        apiFetch(`/api/patients/${patientId}/insurance-policies`),
        apiFetch(`/api/patients/${patientId}/diagnoses`),
      ]);

      if (!c.ok || !s.ok || !p.ok || !d.ok) {
        setError("Unable to load form options");
        return;
      }

      setClinicians(await c.json());
      setServiceCodes(await s.json());
      setPolicies(await p.json());
      setDiagnoses(await d.json());
    }

    load();
  }, [patientId]);

  useEffect(() => {
    async function loadAuthorizations() {
      if (!charge.insurance_policy_id) {
        setAuthorizations([]);
        return;
      }

      const response = await apiFetch(`/api/insurance-policies/${charge.insurance_policy_id}/prior-authorizations`);
      setAuthorizations(response.ok ? await response.json() : []);
    }

    loadAuthorizations();
  }, [charge.insurance_policy_id]);

  // Live rate preview: the server is the only source of rates and totals.
  useEffect(() => {
    if (locked) return;

    const timer = setTimeout(async () => {
      if (!charge.clinician_id || !charge.service_code_id) {
        setPreview(null);
        setPreviewError("");
        return;
      }

      const response = await apiFetch("/api/billing/rate-preview", {
        method: "POST",
        body: JSON.stringify({
          patient_id: patientId,
          clinician_id: charge.clinician_id,
          service_code_id: charge.service_code_id,
          insurance_policy_id: charge.insurance_policy_id,
          billing_method: charge.billing_method,
          units: charge.units || 1,
        }),
      });
      const data = await response.json();

      if (!response.ok) {
        setPreview(null);
        setPreviewError(data.error || "Unable to price this service");
      } else {
        setPreview(data);
        setPreviewError("");
      }
    }, 250);

    return () => clearTimeout(timer);
  }, [locked, patientId, charge.clinician_id, charge.service_code_id, charge.insurance_policy_id, charge.billing_method, charge.units]);

  function update<K extends keyof ChargeInput>(field: K, value: ChargeInput[K]) {
    setCharge((current) => ({ ...current, [field]: value }));
  }

  function toggleDiagnosis(id: string, checked: boolean) {
    setCharge((current) => ({
      ...current,
      diagnosis_ids: checked
        ? [...current.diagnosis_ids, id].slice(0, 4)
        : current.diagnosis_ids.filter((d) => d !== id),
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    setSaving(true);

    try {
      await onSubmit({
        ...charge,
        modifiers: charge.modifiers.filter((m) => m.trim() !== ""),
        patient_responsibility: charge.insurance_policy_id ? charge.patient_responsibility : "",
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save billable service");
    } finally {
      setSaving(false);
    }
  }

  const activeCodes = serviceCodes.filter((s) => s.is_active || s.id === initialCharge.service_code_id);
  const billablePolicies = policies.filter((p) => p.is_active || p.id === initialCharge.insurance_policy_id);
  const availableDiagnoses = diagnoses.filter((d) => d.is_active || initialCharge.diagnosis_ids.includes(d.id));
  const usableAuthorizations = authorizations.filter(
    (a) => a.is_active || a.id === initialCharge.prior_authorization_id,
  );
  const isInsurance = charge.insurance_policy_id !== "";

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      {locked && (
        <div role="status" className="rounded-lg bg-amber-50 p-4 text-sm text-amber-900">
          {lockReason} Only notes can be changed.
        </div>
      )}

      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Service</h2>

        <fieldset disabled={locked} className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label htmlFor="charge-dos" className={labelClass}>Date of service *</label>
            <input
              id="charge-dos"
              type="date"
              required
              className={inputClass}
              value={charge.date_of_service}
              onChange={(e) => update("date_of_service", e.target.value)}
            />
          </div>

          <div>
            <label htmlFor="charge-clinician" className={labelClass}>Clinician *</label>
            <select
              id="charge-clinician"
              required
              className={inputClass}
              value={charge.clinician_id}
              onChange={(e) => update("clinician_id", e.target.value)}
            >
              <option value="">Select clinician</option>
              {clinicians.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.first_name} {c.last_name}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor="charge-service" className={labelClass}>Service code *</label>
            <select
              id="charge-service"
              required
              className={inputClass}
              value={charge.service_code_id}
              onChange={(e) => update("service_code_id", e.target.value)}
            >
              <option value="">Select service</option>
              {activeCodes.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.code} – {s.description}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label htmlFor="charge-units" className={labelClass}>Units *</label>
            <input
              id="charge-units"
              type="number"
              min={1}
              max={999}
              step={1}
              required
              className={inputClass}
              value={charge.units}
              onChange={(e) => update("units", Number(e.target.value))}
            />
          </div>

          <div>
            <label htmlFor="charge-pos" className={labelClass}>Place of service</label>
            <select
              id="charge-pos"
              className={inputClass}
              value={charge.place_of_service}
              onChange={(e) => update("place_of_service", e.target.value)}
            >
              {placeOfServiceOptions.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
              {!placeOfServiceOptions.some((o) => o.value === charge.place_of_service) && (
                <option value={charge.place_of_service}>{charge.place_of_service}</option>
              )}
            </select>
          </div>

          <div>
            <span className={labelClass}>Modifiers</span>
            <div className="grid grid-cols-4 gap-2">
              {charge.modifiers.map((m, i) => (
                <input
                  key={i}
                  aria-label={`Modifier ${i + 1}`}
                  maxLength={2}
                  className={`${inputClass} uppercase`}
                  value={m}
                  onChange={(e) =>
                    update(
                      "modifiers",
                      charge.modifiers.map((x, j) => (j === i ? e.target.value.toUpperCase() : x)),
                    )
                  }
                />
              ))}
            </div>
          </div>
        </fieldset>
      </section>

      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Billing</h2>

        <fieldset disabled={locked} className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label htmlFor="charge-bill-to" className={labelClass}>Bill to</label>
            <select
              id="charge-bill-to"
              className={inputClass}
              value={charge.insurance_policy_id}
              onChange={(e) =>
                setCharge((current) => ({
                  ...current,
                  insurance_policy_id: e.target.value,
                  billing_method: "",
                  prior_authorization_id: "",
                  patient_responsibility: "",
                }))
              }
            >
              <option value="">Direct (patient pays)</option>
              {billablePolicies.map((p) => (
                <option key={p.id} value={p.id}>
                  {priorityLabel(p.priority)} – {p.payer_name}
                  {p.member_id ? ` (${p.member_id})` : ""}
                </option>
              ))}
            </select>
          </div>

          {isInsurance && (
            <div>
              <label htmlFor="charge-method" className={labelClass}>Billing method</label>
              <select
                id="charge-method"
                className={inputClass}
                value={charge.billing_method}
                onChange={(e) => update("billing_method", e.target.value)}
              >
                <option value="">Default for payer</option>
                {insuranceMethods.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </div>
          )}

          {isInsurance && (
            <div className="md:col-span-2">
              <label htmlFor="charge-pa" className={labelClass}>Prior authorization</label>
              <select
                id="charge-pa"
                className={inputClass}
                value={charge.prior_authorization_id}
                onChange={(e) => update("prior_authorization_id", e.target.value)}
              >
                <option value="">None</option>
                {usableAuthorizations.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.authorization_code}
                    {a.applies_to_any_service_code ? " (any service)" : ` (${a.service_codes?.map((s) => s.code).join(", ")})`}
                    {a.uses_remaining !== null ? ` – ${a.uses_remaining} use(s) left` : ""}
                    {!a.is_active ? " – disabled" : ""}
                  </option>
                ))}
              </select>
            </div>
          )}

          <div>
            <label htmlFor="charge-patient-amount" className={labelClass}>Patient responsibility</label>
            <input
              id="charge-patient-amount"
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              disabled={!isInsurance}
              placeholder={preview ? `Default ${money(preview.suggested_patient_responsibility)}` : "Default"}
              className={inputClass}
              value={isInsurance ? charge.patient_responsibility : ""}
              onChange={(e) => update("patient_responsibility", e.target.value)}
            />
            <p className="mt-1 text-xs text-slate-500">
              {isInsurance
                ? "Leave blank to use the policy copay. Insurance is responsible for the rest."
                : "Direct billing: the patient is responsible for the full amount."}
            </p>
          </div>
        </fieldset>

        {!locked && (
          <div className="mt-5 rounded-lg bg-slate-50 p-4 text-sm" aria-live="polite">
            {previewError ? (
              <span className="text-red-700">{previewError}</span>
            ) : preview ? (
              <dl className="grid gap-2 sm:grid-cols-3">
                <div>
                  <dt className="text-slate-500">Rate</dt>
                  <dd className="font-medium">
                    {money(preview.rate_per_unit)} / unit
                    <span className="block text-xs font-normal text-slate-500">
                      {rateSourceLabels[preview.source] ?? preview.source}
                    </span>
                  </dd>
                </div>
                <div>
                  <dt className="text-slate-500">Total charge</dt>
                  <dd className="font-medium">{money(preview.total_charge)}</dd>
                </div>
                <div>
                  <dt className="text-slate-500">Billing method</dt>
                  <dd className="font-medium">{billingMethodLabels[preview.billing_method] ?? preview.billing_method}</dd>
                </div>
              </dl>
            ) : (
              <span className="text-slate-500">Choose a clinician and service to see the rate.</span>
            )}
          </div>
        )}
      </section>

      <section className={cardClass}>
        <h2 className="text-lg font-semibold text-slate-900">Diagnoses</h2>
        <p className="mt-1 text-sm text-slate-500">Select up to 4, in pointer order.</p>

        <fieldset disabled={locked} className="mt-4 space-y-2">
          <legend className="sr-only">Diagnoses</legend>
          {availableDiagnoses.length === 0 ? (
            <p className="text-sm text-slate-500">No active diagnoses. Add them on the patient billing page.</p>
          ) : (
            availableDiagnoses.map((d) => {
              const pointer = charge.diagnosis_ids.indexOf(d.id);

              return (
                <label key={d.id} className="flex items-center gap-3 text-sm">
                  <input
                    type="checkbox"
                    checked={pointer >= 0}
                    disabled={pointer < 0 && charge.diagnosis_ids.length >= 4}
                    onChange={(e) => toggleDiagnosis(d.id, e.target.checked)}
                  />
                  <span className="w-6 text-xs text-slate-500">{pointer >= 0 ? `#${pointer + 1}` : ""}</span>
                  <span className="font-medium">{d.icd10_code}</span>
                  <span className="text-slate-600">{d.description}</span>
                </label>
              );
            })
          )}
        </fieldset>
      </section>

      <section className={cardClass}>
        <label htmlFor="charge-notes" className="text-lg font-semibold text-slate-900">
          Notes
        </label>
        <textarea
          id="charge-notes"
          rows={3}
          maxLength={2000}
          className={`${inputClass} mt-3`}
          value={charge.notes}
          onChange={(e) => update("notes", e.target.value)}
        />
      </section>

      <ErrorBox message={error} />

      <div className="flex justify-end">
        <button disabled={saving || (!locked && previewError !== "")} className={primaryButtonClass}>
          {saving ? "Saving..." : submitLabel}
        </button>
      </div>
    </form>
  );
}
