export type ServiceCode = {
  id?: string;

  code: string;
  description: string;

  is_add_on: boolean;
  allow_multiple_units: boolean;

  // Decimal amount sent as a string, e.g. "150.00".
  standard_rate: string;

  is_active: boolean;
};

export const emptyServiceCode: ServiceCode = {
  code: "",
  description: "",

  is_add_on: false,
  allow_multiple_units: false,

  standard_rate: "",

  is_active: true,
};

export function formatRate(rate: string) {
  return rate ? `$${rate}` : "—";
}
