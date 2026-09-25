"use client";

import { Role } from "@/types/role";

type Props = {
  roles: Role[];
  selectedRoles: string[];
  onChange: (roles: string[]) => void;
};

const groups = [
  {
    title: "Practice Administration",
    keys: ["practice_administrator"],
  },
  {
    title: "Clinical Access",
    keys: [
      "clinician",
      "intern_assistant_associate",
      "supervisor",
      "clinical_administrator",
    ],
  },
  {
    title: "Scheduling Access",
    keys: ["practice_scheduler"],
  },
  {
    title: "Billing Access",
    keys: ["practice_biller"],
  },
];

export default function RoleSelector({
  roles,
  selectedRoles,
  onChange,
}: Props) {
  function toggleRole(key: string) {
    if (selectedRoles.includes(key)) {
      onChange(selectedRoles.filter((role) => role !== key));
    } else {
      onChange([...selectedRoles, key]);
    }
  }

  function getRole(key: string) {
    return roles.find((role) => role.key === key);
  }

  return (
    <section className="rounded-xl border bg-white p-6">
      <h2 className="text-lg font-semibold text-slate-900">
        Roles
      </h2>

      <div className="mt-5 grid gap-6 md:grid-cols-2">
        {groups.map((group) => (
          <div key={group.title}>
            <h3 className="mb-3 text-sm font-semibold text-slate-700">
              {group.title}
            </h3>

            <div className="space-y-3">
              {group.keys.map((key) => {
                const role = getRole(key);

                if (!role) {
                  return null;
                }

                return (
                  <label
                    key={role.key}
                    className="flex items-center gap-3 text-sm text-slate-700"
                  >
                    <input
                      type="checkbox"
                      checked={selectedRoles.includes(role.key)}
                      onChange={() => toggleRole(role.key)}
                    />

                    {role.name}
                  </label>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
