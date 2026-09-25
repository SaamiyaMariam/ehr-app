"use client";

import { FormEvent, useState } from "react";
import { Payer } from "@/types/payer";

type Props = {
  initialPayer: Payer;
  submitLabel: string;
  onSubmit: (payer: Payer) => Promise<void>;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

export default function PayerForm({
  initialPayer,
  submitLabel,
  onSubmit,
}: Props) {
  const [payer, setPayer] = useState<Payer>(initialPayer);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  function update<K extends keyof Payer>(
    field: K,
    value: Payer[K]
  ) {
    setPayer((current) => ({
      ...current,
      [field]: value,
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");
    setSaving(true);

    try {
      await onSubmit(payer);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Unable to save payer"
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Payer Information
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Payer name *</label>
            <input
              required
              className={inputClass}
              value={payer.payer_name}
              onChange={(e) => update("payer_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Payer ID</label>
            <input
              className={inputClass}
              value={payer.payer_id}
              onChange={(e) => update("payer_id", e.target.value)}
            />
          </div>

          <label className="flex items-center gap-3 text-sm md:col-span-2">
            <input
              type="checkbox"
              checked={payer.in_network}
              onChange={(e) =>
                update("in_network", e.target.checked)
              }
            />
            In network
          </label>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Address
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Address line 1</label>
            <input
              className={inputClass}
              value={payer.address_1}
              onChange={(e) => update("address_1", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Address line 2</label>
            <input
              className={inputClass}
              value={payer.address_2}
              onChange={(e) => update("address_2", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>City</label>
            <input
              className={inputClass}
              value={payer.city}
              onChange={(e) => update("city", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>State</label>
            <input
              className={inputClass}
              value={payer.state}
              onChange={(e) => update("state", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>ZIP</label>
            <input
              className={inputClass}
              value={payer.zip}
              onChange={(e) => update("zip", e.target.value)}
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Contact
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Phone</label>
            <input
              className={inputClass}
              value={payer.phone}
              onChange={(e) => update("phone", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Fax</label>
            <input
              className={inputClass}
              value={payer.fax}
              onChange={(e) => update("fax", e.target.value)}
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
