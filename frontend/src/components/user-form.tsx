"use client";

import { FormEvent, useState } from "react";
import { User } from "@/types/user";

type Props = {
  initialUser: User;
  submitLabel: string;
  includePassword?: boolean;
  onSubmit: (user: User) => Promise<void>;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

export default function UserForm({
  initialUser,
  submitLabel,
  includePassword = false,
  onSubmit,
}: Props) {
  const [user, setUser] = useState<User>(initialUser);
  const [languagesText, setLanguagesText] = useState(
    initialUser.languages.join(", ")
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  function update<K extends keyof User>(
    field: K,
    value: User[K]
  ) {
    setUser((current) => ({
      ...current,
      [field]: value,
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");
    setSaving(true);

    try {
      const payload = {
        ...user,
        languages: languagesText
          .split(",")
          .map((item) => item.trim())
          .filter(Boolean),
      };

      await onSubmit(payload);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Unable to save user"
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          User Information
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <div>
            <label className={labelClass}>First name *</label>
            <input
              required
              className={inputClass}
              value={user.first_name}
              onChange={(e) => update("first_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Middle name</label>
            <input
              className={inputClass}
              value={user.middle_name}
              onChange={(e) => update("middle_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Last name *</label>
            <input
              required
              className={inputClass}
              value={user.last_name}
              onChange={(e) => update("last_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Suffix</label>
            <input
              className={inputClass}
              value={user.suffix}
              onChange={(e) => update("suffix", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Preferred name</label>
            <input
              className={inputClass}
              value={user.preferred_name}
              onChange={(e) => update("preferred_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Pronouns</label>
            <input
              className={inputClass}
              value={user.pronouns}
              onChange={(e) => update("pronouns", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Username *</label>
            <input
              required
              className={inputClass}
              value={user.username}
              onChange={(e) => update("username", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Date of birth</label>
            <input
              type="date"
              className={inputClass}
              value={user.date_of_birth}
              onChange={(e) => update("date_of_birth", e.target.value)}
            />
          </div>

          <div className="md:col-span-2">
            <label className={labelClass}>
              Languages
            </label>
            <input
              className={inputClass}
              placeholder="English, Urdu"
              value={languagesText}
              onChange={(e) => setLanguagesText(e.target.value)}
            />
          </div>
        </div>

        <div className="mt-4">
          <label className={labelClass}>User comments</label>
          <textarea
            rows={3}
            className={inputClass}
            value={user.user_comments}
            onChange={(e) => update("user_comments", e.target.value)}
          />
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Contact Information
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Email *</label>
            <input
              required
              type="email"
              className={inputClass}
              value={user.email}
              onChange={(e) => update("email", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Mobile phone</label>
            <input
              className={inputClass}
              value={user.mobile_phone}
              onChange={(e) => update("mobile_phone", e.target.value)}
            />
          </div>

          <label className="flex items-center gap-3 text-sm md:col-span-2">
            <input
              type="checkbox"
              checked={user.can_receive_text_messages}
              onChange={(e) =>
                update("can_receive_text_messages", e.target.checked)
              }
            />
            Can receive text messages
          </label>

          <div>
            <label className={labelClass}>Work phone</label>
            <input
              className={inputClass}
              value={user.work_phone}
              onChange={(e) => update("work_phone", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Home phone</label>
            <input
              className={inputClass}
              value={user.home_phone}
              onChange={(e) => update("home_phone", e.target.value)}
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Address
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <input
            placeholder="Address line 1"
            className={inputClass}
            value={user.address_1}
            onChange={(e) => update("address_1", e.target.value)}
          />

          <input
            placeholder="Address line 2"
            className={inputClass}
            value={user.address_2}
            onChange={(e) => update("address_2", e.target.value)}
          />

          <input
            placeholder="City"
            className={inputClass}
            value={user.city}
            onChange={(e) => update("city", e.target.value)}
          />

          <input
            placeholder="State"
            className={inputClass}
            value={user.state}
            onChange={(e) => update("state", e.target.value)}
          />

          <input
            placeholder="ZIP"
            className={inputClass}
            value={user.zip}
            onChange={(e) => update("zip", e.target.value)}
          />
        </div>
      </section>

      {includePassword && (
        <section className="rounded-xl border bg-white p-6">
          <h2 className="text-lg font-semibold text-slate-900">
            Login
          </h2>

          <div className="mt-5 max-w-md">
            <label className={labelClass}>Password *</label>
            <input
              required
              minLength={8}
              type="password"
              className={inputClass}
              value={user.password || ""}
              onChange={(e) => update("password", e.target.value)}
            />
          </div>
        </section>
      )}

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
