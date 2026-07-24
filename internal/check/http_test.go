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
	want := [][2]string{{"checkType", "The check type 'dns' is not supported by this build."}}
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
		{"schema", 2, json.RawMessage(`{"url":"https://example.test"}`), [][2]string{{"checkSchemaVersion", "Check type 'http' supports configuration schema version 1 only."}}},
		{"array", 1, json.RawMessage(`[]`), [][2]string{{"checkConfiguration", "The configuration must be a JSON object."}}},
		{"null", 1, json.RawMessage(`null`), [][2]string{{"checkConfiguration", "The configuration must be a JSON object."}}},
		{"malformed", 1, json.RawMessage(`{`), [][2]string{{"checkConfiguration", "The configuration must be a JSON object."}}},
		{"unknown and missing", 1, json.RawMessage(`{"timeout":5}`), [][2]string{
			{"checkConfiguration.timeout", "The field is not part of 'http' configuration schema version 1."},
			{"checkConfiguration.url", "The field is required."},
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
	assertFailures(t, failures, [][2]string{{"checkConfiguration", "The configuration document must not exceed 16384 bytes."}})
}

func TestURLValidationMessages(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, value, message string }{
		{"wrong type", `42`, "The value must be a string."},
		{"relative", `"not a url"`, "The value must be an absolute URL."},
		{"protocol relative", `"//example.test/health"`, "The URL scheme must be 'http' or 'https'."},
		{"scheme", `"ftp://example.test/file"`, "The URL scheme must be 'http' or 'https'."},
		{"userinfo", `"https://user:secret@example.test/"`, "The URL must not carry user information."},
		{"empty userinfo", `"https://@example.test/"`, "The URL must not carry user information."},
		{"fragment", `"https://example.test/health#fragment"`, "The URL must not carry a fragment."},
		{"empty fragment", `"https://example.test/health#"`, "The URL must not carry a fragment."},
		{"missing host", `"https:/health"`, "The value must be an absolute URL."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"url":` + test.value + `}`)
			_, _, failures := ValidateHTTP(1, raw)
			assertFailures(t, failures, [][2]string{{"checkConfiguration.url", test.message}})
		})
	}
	overlong := json.RawMessage(`{"url":"https://example.test/` + strings.Repeat("a", MaxURLLength) + `"}`)
	_, _, failures := ValidateHTTP(1, overlong)
	assertFailures(t, failures, [][2]string{{"checkConfiguration.url", "The URL must not exceed 2048 characters."}})
}

func TestMethodAndScalarValidationMessages(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, message string }{
		{`"method":"get"`, "checkConfiguration.method", "The method must be one of: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS."},
		{`"method":1`, "checkConfiguration.method", "The value must be a string."},
		{`"timeoutSeconds":0`, "checkConfiguration.timeoutSeconds", "The value must be between 1 and 60."},
		{`"timeoutSeconds":61`, "checkConfiguration.timeoutSeconds", "The value must be between 1 and 60."},
		{`"timeoutSeconds":30.0`, "checkConfiguration.timeoutSeconds", "The value must be an integer."},
		{`"timeoutSeconds":2147483648`, "checkConfiguration.timeoutSeconds", "The value must be an integer."},
		{`"followRedirects":"true"`, "checkConfiguration.followRedirects", "The value must be a boolean."},
		{`"maxRedirects":-1`, "checkConfiguration.maxRedirects", "The value must be between 0 and 10."},
		{`"maxRedirects":11`, "checkConfiguration.maxRedirects", "The value must be between 0 and 10."},
	}
	for _, test := range tests {
		t.Run(test.fragment, func(t *testing.T) {
			_, _, failures := ValidateHTTP(1, withURL(test.fragment))
			assertFailures(t, failures, [][2]string{{test.field, test.message}})
		})
	}
}

func TestExpectedStatusCodeValidation(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, message string }{
		{`"expectedStatusCodes":200`, "checkConfiguration.expectedStatusCodes", "The value must be an array of integers."},
		{`"expectedStatusCodes":[200,"204"]`, "checkConfiguration.expectedStatusCodes[1]", "The value must be an integer."},
		{`"expectedStatusCodes":[99]`, "checkConfiguration.expectedStatusCodes[0]", "Status codes must be between 100 and 599."},
		{`"expectedStatusCodes":[600]`, "checkConfiguration.expectedStatusCodes[0]", "Status codes must be between 100 and 599."},
		{`"expectedStatusCodes":[200,200]`, "checkConfiguration.expectedStatusCodes[1]", "Status code 200 is listed more than once."},
	}
	for _, test := range tests {
		_, _, failures := ValidateHTTP(1, withURL(test.fragment))
		assertFailures(t, failures, [][2]string{{test.field, test.message}})
	}
	codes := make([]string, MaxExpectedStatusCodes+1)
	for index := range codes {
		codes[index] = strconvItoa(200 + index)
	}
	_, _, failures := ValidateHTTP(1, withURL(`"expectedStatusCodes":[`+strings.Join(codes, ",")+`]`))
	assertFailures(t, failures, [][2]string{{"checkConfiguration.expectedStatusCodes", "At most 20 status codes are allowed."}})
}

func TestHeaderShapeNameValueForbiddenAndDuplicateValidation(t *testing.T) {
	t.Parallel()
	tests := []struct{ fragment, field, message string }{
		{`"headers":{}`, "checkConfiguration.headers", "The value must be an array of name/value objects."},
		{`"headers":["Accept: json"]`, "checkConfiguration.headers[0]", "Each header must be an object with 'name' and 'value'."},
		{`"headers":[{"name":"Accept"}]`, "checkConfiguration.headers[0]", "Each header must be an object with 'name' and 'value'."},
		{`"headers":[{"name":1,"value":"x"}]`, "checkConfiguration.headers[0].name", "The value must be a string."},
		{`"headers":[{"name":"Accept","value":"x","extra":1}]`, "checkConfiguration.headers[0].extra", "The field is not part of a header entry."},
		{`"headers":[{"name":"Bad Header","value":"x"}]`, "checkConfiguration.headers[0].name", "Header names must be HTTP tokens of at most 128 characters."},
		{`"headers":[{"name":"Authorization","value":"x"}]`, "checkConfiguration.headers[0].name", "The header 'Authorization' cannot be set by check configuration."},
		{`"headers":[{"name":"Accept","value":"x"},{"name":"accept","value":"y"}]`, "checkConfiguration.headers[1].name", "The header 'accept' is listed more than once."},
		{`"headers":[{"name":"Accept","value":"a\tb"}]`, "checkConfiguration.headers[0].value", "Header values must not contain control characters."},
	}
	for _, test := range tests {
		_, _, failures := ValidateHTTP(1, withURL(test.fragment))
		assertFailures(t, failures, [][2]string{{test.field, test.message}})
	}
	longValue := strings.Repeat("v", MaxHeaderValueLength+1)
	_, _, failures := ValidateHTTP(1, withURL(`"headers":[{"name":"X-Probe","value":"`+longValue+`"}]`))
	assertFailures(t, failures, [][2]string{{"checkConfiguration.headers[0].value", "Header values must not exceed 1024 characters."}})
}

func TestEveryForbiddenHeaderIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Authorization", "proxy-authorization", "COOKIE", "Host", "Content-Length", "Transfer-Encoding"} {
		_, _, failures := ValidateHTTP(1, withURL(`"headers":[{"name":"`+name+`","value":"x"}]`))
		want := fmt.Sprintf("The header '%s' cannot be set by check configuration.", name)
		assertFailures(t, failures, [][2]string{{"checkConfiguration.headers[0].name", want}})
	}
}

func TestMultipleFailuresPreserveEncounterOrderAndDuplicateProperties(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"method":"FETCH","url":1,"timeoutSeconds":0,"url":"https://example.test","unknown":true}`)
	_, _, failures := ValidateHTTP(1, raw)
	want := [][2]string{
		{"checkConfiguration.method", "The method must be one of: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS."},
		{"checkConfiguration.url", "The value must be a string."},
		{"checkConfiguration.timeoutSeconds", "The value must be between 1 and 60."},
		{"checkConfiguration.unknown", "The field is not part of 'http' configuration schema version 1."},
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

func assertFailures(t *testing.T, got, want [][2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("failures = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("failure %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func strconvItoa(value int) string { return fmt.Sprintf("%d", value) }
