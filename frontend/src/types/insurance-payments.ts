export type AllocationAdjustment = {
  id: string;
  type: string;
  amount: string;
  reason: string;
  reference: string;
  status: "active" | "voided";
};

export type AllocationTransfer = {
  id: string;
  from_party: string;
  to_party: string;
  reason: string;
  amount: string;
  note: string;
  status: "active" | "voided";
};

export type InsuranceAllocation = {
  id: string;
  claim_id: string;
  claim_number: string;
  claim_line_id: string;
  charge_id: string;
  patient_id: string;
  patient_name: string;
  date_of_service: string;
  service_code: string;
  billed: string;
  amount_paid: string;
  allowed_amount: string;
  is_final: boolean;
  status: "active" | "voided";
  adjustments: AllocationAdjustment[];
  transfers: AllocationTransfer[];
};

export type InsurancePayment = {
  id: string;
  payer_id: string;
  payer_name: string;
  payment_date: string;
  amount: string;
  payment_type: string;
  reference_number: string;
  notes: string;
  status: "posted" | "voided";
  void_reason: string;
  allocated: string;
  unallocated: string;
  adjusted: string;
  transferred_to_patient: string;
  created_by: string;
  created_at: string;
  allocations?: InsuranceAllocation[];
};

export type ClaimOutcome = { claim_id: string; claim_number: string; status: string };

export type OutstandingLine = {
  claim_id: string;
  claim_number: string;
  claim_status: string;
  sequence: string;
  payer_id: string;
  payer_name: string;
  patient_id: string;
  patient_name: string;
  claim_line_id: string;
  charge_id: string;
  line_number: number;
  date_of_service: string;
  service_code: string;
  billed: string;
  insurance_responsibility: string;
  insurance_paid: string;
  insurance_balance: string;
  patient_balance: string;
  adjudicated: boolean;
};

export const insurancePaymentTypeOptions = [
  { value: "check", label: "Check" },
  { value: "eft", label: "EFT / ACH" },
  { value: "virtual_card", label: "Virtual card" },
  { value: "other", label: "Other" },
];

export function insurancePaymentTypeLabel(type: string) {
  return insurancePaymentTypeOptions.find((o) => o.value === type)?.label ?? type;
}

export const otherAdjustmentOptions = [
  { value: "payer_adjustment", label: "Payer adjustment" },
  { value: "manual_adjustment", label: "Manual adjustment" },
  { value: "small_balance_writeoff", label: "Small balance write-off" },
  { value: "bad_debt_writeoff", label: "Bad debt write-off" },
  { value: "courtesy_writeoff", label: "Courtesy write-off" },
];

export const transferReasonLabels: Record<string, string> = {
  deductible: "Deductible",
  copay: "Copay",
  coinsurance: "Coinsurance",
  noncovered: "Non-covered",
  correction: "Correction",
  other: "Other",
};

// One service line being entered on a remittance. Amounts are the strings
// the biller typed; the server parses and validates them.
export type EntryLine = {
  claim_id: string;
  claim_number: string;
  claim_line_id: string;
  patient_name: string;
  date_of_service: string;
  service_code: string;
  billed: string;
  insurance_balance: string;
  paid: string;
  allowed: string;
  contractual: string;
  other: string;
  other_type: string;
  deductible: string;
  copay: string;
  coinsurance: string;
  noncovered: string;
  final: boolean;
};

export function entryLineFrom(l: OutstandingLine): EntryLine {
  return {
    claim_id: l.claim_id,
    claim_number: l.claim_number,
    claim_line_id: l.claim_line_id,
    patient_name: l.patient_name,
    date_of_service: l.date_of_service,
    service_code: l.service_code,
    billed: l.billed,
    insurance_balance: l.insurance_balance,
    paid: "",
    allowed: "",
    contractual: "",
    other: "",
    other_type: "payer_adjustment",
    deductible: "",
    copay: "",
    coinsurance: "",
    noncovered: "",
    final: true,
  };
}

// Display helper only: the backend is authoritative for every amount.
// Returns integer cents, 0 for blank, or null when the text is not a
// money amount.
export function toCents(value: string): number | null {
  const text = value.trim();
  if (text === "") return 0;
  if (!/^\d{1,10}(\.\d{1,2})?$/.test(text)) return null;
  const [whole, fraction = ""] = text.split(".");
  return Number(whole) * 100 + Number((fraction + "00").slice(0, 2));
}

export function centsToMoney(cents: number) {
  const sign = cents < 0 ? "-" : "";
  const abs = Math.abs(cents);
  return `${sign}$${Math.floor(abs / 100).toLocaleString("en-US")}.${String(abs % 100).padStart(2, "0")}`;
}

export function lineAmounts(l: EntryLine) {
  const parts = {
    paid: toCents(l.paid),
    contractual: toCents(l.contractual),
    other: toCents(l.other),
    deductible: toCents(l.deductible),
    copay: toCents(l.copay),
    coinsurance: toCents(l.coinsurance),
    noncovered: toCents(l.noncovered),
  };

  const invalid = Object.values(parts).some((v) => v === null);
  const n = (v: number | null) => v ?? 0;
  const transferred = n(parts.deductible) + n(parts.copay) + n(parts.coinsurance) + n(parts.noncovered);
  const adjusted = n(parts.contractual) + n(parts.other);

  return { invalid, paid: n(parts.paid), adjusted, transferred };
}

export function remainingAfter(l: EntryLine) {
  const { paid, adjusted, transferred } = lineAmounts(l);
  return (toCents(l.insurance_balance) ?? 0) - paid - adjusted - transferred;
}

// Builds the API body for a line.
export function toRemittanceLine(l: EntryLine) {
  const adjustments: { type: string; amount: string; reason?: string }[] = [];
  if (toCents(l.contractual)) adjustments.push({ type: "contractual_writeoff", amount: l.contractual.trim() });
  if (toCents(l.other)) adjustments.push({ type: l.other_type, amount: l.other.trim() });

  const transfers: { reason: string; amount: string }[] = [];
  for (const reason of ["deductible", "copay", "coinsurance", "noncovered"] as const) {
    if (toCents(l[reason])) transfers.push({ reason, amount: l[reason].trim() });
  }

  return {
    claim_id: l.claim_id,
    claim_line_id: l.claim_line_id,
    amount_paid: l.paid.trim() || "0",
    allowed_amount: l.allowed.trim(),
    is_final: l.final,
    adjustments,
    transfers,
  };
}

export function newIdempotencyKey() {
  return crypto.randomUUID();
}
