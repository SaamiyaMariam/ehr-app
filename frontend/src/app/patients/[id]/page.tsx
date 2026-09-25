"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import PatientForm from "@/components/patient-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Patient } from "@/types/patient";

export default function PatientPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [patient, setPatient] = useState<Patient | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPatient() {
      try {
        const response = await apiFetch(
          `/api/patients/${params.id}`,
        );

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        const data = await response.json();

        if (!response.ok) {
          throw new Error(
            data.error ||
              `Unable to load patient (${response.status})`,
          );
        }

        setPatient(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load patient",
        );
      } finally {
        setLoading(false);
      }
    }

    if (params.id) {
      loadPatient();
    }
  }, [params.id, router]);

  async function updatePatient(updatedPatient: Patient) {
    setMessage("");
    setError("");

    const response = await apiFetch(
      `/api/patients/${params.id}`,
      {
        method: "PUT",
        body: JSON.stringify(updatedPatient),
      },
    );

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error ||
          `Unable to update patient (${response.status})`,
      );
    }

    setPatient(data);
    setMessage("Patient updated successfully.");
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href="/patients"
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Patients
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {loading && <p>Loading patient...</p>}

        {error && (
          <div className="rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {patient && (
          <>
            <div className="mb-6">
              <h1 className="text-2xl font-semibold text-slate-900">
                {patient.first_name} {patient.last_name}
              </h1>

              <p className="mt-1 text-sm text-slate-500">
                Patient record
              </p>
            </div>

            {message && (
              <div className="mb-5 rounded-lg bg-green-50 p-3 text-sm text-green-700">
                {message}
              </div>
            )}

            <PatientForm
              key={patient.id}
              initialPatient={patient}
              submitLabel="Save Changes"
              onSubmit={updatePatient}
            />
          </>
        )}
      </div>
    </main>
  );
}
