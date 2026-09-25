export type Patient = {
  id?: string;

  patient_comments: string;

  first_name: string;
  middle_name: string;
  last_name: string;
  suffix: string;

  preferred_name: string;
  pronouns: string;

  date_of_birth: string;
  account_number: string;

  address_1: string;
  address_2: string;
  zip: string;
  city: string;
  state: string;
  time_zone: string;

  mobile_phone: string;
  mobile_message_preference: string;

  home_phone: string;
  home_message_preference: string;

  work_phone: string;
  work_message_preference: string;

  other_phone: string;
  other_message_preference: string;

  email: string;
  appointment_reminder_setting: string;

  administrative_sex: string;
  gender_identity: string;

  hipaa_npp_on_file: boolean;
  pcp_release: string;

  pad_acknowledged: boolean;
  pad_acknowledged_date: string;

  assigned_clinician_id: string;
};

export const emptyPatient: Patient = {
  patient_comments: "",

  first_name: "",
  middle_name: "",
  last_name: "",
  suffix: "",

  preferred_name: "",
  pronouns: "",

  date_of_birth: "",
  account_number: "",

  address_1: "",
  address_2: "",
  zip: "",
  city: "",
  state: "",
  time_zone: "",

  mobile_phone: "",
  mobile_message_preference: "",

  home_phone: "",
  home_message_preference: "",

  work_phone: "",
  work_message_preference: "",

  other_phone: "",
  other_message_preference: "",

  email: "",
  appointment_reminder_setting: "",

  administrative_sex: "",
  gender_identity: "",

  hipaa_npp_on_file: false,
  pcp_release: "",

  pad_acknowledged: false,
  pad_acknowledged_date: "",

  assigned_clinician_id: "",
};
