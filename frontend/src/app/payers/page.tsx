"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { apiFetch } from "@/lib/api";

type PayerListItem = {
  id: string;
  payer_name: string;
  payer_id: string;
  in_network: boolean;
  phone: string;
  is_active: boolean;
};

export default function PayersPage() {
  const [payers, setPayers] = useState<PayerListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadPayers() {
      try {
        const response = await apiFetch("/api/payers");
        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load payers");
        }

        setPayers(data);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Unable to load payers"
        );
      } finally {
        setLoading(false);
      }
    }

    loadPayers();
  }, []);

  return (
    <main className="flex-1 bg-slate-100">

      <div className="mx-auto max-w-7xl px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-2xl font-semibold text-slate-900">Payers</h1>
          <Link
            href="/payers/new"
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
          >
            Add Payer
          </Link>
        </div>

        {loading && (
          <p className="mt-6 text-slate-500">
            Loading payers...
          </p>
        )}

        {error && (
          <div className="mt-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {!loading && !error && (
          <div className="mt-6 overflow-hidden rounded-xl border bg-white">
            {payers.length === 0 ? (
              <div className="p-8 text-center text-slate-500">
                No payers yet.
              </div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50">
                  <tr>
                    <th className="px-5 py-3">Payer</th>
                    <th className="px-5 py-3">Payer ID</th>
                    <th className="px-5 py-3">Network</th>
                    <th className="px-5 py-3">Status</th>
                    <th className="px-5 py-3"></th>
                  </tr>
                </thead>

                <tbody>
                  {payers.map((payer) => (
                    <tr
                      key={payer.id}
                      className="border-b last:border-0"
                    >
                      <td className="px-5 py-4 font-medium">
                        {payer.payer_name}
                      </td>

                      <td className="px-5 py-4">
                        {payer.payer_id || "—"}
                      </td>

                      <td className="px-5 py-4">
                        {payer.in_network
                          ? "In network"
                          : "Out of network"}
                      </td>

                      <td className="px-5 py-4">
                        {payer.is_active ? "Enabled" : "Disabled"}
                      </td>

                      <td className="px-5 py-4 text-right">
                        <Link
                          href={`/payers/${payer.id}`}
                          className="font-medium underline"
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
