"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import RateScheduleForm from "@/components/rate-schedule-form";
import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { Badge, ErrorBox, SuccessBox, secondaryButtonClass } from "@/lib/ui";
import { RateSchedule } from "@/types/rates";

export default function RateSchedulePage() {
  const params = useParams<{ id: string; scheduleId: string }>();
  const router = useRouter();

  const [schedule, setSchedule] = useState<RateSchedule | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      try {
        const response = await apiFetch(`/api/rate-schedules/${params.scheduleId}`);

        if (response.status === 401) {
          clearToken();
          router.replace("/login");
          return;
        }

        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load rate schedule");
        }

        if (data.payer_id !== params.id) {
          throw new Error("Rate schedule not found for this payer");
        }

        setSchedule(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Unable to load rate schedule");
      } finally {
        setLoading(false);
      }
    }

    load();
  }, [params.id, params.scheduleId, router]);

  async function updateSchedule(updated: RateSchedule) {
    setMessage("");

    const response = await apiFetch(`/api/rate-schedules/${params.scheduleId}`, {
      method: "PUT",
      body: JSON.stringify(updated),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to update rate schedule");
    }

    setSchedule(data);
    router.push(`/payers/${params.id}`);
  }

  async function toggleStatus() {
    if (!schedule) {
      return;
    }

    setMessage("");
    setError("");

    const response = await apiFetch(`/api/rate-schedules/${params.scheduleId}/status`, {
      method: "PATCH",
      body: JSON.stringify({ is_active: !schedule.is_active }),
    });

    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update rate schedule status");
      return;
    }

    setSchedule({ ...schedule, is_active: data.is_active });
    setMessage(data.is_active ? "Rate schedule enabled." : "Rate schedule disabled.");
  }

  return (
    <main className="flex-1 bg-slate-100">
      <nav aria-label="Page navigation" className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/payers/${params.id}`} className="text-sm font-medium text-slate-600">
            ← Back to Payer
          </Link>
        </div>
      </nav>

      <div className="mx-auto max-w-5xl space-y-5 px-6 py-8">
        {loading && <p>Loading rate schedule...</p>}
        <ErrorBox message={error} />

        {schedule && (
          <>
            <div className="flex items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-semibold text-slate-900">{schedule.name}</h1>
                <p className="mt-1 flex items-center gap-2 text-sm text-slate-500">
                  {schedule.payer_name}
                  <Badge tone={schedule.is_active ? "green" : "slate"}>
                    {schedule.is_active ? "Active" : "Disabled"}
                  </Badge>
                </p>
              </div>

              <button type="button" onClick={toggleStatus} className={secondaryButtonClass}>
                {schedule.is_active ? "Disable Rate Schedule" : "Enable Rate Schedule"}
              </button>
            </div>

            <SuccessBox message={message} />

            <RateScheduleForm
              key={schedule.id}
              initialSchedule={schedule}
              submitLabel="Save Changes"
              onSubmit={updateSchedule}
            />
          </>
        )}
      </div>
    </main>
  );
}
