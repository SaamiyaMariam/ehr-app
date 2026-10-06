package billing

import (
	"testing"
	"time"
)

func TestPlanAutoAllocationOldestFirst(t *testing.T) {
	t0 := time.Now()
	charges := []openCharge{
		{ID: "newest", DateOfService: "2026-09-20", CreatedAt: t0, Balance: 5000},
		{ID: "oldest", DateOfService: "2026-09-01", CreatedAt: t0, Balance: 3000},
		{ID: "paid", DateOfService: "2026-08-01", CreatedAt: t0, Balance: 0},
		{ID: "middle", DateOfService: "2026-09-10", CreatedAt: t0, Balance: 4000},
	}

	plan := planAutoAllocation(6000, charges)

	if len(plan) != 2 || plan[0].ChargeID != "oldest" || plan[0].Amount != 3000 || plan[1].ChargeID != "middle" || plan[1].Amount != 3000 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	// More money than balances: everything paid, remainder stays credit.
	plan = planAutoAllocation(20000, charges)
	var total int64
	for _, a := range plan {
		total += a.Amount
	}
	if total != 12000 {
		t.Fatalf("auto allocation must not exceed balances, allocated %d", total)
	}
}

func TestPlanExplicitAllocations(t *testing.T) {
	balances := map[string]int64{"a": 5000, "b": 2000}

	plan, message := planExplicitAllocations([]AllocationRequest{{ChargeID: "a", Amount: "50"}, {ChargeID: "b", Amount: "10.50"}}, balances, 10000)
	if message != "" || len(plan) != 2 || plan[1].Amount != 1050 {
		t.Fatalf("expected valid plan, got %+v %q", plan, message)
	}

	cases := map[string]struct {
		req       []AllocationRequest
		available int64
	}{
		"over charge balance":      {[]AllocationRequest{{ChargeID: "b", Amount: "20.01"}}, 10000},
		"split over charge":        {[]AllocationRequest{{ChargeID: "b", Amount: "15"}, {ChargeID: "b", Amount: "5.01"}}, 10000},
		"over payment":             {[]AllocationRequest{{ChargeID: "a", Amount: "50"}, {ChargeID: "b", Amount: "20"}}, 6000},
		"unknown charge":           {[]AllocationRequest{{ChargeID: "z", Amount: "1"}}, 10000},
		"zero":                     {[]AllocationRequest{{ChargeID: "a", Amount: "0"}}, 10000},
		"negative":                 {[]AllocationRequest{{ChargeID: "a", Amount: "-1"}}, 10000},
		"three decimal allocation": {[]AllocationRequest{{ChargeID: "a", Amount: "1.001"}}, 10000},
	}

	for name, c := range cases {
		if _, message := planExplicitAllocations(c.req, balances, c.available); message == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestValidatePaymentInput(t *testing.T) {
	today := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	valid := func() PaymentInput {
		return PaymentInput{
			PaymentDate: "2026-10-05", Amount: "40", Method: "check", CheckNumber: "1001",
			IdempotencyKey: testServiceCodeA,
		}
	}

	in := valid()
	if message := validatePaymentInput(&in, today); message != "" {
		t.Fatalf("expected valid, got %q", message)
	}

	cases := map[string]func(*PaymentInput){
		"no idempotency key": func(p *PaymentInput) { p.IdempotencyKey = "" },
		"future date":        func(p *PaymentInput) { p.PaymentDate = "2026-10-06" },
		"zero amount":        func(p *PaymentInput) { p.Amount = "0" },
		"bad method":         func(p *PaymentInput) { p.Method = "bitcoin" },
		"check w/o number":   func(p *PaymentInput) { p.CheckNumber = "" },
		"card number":        func(p *PaymentInput) { p.Method = "external_card"; p.ReferenceNumber = "4111 1111 1111 1111" },
		"auto + explicit": func(p *PaymentInput) {
			p.AutoAllocate = true
			p.Allocations = []AllocationRequest{{ChargeID: testServiceCodeA, Amount: "1"}}
		},
	}

	for name, mutate := range cases {
		p := valid()
		mutate(&p)
		if validatePaymentInput(&p, today) == "" {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestLooksLikeCardNumber(t *testing.T) {
	for _, v := range []string{"4111111111111111", "4111-1111-1111-1111", "card 5500 0000 0000 0004 used"} {
		if !looksLikeCardNumber(v) {
			t.Errorf("%q should be flagged", v)
		}
	}

	for _, v := range []string{"ch_3NabcXYZ", "TXN-123456", "check 1001", "auth 123456789012"} {
		if looksLikeCardNumber(v) {
			t.Errorf("%q should not be flagged", v)
		}
	}
}
