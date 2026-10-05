"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { clearToken } from "@/lib/auth";
import { ErrorBox, money, primaryButtonClass } from "@/lib/ui";
import { Charge } from "@/types/charges";
import { billingMethodLabel } from "@/types/rates";

// Groups claimable services by the policy + submission method a single
// claim can carry.
function groupKey(c: Charge) {
  return `${c.insurance_policy_id}|${c.billing_method}`;
}

export default function NewClaimPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();

  const [charges, setCharges] = useState<Charge[] | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patients/${params.id}/claimable-charges`);

      if (response.status === 401) {
        clearToken();
        router.replace("/login");
        return;
      }

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load billable services");
        return;
      }

      setCharges(data);
    }

    load();
  }, [params.id, router]);

  const groups = new Map<string, Charge[]>();
  (charges ?? []).forEach((c) => groups.set(groupKey(c), [...(groups.get(groupKey(c)) ?? []), c]));

  const selectedGroup = selected.length
    ? groupKey((charges ?? []).find((c) => c.id === selected[0])!)
    : "";

  function toggle(charge: Charge, checked: boolean) {
    setSelected((current) => (checked ? [...current, charge.id] : current.filter((id) => id !== charge.id)));
  }

  async function createClaim() {
    setError("");
    setCreating(true);

    try {
      const response = await apiFetch("/api/claims", {
        method: "POST",
        body: JSON.stringify({ charge_ids: selected }),
      });
      const data = await response.json();

      if (!response.ok) {
        throw new Error(data.error || "Unable to create claim");
      }

      router.push(`/billing/claims/${data.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to create claim");
      setCreating(false);
    }
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link href={`/patients/${params.id}/billing`} className="text-sm font-medium text-slate-600">
            ← Back to Billing
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl space-y-6 px-6 py-8">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900">Create Claim</h1>
          <p className="mt-1 text-sm text-slate-500">
            Select insurance-billed services not yet on a primary claim. One claim covers one
            policy and one submission method.
          </p>
        </div>

        <ErrorBox message={error} />

        {charges === null ? (
          <p className="text-slate-500">Loading billable services...</p>
        ) : charges.length === 0 ? (
          <div className="rounded-xl border bg-white p-8 text-center text-slate-500">
            No insurance-billed services are waiting for a claim.
          </div>
        ) : (
          [...groups.entries()].map(([key, items]) => {
            const disabled = selectedGroup !== "" && selectedGroup !== key;

            return (
              <fieldset key={key} disabled={disabled} className="rounded-xl border bg-white p-5 disabled:opacity-50">
                <legend className="px-1 text-sm font-semibold text-slate-900">
                  {items[0].payer_name} · {billingMethodLabel(items[0].billing_method)}
                </legend>

                <div className="overflow-x-auto">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b bg-slate-50">
                      <tr>
                        <th className="px-3 py-2"><span className="sr-only">Select</span></th>
                        <th className="px-3 py-2">Date</th>
                        <th className="px-3 py-2">Service</th>
                        <th className="px-3 py-2">Diagnoses</th>
                        <th className="px-3 py-2 text-right">Charge</th>
                      </tr>
                    </thead>
                    <tbody>
                      {items.map((c) => (
                        <tr key={c.id} className="border-b last:border-0">
                          <td className="px-3 py-2">
                            <input
                              type="checkbox"
                              aria-label={`Select ${c.service_code} on ${c.date_of_service}`}
                              checked={selected.includes(c.id)}
                              onChange={(e) => toggle(c, e.target.checked)}
                            />
                          </td>
                          <td className="whitespace-nowrap px-3 py-2">{c.date_of_service}</td>
                          <td className="px-3 py-2">
                            {c.service_code}{c.units > 1 && ` × ${c.units}`}
                            <div className="text-xs text-slate-500">{c.clinician_name}</div>
                          </td>
                          <td className="px-3 py-2">
                            {c.diagnoses.length ? c.diagnoses.map((d) => d.icd10_code).join(", ") : <span className="text-red-700">None</span>}
                          </td>
                          <td className="whitespace-nowrap px-3 py-2 text-right">{money(c.total_charge)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </fieldset>
            );
          })
        )}

        <div className="flex items-center justify-end gap-4">
          <span className="text-sm text-slate-600">{selected.length} service(s) selected</span>
          <button type="button" onClick={createClaim} disabled={creating || selected.length === 0} className={primaryButtonClass}>
            {creating ? "Creating..." : "Create Claim"}
          </button>
        </div>
      </div>
    </main>
  );
}
