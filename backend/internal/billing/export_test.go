package billing

import (
	"bytes"
	"encoding/csv"
	"testing"
)

func TestCSVTextNeutralisesFormulas(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"Smith, Jane":          "Smith, Jane",
		`He said "hi"`:         `He said "hi"`,
		"=SUM(A1:A9)":          "'=SUM(A1:A9)",
		"+1 555 0100":          "'+1 555 0100",
		"-cmd":                 "'-cmd",
		"@import":              "'@import",
		"\tTabbed":             "'\tTabbed",
		"Payer = Aetna":        "Payer = Aetna",
		`=HYPERLINK("x","y")`:  `'=HYPERLINK("x","y")`,
		"O'Brien":              "O'Brien",
		"normal text, with ,,": "normal text, with ,,",
	}

	for in, want := range cases {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}

// Round-trips awkward values through encoding/csv exactly as the export
// does, so quoting of commas, quotes and newlines is covered.
func TestCSVRoundTripKeepsAwkwardValues(t *testing.T) {
	values := []string{`O'Brien, "Jr"` + "\nSecond", "plain", "a,b,c", `""`}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	row := make([]string, len(values))
	for i, v := range values {
		row[i] = csvText(v)
	}
	_ = w.Write(row)
	w.Flush()

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("read back: %v %v", records, err)
	}

	for i, v := range values {
		if records[0][i] != v {
			t.Errorf("value %d changed: %q -> %q", i, v, records[0][i])
		}
	}
}
