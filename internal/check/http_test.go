package check

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCatalogIdentityAndUnsupportedCheck(t *testing.T) {
	t.Parallel()
	catalog := NewCatalog()
	if !catalog.IsSupported("http") || catalog.IsSupported("dns") {
		t.Fatal("unexpected catalog support")
	}
	_, failures := catalog.Validate("dns", 1, json.RawMessage(`{}`))
	want := [][2]string{{CheckTypeUnsupportedCode, "checkType"}}
	assertFailures(t, failures, want)
}

func TestMinimalAndCompleteHTTPConfigurations(t *testing.T) {
	t.Parallel()
	minimal, canonical, failures := ValidateHTTP(1, json.RawMessage("  { \"url\" : \"https://example.test/health\" }  "))
	assertFailures(t, failures, nil)
	if string(canonical) != `{"url":"https://example.test/health"}` {
		t.Fatalf("canonical = %s", canonical)
	}
	if minimal.Method != "GET" || minimal.TimeoutSeconds != 30 || !minimal.FollowRedirects || minimal.MaxRedirects != 5 || len(minimal.ExpectedStatusCodes) != 0 || len(minimal.Headers) != 0 {
		t.Fatalf("semantic defaults = %#v", minimal)
	}
	if !minimal.AcceptsStatus(200) || !minimal.AcceptsStatus(299) || minimal.AcceptsStatus(199) || minimal.AcceptsStatus(300) {
		t.Fatal("default status range is wrong")
	}

	raw := json.RawMessage(`{
		"url":"http://192.0.2.10:8080/health?probe=1",
		"method":"POST",
		"expectedStatusCodes":[200,204,503],
		"timeoutSeconds":60,
		"followRedirects":false,
		"maxRedirects":0,
		"headers":[{"name":"Accept","value":"application/json"},{"name":"X-Probe","value":"probehive"}]
	}`)
	complete, _, failures := ValidateHTTP(1, raw)
	assertFailures(t, failures, nil)
	if complete.Method != "POST" || complete.TimeoutSeconds != 60 || complete.FollowRedirects || complete.MaxRedirects != 0 || !complete.AcceptsStatus(503) || complete.AcceptsStatus(201) || len(complete.Headers) != 2 {
		t.Fatalf("complete configuration = %#v", complete)
	}
}

func TestSchemaDocumentAndUnknownFieldFailuresAreExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version int
		raw     json.RawMessage
		want    [][2]string
	}{
		{"schema", 2, json.RawMessage(`{"url":"https://example.test"}`), [][2]string{{SchemaVersionUnsupportedCode, "checkSchemaVersion"}}},
		{"array", 1, json.RawMessage(`[]`), [][2]string{{ConfigurationNotObjectCode, "checkConfiguration"}}},
		{"null", 1, json.RawMessage(`null`), [][2]string{{ConfigurationNotObjectCode, "checkConfiguration"}}},
		{"malformed", 1, json.RawMessage(`{`), [][2]string{{ConfigurationNotObjectCode, "checkConfiguration"}}},
		{"unknown and missing", 1, json.RawMessage(`{"timeout":5}`), [][2]string{
			{UnknownFieldCode, "checkConfiguration.timeout"},
			{URLRequiredCode, "checkConfiguration.url"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, canonical, failures := ValidateHTTP(test.version, test.raw)
			if canonical != nil {
				t.Fatalf("invalid canonical = %s", canonical)
			}
			assertFailures(t, failures, test.want)
		})
	}
}

func TestRawDocumentLimitPrecedesFieldValidation(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(fmt.Sprintf(`{"url":"https://example.test/%s"}`, strings.Repeat("a", MaxDocumentBytes)))
	_, _, failures := ValidateHTTP(1, raw)
	assertFailures(t, failures, [][2]string{{ConfigurationTooLargeCode, "checkConfiguration"}})
}

func TestURLValidationMessages(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, value, code string }{
		{"wrong type", `42`, URLNotStringCode},
		{"relative", `"not a url"`, URLNotAbsoluteCode},
		{"protocol relative", `"//example.test/health"`, URLSchemeCode},
		{"scheme", `"ftp://example.test/file"`, URLSchemeCode},
		{"userinfo", `"https://user:secret@example.test/"`, URLUserInfoCode},
		{"empty userinfo", `"https://@example.test/"`, URLUserInfoCode},
		{"fragment", `"https://example.test/health#fragment"`, URLFragmentCode},
		{"empty fragment", `"https://example.test/health#"`, URLFragmentCode},
		{"missing host", `"https:/health"`, URLNotAbsoluteCode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"url":` + test.value + `}`)
			_, _, failures := ValidateHTTP(1, raw)
			assertFailures(t, failures, [][2]string{{test.code, "checkConfiguration.url"}})
		})
	}
	overlong := json.RawMessage(`{"url":"https://example.test/` + strings.Repeat("a", MaxURLLength) + `"}`)
	_, _, failures := ValidateHTTP(1, overlong)
	assertFailures(t, failures, [][2]string{{URLTooLongCode, "checkConfiguration.url"}})
}

func TestMethodAndScalarValidationMessages(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, code string }{
		{`"method":"get"`, "checkConfiguration.method", MethodUnsupportedCode},
		{`"method":1`, "checkConfiguration.method", MethodNotStringCode},
		{`"timeoutSeconds":0`, "checkConfiguration.timeoutSeconds", "check.http.timeoutSeconds.outOfRange"},
		{`"timeoutSeconds":61`, "checkConfiguration.timeoutSeconds", "check.http.timeoutSeconds.outOfRange"},
		{`"timeoutSeconds":30.0`, "checkConfiguration.timeoutSeconds", "check.http.timeoutSeconds.notInteger"},
		{`"timeoutSeconds":2147483648`, "checkConfiguration.timeoutSeconds", "check.http.timeoutSeconds.notInteger"},
		{`"followRedirects":"true"`, "checkConfiguration.followRedirects", FollowRedirectsNotBoolCode},
		{`"maxRedirects":-1`, "checkConfiguration.maxRedirects", "check.http.maxRedirects.outOfRange"},
		{`"maxRedirects":11`, "checkConfiguration.maxRedirects", "check.http.maxRedirects.outOfRange"},
	}
	for _, test := range tests {
		t.Run(test.fragment, func(t *testing.T) {
			_, _, failures := ValidateHTTP(1, withURL(test.fragment))
			assertFailures(t, failures, [][2]string{{test.code, test.field}})
		})
	}
}

func TestExpectedStatusCodeValidation(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, code string }{
		{`"expectedStatusCodes":200`, "checkConfiguration.expectedStatusCodes", StatusCodesNotArrayCode},
		{`"expectedStatusCodes":[200,"204"]`, "checkConfiguration.expectedStatusCodes[1]", StatusCodeNotIntegerCode},
		{`"expectedStatusCodes":[99]`, "checkConfiguration.expectedStatusCodes[0]", StatusCodeOutOfRangeCode},
		{`"expectedStatusCodes":[600]`, "checkConfiguration.expectedStatusCodes[0]", StatusCodeOutOfRangeCode},
		{`"expectedStatusCodes":[200,200]`, "checkConfiguration.expectedStatusCodes[1]", StatusCodeDuplicateCode},
	}
	for _, test := range tests {
		_, _, failures := ValidateHTTP(1, withURL(test.fragment))
		assertFailures(t, failures, [][2]string{{test.code, test.field}})
	}
	codes := make([]string, MaxExpectedStatusCodes+1)
	for index := range codes {
		codes[index] = strconvItoa(200 + index)
	}
	_, _, failures := ValidateHTTP(1, withURL(`"expectedStatusCodes":[`+strings.Join(codes, ",")+`]`))
	assertFailures(t, failures, [][2]string{{StatusCodesTooManyCode, "checkConfiguration.expectedStatusCodes"}})
}

func TestHeaderShapeNameValueForbiddenAndDuplicateValidation(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, code string }{
		{`"headers":{}`, "checkConfiguration.headers", HeadersNotArrayCode},
		{`"headers":["Accept: json"]`, "checkConfiguration.headers[0]", HeaderEntryNotObjectCode},
		{`"headers":[{"name":"Accept"}]`, "checkConfiguration.headers[0]", HeaderEntryNotObjectCode},
		{`"headers":[{"name":1,"value":"x"}]`, "checkConfiguration.headers[0].name", HeaderNameNotStringCode},
		{`"headers":[{"name":"Accept","value":"x","extra":1}]`, "checkConfiguration.headers[0].extra", HeaderEntryUnknownFieldCode},
		{`"headers":[{"name":"Bad Header","value":"x"}]`, "checkConfiguration.headers[0].name", HeaderNameInvalidCode},
		{`"headers":[{"name":"Authorization","value":"x"}]`, "checkConfiguration.headers[0].name", HeaderNameForbiddenCode},
		{`"headers":[{"name":"Accept","value":"x"},{"name":"accept","value":"y"}]`, "checkConfiguration.headers[1].name", HeaderNameDuplicateCode},
		{`"headers":[{"name":"Accept","value":"a\tb"}]`, "checkConfiguration.headers[0].value", HeaderValueControlCharacterCode},
	}
	for _, test := range tests {
		_, _, failures := ValidateHTTP(1, withURL(test.fragment))
		assertFailures(t, failures, [][2]string{{test.code, test.field}})
	}
	longValue := strings.Repeat("v", MaxHeaderValueLength+1)
	_, _, failures := ValidateHTTP(1, withURL(`"headers":[{"name":"X-Probe","value":"`+longValue+`"}]`))
	assertFailures(t, failures, [][2]string{{HeaderValueTooLongCode, "checkConfiguration.headers[0].value"}})
}

func TestEveryForbiddenHeaderIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Authorization", "proxy-authorization", "COOKIE", "Host", "Content-Length", "Transfer-Encoding"} {
		_, _, failures := ValidateHTTP(1, withURL(`"headers":[{"name":"`+name+`","value":"x"}]`))
		assertFailures(t, failures, [][2]string{{HeaderNameForbiddenCode, "checkConfiguration.headers[0].name"}})
	}
}

func TestMultipleFailuresPreserveEncounterOrderAndDuplicateProperties(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"method":"FETCH","url":1,"timeoutSeconds":0,"url":"https://example.test","unknown":true}`)
	_, _, failures := ValidateHTTP(1, raw)
	want := [][2]string{
		{MethodUnsupportedCode, "checkConfiguration.method"},
		{URLNotStringCode, "checkConfiguration.url"},
		{"check.http.timeoutSeconds.outOfRange", "checkConfiguration.timeoutSeconds"},
		{UnknownFieldCode, "checkConfiguration.unknown"},
	}
	assertFailures(t, failures, want)
}

func TestCanonicalJSONDoesNotMaterializeDefaults(t *testing.T) {
	t.Parallel()
	_, canonical, failures := ValidateHTTP(1, json.RawMessage("{\n  \"url\": \"https://example.test\"\n}"))
	assertFailures(t, failures, nil)
	if string(canonical) != `{"url":"https://example.test"}` {
		t.Fatalf("canonical = %s", canonical)
	}
}

func withURL(fragment string) json.RawMessage {
	return json.RawMessage(`{"url":"https://example.test/health",` + fragment + `}`)
}

// assertFailures compares the stable code and field path of each failure in order.
// English prose is documentation and is deliberately not asserted, so
// copy edits do not break tests.
func assertFailures(t *testing.T, got [][3]string, want [][2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index][0] != want[index][0] || got[index][1] != want[index][1] {
			t.Fatalf(
				"failure %d = code %q field %q, want code %q field %q",
				index, got[index][0], got[index][1], want[index][0], want[index][1],
			)
		}
	}
}

func strconvItoa(value int) string { return fmt.Sprintf("%d", value) }
