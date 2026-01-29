package quarantine

import (

	"sort"

	"strings"
)

type Code string

const (

	PIIEmail      Code = "PII_EMAIL"

	PIIPhone      Code = "PII_PHONE"

	PIISSN        Code = "PII_SSN"

	PIICreditCard Code = "PII_CREDITCARD"


	SchemaMissingRequired Code = "SCHEMA_MISSING_REQUIRED"

	SchemaTypeMismatch    Code = "SCHEMA_TYPE_MISMATCH"


	OutlierZScore Code = "OUTLIER_ZSCORE"

	OutlierIQR    Code = "OUTLIER_IQR"


	ParseJSON Code = "PARSE_JSON"

	ParseCSV  Code = "PARSE_CSV"


	PolicyTenantDeny Code = "POLICY_TENANT_DENY"

	PolicyRateLimit  Code = "POLICY_RATE_LIMIT"


	Unknown Code = "UNKNOWN"
)

func Classify(reason Reason, details map[string]string) []Code {

	set := make(map[Code]struct{})

	add := func(c Code) {


		if c == "" {



			return


		}


		set[c] = struct{}{}

	}


	r := strings.ToLower(strings.TrimSpace(string(reason)))

	kind := strings.ToLower(strings.TrimSpace(details["kind"]))

	typ := strings.ToLower(strings.TrimSpace(details["type"]))

	method := strings.ToLower(strings.TrimSpace(details["method"]))

	format := strings.ToLower(strings.TrimSpace(details["format"]))

	policy := strings.ToLower(strings.TrimSpace(details["policy"]))


	switch r {

	case "pii_detected":


		switch kind {


		case "email":



			add(PIIEmail)


		case "phone":



			add(PIIPhone)


		case "ssn":



			add(PIISSN)


		case "creditcard", "credit_card", "cc":



			add(PIICreditCard)


		default:



			add(Unknown)


		}

	case "schema_violation":


		switch typ {


		case "missing", "missing_required":



			add(SchemaMissingRequired)


		case "type", "type_mismatch":



			add(SchemaTypeMismatch)


		default:



			add(Unknown)


		}

	case "outlier_detected":


		switch method {


		case "zscore", "zs", "z":



			add(OutlierZScore)


		case "iqr":



			add(OutlierIQR)


		default:



			add(Unknown)


		}

	case "parse_error":


		switch format {


		case "json":



			add(ParseJSON)


		case "csv":



			add(ParseCSV)


		default:



			add(Unknown)


		}

	case "policy_denied":


		switch policy {


		case "tenant_deny", "tenant":



			add(PolicyTenantDeny)


		case "rate_limit", "ratelimit":



			add(PolicyRateLimit)


		default:



			add(Unknown)


		}

	default:


		add(Unknown)

	}


	out := make([]Code, 0, len(set))

	for c := range set {


		out = append(out, c)

	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })

	return out
}
