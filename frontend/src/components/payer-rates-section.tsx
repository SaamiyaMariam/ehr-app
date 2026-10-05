"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import {
  Badge,
  ErrorBox,
  SuccessBox,
  inputClass,
  linkButtonClass,
  primaryButtonClass,
} from "@/lib/ui";
import { ClinicianRateSchedule, RateSchedule } from "@/types/rates";

type Clinician = {
  id: string;
  first_name: string;
  last_name: string;
};

export default function PayerRatesSection({ payerId }: { payerId: string }) {
  const [schedules, setSchedules] = useState<RateSchedule[]>([]);
  const [clinicians, setClinicians] = useState<Clinician[]>([]);
  // clinician_id → rate_schedule_id ("" = none)
  const [assignments, setAssignments] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    async function load() {
      const [schedulesResponse, cliniciansResponse, assignmentsResponse] =
        await Promise.all([
          apiFetch(`/api/payers/${payerId}/rate-schedules`),
          apiFetch("/api/clinicians"),
          apiFetch(`/api/payers/${payerId}/clinician-rate-schedules`),
        ]);

      if (!schedulesResponse.ok || !cliniciansResponse.ok || !assignmentsResponse.ok) {
        setError("Unable to load rate schedules");
        return;
      }

      setSchedules(await schedulesResponse.json());
      setClinicians(await cliniciansResponse.json());

      const current: ClinicianRateSchedule[] = await assignmentsResponse.json();
      setAssignments(
        Object.fromEntries(current.map((a) => [a.clinician_id, a.rate_schedule_id])),
      );
    }

    load();
  }, [payerId]);

  async function toggleSchedule(schedule: RateSchedule) {
    setMessage("");
    setError("");

    const response = await apiFetch(`/api/rate-schedules/${schedule.id}/status`, {
      method: "PATCH",
      body: JSON.stringify({ is_active: !schedule.is_active }),
    });

    const data = await response.json();

    if (!response.ok) {
      setError(data.error || "Unable to update rate schedule");
      return;
    }

    setSchedules((current) =>
      current.map((s) => (s.id === schedule.id ? { ...s, is_active: data.is_active } : s)),
    );
    setMessage(data.is_active ? "Rate schedule enabled." : "Rate schedule disabled.");
  }

  async function saveAssignments() {
    setMessage("");
    setError("");
    setSaving(true);

    try {
      const response = await apiFetch(`/api/payers/${payerId}/clinician-rate-schedules`, {
        method: "PUT",
        body: JSON.stringify({
          assignments: Object.entries(assignments)
            .filter(([, scheduleId]) => scheduleId)
            .map(([clinician_id, rate_schedule_id]) => ({ clinician_id, rate_schedule_id })),
        }),
      });

      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to save clinician assignments");
      }

      setMessage("Clinician rate schedules saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to save clinician assignments");
    } finally {
      setSaving(false);
    }
  }

  const activeSchedules = schedules.filter((s) => s.is_active);

  return (
    <div className="mt-10 space-y-8">
      <SuccessBox message={message} />
      <ErrorBox message={error} />

      <section>
        <div className="flex items-center justify-between gap-4">
          <h2 className="text-lg font-semibold text-slate-900">Rate Schedules</h2>
          <Link href={`/payers/${payerId}/rate-schedules/new`} className={primaryButtonClass}>
            Add Rate Schedule
          </Link>
        </div>

        <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
          {schedules.length === 0 ? (
            <div className="p-6 text-center text-sm text-slate-500">
              No rate schedules. Services for this payer bill at standard rates.
            </div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-4 py-3">Name</th>
                  <th className="px-4 py-3">Rates</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3"></th>
                </tr>
              </thead>
              <tbody>
                {schedules.map((schedule) => (
                  <tr key={schedule.id} className="border-b last:border-0">
                    <td className="px-4 py-3 font-medium">{schedule.name}</td>
                    <td className="px-4 py-3">
                      {schedule.use_standard_practice_rates
                        ? "Standard practice rates"
                        : `${schedule.item_count ?? 0} custom rate(s)`}
                    </td>
                    <td className="px-4 py-3">
                      <Badge tone={schedule.is_active ? "green" : "slate"}>
                        {schedule.is_active ? "Active" : "Disabled"}
                      </Badge>
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      <Link
                        href={`/payers/${payerId}/rate-schedules/${schedule.id}`}
                        className={linkButtonClass}
                      >
                        View / Edit
                      </Link>
                      <button
                        type="button"
                        onClick={() => toggleSchedule(schedule)}
                        className={`ml-4 ${linkButtonClass}`}
                      >
                        {schedule.is_active ? "Disable" : "Enable"}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
        <p className="mt-2 text-xs text-slate-500">
          A clinician&apos;s assigned schedule is used first. Without an assignment, the
          payer&apos;s schedule is used only when exactly one is active; otherwise standard
          rates apply.
        </p>
      </section>

      <section>
        <h2 className="text-lg font-semibold text-slate-900">Clinician Assignments</h2>

        <div className="mt-4 overflow-x-auto rounded-xl border bg-white">
          {clinicians.length === 0 ? (
            <div className="p-6 text-center text-sm text-slate-500">No active clinicians.</div>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="border-b bg-slate-50">
                <tr>
                  <th className="px-4 py-3">Clinician</th>
                  <th className="px-4 py-3">Rate Schedule</th>
                </tr>
              </thead>
              <tbody>
                {clinicians.map((clinician) => {
                  const assigned = assignments[clinician.id] ?? "";
                  const assignedSchedule = schedules.find((s) => s.id === assigned);

                  return (
                    <tr key={clinician.id} className="border-b last:border-0">
                      <td className="px-4 py-3 font-medium">
                        {clinician.first_name} {clinician.last_name}
                      </td>
                      <td className="px-4 py-3">
                        <select
                          aria-label={`Rate schedule for ${clinician.first_name} ${clinician.last_name}`}
                          className={`${inputClass} max-w-sm`}
                          value={assigned}
                          onChange={(e) =>
                            setAssignments((current) => ({
                              ...current,
                              [clinician.id]: e.target.value,
                            }))
                          }
                        >
                          <option value="">No assignment</option>
                          {activeSchedules.map((s) => (
                            <option key={s.id} value={s.id}>
                              {s.name}
                            </option>
                          ))}
                          {assignedSchedule && !assignedSchedule.is_active && (
                            <option value={assignedSchedule.id}>
                              {assignedSchedule.name} (disabled)
                            </option>
                          )}
                        </select>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div className="mt-4 flex justify-end">
          <button
            type="button"
            onClick={saveAssignments}
            disabled={saving}
            className={primaryButtonClass}
          >
            {saving ? "Saving..." : "Save Clinician Assignments"}
          </button>
        </div>
      </section>
    </div>
  );
}
