"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";

import PatientForm from "@/components/patient-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Patient, emptyPatient } from "@/types/patient";

export default function NewPatientPage() {
  const router = useRouter();

  async function createPatient(patient: Patient) {
    const response = await apiFetch("/api/patients", {
      method: "POST",
      body: JSON.stringify(patient),
    });

    if (response.status === 401) {
      clearToken();
      router.replace("/login");
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(
        data.error || `Unable to create patient (${response.status})`
      );
    }

    if (!data.id) {
      throw new Error("Patient created but server did not return an ID");
    }

    router.push(`/patients/${data.id}`);
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
        <h1 className="text-2xl font-semibold text-slate-900">
          Add New Patient
        </h1>

        <p className="mt-1 text-sm text-slate-500">
          Enter the patient information below.
        </p>

        <div className="mt-6">
          <PatientForm
            initialPatient={emptyPatient}
            submitLabel="Create Patient"
            onSubmit={createPatient}
          />
        </div>
      </div>
    </main>
  );
}
