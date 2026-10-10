"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { apiFetch } from "@/lib/api";

type UserListItem = {
  id: string;
  first_name: string;
  last_name: string;
  preferred_name: string;
  username: string;
  email: string;
};

export default function UsersPage() {
  const [users, setUsers] = useState<UserListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadUsers() {
      try {
        const response = await apiFetch("/api/users");

        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load users");
        }

        setUsers(data);
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load users"
        );
      } finally {
        setLoading(false);
      }
    }

    loadUsers();
  }, []);

  return (
    <main className="flex-1 bg-slate-100">

      <div className="mx-auto max-w-7xl px-6 py-8">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-2xl font-semibold text-slate-900">Users</h1>
          <Link
            href="/users/new"
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white"
          >
            Add User
          </Link>
        </div>

        <p className="mt-1 text-sm text-slate-500">
          View and manage users.
        </p>

        {loading && (
          <p className="mt-6 text-slate-500">
            Loading users...
          </p>
        )}

        {error && (
          <div className="mt-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {!loading && !error && (
          <div className="mt-6 overflow-hidden rounded-xl border bg-white">
            {users.length === 0 ? (
              <div className="p-8 text-center text-slate-500">
                No users yet.
              </div>
            ) : (
              <table className="w-full text-left text-sm">
                <thead className="border-b bg-slate-50 text-slate-600">
                  <tr>
                    <th className="px-5 py-3">Name</th>
                    <th className="px-5 py-3">Username</th>
                    <th className="px-5 py-3">Email</th>
                    <th className="px-5 py-3"></th>
                  </tr>
                </thead>

                <tbody>
                  {users.map((user) => (
                    <tr
                      key={user.id}
                      className="border-b last:border-0"
                    >
                      <td className="px-5 py-4 font-medium text-slate-900">
                        {user.first_name} {user.last_name}

                        {user.preferred_name && (
                          <span className="ml-2 text-xs font-normal text-slate-500">
                            ({user.preferred_name})
                          </span>
                        )}
                      </td>

                      <td className="px-5 py-4">
                        {user.username}
                      </td>

                      <td className="px-5 py-4">
                        {user.email}
                      </td>

                      <td className="px-5 py-4 text-right">
                        <Link
                          href={`/users/${user.id}`}
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
