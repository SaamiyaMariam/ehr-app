export type Diagnosis = {
  id: string;
  patient_id: string;
  icd10_code: string;
  description: string;
  is_primary: boolean;
  is_active: boolean;
  in_use: boolean;
};

export type ChargeDiagnosis = {
  id: string;
  icd10_code: string;
  description: string;
  pointer: number;
};

export type ChargeBalances = {
  patient_responsibility: string;
  insurance_responsibility: string;
  patient_payments: string;
  insurance_payments: string;
  patient_adjustments: string;
  insurance_adjustments: string;
  patient_balance: string;
  insurance_balance: string;
  total_balance: string;
};

export type Charge = {
  id: string;
  patient_id: string;
  patient_name: string;

  clinician_id: string;
  clinician_name: string;

  service_code_id: string;
  service_code: string;
  service_description: string;

  date_of_service: string;
  units: number;
  modifiers: string[];
  place_of_service: string;

  billing_method: string;
  insurance_policy_id: string;
  payer_id: string;
  payer_name: string;
  policy_priority: string;

  prior_authorization_id: string;
  prior_authorization_code: string;

  rate_per_unit: string;
  rate_source: string;
  rate_schedule_id: string;
  total_charge: string;

  patient_responsibility: string;
  insurance_responsibility: string;

  balances: ChargeBalances;

  status: "active" | "voided";
  display_status: string;

  diagnoses: ChargeDiagnosis[];
  diagnosis_ids: string[];

  notes: string;
  void_reason: string;
  voided_at: string;
  created_at: string;

  lock_reason: string;
};

export type ChargeInput = {
  clinician_id: string;
  service_code_id: string;
  date_of_service: string;
  units: number;
  modifiers: string[];
  place_of_service: string;
  insurance_policy_id: string;
  billing_method: string;
  prior_authorization_id: string;
  patient_responsibility: string;
  diagnosis_ids: string[];
  notes: string;
};

export function emptyChargeInput(today: string): ChargeInput {
  return {
    clinician_id: "",
    service_code_id: "",
    date_of_service: today,
    units: 1,
    modifiers: ["", "", "", ""],
    place_of_service: "11",
    insurance_policy_id: "",
    billing_method: "",
    prior_authorization_id: "",
    patient_responsibility: "",
    diagnosis_ids: [],
    notes: "",
  };
}

export function chargeToInput(charge: Charge): ChargeInput {
  const modifiers = [...charge.modifiers];
  while (modifiers.length < 4) modifiers.push("");

  return {
    clinician_id: charge.clinician_id,
    service_code_id: charge.service_code_id,
    date_of_service: charge.date_of_service,
    units: charge.units,
    modifiers,
    place_of_service: charge.place_of_service,
    insurance_policy_id: charge.insurance_policy_id,
    billing_method: charge.billing_method,
    prior_authorization_id: charge.prior_authorization_id,
    // Direct billing always assigns the full charge to the patient server-side.
    patient_responsibility: charge.billing_method === "direct" ? "" : charge.patient_responsibility,
    diagnosis_ids: charge.diagnosis_ids,
    notes: charge.notes,
  };
}

export const chargeStatusLabels: Record<string, { label: string; tone: "green" | "slate" | "amber" | "red" | "blue" }> = {
  open: { label: "Open", tone: "amber" },
  on_claim: { label: "On claim", tone: "blue" },
  closed: { label: "Closed", tone: "green" },
  voided: { label: "Voided", tone: "slate" },
};

export const placeOfServiceOptions = [
  { value: "11", label: "11 – Office" },
  { value: "02", label: "02 – Telehealth (not in patient's home)" },
  { value: "10", label: "10 – Telehealth in patient's home" },
  { value: "12", label: "12 – Home" },
  { value: "22", label: "22 – On campus outpatient hospital" },
  { value: "53", label: "53 – Community mental health center" },
  { value: "99", label: "99 – Other place of service" },
];
