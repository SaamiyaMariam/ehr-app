"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";
import { Badge, ErrorBox, linkButtonClass, money, primaryButtonClass } from "@/lib/ui";
import { PatientPayment, paymentMethodLabel } from "@/types/payments";

export default function PaymentsSection({ patientId }: { patientId: string }) {
  const [payments, setPayments] = useState<PatientPayment[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const response = await apiFetch(`/api/patients/${patientId}/payments`);
      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to load payments");
        return;
      }

      setPayments(data);
    }

    load();
  }, [patientId]);

  return (
    <section className="mt-8">
      <div className="flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold text-slate-900">Patient Payments</h2>
        <Link href={`/patients/${patientId}/billing/payments/new`} className={primaryButtonClass}>Enter Patient Payment</Link>
      </div>

      <div className="mt-3"><ErrorBox message={error} /></div>

      <div className="mt-3 overflow-x-auto rounded-xl border bg-white">
        {payments === null ? (
          <div className="p-6 text-center text-slate-500">Loading payments...</div>
        ) : payments.length === 0 ? (
          <div className="p-6 text-center text-slate-500">No patient payments yet.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="border-b bg-slate-50">
              <tr>
                <th className="px-3 py-2">Date</th>
                <th className="px-3 py-2">Method</th>
                <th className="px-3 py-2 text-right">Amount</th>
                <th className="px-3 py-2 text-right">Applied</th>
                <th className="px-3 py-2 text-right">Credit</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {payments.map((p) => (
                <tr key={p.id} className={`border-b last:border-0 ${p.status === "voided" ? "text-slate-500" : ""}`}>
                  <td className="whitespace-nowrap px-3 py-2">{p.payment_date}</td>
                  <td className="px-3 py-2">{paymentMethodLabel(p.method)}{p.check_number && ` #${p.check_number}`}</td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.amount)}</td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.allocated)}</td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">{money(p.unallocated)}</td>
                  <td className="px-3 py-2"><Badge tone={p.status === "posted" ? "green" : "slate"}>{p.status === "posted" ? "Posted" : "Voided"}</Badge></td>
                  <td className="px-3 py-2 text-right">
                    <Link href={`/patients/${patientId}/billing/payments/${p.id}`} className={linkButtonClass}>View</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
