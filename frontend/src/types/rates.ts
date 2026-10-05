export type SubmissionMethod = "electronic" | "paper" | "external";

export type PracticeBillingSettings = {
  default_in_network_billing_method: SubmissionMethod;
  default_out_of_network_billing_method: SubmissionMethod;
};

export const submissionMethodOptions: { value: SubmissionMethod; label: string }[] = [
  { value: "electronic", label: "Electronic" },
  { value: "paper", label: "Paper (CMS-1500)" },
  { value: "external", label: "External (submitted outside this app)" },
];

export type RateScheduleItem = {
  service_code_id: string;
  code?: string;
  description?: string;
  standard_rate?: string;
  custom_rate: string;
  service_code_is_active?: boolean;
};

export type RateSchedule = {
  id?: string;
  payer_id?: string;
  payer_name?: string;
  name: string;
  use_standard_practice_rates: boolean;
  is_active: boolean;
  item_count?: number;
  items: RateScheduleItem[];
};

export const emptyRateSchedule: RateSchedule = {
  name: "",
  use_standard_practice_rates: false,
  is_active: true,
  items: [],
};

export type ClinicianRateSchedule = {
  clinician_id: string;
  clinician_name?: string;
  rate_schedule_id: string;
  rate_schedule_name?: string;
  rate_schedule_is_active?: boolean;
};

export type PatientCashRate = {
  service_code_id: string;
  code?: string;
  description?: string;
  standard_rate?: string;
  rate: string;
};

export type RatePreview = {
  rate_per_unit: string;
  source: "patient_cash_rate" | "payer_rate_schedule" | "standard_rate";
  rate_schedule_id: string | null;
  billing_method: string;
  payer_id: string;
  units: number;
  total_charge: string;
};

export const rateSourceLabels: Record<string, string> = {
  patient_cash_rate: "Patient cash rate",
  payer_rate_schedule: "Payer rate schedule",
  standard_rate: "Standard rate",
};

export const billingMethodLabels: Record<string, string> = {
  direct: "Direct (patient)",
  insurance_in_network_electronic: "Insurance · In network · Electronic",
  insurance_in_network_paper: "Insurance · In network · Paper",
  insurance_in_network_external: "Insurance · In network · External",
  insurance_out_of_network_electronic: "Insurance · Out of network · Electronic",
  insurance_out_of_network_paper: "Insurance · Out of network · Paper",
  insurance_out_of_network_external: "Insurance · Out of network · External",
};

export function billingMethodLabel(method: string) {
  return billingMethodLabels[method] ?? method;
}
