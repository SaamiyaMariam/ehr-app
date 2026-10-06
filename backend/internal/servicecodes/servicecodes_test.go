package servicecodes

import "testing"

func TestValidateServiceCode(t *testing.T) {
	tests := []struct {
		name    string
		code    ServiceCode
		wantErr bool
	}{
		{"valid", ServiceCode{Code: "90834", Description: "Psychotherapy, 45 min", StandardRate: "150.00"}, false},
		{"valid without rate", ServiceCode{Code: "90834", Description: "Psychotherapy"}, false},
		{"missing code", ServiceCode{Description: "Psychotherapy"}, true},
		{"missing description", ServiceCode{Code: "90834"}, true},
		{"negative rate", ServiceCode{Code: "90834", Description: "x", StandardRate: "-1"}, true},
		{"three decimal rate", ServiceCode{Code: "90834", Description: "x", StandardRate: "1.005"}, true},
		{"non-numeric rate", ServiceCode{Code: "90834", Description: "x", StandardRate: "free"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.code
			cleanServiceCode(&s)

			if got := validateServiceCode(&s) != ""; got != tt.wantErr {
				t.Fatalf("validateServiceCode() error = %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestMultipleUnitsOnlyForAddOnCodes(t *testing.T) {
	base := ServiceCode{Code: "90785", Description: "Interactive complexity", AllowMultipleUnits: true}

	if validateServiceCode(&base); base.AllowMultipleUnits {
		t.Errorf("non add-on code should not allow multiple units")
	}

	addOn := ServiceCode{Code: "90785", Description: "Interactive complexity", IsAddOn: true, AllowMultipleUnits: true}

	if validateServiceCode(&addOn); !addOn.AllowMultipleUnits {
		t.Errorf("add-on code should keep allow multiple units")
	}
}
