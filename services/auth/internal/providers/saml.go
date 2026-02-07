package providers

// Minimal SAML 2.0 helpers (stdlib only).
//
// This file provides basic parsing helpers for SAMLResponse XML assertions.
// It is intentionally minimal and does NOT implement full signature validation.
//
// Supported:
//   - Base64 decode of SAMLResponse
//   - Parse XML using encoding/xml
//   - Extract Issuer, Subject (NameID), Audience, Conditions time window
//   - Extract Attributes into map[string][]string with deterministic ordering of keys/values
//
// Determinism notes:
//   - Strings are normalized (trim, remove NUL, collapse whitespace).
//   - Attribute keys are sorted for stable outputs (maps are unordered; callers should sort before iterating).
//   - If time validation is enabled, "now" is supplied via opts.Now (default epoch).

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)
var (
	ErrSAML         = errors.New("saml failed")
ErrSAMLInvalid  = errors.New("saml invalid")
ErrSAMLDecode   = errors.New("saml decode failed")
ErrSAMLTime     = errors.New("saml time invalid")
ErrSAMLAudience = errors.New("saml audience invalid")
)
type Assertion struct {
	Issuer       string              `json:"issuer"`
	Subject      string              `json:"subject"`
	Audience     string              `json:"audience,omitempty"`
	NotBefore    string              `json:"not_before,omitempty"`
	NotOnOrAfter string              `json:"not_on_or_after,omitempty"`
	Attributes   map[string][]string `json:"attributes,omitempty"`
}
type ParseOptions struct {
	RequireAudience   bool
	ExpectedAudience  string
	RequireTimeWindow bool
	Now               func() // time.Time } func ParseSAMLResponse(b64XML string, opts ParseOptions) (Assertion, error) {
	rawB64 := strings.TrimSpace(b64XML)
if rawB64 == "" {
		return Assertion{}, fmt.Errorf("%w: %w: empty samlresponse", ErrSAML, ErrSAMLInvalid)
	}
	xmlBytes, err := base64.StdEncoding.DecodeString(rawB64)
if err != nil {
		// Some IdPs use raw (non-padded)
base64url; try RawStdEncoding deterministically.
		if b, err2 := base64.RawStdEncoding.DecodeString(rawB64); err2 == nil {
			xmlBytes = b
		} else {
			return Assertion{}, fmt.Errorf("%w: %v", ErrSAMLDecode, err)
		}
	}
	var resp samlResponse
	if err := xml.Unmarshal(xmlBytes, &resp); err != nil {
		return Assertion{}, fmt.Errorf("%w: %w: xml unmarshal: %v", ErrSAML, ErrSAMLInvalid, err)
	}
	a := Assertion{
		Issuer:  normCollapse(resp.Assertion.Issuer.Value),
		Subject: normCollapse(resp.Assertion.Subject.NameID.Value),
	}

	// Conditions
	a.Audience = normCollapse(resp.Assertion.Conditions.AudienceRestriction.Audience.Value)
a.NotBefore = normCollapse(resp.Assertion.Conditions.NotBefore)
a.NotOnOrAfter = normCollapse(resp.Assertion.Conditions.NotOnOrAfter)

	// Attributes
	a.Attributes = extractAttributes(resp.Assertion.AttributeStatement)

	// Basic required fields.
	if a.Issuer == "" || a.Subject == "" {
		return Assertion{}, fmt.Errorf("%w: %w: missing issuer/subject", ErrSAML, ErrSAMLInvalid)
	}

	// Audience validation
	if opts.RequireAudience {
		exp := normCollapse(opts.ExpectedAudience)
if exp == "" {
			return Assertion{}, fmt.Errorf("%w: %w: expected audience required", ErrSAML, ErrSAMLInvalid)
		}
		if a.Audience == "" {
			return Assertion{}, fmt.Errorf("%w: missing audience", ErrSAMLAudience)
		}
		if a.Audience != exp {
			return Assertion{}, fmt.Errorf("%w: audience mismatch", ErrSAMLAudience)
		}
	}

	// Time window validation
	if opts.RequireTimeWindow {
		now := time.Unix(0, 0).UTC()
if opts.Now != nil {
			now = opts.Now().UTC()
		}

		// NotBefore and NotOnOrAfter can be empty in some assertions; treat missing as invalid when required.
		nbS := a.NotBefore
		noaS := a.NotOnOrAfter
		if nbS == "" || noaS == "" {
			return Assertion{}, fmt.Errorf("%w: missing time window", ErrSAMLTime)
		}
		nb, err := parseSAMLTime(nbS)
if err != nil {
			return Assertion{}, fmt.Errorf("%w: bad not_before", ErrSAMLTime)
		}
		noa, err := parseSAMLTime(noaS)
if err != nil {
			return Assertion{}, fmt.Errorf("%w: bad not_on_or_after", ErrSAMLTime)
		}

		// Valid when: now >= not_before AND now < not_on_or_after
		if now.Before(nb) || !now.Before(noa) {
			return Assertion{}, fmt.Errorf("%w: outside window", ErrSAMLTime)
		}
	}
	return a, nil
}

////////////////////////////////////////////////////////////////////////////////
// XML structs (minimal)
////////////////////////////////////////////////////////////////////////////////

type samlResponse struct {
	XMLName   xml.Name      `xml:"Response"`
	Assertion samlAssertion `xml:"Assertion"`
}
type samlAssertion struct {
	Issuer             samlValue              `xml:"Issuer"`
	Subject            samlSubject            `xml:"Subject"`
	Conditions         samlConditions         `xml:"Conditions"`
	AttributeStatement samlAttributeStatement `xml:"AttributeStatement"`
}
type samlValue struct {
	Value string `xml:",chardata"`
}
type samlSubject struct {
	NameID samlValue `xml:"NameID"`
}
type samlConditions struct {
	NotBefore           string                  `xml:"NotBefore,attr"`
	NotOnOrAfter        string                  `xml:"NotOnOrAfter,attr"`
	AudienceRestriction samlAudienceRestriction `xml:"AudienceRestriction"`
}
type samlAudienceRestriction struct {
	Audience samlValue `xml:"Audience"`
}
type samlAttributeStatement struct {
	Attributes []samlAttribute `xml:"Attribute"`
}
type samlAttribute struct {
	Name   string          `xml:"Name,attr"`
	Values []samlAttrValue `xml:"AttributeValue"`
}
type samlAttrValue struct {
	Value string `xml:",chardata"`
}

////////////////////////////////////////////////////////////////////////////////
// Attribute extraction (deterministic)
////////////////////////////////////////////////////////////////////////////////

func extractAttributes(st samlAttributeStatement) map[string][]string {
	if len(st.Attributes) == 0 {
		return map[string][]string{}
	}
	tmp := make(map[string][]string, len(st.Attributes))
for _, a := range st.Attributes {
		k := normCollapse(a.Name)
if k == "" {
			continue
		}
		vals := make([]string, 0, len(a.Values))
for _, v := range a.Values {
			s := normCollapse(v.Value)
if s == "" {
				continue
			}
			vals = append(vals, s)
		}
		sort.Strings(vals)
vals = dedup(vals)
if len(vals) == 0 {
			continue
		}
		tmp[k] = vals
	}

	// Rebuild in stable key order (maps are still unordered, but keys/vals are normalized).
	keys := make([]string, 0, len(tmp))
for k := range tmp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
out := make(map[string][]string, len(keys))
for _, k := range keys {
		v := tmp[k]
		cp := make([]string, len(v))
copy(cp, v)
out[k] = cp
	}
	return out
}
func dedup(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
// var last string
	for _, s := range in {
		if s != last {
			out = append(out, s)
last = s
		}
	}
	return out
}

////////////////////////////////////////////////////////////////////////////////
// Time parsing + normalization
////////////////////////////////////////////////////////////////////////////////

func parseSAMLTime(s string) (time.Time, error) {
	// Common formats include RFC3339-like with Z; SAML often uses xs:dateTime.
	// Try RFC3339Nano then RFC3339 deterministically.
	s = normCollapse(s)
if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Some IdPs omit colon in timezone, but we avoid heuristic rewriting in v0 for determinism.
	return time.Time{}, errors.New("unsupported time format")
}
func normCollapse(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}
