export type BillingDashboard = {
  claims: Record<string, number>;
  clearinghouse_configured: boolean;
  patient_balance: string;
  insurance_balance: string;
  unallocated_patient_credit: string;
  patients_with_balance: number;
  unbilled_services: { count: number; amount: string };
  unapplied_insurance_payments: { count: number; amount: string };
};

export type AgingTotals = {
  bucket_0_30: string;
  bucket_31_60: string;
  bucket_61_90: string;
  bucket_91_plus: string;
  total: string;
};

export type InsuranceAgingRow = {
  charge_id: string;
  patient_id: string;
  patient_name: string;
  date_of_service: string;
  service_code: string;
  payer_id: string;
  payer_name: string;
  claim_id: string;
  claim_number: string;
  claim_status: string;
  claim_sequence: string;
  age_days: number;
  bucket: string;
  insurance_balance: string;
};

export type CollectionsLine = { label: string; count: number; amount: string };

export type CollectionsReport = {
  from: string;
  to: string;
  charges_created: CollectionsLine;
  patient_payments: CollectionsLine;
  insurance_payments: CollectionsLine;
  refunds: CollectionsLine;
  total_collections: string;
  writeoffs: CollectionsLine;
  adjustments: CollectionsLine;
  insurance_by_payer: CollectionsLine[];
  patient_by_method: CollectionsLine[];
  definitions: Record<string, string>;
};

export const bucketKeys: { value: string; label: string; key: keyof AgingTotals }[] = [
  { value: "0-30", label: "0–30 days", key: "bucket_0_30" },
  { value: "31-60", label: "31–60 days", key: "bucket_31_60" },
  { value: "61-90", label: "61–90 days", key: "bucket_61_90" },
  { value: "91+", label: "91+ days", key: "bucket_91_plus" },
];

export const claimQueueLabels: Record<string, string> = {
  pending: "Pending claims",
  validation_errors: "Claims with validation errors",
  rejected: "Rejected claims",
  paper_pending: "Pending paper claims",
  paper_generated: "Paper generated, not yet mailed",
  external_pending: "Pending external claims",
  electronic_ready: "Prepared electronic claims",
};
