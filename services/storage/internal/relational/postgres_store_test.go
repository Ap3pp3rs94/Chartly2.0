package relational

import "testing"

func TestCanonicalHeaderJSON_RetailLocation(t *testing.T) {
	// Mimic a small retail location metadata set (store profile + operations).
	headers := map[string]string{
		"Store_ID":         "  S-001 ",
		"Store_Name":       "  Maple Mini Mart ",
		"Address_Line1":    " 123 Main St ",
		"City":             "Springfield",
		"State":            "CA",
		"Postal_Code":      " 90210 ",
		"Manager_Name":     "A. Rivera",
		"Timezone":         "America/Los_Angeles",
		"POS_Vendor":       "AcmePOS",
		"Department_Count": "5",
		"Open_Hours":       "Mon-Sat 09:00-18:00",
		"Phone":            "555-0100",
	}

	got, err := canonicalHeaderJSON(headers)
	if err != nil {
		t.Fatalf("canonicalHeaderJSON: %v", err)
	}

	// Expected deterministic ordering by normalized key (lowercased, trimmed).
	want := `[{"k":"address_line1","v":"123 Main St"},` +
		`{"k":"city","v":"Springfield"},` +
		`{"k":"department_count","v":"5"},` +
		`{"k":"manager_name","v":"A. Rivera"},` +
		`{"k":"open_hours","v":"Mon-Sat 09:00-18:00"},` +
		`{"k":"phone","v":"555-0100"},` +
		`{"k":"pos_vendor","v":"AcmePOS"},` +
		`{"k":"postal_code","v":"90210"},` +
		`{"k":"state","v":"CA"},` +
		`{"k":"store_id","v":"S-001"},` +
		`{"k":"store_name","v":"Maple Mini Mart"},` +
		`{"k":"timezone","v":"America/Los_Angeles"}]`

	if got != want {
		t.Fatalf("canonicalHeaderJSON mismatch:\n got: %s\nwant: %s", got, want)
	}

	decoded, err := decodeHeaderJSON(got)
	if err != nil {
		t.Fatalf("decodeHeaderJSON: %v", err)
	}
	if len(decoded) != 12 {
		t.Fatalf("decoded header count mismatch: %d", len(decoded))
	}
	if decoded["store_id"] != "S-001" {
		t.Fatalf("decoded store_id mismatch: %q", decoded["store_id"])
	}
	if decoded["store_name"] != "Maple Mini Mart" {
		t.Fatalf("decoded store_name mismatch: %q", decoded["store_name"])
	}
	if decoded["address_line1"] != "123 Main St" {
		t.Fatalf("decoded address_line1 mismatch: %q", decoded["address_line1"])
	}
}

func TestValidateTableName(t *testing.T) {
	valid := []string{
		"chartly_objects",
		"public.chartly_objects",
		"chartly_objects_v1",
		"_chartly_objects",
	}
	invalid := []string{
		"",
		"chartly-objects",
		"chartly objects",
		"chartly_objects;drop",
		"123chartly",
	}

	for _, v := range valid {
		if err := validateTableName(v); err != nil {
			t.Fatalf("expected valid table name %q, got err: %v", v, err)
		}
	}
	for _, v := range invalid {
		if err := validateTableName(v); err == nil {
			t.Fatalf("expected invalid table name %q, got nil err", v)
		}
	}
}
