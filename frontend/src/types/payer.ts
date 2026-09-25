export type Payer = {
  id?: string;

  payer_name: string;
  payer_id: string;

  in_network: boolean;

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

  address_1: "",
  address_2: "",
  zip: "",
  city: "",
  state: "",

  phone: "",
  fax: "",

  is_active: true,
};
