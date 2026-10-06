"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { ErrorBox, SuccessBox, cardClass, inputClass, labelClass, primaryButtonClass } from "@/lib/ui";

type Profile = Record<string, string>;

const fields: { key: string; label: string; span?: boolean; max: number }[] = [
  { key: "practice_name", label: "Billing provider name", span: true, max: 150 },
  { key: "npi", label: "Group NPI", max: 10 },
  { key: "taxonomy_code", label: "Taxonomy code", max: 10 },
  { key: "tax_id", label: "Tax ID (EIN/SSN, 9 digits)", max: 11 },
  { key: "address_1", label: "Address line 1", max: 255 },
  { key: "address_2", label: "Address line 2", max: 255 },
  { key: "city", label: "City", max: 100 },
  { key: "state", label: "State (2 letters)", max: 2 },
  { key: "zip", label: "ZIP", max: 10 },
  { key: "phone", label: "Phone", max: 30 },
  { key: "billing_contact_name", label: "Billing contact name", max: 150 },
  { key: "billing_contact_email", label: "Billing contact email", max: 255 },
];

// Practice data printed in the billing-provider fields of every claim.
export default function PracticeProfileForm() {
  const [profile, setProfile] = useState<Profile | null>(null);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch("/api/billing/practice-profile");
      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load practice billing profile");
        return;
      }

      setProfile(data);
    }

    load();
  }, []);

  async function save(event: FormEvent) {
    event.preventDefault();
    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch("/api/billing/practice-profile", {
        method: "PUT",
        body: JSON.stringify(profile),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to save practice billing profile");
      }

      setProfile(data);
      setMessage("Practice billing profile saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save practice billing profile");
    } finally {
      setSaving(false);
    }
  }

  if (!profile) {
    return <ErrorBox message={error} />;
  }

  return (
    <form onSubmit={save} className={cardClass}>
      <h2 className="text-lg font-semibold text-slate-900">Practice Billing Profile</h2>
      <p className="mt-1 text-sm text-slate-500">
        Billing provider details used on claims (CMS-1500 items 25 and 33). No banking or
        payment credentials are stored.
      </p>

      <div className="mt-5 grid gap-4 md:grid-cols-2">
        {fields.map((f) => (
          <div key={f.key} className={f.span ? "md:col-span-2" : ""}>
            <label htmlFor={`pp-${f.key}`} className={labelClass}>{f.label}</label>
            <input
              id={`pp-${f.key}`}
              maxLength={f.max}
              className={inputClass}
              value={profile[f.key] ?? ""}
              onChange={(e) => setProfile({ ...profile, [f.key]: e.target.value })}
            />
          </div>
        ))}

        <div>
          <label htmlFor="pp-tax-type" className={labelClass}>Tax ID type</label>
          <select
            id="pp-tax-type"
            className={inputClass}
            value={profile.tax_id_type}
            onChange={(e) => setProfile({ ...profile, tax_id_type: e.target.value })}
          >
            <option value="ein">EIN</option>
            <option value="ssn">SSN</option>
          </select>
        </div>
      </div>

      <div className="mt-4 space-y-3">
        <SuccessBox message={message} />
        <ErrorBox message={error} />
      </div>

      <div className="mt-5 flex justify-end">
        <button disabled={saving} className={primaryButtonClass}>
          {saving ? "Saving..." : "Save Practice Profile"}
        </button>
      </div>
    </form>
  );
}
