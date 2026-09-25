"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";

import RoleSelector from "@/components/role-selector";
import UserForm from "@/components/user-form";
import { apiFetch } from "@/lib/api";
import { Role } from "@/types/role";
import { User } from "@/types/user";

export default function UserPage() {
  const params = useParams<{ id: string }>();

  const [user, setUser] = useState<User | null>(null);
  const [roles, setRoles] = useState<Role[]>([]);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);

  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    async function loadData() {
      try {
        const [
          userResponse,
          rolesResponse,
          userRolesResponse,
        ] = await Promise.all([
          apiFetch(`/api/users/${params.id}`),
          apiFetch("/api/roles"),
          apiFetch(`/api/users/${params.id}/roles`),
        ]);

        const userData = await userResponse.json();
        const rolesData = await rolesResponse.json();
        const userRolesData = await userRolesResponse.json();

        if (!userResponse.ok) {
          throw new Error(
            userData.error || "Unable to load user"
          );
        }

        if (!rolesResponse.ok) {
          throw new Error(
            rolesData.error || "Unable to load roles"
          );
        }

        if (!userRolesResponse.ok) {
          throw new Error(
            userRolesData.error ||
              "Unable to load assigned roles"
          );
        }

        setUser(userData);
        setRoles(rolesData);

        setSelectedRoles(
          userRolesData.map((role: Role) => role.key)
        );
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : "Unable to load user"
        );
      } finally {
        setLoading(false);
      }
    }

    loadData();
  }, [params.id]);

  async function updateUser(updatedUser: User) {
    setMessage("");
    setError("");

    const userResponse = await apiFetch(
      `/api/users/${params.id}`,
      {
        method: "PUT",
        body: JSON.stringify(updatedUser),
      }
    );

    const userData = await userResponse.json();

    if (!userResponse.ok) {
      throw new Error(
        userData.error || "Unable to update user"
      );
    }

    const roleResponse = await apiFetch(
      `/api/users/${params.id}/roles`,
      {
        method: "PUT",
        body: JSON.stringify({
          roles: selectedRoles,
        }),
      }
    );

    const roleData = await roleResponse.json();

    if (!roleResponse.ok) {
      throw new Error(
        roleData.error || "Unable to update roles"
      );
    }

    setUser(userData);

    setSelectedRoles(
      roleData.map((role: Role) => role.key)
    );

    setMessage("User updated successfully.");
  }

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center">
        Loading user...
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-slate-100">
      <header className="border-b bg-white">
        <div className="mx-auto max-w-5xl px-6 py-4">
          <Link
            href="/users"
            className="text-sm font-medium text-slate-600"
          >
            ← Back to Users
          </Link>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        {error && (
          <div className="mb-6 rounded-lg bg-red-50 p-4 text-red-700">
            {error}
          </div>
        )}

        {user && (
          <>
            <h1 className="text-2xl font-semibold text-slate-900">
              {user.first_name} {user.last_name}
            </h1>

            <p className="mt-1 text-sm text-slate-500">
              User information and roles
            </p>

            {message && (
              <div className="mt-5 rounded-lg bg-green-50 p-3 text-green-700">
                {message}
              </div>
            )}

            <div className="mt-6 space-y-6">
              <RoleSelector
                roles={roles}
                selectedRoles={selectedRoles}
                onChange={setSelectedRoles}
              />

              <UserForm
                key={user.id}
                initialUser={user}
                submitLabel="Save Changes"
                onSubmit={updateUser}
              />
            </div>
          </>
        )}
      </div>
    </main>
  );
}
