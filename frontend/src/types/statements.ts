export type StatementLine = {
  date: string;
  description: string;
  charge: string;
  responsibility: string;
  payments: string;
  adjustments: string;
  balance: string;
};

export type PatientStatement = {
  id: string;
  statement_number: string;
  patient_id: string;
  patient_name: string;
  statement_type: "open_balance" | "date_range";
  statement_date: string;
  start_date: string;
  end_date: string;
  balance_due: string;
  credit_on_account: string;
  amount_due: string;
  comment: string;
  batch_id: string;
  generated_by: string;
  created_at: string;
};

export type PatientAgingRow = {
  patient_id: string;
  patient_name: string;
  bucket_0_30: string;
  bucket_31_60: string;
  bucket_61_90: string;
  bucket_91_plus: string;
  total: string;
  unallocated_credit: string;
  last_statement_date: string;
  oldest_service_date: string;
};

export const agingBucketOptions = [
  { value: "0-30", label: "0–30 days" },
  { value: "31-60", label: "31–60 days" },
  { value: "61-90", label: "61–90 days" },
  { value: "91+", label: "91+ days" },
];

export function statementPeriod(s: Pick<PatientStatement, "statement_type" | "start_date" | "end_date">) {
  return s.statement_type === "date_range" ? `${s.start_date} to ${s.end_date}` : "Open balance";
}
