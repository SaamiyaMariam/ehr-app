"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  PriorAuthorization,
  usageSettingOptions,
} from "@/types/billing";
import { ServiceCode } from "@/types/service-code";

type Props = {
  initialAuthorization: PriorAuthorization;
  submitLabel: string;
  onSubmit: (authorization: PriorAuthorization) => Promise<void>;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

function toCount(value: string) {
  return value === "" ? null : Number(value);
}

export default function PriorAuthorizationForm({
  initialAuthorization,
  submitLabel,
  onSubmit,
}: Props) {
  const [authorization, setAuthorization] =
    useState<PriorAuthorization>(initialAuthorization);
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

  function update<K extends keyof PriorAuthorization>(
    field: K,
    value: PriorAuthorization[K],
  ) {
    setAuthorization((current) => ({
      ...current,
      [field]: value,
    }));
  }

  function toggleServiceCode(id: string, checked: boolean) {
    setAuthorization((current) => ({
      ...current,
      service_code_ids: checked
        ? [...current.service_code_ids, id]
        : current.service_code_ids.filter((item) => item !== id),
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");

    if (
      !authorization.applies_to_any_service_code &&
      authorization.service_code_ids.length === 0
    ) {
      setError("Select at least one service code, or choose Any Service Code.");
      return;
    }

    setSaving(true);

    try {
      await onSubmit(authorization);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Unable to save prior authorization",
      );
    } finally {
      setSaving(false);
    }
  }

  // Disabled codes can't be newly selected, but codes the authorization
  // already uses stay visible (and checked) so history isn't silently lost.
  const initialIds = new Set(initialAuthorization.service_code_ids);
  const selectableCodes = serviceCodes.filter(
    (code) => code.is_active || initialIds.has(code.id ?? ""),
  );

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Authorization
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div className="md:col-span-2">
            <label className={labelClass}>Authorization code *</label>
            <input
              required
              maxLength={100}
              className={inputClass}
              value={authorization.authorization_code}
              onChange={(e) =>
                update("authorization_code", e.target.value)
              }
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Service Codes
        </h2>

        <label className="mt-5 flex items-center gap-3 text-sm font-medium">
          <input
            type="checkbox"
            checked={authorization.applies_to_any_service_code}
            onChange={(e) => {
              const any = e.target.checked;

              setAuthorization((current) => ({
                ...current,
                applies_to_any_service_code: any,
                service_code_ids: any ? [] : current.service_code_ids,
              }));
            }}
          />
          Any Service Code
        </label>

        <fieldset
          disabled={authorization.applies_to_any_service_code}
          className="mt-4 rounded-lg border p-4 disabled:opacity-50"
        >
          <legend className="px-1 text-sm text-slate-500">
            Or select one or more service codes
          </legend>

          {selectableCodes.length === 0 ? (
            <p className="text-sm text-slate-500">
              No active service codes. Add them under Service Codes.
            </p>
          ) : (
            <div className="grid gap-2 md:grid-cols-2">
              {selectableCodes.map((code) => (
                <label
                  key={code.id}
                  className="flex items-start gap-3 text-sm"
                >
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={authorization.service_code_ids.includes(
                      code.id ?? "",
                    )}
                    onChange={(e) =>
                      toggleServiceCode(code.id ?? "", e.target.checked)
                    }
                  />
                  <span>
                    <span className="font-medium">{code.code}</span>{" "}
                    <span className="text-slate-600">
                      {code.description}
                    </span>
                    {!code.is_active && (
                      <span className="ml-1 text-xs text-slate-500">
                        (disabled)
                      </span>
                    )}
                  </span>
                </label>
              ))}
            </div>
          )}
        </fieldset>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Dates &amp; Usage
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Start date</label>
            <input
              type="date"
              className={inputClass}
              value={authorization.start_date}
              onChange={(e) => update("start_date", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Expiration date</label>
            <input
              type="date"
              min={authorization.start_date || undefined}
              className={inputClass}
              value={authorization.expiration_date}
              onChange={(e) => update("expiration_date", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Uses allowed</label>
            <input
              type="number"
              min="0"
              step="1"
              className={inputClass}
              value={authorization.uses_allowed ?? ""}
              onChange={(e) =>
                update("uses_allowed", toCount(e.target.value))
              }
            />
          </div>

          <div>
            <label className={labelClass}>Uses remaining</label>
            <input
              type="number"
              min="0"
              max={authorization.uses_allowed ?? undefined}
              step="1"
              className={inputClass}
              value={authorization.uses_remaining ?? ""}
              onChange={(e) =>
                update("uses_remaining", toCount(e.target.value))
              }
            />
          </div>

          <fieldset className="md:col-span-2">
            <legend className={labelClass}>Usage setting</legend>

            <div className="flex flex-wrap gap-6">
              {usageSettingOptions.map((option) => (
                <label
                  key={option.value}
                  className="flex items-center gap-2 text-sm"
                >
                  <input
                    type="radio"
                    name="usage_setting"
                    value={option.value}
                    checked={authorization.usage_setting === option.value}
                    onChange={() =>
                      update("usage_setting", option.value)
                    }
                  />
                  {option.label}
                </label>
              ))}
            </div>
          </fieldset>

          <div className="md:col-span-2">
            <label className={labelClass}>Comments</label>
            <textarea
              rows={3}
              className={inputClass}
              value={authorization.comments}
              onChange={(e) => update("comments", e.target.value)}
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
