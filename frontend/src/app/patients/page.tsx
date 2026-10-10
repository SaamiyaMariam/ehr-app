"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import { apiFetch } from "@/lib/api";
import { clearToken, getToken } from "@/lib/auth";

type PatientListItem = {
  id: string;
  first_name: string;
  last_name: string;
  preferred_name: string;
  date_of_birth: string;
  email: string;
  mobile_phone: string;
};

export default function PatientsPage() {
  const router = useRouter();

  const [patients, setPatients] = useState<PatientListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPatients() {
      if (!getToken()) {
        router.replace("/login");
        return;
      }

      try {
        const response = await apiFetch("/api/patients");

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        if (!response.ok) {
          throw new Error("Unable to load patients");
        }

        setPatients(await response.json());
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load patients",
        );
      } finally {
        setLoading(false);
      }
    }

    loadPatients();
  }, [router]);

  return (
    <main className="flex-1 bg-slate-100">

      <div className="mx-auto max-w-7xl px-6 py-8">
        <div>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <h1 className="text-2xl font-semibold text-slate-900">Patients</h1>
            <Link
              href="/patients/new"
              className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
            >
              Add Patient
            </Link>
          </div>

          <p className="mt-1 text-sm text-slate-500">
            View and manage patient records.
          </p>
        </div>

        {loading && (
          <p className="mt-8 text-slate-500">
            Loading patients...
          </p>
        )}

        {error && (
          <div className="mt-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {!loading && !error && (
          <div className="mt-6 overflow-hidden rounded-xl border bg-white">
            {patients.length === 0 ? (
              <div className="p-8 text-center text-slate-500">
                No patients yet.
              </div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50 text-slate-600">
                  <tr>
                    <th className="px-5 py-3">Patient</th>
                    <th className="px-5 py-3">Date of birth</th>
                    <th className="px-5 py-3">Email</th>
                    <th className="px-5 py-3">Phone</th>
                    <th className="px-5 py-3"></th>
                  </tr>
                </thead>

                <tbody>
                  {patients.map((patient) => (
                    <tr
                      key={patient.id}
                      className="border-b last:border-0"
                    >
                      <td className="px-5 py-4 font-medium text-slate-900">
                        {patient.first_name} {patient.last_name}

                        {patient.preferred_name && (
                          <span className="ml-2 text-xs font-normal text-slate-500">
                            ({patient.preferred_name})
                          </span>
                        )}
                      </td>

                      <td className="px-5 py-4">
                        {patient.date_of_birth || "—"}
                      </td>

                      <td className="px-5 py-4">
                        {patient.email || "—"}
                      </td>

                      <td className="px-5 py-4">
                        {patient.mobile_phone || "—"}
                      </td>

                      <td className="px-5 py-4 text-right">
                        <Link
                          href={`/patients/${patient.id}`}
                          className="font-medium text-slate-900 underline"
                        >
                          View / Edit
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>
    </main>
  );
}
