export type BillingSettings = {
  patient_id: string;
  billing_comments: string;
};

export type InsurancePriority =
  | "primary"
  | "secondary"
  | "tertiary"
  | "quaternary";

export type AppointmentLimitType = "number" | "unlimited" | "unknown";

export type InsurancePolicy = {
  id?: string;
  patient_id?: string;

  payer_id: string;
  payer_name?: string;

  priority: InsurancePriority;

  member_id: string;
  policy_group: string;
  plan_name: string;

  policy_comments: string;

  signature_on_file: boolean;

  coverage_start: string;
  coverage_end: string;

  // Decimal amounts are sent as strings, e.g. "25.00".
  copay: string;
  deductible: string;

  appointment_limit_type: AppointmentLimitType;
  appointments_allowed: number | null;
  appointments_expiration: string;

  relationship_to_policy_holder: string;

  policy_holder_first_name: string;
  policy_holder_middle_name: string;
  policy_holder_last_name: string;

  policy_holder_date_of_birth: string;
  policy_holder_sex: string;

  policy_holder_address_1: string;
  policy_holder_address_2: string;
  policy_holder_city: string;
  policy_holder_state: string;
  policy_holder_zip: string;

  msp_qualification: string;

  is_active: boolean;
};

export type InsurancePolicyListItem = {
  id: string;
  payer_id: string;
  payer_name: string;
  priority: InsurancePriority;
  member_id: string;
  plan_name: string;
  coverage_start: string;
  coverage_end: string;
  is_active: boolean;
};

export const emptyInsurancePolicy: InsurancePolicy = {
  payer_id: "",

  priority: "primary",

  member_id: "",
  policy_group: "",
  plan_name: "",

  policy_comments: "",

  signature_on_file: false,

  coverage_start: "",
  coverage_end: "",

  copay: "",
  deductible: "",

  appointment_limit_type: "unknown",
  appointments_allowed: null,
  appointments_expiration: "",

  relationship_to_policy_holder: "",

  policy_holder_first_name: "",
  policy_holder_middle_name: "",
  policy_holder_last_name: "",

  policy_holder_date_of_birth: "",
  policy_holder_sex: "",

  policy_holder_address_1: "",
  policy_holder_address_2: "",
  policy_holder_city: "",
  policy_holder_state: "",
  policy_holder_zip: "",

  msp_qualification: "",

  is_active: true,
};

export const priorityOptions: { value: InsurancePriority; label: string }[] = [
  { value: "primary", label: "Primary" },
  { value: "secondary", label: "Secondary" },
  { value: "tertiary", label: "Tertiary" },
  { value: "quaternary", label: "Quaternary" },
];

export const appointmentLimitOptions: {
  value: AppointmentLimitType;
  label: string;
}[] = [
  { value: "number", label: "Number" },
  { value: "unlimited", label: "Unlimited" },
  { value: "unknown", label: "Unknown" },
];

export const relationshipOptions = [
  { value: "", label: "Select relationship" },
  { value: "self", label: "Self" },
  { value: "spouse", label: "Spouse" },
  { value: "child", label: "Child" },
  { value: "other", label: "Other" },
];

export const policyHolderSexOptions = [
  { value: "", label: "Select sex" },
  { value: "male", label: "Male" },
  { value: "female", label: "Female" },
  { value: "unknown", label: "Unknown" },
];

export function priorityLabel(priority: string) {
  return (
    priorityOptions.find((option) => option.value === priority)?.label ??
    priority
  );
}
