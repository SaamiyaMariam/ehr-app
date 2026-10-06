package billing

import (
	"strings"
	"testing"
)

func TestNextSequenceOrdering(t *testing.T) {
	cases := map[string]string{
		"primary": "secondary", "secondary": "tertiary", "tertiary": "quaternary", "quaternary": "", "": "", "fifth": "",
	}

	for in, want := range cases {
		if got := nextSequence(in); got != want {
			t.Errorf("nextSequence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextPolicyCoverage(t *testing.T) {
	p := NextPolicy{CoverageStart: "2026-01-01", CoverageEnd: "2026-06-30"}

	for date, want := range map[string]bool{
		"2025-12-31": false, "2026-01-01": true, "2026-03-15": true, "2026-06-30": true, "2026-07-01": false,
	} {
		if got := p.covers(date); got != want {
			t.Errorf("covers(%s) = %v, want %v", date, got, want)
		}
	}

	if !(NextPolicy{}).covers("1999-01-01") {
		t.Error("a policy without dates covers every date")
	}
}

func eligibleLine(id string, dos string, eligible int64) nextLineInput {
	return nextLineInput{
		ChargeID: id, ClaimLineID: "line-" + id, LineNumber: 1, ServiceCode: "90837", DateOfService: dos,
		ChargeActive: true, Adjudicated: true, Eligible: eligible,
	}
}

func TestEvaluateNextSequence(t *testing.T) {
	secondary := NextPolicy{ID: "pol-2", PayerName: "Second Payer", CoverageStart: "2026-01-01"}
	lines := func(ls ...nextLineInput) []nextLineInput { return ls }
	policies := func(ps ...NextPolicy) []NextPolicy { return ps }

	type tc struct {
		name       string
		status     string
		sequence   string
		lines      []nextLineInput
		policies   []NextPolicy
		requested  string
		canCreate  bool
		reasonPart string
		eligible   string
	}

	notAdjudicated := eligibleLine("a", "2026-03-01", 3000)
	notAdjudicated.Adjudicated = false
	voided := eligibleLine("a", "2026-03-01", 3000)
	voided.ChargeActive = false
	already := eligibleLine("a", "2026-03-01", 3000)
	already.ExistingClaim = "CLM-0001"

	cases := []tc{
		{"eligible", "externally_submitted", "primary", lines(eligibleLine("a", "2026-03-01", 3000)), policies(secondary), "", true, "", "30.00"},
		{"primary fully resolved", "paid", "primary", lines(eligibleLine("a", "2026-03-01", 0)), policies(secondary), "", false, "No remaining insurance responsibility", "0.00"},
		{"no secondary policy", "submitted", "primary", lines(eligibleLine("a", "2026-03-01", 3000)), nil, "", false, "No secondary insurance policy", "0.00"},
		{"coverage inactive", "submitted", "primary", lines(eligibleLine("a", "2025-03-01", 3000)), policies(secondary), "", false, "Coverage inactive", "0.00"},
		{"payer has not adjudicated", "submitted", "primary", lines(notAdjudicated), policies(secondary), "", false, "has not finished adjudicating", "0.00"},
		{"claim not submitted", "draft", "primary", lines(eligibleLine("a", "2026-03-01", 3000)), policies(secondary), "", false, "has not been adjudicated", "0.00"},
		{"service voided", "submitted", "primary", lines(voided), policies(secondary), "", false, "voided", "0.00"},
		{"duplicate claim", "paid", "primary", lines(already), policies(secondary), "", false, "already exists", "0.00"},
		{"last sequence", "paid", "quaternary", lines(eligibleLine("a", "2026-03-01", 3000)), policies(secondary), "", false, "last insurance sequence", "0.00"},
		{"tertiary follows secondary", "paid", "secondary", lines(eligibleLine("a", "2026-03-01", 500)), policies(NextPolicy{ID: "pol-3"}), "", true, "", "5.00"},
		{"two eligible lines add up", "submitted", "primary", lines(eligibleLine("a", "2026-03-01", 3000), eligibleLine("b", "2026-03-02", 1250)), policies(secondary), "", true, "", "42.50"},
		{"unknown requested policy", "submitted", "primary", lines(eligibleLine("a", "2026-03-01", 3000)), policies(secondary), "someone-else", false, "not an active", "0.00"},
		{"requested policy that covers nothing", "submitted", "primary", lines(eligibleLine("a", "2025-03-01", 3000)), policies(secondary), "pol-2", false, "Coverage inactive", "0.00"},
	}

	for _, c := range cases {
		ev := evaluateNextSequence(c.status, c.sequence, c.lines, c.policies, c.requested)

		if ev.CanCreate != c.canCreate {
			t.Errorf("%s: can_create = %v (reason %q), want %v", c.name, ev.CanCreate, ev.Reason, c.canCreate)
		}

		if !c.canCreate && !strings.Contains(ev.Reason, c.reasonPart) {
			t.Errorf("%s: reason %q does not mention %q", c.name, ev.Reason, c.reasonPart)
		}

		if ev.EligibleAmount != c.eligible {
			t.Errorf("%s: eligible amount %s, want %s", c.name, ev.EligibleAmount, c.eligible)
		}

		if len(ev.Lines) != len(c.lines) {
			t.Errorf("%s: %d line results for %d lines", c.name, len(ev.Lines), len(c.lines))
		}
	}
}

func TestEvaluateNextSequencePolicyChoice(t *testing.T) {
	a := NextPolicy{ID: "pol-a", PayerName: "A"}
	b := NextPolicy{ID: "pol-b", PayerName: "B", CoverageStart: "2026-02-01"}
	one := []nextLineInput{eligibleLine("x", "2026-03-01", 1000)}

	ev := evaluateNextSequence("submitted", "primary", one, []NextPolicy{a, b}, "")
	if ev.CanCreate || !ev.NeedsPolicyChoice || len(ev.CandidatePolicies) != 2 {
		t.Fatalf("two applicable policies must require a choice: %+v", ev)
	}

	ev = evaluateNextSequence("submitted", "primary", one, []NextPolicy{a, b}, "pol-b")
	if !ev.CanCreate || ev.Policy == nil || ev.Policy.ID != "pol-b" {
		t.Fatalf("explicit choice should be honoured: %+v", ev)
	}

	// Only one policy covers the service date: no choice needed.
	early := []nextLineInput{eligibleLine("x", "2026-01-15", 1000)}
	ev = evaluateNextSequence("submitted", "primary", early, []NextPolicy{a, b}, "")
	if !ev.CanCreate || ev.Policy.ID != "pol-a" {
		t.Fatalf("the covering policy should be chosen: %+v", ev)
	}

	// A line the chosen policy does not cover is excluded, not billed.
	mixed := []nextLineInput{eligibleLine("x", "2026-03-01", 1000), eligibleLine("y", "2026-01-10", 700)}
	ev = evaluateNextSequence("submitted", "primary", mixed, []NextPolicy{b}, "")
	if !ev.CanCreate || ev.EligibleAmount != "10.00" || ev.Lines[1].Eligible || !strings.Contains(ev.Lines[1].Reason, "Coverage inactive") {
		t.Fatalf("uncovered line must be excluded: %+v", ev)
	}
}
