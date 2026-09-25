"use client";

import { FormEvent, useEffect, useState } from "react";
import { Patient } from "@/types/patient";
import { apiFetch } from "@/lib/api";

type Props = {
  initialPatient: Patient;
  submitLabel: string;
  onSubmit: (patient: Patient) => Promise<void>;
};

const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500";

const labelClass =
  "mb-1 block text-sm font-medium text-slate-700";

export default function PatientForm({
  initialPatient,
  submitLabel,
  onSubmit,
}: Props) {
  const [patient, setPatient] = useState<Patient>(initialPatient);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const [clinicians, setClinicians] = useState<
    {
      id: string;
      first_name: string;
      last_name: string;
      preferred_name: string;
    }[]
  >([]);

  useEffect(() => {
    async function loadClinicians() {
      const response = await apiFetch("/api/clinicians");

      if (!response.ok) {
        return;
      }

      setClinicians(await response.json());
    }

    loadClinicians();
  }, []);

  function update<K extends keyof Patient>(
    field: K,
    value: Patient[K],
  ) {
    setPatient((current) => ({
      ...current,
      [field]: value,
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();

    setError("");
    setSaving(true);

    try {
      await onSubmit(patient);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Unable to save patient",
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-8">
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Patient Information
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <div>
            <label className={labelClass}>First name</label>
            <input
              className={inputClass}
              value={patient.first_name}
              onChange={(e) => update("first_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Middle name</label>
            <input
              className={inputClass}
              value={patient.middle_name}
              onChange={(e) => update("middle_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>
              Last name *
            </label>
            <input
              required
              className={inputClass}
              value={patient.last_name}
              onChange={(e) => update("last_name", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Suffix</label>
            <input
              className={inputClass}
              value={patient.suffix}
              onChange={(e) => update("suffix", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Preferred name</label>
            <input
              className={inputClass}
              value={patient.preferred_name}
              onChange={(e) =>
                update("preferred_name", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Pronouns</label>
            <input
              className={inputClass}
              value={patient.pronouns}
              onChange={(e) => update("pronouns", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Date of birth</label>
            <input
              type="date"
              className={inputClass}
              value={patient.date_of_birth}
              onChange={(e) =>
                update("date_of_birth", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Account number</label>
            <input
              className={inputClass}
              value={patient.account_number}
              onChange={(e) =>
                update("account_number", e.target.value)
              }
            />
          </div>
        </div>

        <div className="mt-4">
          <label className={labelClass}>Patient comments</label>
          <textarea
            rows={3}
            className={inputClass}
            value={patient.patient_comments}
            onChange={(e) =>
              update("patient_comments", e.target.value)
            }
          />
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Contact Information
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>Email</label>
            <input
              type="email"
              className={inputClass}
              value={patient.email}
              onChange={(e) => update("email", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Mobile phone</label>
            <input
              className={inputClass}
              value={patient.mobile_phone}
              onChange={(e) =>
                update("mobile_phone", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>
              Mobile message preference
            </label>
            <input
              className={inputClass}
              value={patient.mobile_message_preference}
              onChange={(e) =>
                update(
                  "mobile_message_preference",
                  e.target.value,
                )
              }
            />
          </div>

          <div>
            <label className={labelClass}>Home phone</label>
            <input
              className={inputClass}
              value={patient.home_phone}
              onChange={(e) =>
                update("home_phone", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>
              Home message preference
            </label>
            <input
              className={inputClass}
              value={patient.home_message_preference}
              onChange={(e) =>
                update(
                  "home_message_preference",
                  e.target.value,
                )
              }
            />
          </div>

          <div>
            <label className={labelClass}>Work phone</label>
            <input
              className={inputClass}
              value={patient.work_phone}
              onChange={(e) =>
                update("work_phone", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Other phone</label>
            <input
              className={inputClass}
              value={patient.other_phone}
              onChange={(e) =>
                update("other_phone", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>
              Appointment reminder setting
            </label>
            <input
              className={inputClass}
              value={patient.appointment_reminder_setting}
              onChange={(e) =>
                update(
                  "appointment_reminder_setting",
                  e.target.value,
                )
              }
            />
          </div>
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
              value={patient.address_1}
              onChange={(e) =>
                update("address_1", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>Address line 2</label>
            <input
              className={inputClass}
              value={patient.address_2}
              onChange={(e) =>
                update("address_2", e.target.value)
              }
            />
          </div>

          <div>
            <label className={labelClass}>City</label>
            <input
              className={inputClass}
              value={patient.city}
              onChange={(e) => update("city", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>State</label>
            <input
              className={inputClass}
              value={patient.state}
              onChange={(e) => update("state", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>ZIP</label>
            <input
              className={inputClass}
              value={patient.zip}
              onChange={(e) => update("zip", e.target.value)}
            />
          </div>

          <div>
            <label className={labelClass}>Time zone</label>
            <input
              className={inputClass}
              value={patient.time_zone}
              onChange={(e) =>
                update("time_zone", e.target.value)
              }
            />
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Demographics
        </h2>

        <div className="mt-5 grid gap-4 md:grid-cols-2">
          <div>
            <label className={labelClass}>
              Administrative sex
            </label>

            <select
              className={inputClass}
              value={patient.administrative_sex}
              onChange={(e) =>
                update("administrative_sex", e.target.value)
              }
            >
              <option value="">Select</option>
              <option value="Female">Female</option>
              <option value="Male">Male</option>
              <option value="Unknown">Unknown</option>
            </select>
          </div>

          <div>
            <label className={labelClass}>Gender identity</label>
            <input
              className={inputClass}
              value={patient.gender_identity}
              onChange={(e) =>
                update("gender_identity", e.target.value)
              }
            />
          </div>
        </div>
      </section>
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Assigned Clinician
        </h2>

        <div className="mt-5 max-w-md">
          <label className={labelClass}>
            Clinician
          </label>

          <select
            className={inputClass}
            value={patient.assigned_clinician_id}
            onChange={(e) =>
              update("assigned_clinician_id", e.target.value)
            }
          >
            <option value="">
              No clinician assigned
            </option>

            {clinicians.map((clinician) => (
              <option
                key={clinician.id}
                value={clinician.id}
              >
                {clinician.first_name} {clinician.last_name}
                {clinician.preferred_name
                  ? ` (${clinician.preferred_name})`
                  : ""}
              </option>
            ))}
          </select>
        </div>
      </section>
      <section className="rounded-xl border bg-white p-6">
        <h2 className="text-lg font-semibold text-slate-900">
          Documentation
        </h2>

        <div className="mt-5 space-y-4">
          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              checked={patient.hipaa_npp_on_file}
              onChange={(e) =>
                update("hipaa_npp_on_file", e.target.checked)
              }
            />
            HIPAA NPP on file
          </label>

          <div>
            <label className={labelClass}>PCP release</label>
            <input
              className={inputClass}
              value={patient.pcp_release}
              onChange={(e) =>
                update("pcp_release", e.target.value)
              }
            />
          </div>

          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              checked={patient.pad_acknowledged}
              onChange={(e) =>
                update("pad_acknowledged", e.target.checked)
              }
            />
            PAD acknowledged
          </label>

          {patient.pad_acknowledged && (
            <div className="max-w-sm">
              <label className={labelClass}>
                PAD acknowledged date
              </label>

              <input
                type="date"
                className={inputClass}
                value={patient.pad_acknowledged_date}
                onChange={(e) =>
                  update(
                    "pad_acknowledged_date",
                    e.target.value,
                  )
                }
              />
            </div>
          )}
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
