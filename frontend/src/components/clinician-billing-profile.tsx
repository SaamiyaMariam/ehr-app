"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { ErrorBox, SuccessBox, cardClass, inputClass, labelClass, primaryButtonClass } from "@/lib/ui";

type Profile = { npi: string; taxonomy_code: string; license_number: string };

// Rendering-provider identifiers used on claim lines (CMS-1500 item 24J).
export default function ClinicianBillingProfile({ userId }: { userId: string }) {
  const [profile, setProfile] = useState<Profile | null>(null);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/users/${userId}/billing-profile`);
      const data = await response.json();

      if (response.ok) {
        setProfile(data);
      } else {
        setError(data.error || "Unable to load billing profile");
      }
    }

    load();
  }, [userId]);

  async function save(event: FormEvent) {
    event.preventDefault();
    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch(`/api/users/${userId}/billing-profile`, {
        method: "PUT",
        body: JSON.stringify(profile),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to save billing profile");
      }

      setProfile(data);
      setMessage("Billing profile saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save billing profile");
    } finally {
      setSaving(false);
    }
  }

  if (!profile) {
    return <ErrorBox message={error} />;
  }

  return (
    <form onSubmit={save} className={`${cardClass} mt-8`}>
      <h2 className="text-lg font-semibold text-slate-900">Billing Profile</h2>
      <p className="mt-1 text-sm text-slate-500">Rendering-provider identifiers printed on claims.</p>

      <div className="mt-5 grid gap-4 md:grid-cols-3">
        <div>
          <label htmlFor="cbp-npi" className={labelClass}>Individual NPI</label>
          <input id="cbp-npi" maxLength={10} className={inputClass} value={profile.npi} onChange={(e) => setProfile({ ...profile, npi: e.target.value })} />
        </div>
        <div>
          <label htmlFor="cbp-taxonomy" className={labelClass}>Taxonomy code</label>
          <input id="cbp-taxonomy" maxLength={10} className={inputClass} value={profile.taxonomy_code} onChange={(e) => setProfile({ ...profile, taxonomy_code: e.target.value })} />
        </div>
        <div>
          <label htmlFor="cbp-license" className={labelClass}>License number</label>
          <input id="cbp-license" maxLength={50} className={inputClass} value={profile.license_number} onChange={(e) => setProfile({ ...profile, license_number: e.target.value })} />
        </div>
      </div>

      <div className="mt-4 space-y-3">
        <SuccessBox message={message} />
        <ErrorBox message={error} />
      </div>

      <div className="mt-5 flex justify-end">
        <button disabled={saving} className={primaryButtonClass}>
          {saving ? "Saving..." : "Save Billing Profile"}
        </button>
      </div>
    </form>
  );
}
