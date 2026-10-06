export type Payer = {
  id?: string;

  payer_name: string;
  payer_id: string;

  in_network: boolean;

  // "" = use the practice default for in/out-of-network claims.
  billing_method: "" | "electronic" | "paper" | "external";
  insurance_type: string;

  address_1: string;
  address_2: string;
  zip: string;
  city: string;
  state: string;

  phone: string;
  fax: string;

  is_active: boolean;
};

export const emptyPayer: Payer = {
  payer_name: "",
  payer_id: "",

  in_network: false,

  billing_method: "",
  insurance_type: "",

  address_1: "",
  address_2: "",
  zip: "",
  city: "",
  state: "",

  phone: "",
  fax: "",

  is_active: true,
};

export const payerBillingMethodOptions = [
  { value: "", label: "Use practice default" },
  { value: "electronic", label: "Electronic" },
  { value: "paper", label: "Paper (CMS-1500)" },
  { value: "external", label: "External (submitted outside this app)" },
];

export const insuranceTypeOptions = [
  { value: "", label: "Select insurance type" },
  { value: "group_health_plan", label: "Group Health Plan" },
  { value: "medicare", label: "Medicare" },
  { value: "medicaid", label: "Medicaid" },
  { value: "tricare", label: "TRICARE" },
  { value: "champva", label: "CHAMPVA" },
  { value: "feca_black_lung", label: "FECA / Black Lung" },
  { value: "other", label: "Other" },
];
