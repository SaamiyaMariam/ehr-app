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

export type UsageSetting = "once_per_service" | "per_unit";

export type PriorAuthorizationServiceCode = {
  id: string;
  code: string;
  description: string;
  is_active: boolean;
};

export type PriorAuthorization = {
  id?: string;
  insurance_policy_id?: string;

  authorization_code: string;

  applies_to_any_service_code: boolean;

  // Write model; service_codes is returned by the API for display.
  service_code_ids: string[];
  service_codes?: PriorAuthorizationServiceCode[];

  start_date: string;
  expiration_date: string;

  uses_allowed: number | null;
  uses_remaining: number | null;

  usage_setting: UsageSetting;

  comments: string;

  is_active: boolean;
};

export const emptyPriorAuthorization: PriorAuthorization = {
  authorization_code: "",

  applies_to_any_service_code: false,

  service_code_ids: [],

  start_date: "",
  expiration_date: "",

  uses_allowed: null,
  uses_remaining: null,

  usage_setting: "once_per_service",

  comments: "",

  is_active: true,
};

export const usageSettingOptions: { value: UsageSetting; label: string }[] = [
  { value: "once_per_service", label: "Once per Service" },
  { value: "per_unit", label: "Per Unit" },
];

export function usageSettingLabel(setting: string) {
  return (
    usageSettingOptions.find((option) => option.value === setting)?.label ??
    setting
  );
}

function todayISO() {
  // en-CA formats as YYYY-MM-DD in the viewer's local time zone.
  return new Date().toLocaleDateString("en-CA");
}

// Simple deterministic warnings; no claims-based usage tracking yet.
export function priorAuthorizationWarnings(
  authorization: Pick<PriorAuthorization, "expiration_date" | "uses_remaining">,
) {
  const warnings: string[] = [];

  if (
    authorization.expiration_date &&
    authorization.expiration_date < todayISO()
  ) {
    warnings.push("Expired");
  }

  if (authorization.uses_remaining === 0) {
    warnings.push("No uses remaining");
  }

  return warnings;
}
