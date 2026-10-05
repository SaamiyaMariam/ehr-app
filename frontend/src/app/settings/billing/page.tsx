"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import {
  ErrorBox,
  SuccessBox,
  cardClass,
  inputClass,
  labelClass,
  primaryButtonClass,
} from "@/lib/ui";
import {
  PracticeBillingSettings,
  SubmissionMethod,
  submissionMethodOptions,
} from "@/types/rates";

export default function BillingSettingsPage() {
  const router = useRouter();

  const [settings, setSettings] = useState<PracticeBillingSettings | null>(null);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch("/api/billing/settings");

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load billing settings");
        return;
      }

      setSettings(data);
    }

    load();
  }, [router]);

  async function save(event: FormEvent) {
    event.preventDefault();

    if (!settings) {
      return;
    }

    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch("/api/billing/settings", {
        method: "PUT",
        body: JSON.stringify(settings),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to save billing settings");
      }

      setSettings(data);
      setMessage("Billing settings saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save billing settings");
    } finally {
      setSaving(false);
    }
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-4">
          <Link href="/dashboard" className="font-semibold">
            EHR
          </Link>
          <Link href="/settings/service-codes" className="text-sm font-medium underline">
            Service Codes
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Billing Settings</h1>

        <ErrorBox message={error} />
        <SuccessBox message={message} />

        {settings && (
          <form onSubmit={save} className={cardClass}>
            <h2 className="text-lg font-semibold text-slate-900">
              Default Insurance Billing Methods
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              Used when a payer has no billing method of its own. A billing method chosen on
              an individual service overrides both.
            </p>

            <div className="mt-5 grid gap-4 md:grid-cols-2">
              <div>
                <label htmlFor="in-network-method" className={labelClass}>
                  In-network payers
                </label>
                <select
                  id="in-network-method"
                  className={inputClass}
                  value={settings.default_in_network_billing_method}
                  onChange={(e) =>
                    setSettings({
                      ...settings,
                      default_in_network_billing_method: e.target.value as SubmissionMethod,
                    })
                  }
                >
                  {submissionMethodOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label htmlFor="out-network-method" className={labelClass}>
                  Out-of-network payers
                </label>
                <select
                  id="out-network-method"
                  className={inputClass}
                  value={settings.default_out_of_network_billing_method}
                  onChange={(e) =>
                    setSettings({
                      ...settings,
                      default_out_of_network_billing_method: e.target.value as SubmissionMethod,
                    })
                  }
                >
                  {submissionMethodOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="mt-5 flex justify-end">
              <button disabled={saving} className={primaryButtonClass}>
                {saving ? "Saving..." : "Save Billing Settings"}
              </button>
            </div>
          </form>
        )}
      </div>
    </main>
  );
}
