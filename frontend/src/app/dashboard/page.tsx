"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import { clearToken, getToken } from "@/lib/auth";

type User = {
  id: string;
  first_name: string;
  last_name: string;
  username: string;
  email: string;
};

export default function DashboardPage() {
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [roleCount, setRoleCount] = useState<number | null>(null);

  useEffect(() => {
    async function loadUser() {
      const token = getToken();

      if (!token) {
        router.replace("/login");
        return;
      }

      const response = await fetch("/api/auth/me", {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });

      if (!response.ok) {
        clearToken();
        router.replace("/login");
        return;
      }

      const me: User = await response.json();
      setUser(me);

      // Accounts created by sign-up have no role until an administrator
      // assigns one; the API then refuses all practice data.
      const rolesResponse = await fetch(`/api/users/${me.id}/roles`, {
        headers: { Authorization: `Bearer ${token}` },
      });

      if (rolesResponse.ok) {
        setRoleCount((await rolesResponse.json()).length);
      }
    }

    loadUser();
  }, [router]);

  if (!user) {
    return (
      <main className="flex flex-1 items-center justify-center">
        Loading...
      </main>
    );
  }

  return (
    <main className="flex-1 bg-slate-100">

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h2 className="text-2xl font-semibold text-slate-900">
          Dashboard
        </h2>

        <p className="mt-1 text-slate-500">
          Welcome, {user.first_name}.
        </p>

        {roleCount === 0 && (
          <div role="status" className="mt-6 rounded-lg bg-amber-50 p-4 text-sm text-amber-900" data-testid="no-role-notice">
            Your account has no role yet, so practice data is hidden. Ask a practice
            administrator to assign you a role.
          </div>
        )}

        <div className={`mt-8 grid gap-4 md:grid-cols-3 ${roleCount === 0 ? "hidden" : ""}`}>
          <Link
            href="/billing"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Billing
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Billing transactions, claims, payments and reports.
            </p>
          </Link>
          <Link
            href="/patients"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Patients
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Add, view, and update patient records.
            </p>
          </Link>
          <Link
            href="/users"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Users
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Add and manage users.
            </p>
          </Link>
          <Link
            href="/payers"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Payers
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Add and manage payers.
            </p>
          </Link>
          <Link
            href="/settings/service-codes"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Service Codes
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Add and manage billing service codes.
            </p>
          </Link>
          <Link
            href="/settings/billing"
            className="rounded-xl border bg-white p-6 transition hover:shadow-sm"
          >
            <h3 className="font-semibold text-slate-900">
              Billing Settings
            </h3>

            <p className="mt-2 text-sm text-slate-500">
              Practice billing defaults and claim profile.
            </p>
          </Link>
        </div>
      </div>
    </main>
  );
}
