"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  InsurancePolicy,
  appointmentLimitOptions,
  policyHolderSexOptions,
  priorityOptions,
  relationshipOptions,
} from "@/types/billing";

type Props = {
  initialPolicy: InsurancePolicy;
  submitLabel: string;
  onSubmit: (policy: InsurancePolicy) => Promise<void>;
};

type PayerOption = {
  id: string;
  payer_name: string;
  payer_id: string;
  is_active: boolean;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

export default function InsurancePolicyForm({
  initialPolicy,
  submitLabel,
  onSubmit,
}: Props) {
  const [policy, setPolicy] = useState<InsurancePolicy>(initialPolicy);
  const [payers, setPayers] = useState<PayerOption[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPayers() {
      const response = await apiFetch("/api/payers");

      if (!response.ok) {
        setError("Unable to load payers");
        return;
      }

      setPayers(await response.json());
    }

    loadPayers();
  }, []);

  function update<K extends keyof InsurancePolicy>(
    field: K,
    value: InsurancePolicy[K],
  ) {
    setPolicy((current) => ({
      ...current,
      [field]: value,
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");
    setSaving(true);

    try {
      await onSubmit(policy);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Unable to save insurance policy",
      );
    } finally {
      setSaving(false);
    }
  }

  // Disabled payers can't be chosen for a policy, but a policy that already
  // uses one keeps it selectable so it still displays and saves correctly.
  const payerOptions = payers.filter(
    (payer) => payer.is_active || payer.id === initialPolicy.payer_id,
  );

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Insurance
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Payer *</label>
            <select
              required
              className={inputClass}
              value={policy.payer_id}
              onChange={(e) => update("payer_id", e.target.value)}
            >
              <option value="">Select payer</option>

              {payerOptions.map((payer) => (
                <option key={payer.id} value={payer.id}>
                  {payer.payer_name}
                  {payer.payer_id ? ` (${payer.payer_id})` : ""}
                  {!payer.is_active ? " — disabled" : ""}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className={labelClass}>Priority</label>
            <select
              className={inputClass}
              value={policy.priority}
              onChange={(e) =>
                update(
                  "priority",
                  e.target.value as InsurancePolicy["priority"],
                )
              }
            >
              {priorityOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className={labelClass}>Member ID</label>
            <input
              className={inputClass}
              value={policy.member_id}
              onChange={(e) => update("member_id", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Policy group</label>
            <input
              className={inputClass}
              value={policy.policy_group}
              onChange={(e) => update("policy_group", e.target.value)}
            />
          </div>

          <div className="md:col-span-2">
            <label className={labelClass}>Plan name</label>
            <input
              className={inputClass}
              value={policy.plan_name}
              onChange={(e) => update("plan_name", e.target.value)}
            />
          </div>

          <div className="md:col-span-2">
            <label className={labelClass}>Policy comments</label>
            <textarea
              rows={3}
              className={inputClass}
              value={policy.policy_comments}
              onChange={(e) =>
                update("policy_comments", e.target.value)
              }
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Coverage
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Coverage start</label>
            <input
              type="date"
              className={inputClass}
              value={policy.coverage_start}
              onChange={(e) => update("coverage_start", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Coverage end</label>
            <input
              type="date"
              className={inputClass}
              min={policy.coverage_start || undefined}
              value={policy.coverage_end}
              onChange={(e) => update("coverage_end", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Copay</label>
            <input
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              className={inputClass}
              value={policy.copay}
              onChange={(e) => update("copay", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Deductible</label>
            <input
              type="number"
              min="0"
              step="0.01"
              inputMode="decimal"
              className={inputClass}
              value={policy.deductible}
              onChange={(e) => update("deductible", e.target.value)}
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Appointment Limits
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Appointment limit type</label>
            <select
              className={inputClass}
              value={policy.appointment_limit_type}
              onChange={(e) =>
                update(
                  "appointment_limit_type",
                  e.target.value as InsurancePolicy["appointment_limit_type"],
                )
              }
            >
              {appointmentLimitOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          {policy.appointment_limit_type === "number" && (
            <>
              <div>
                <label className={labelClass}>Appointments allowed</label>
                <input
                  type="number"
                  min="0"
                  step="1"
                  className={inputClass}
                  value={policy.appointments_allowed ?? ""}
                  onChange={(e) =>
                    update(
                      "appointments_allowed",
                      e.target.value === ""
                        ? null
                        : Number(e.target.value),
                    )
                  }
                />
              </div>

              <div>
                <label className={labelClass}>
                  Appointments expiration
                </label>
                <input
                  type="date"
                  className={inputClass}
                  value={policy.appointments_expiration}
                  onChange={(e) =>
                    update("appointments_expiration", e.target.value)
                  }
                />
              </div>
            </>
          )}
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Policy Holder
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-3">
          <div className="md:col-span-3">
            <label className={labelClass}>
              Relationship to policy holder
            </label>
            <select
              className={inputClass}
              value={policy.relationship_to_policy_holder}
              onChange={(e) =>
                update("relationship_to_policy_holder", e.target.value)
              }
            >
              {relationshipOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className={labelClass}>First name</label>
            <input
              className={inputClass}
              value={policy.policy_holder_first_name}
              onChange={(e) =>
                update("policy_holder_first_name", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Middle name</label>
            <input
              className={inputClass}
              value={policy.policy_holder_middle_name}
              onChange={(e) =>
                update("policy_holder_middle_name", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Last name</label>
            <input
              className={inputClass}
              value={policy.policy_holder_last_name}
              onChange={(e) =>
                update("policy_holder_last_name", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Date of birth</label>
            <input
              type="date"
              className={inputClass}
              value={policy.policy_holder_date_of_birth}
              onChange={(e) =>
                update("policy_holder_date_of_birth", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Sex</label>
            <select
              className={inputClass}
              value={policy.policy_holder_sex}
              onChange={(e) =>
                update("policy_holder_sex", e.target.value)
              }
            >
              {policyHolderSexOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="mt-4 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Address line 1</label>
            <input
              className={inputClass}
              value={policy.policy_holder_address_1}
              onChange={(e) =>
                update("policy_holder_address_1", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Address line 2</label>
            <input
              className={inputClass}
              value={policy.policy_holder_address_2}
              onChange={(e) =>
                update("policy_holder_address_2", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>City</label>
            <input
              className={inputClass}
              value={policy.policy_holder_city}
              onChange={(e) =>
                update("policy_holder_city", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>State</label>
            <input
              className={inputClass}
              value={policy.policy_holder_state}
              onChange={(e) =>
                update("policy_holder_state", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>ZIP</label>
            <input
              className={inputClass}
              value={policy.policy_holder_zip}
              onChange={(e) =>
                update("policy_holder_zip", e.target.value)
              }
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Other
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <label className="flex items-center gap-3 text-sm md:col-span-2">
            <input
              type="checkbox"
              checked={policy.signature_on_file}
              onChange={(e) =>
                update("signature_on_file", e.target.checked)
              }
            />
            Signature on file
          </label>

          <div>
            <label className={labelClass}>MSP qualification</label>
            <input
              className={inputClass}
              value={policy.msp_qualification}
              onChange={(e) =>
                update("msp_qualification", e.target.value)
              }
            />
          </div>
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
