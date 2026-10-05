"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";

import RateScheduleForm from "@/components/rate-schedule-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { RateSchedule, emptyRateSchedule } from "@/types/rates";

export default function NewRateSchedulePage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  async function createSchedule(schedule: RateSchedule) {
    const response = await apiFetch(`/api/payers/${params.id}/rate-schedules`, {
      method: "POST",
      body: JSON.stringify(schedule),
    });

    if (response.status === 401) {
      clearToken();
      router.replace("/login");
      return;
    }

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || `Unable to create rate schedule (${response.status})`);
    }

    router.push(`/payers/${params.id}`);
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/payers/${params.id}`} className="text-sm font-medium text-slate-600">
            ← Back to Payer
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-2xl font-semibold text-slate-900">Add Rate Schedule</h1>

        <div className="mt-6">
          <RateScheduleForm
            initialSchedule={emptyRateSchedule}
            submitLabel="Create Rate Schedule"
            onSubmit={createSchedule}
          />
        </div>
      </div>
    </main>
  );
}
