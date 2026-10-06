// Shared styling + small display helpers for billing screens. Class strings
// match the ones already used across the existing forms.

export const inputClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 outline-none focus:border-slate-500 disabled:bg-slate-100 disabled:text-slate-500";

export const labelClass = "mb-1 block text-sm font-medium text-slate-700";

export const primaryButtonClass =
  "rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50";

export const secondaryButtonClass =
  "rounded-lg border bg-white px-4 py-2 text-sm font-medium text-slate-900 disabled:opacity-50";

export const linkButtonClass =
  "font-medium text-slate-900 underline disabled:opacity-50";

export const cardClass = "rounded-xl border bg-white p-6";

type Tone = "green" | "slate" | "amber" | "red" | "blue";

const toneClass: Record<Tone, string> = {
  green: "bg-green-100 text-green-800",
  slate: "bg-slate-200 text-slate-700",
  amber: "bg-amber-100 text-amber-800",
  red: "bg-red-100 text-red-800",
  blue: "bg-blue-100 text-blue-800",
};

// Badges always carry text, so status never relies on color alone.
export function Badge({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return (
    <span
      className={`inline-block whitespace-nowrap rounded-full px-2.5 py-0.5 text-xs font-medium ${toneClass[tone]}`}
    >
      {children}
    </span>
  );
}

export function ErrorBox({ message }: { message: string }) {
  if (!message) {
    return null;
  }

  return (
    <div role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-700">
      {message}
    </div>
  );
}

export function SuccessBox({ message }: { message: string }) {
  if (!message) {
    return null;
  }

  return (
    <div role="status" className="rounded-lg bg-green-50 p-3 text-sm text-green-700">
      {message}
    </div>
  );
}

// Formats an API decimal string ("1234.5" / "-3.00") for display only.
// Amounts are never computed from these strings in the browser.
export function money(value: string | null | undefined) {
  if (value === null || value === undefined || value === "") {
    return "—";
  }

  const negative = value.startsWith("-");
  const [whole, fraction = ""] = (negative ? value.slice(1) : value).split(".");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");

  return `${negative ? "-" : ""}$${grouped}.${(fraction + "00").slice(0, 2)}`;
}

export function titleCase(value: string) {
  return value
    .split("_")
    .map((part) => (part ? part[0].toUpperCase() + part.slice(1) : part))
    .join(" ");
}

export async function readJSON(response: Response) {
  const text = await response.text();

  try {
    return text ? JSON.parse(text) : {};
  } catch {
    return { error: text || `Request failed (${response.status})` };
  }
}
