export type PatientBalanceSummary = {
  patient_id: string;
  patient_balance: string;
  insurance_balance: string;
  total_outstanding: string;
  unallocated_patient_credit: string;
  open_charges: number;
};

export type LedgerEvent = {
  charge_id: string;
  date_of_service: string;
  service_code: string;
  occurred_on: string;
  recorded_at: string;
  event_type: string;
  party: "patient" | "insurance";
  effect: 1 | -1;
  amount: string;
  status: "active" | "voided";
  source_id: string;
  detail: string;
  voidable: boolean;
};

export type ChargeBreakdown = {
  original_charge: string;
  initial_patient_responsibility: string;
  initial_insurance_responsibility: string;
  transfers_to_patient: string;
  transfers_to_insurance: string;
  patient_responsibility: string;
  insurance_responsibility: string;
  patient_paid: string;
  insurance_paid: string;
  patient_adjustments: string;
  insurance_adjustments: string;
  writeoffs: string;
  patient_balance: string;
  insurance_balance: string;
  total_balance: string;
};

export const ledgerEventLabels: Record<string, string> = {
  charge: "Charge",
  patient_payment: "Patient payment applied",
  insurance_payment: "Insurance payment",
  adjustment: "Adjustment / write-off",
  transfer_out: "Responsibility moved away",
  transfer_in: "Responsibility moved here",
};

// Signed display of an event's effect on what the party owes.
export function signedAmount(event: Pick<LedgerEvent, "effect" | "amount">) {
  return `${event.effect < 0 ? "−" : "+"}${event.amount}`;
}
