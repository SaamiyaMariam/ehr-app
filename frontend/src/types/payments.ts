export type PaymentAllocation = {
  id: string;
  charge_id: string;
  amount: string;
  status: "active" | "voided";
  date_of_service: string;
  service_code: string;
  created_at: string;
};

export type PaymentRefund = {
  id: string;
  amount: string;
  refund_date: string;
  method: string;
  reference_number: string;
  reason: string;
  created_at: string;
};

export type PatientPayment = {
  id: string;
  patient_id: string;
  patient_name: string;
  payment_date: string;
  amount: string;
  method: string;
  reference_number: string;
  check_number: string;
  notes: string;
  status: "posted" | "voided";
  void_reason: string;
  allocated: string;
  refunded: string;
  unallocated: string;
  created_by: string;
  created_at: string;
  allocations?: PaymentAllocation[];
  refunds?: PaymentRefund[];
};

export const paymentMethodOptions = [
  { value: "cash", label: "Cash" },
  { value: "check", label: "Check" },
  { value: "external_card", label: "Card (processed outside this app)" },
  { value: "external_other", label: "Other external payment" },
];

export function paymentMethodLabel(method: string) {
  return paymentMethodOptions.find((o) => o.value === method)?.label ?? method;
}

export function newIdempotencyKey() {
  return crypto.randomUUID();
}
