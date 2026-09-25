export type User = {
  id?: string;

  user_comments: string;

  first_name: string;
  middle_name: string;
  last_name: string;
  suffix: string;

  preferred_name: string;
  pronouns: string;

  username: string;
  date_of_birth: string;
  languages: string[];

  email: string;
  mobile_phone: string;
  can_receive_text_messages: boolean;
  work_phone: string;
  home_phone: string;

  address_1: string;
  address_2: string;
  zip: string;
  city: string;
  state: string;

  password?: string;
};

export const emptyUser: User = {
  user_comments: "",

  first_name: "",
  middle_name: "",
  last_name: "",
  suffix: "",

  preferred_name: "",
  pronouns: "",

  username: "",
  date_of_birth: "",
  languages: [],

  email: "",
  mobile_phone: "",
  can_receive_text_messages: false,
  work_phone: "",
  home_phone: "",

  address_1: "",
  address_2: "",
  zip: "",
  city: "",
  state: "",

  password: "",
};
