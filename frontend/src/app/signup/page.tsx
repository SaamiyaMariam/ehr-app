"use client";

import Link from "next/link";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { setToken } from "@/lib/auth";

export default function SignupPage() {
  const router = useRouter();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setLoading(true);

    const form = new FormData(event.currentTarget);

    const payload = {
      first_name: form.get("first_name"),
      last_name: form.get("last_name"),
      username: form.get("username"),
      email: form.get("email"),
      password: form.get("password"),
    };

    try {
      const response = await fetch("/api/auth/signup", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      const data = await response.json();

      if (!response.ok) {
        setError(data.error || "Unable to create account");
        return;
      }

      setToken(data.token);
      router.push("/dashboard");
    } catch {
      setError("Unable to connect to the server");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-100 px-4">
      <div className="w-full max-w-md rounded-xl bg-white p-8 shadow-sm">
        <h1 className="text-2xl font-semibold text-slate-900">
          Create account
        </h1>

        <form onSubmit={handleSubmit} className="mt-6 space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <input
              name="first_name"
              placeholder="First name"
              required
              className="rounded-lg border px-3 py-2"
            />
            <input
              name="last_name"
              placeholder="Last name"
              required
              className="rounded-lg border px-3 py-2"
            />
          </div>

          <input
            name="username"
            placeholder="Username"
            required
            className="w-full rounded-lg border px-3 py-2"
          />

          <input
            name="email"
            type="email"
            placeholder="Email"
            required
            className="w-full rounded-lg border px-3 py-2"
          />

          <input
            name="password"
            type="password"
            placeholder="Password"
            minLength={8}
            required
            className="w-full rounded-lg border px-3 py-2"
          />

          {error && <p className="text-sm text-red-600">{error}</p>}

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-lg bg-slate-900 px-4 py-2 text-white disabled:opacity-50"
          >
            {loading ? "Creating account..." : "Sign up"}
          </button>
        </form>

        <p className="mt-6 text-center text-sm text-slate-500">
          Already have an account?{" "}
          <Link href="/login" className="font-medium text-slate-900">
            Login
          </Link>
        </p>
      </div>
    </main>
  );
}
