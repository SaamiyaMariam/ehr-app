"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import RoleSelector from "@/components/role-selector";
import UserForm from "@/components/user-form";
import { apiFetch } from "@/lib/api";
import { Role } from "@/types/role";
import { emptyUser, User } from "@/types/user";

export default function NewUserPage() {
  const router = useRouter();

  const [roles, setRoles] = useState<Role[]>([]);
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [roleError, setRoleError] = useState("");

  useEffect(() => {
    async function loadRoles() {
      try {
        const response = await apiFetch("/api/roles");
        const data = await response.json();

        if (!response.ok) {
          throw new Error(data.error || "Unable to load roles");
        }

        setRoles(data);
      } catch (err) {
        setRoleError(
          err instanceof Error
            ? err.message
            : "Unable to load roles"
        );
      }
    }

    loadRoles();
  }, []);

  async function createUser(user: User) {
    const response = await apiFetch("/api/users", {
      method: "POST",
      body: JSON.stringify(user),
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || "Unable to create user");
    }

    if (!data.id) {
      throw new Error("User created but server did not return an ID");
    }

    const roleResponse = await apiFetch(
      `/api/users/${data.id}/roles`,
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
        roleData.error || "User created but roles could not be saved"
      );
    }

    router.push(`/users/${data.id}`);
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
        <h1 className="text-2xl font-semibold text-slate-900">
          Add New User
        </h1>

        <p className="mt-1 text-sm text-slate-500">
          Enter user information and assign roles.
        </p>

        <div className="mt-6 space-y-6">
          {roleError && (
            <div className="rounded-lg bg-red-50 p-4 text-red-700">
              {roleError}
            </div>
          )}

          <RoleSelector
            roles={roles}
            selectedRoles={selectedRoles}
            onChange={setSelectedRoles}
          />

          <UserForm
            initialUser={emptyUser}
            includePassword
            submitLabel="Create User"
            onSubmit={createUser}
          />
        </div>
      </div>
    </main>
  );
}
