export type ClaimValidation = {
  errors: string[];
  warnings: string[];
};

export type ClaimLine = {
  id: string;
  charge_id: string;
  line_number: number;
  date_of_service: string;
  service_code: string;
  service_description: string;
  units: number;
  modifiers: string[];
  place_of_service: string;
  rate: string;
  line_total: string;
  rendering_clinician_id: string;
  rendering_name: string;
  rendering_npi: string;
  diagnosis_pointers: string;
  prior_authorization_id: string;
  prior_authorization_code: string;
  is_current: boolean;
  insurance_paid: string;
  insurance_balance: string;
  adjudicated: boolean;
};

export type ClaimDiagnosis = {
  position: number;
  letter: string;
  icd10_code: string;
  description: string;
};

export type ClaimHistoryEvent = {
  id: string;
  claim_id: string;
  event_type: string;
  from_status: string;
  to_status: string;
  source: string;
  message: string;
  created_by: string;
  created_at: string;
};

export type ClaimComment = {
  id: string;
  author: string;
  comment: string;
  created_at: string;
};

type Address = { address_1: string; address_2: string; city: string; state: string; zip: string };

export type ClaimSnapshot = {
  captured_at: string;
  patient: {
    first_name: string;
    middle_name: string;
    last_name: string;
    date_of_birth: string;
    sex: string;
    phone: string;
    account_number: string;
    address: Address;
  };
  insured: {
    relationship: string;
    first_name: string;
    middle_name: string;
    last_name: string;
    date_of_birth: string;
    sex: string;
    address: Address;
    member_id: string;
    policy_group: string;
    plan_name: string;
    signature_on_file: boolean;
  };
  payer: { id: string; name: string; payer_id: string; insurance_type: string; in_network: boolean; address: Address };
  practice: { name: string; npi: string; tax_id: string; tax_id_type: string; phone: string; address: Address };
  policy: { id: string; priority: string; coverage_start: string; coverage_end: string };
  other_insurance: {
    sequence: string;
    payer_name: string;
    member_id: string;
    claim_number: string;
    amount_paid: string;
  }[];
  adjudication?: Adjudication;
};

export type Claim = {
  id: string;
  claim_number: string;
  patient_id: string;
  patient_name: string;
  insurance_policy_id: string;
  payer_id: string;
  payer_name: string;
  sequence: string;
  submission_method: "electronic" | "paper" | "external";
  status: string;
  resubmission_type: "new" | "amended" | "void";
  payer_claim_control_number: string;
  previous_claim_id: string;
  total_billed: string;
  first_date_of_service: string;
  last_date_of_service: string;
  validation: ClaimValidation | null;
  validated_at: string;
  external_reference: string;
  submitted_at: string;
  created_at: string;
  updated_at: string;
  snapshot?: ClaimSnapshot;
  lines?: ClaimLine[];
  diagnoses?: ClaimDiagnosis[];
  history?: ClaimHistoryEvent[];
  comments?: ClaimComment[];
  documents?: ClaimDocument[];
  remittances?: ClaimRemittance[];
  follow_up_claims?: ClaimRef[];
};

export type ClaimRef = { id: string; claim_number: string; sequence: string; status: string };

export type AdjudicationLine = {
  charge_id: string;
  line_number: number;
  date_of_service: string;
  service_code: string;
  billed: string;
  previous_paid: string;
  previous_allowed: string;
  previous_adjustments: string;
  transferred_to_patient: string;
  eligible_amount: string;
};

export type Adjudication = {
  previous_claim_id: string;
  previous_claim_number: string;
  previous_sequence: string;
  previous_payer_name: string;
  total_eligible: string;
  lines: AdjudicationLine[];
};

export type NextPolicy = {
  id: string;
  payer_id: string;
  payer_name: string;
  member_id: string;
  plan_name: string;
  coverage_start: string;
  coverage_end: string;
};

export type NextSequenceEval = {
  previous_claim_id: string;
  previous_claim_number: string;
  previous_sequence: string;
  next_sequence: string;
  can_create: boolean;
  reason: string;
  needs_policy_choice: boolean;
  policy: NextPolicy | null;
  candidate_policies: NextPolicy[];
  eligible_amount: string;
  lines: { charge_id: string; claim_line_id: string; line_number: number; service_code: string; date_of_service: string; eligible: boolean; eligible_amount: string; reason: string }[];
  existing_claims: ClaimRef[];
};

export type ClaimRemittance = {
  allocation_id: string;
  payment_id: string;
  payment_date: string;
  reference_number: string;
  payment_status: "posted" | "voided";
  line_number: number;
  service_code: string;
  amount_paid: string;
  allowed_amount: string;
  is_final: boolean;
  allocation_status: "active" | "voided";
  adjusted: string;
  transferred_to_patient: string;
};

export type ClaimDocument = {
  id: string;
  version: number;
  frequency_code: string;
  page_count: number;
  generated_by: string;
  created_at: string;
};

type Tone = "green" | "slate" | "amber" | "red" | "blue";

export const claimStatusLabels: Record<string, { label: string; tone: Tone }> = {
  draft: { label: "Draft", tone: "slate" },
  validation_error: { label: "Validation errors", tone: "red" },
  ready: { label: "Ready", tone: "blue" },
  pending_submission: { label: "Pending submission", tone: "amber" },
  submitted: { label: "Submitted", tone: "blue" },
  sent: { label: "Sent to payer", tone: "blue" },
  paper_generated: { label: "Paper generated", tone: "amber" },
  externally_submitted: { label: "Submitted externally", tone: "blue" },
  rejected_new: { label: "Rejected (new)", tone: "red" },
  rejected: { label: "Rejected (reviewed)", tone: "red" },
  resubmitted: { label: "Resubmitted", tone: "blue" },
  paid: { label: "Paid / adjudicated", tone: "green" },
  voided: { label: "Voided", tone: "slate" },
};

export function claimStatus(status: string) {
  return claimStatusLabels[status] ?? { label: status, tone: "slate" as Tone };
}

export const editableClaimStatuses = ["draft", "validation_error", "ready"];

export const submissionMethodLabels: Record<string, string> = {
  electronic: "Electronic",
  paper: "Paper (CMS-1500)",
  external: "External",
};

export function formatTimestamp(value: string) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
