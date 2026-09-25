"use client";

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

      setUser(await response.json());
    }

    loadUser();
  }, [router]);

  function logout() {
    clearToken();
    router.replace("/login");
  }

  if (!user) {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p>Loading...</p>
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-4">
          <h1 className="font-semibold text-slate-900">EHR</h1>

          <div className="flex items-center gap-4">
            <span className="text-sm text-slate-600">
              {user.first_name} {user.last_name}
            </span>

            <button
              onClick={logout}
              className="rounded-lg border px-3 py-2 text-sm"
            >
              Logout
            </button>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl px-6 py-8">
        <h2 className="text-2xl font-semibold text-slate-900">Dashboard</h2>
        <p className="mt-2 text-slate-500">
          Welcome, {user.first_name}.
        </p>
      </div>
    </main>
  );
}
