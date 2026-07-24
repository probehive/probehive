package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

const (
	HTTPCheckType            = "http"
	HTTPCurrentSchemaVersion = 1
	MaxDocumentBytes         = 16 * 1024
	MaxURLLength             = 2048
	MinTimeoutSeconds        = 1
	MaxTimeoutSeconds        = 60
	DefaultTimeoutSeconds    = 30
	MaxRedirects             = 10
	DefaultMaxRedirects      = 5
	MaxExpectedStatusCodes   = 20
	MaxHeaders               = 20
	MaxHeaderNameLength      = 128
	MaxHeaderValueLength     = 1024
	DefaultMethod            = "GET"
)

var allowedMethods = map[string]struct{}{
	"GET": {}, "HEAD": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "OPTIONS": {},
}

var forbiddenHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"host":                {},
	"content-length":      {},
	"transfer-encoding":   {},
}

// Header is one validated HTTP request header.
type Header struct {
	Name  string
	Value string
}

// HTTPConfiguration contains effective schema-v1 values. Defaults appear here but
// remain omitted from Canonical JSON when the caller omitted them.
type HTTPConfiguration struct {
	URL                 string
	Method              string
	ExpectedStatusCodes []int
	TimeoutSeconds      int
	FollowRedirects     bool
	MaxRedirects        int
	Headers             []Header
}

// AcceptsStatus applies the semantic default of any 200-299 response when no list was supplied.
func (configuration HTTPConfiguration) AcceptsStatus(statusCode int) bool {
	if len(configuration.ExpectedStatusCodes) == 0 {
		return statusCode >= 200 && statusCode <= 299
	}
	for _, expected := range configuration.ExpectedStatusCodes {
		if statusCode == expected {
			return true
		}
	}
	return false
}

// ValidateHTTP validates schema version 1 and returns semantic values plus compact JSON.
func ValidateHTTP(schemaVersion int, raw json.RawMessage) (HTTPConfiguration, json.RawMessage, [][2]string) {
	if schemaVersion != HTTPCurrentSchemaVersion {
		return HTTPConfiguration{}, nil, failures(failure(
			"checkSchemaVersion",
			"Check type 'http' supports configuration schema version 1 only.",
		))
	}

	document, err := decodeDocument(raw)
	if err != nil || document.kind != objectKind {
		return HTTPConfiguration{}, nil, failures(failure("checkConfiguration", "The configuration must be a JSON object."))
	}
	if len(raw) > MaxDocumentBytes {
		return HTTPConfiguration{}, nil, failures(failure(
			"checkConfiguration",
			"The configuration document must not exceed 16384 bytes.",
		))
	}

	configuration := HTTPConfiguration{
		Method: DefaultMethod, TimeoutSeconds: DefaultTimeoutSeconds,
		FollowRedirects: true, MaxRedirects: DefaultMaxRedirects,
	}
	var result [][2]string
	hasURL := false
	for _, property := range document.object {
		switch property.name {
		case "url":
			hasURL = true
			validateURL(property.value, &configuration, &result)
		case "method":
			validateMethod(property.value, &configuration, &result)
		case "expectedStatusCodes":
			validateExpectedStatusCodes(property.value, &configuration, &result)
		case "timeoutSeconds":
			if value, ok := validateBoundedInteger(property.value, "checkConfiguration.timeoutSeconds", MinTimeoutSeconds, MaxTimeoutSeconds, &result); ok {
				configuration.TimeoutSeconds = value
			}
		case "followRedirects":
			if property.value.kind != boolKind {
				result = append(result, failure("checkConfiguration.followRedirects", "The value must be a boolean."))
			} else {
				configuration.FollowRedirects = property.value.boolean
			}
		case "maxRedirects":
			if value, ok := validateBoundedInteger(property.value, "checkConfiguration.maxRedirects", 0, MaxRedirects, &result); ok {
				configuration.MaxRedirects = value
			}
		case "headers":
			validateHeaders(property.value, &configuration, &result)
		default:
			result = append(result, failure(
				"checkConfiguration."+property.name,
				"The field is not part of 'http' configuration schema version 1.",
			))
		}
	}
	if !hasURL {
		result = append(result, failure("checkConfiguration.url", "The field is required."))
	}
	if len(result) != 0 {
		return HTTPConfiguration{}, nil, result
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return HTTPConfiguration{}, nil, failures(failure("checkConfiguration", "The configuration must be a JSON object."))
	}
	canonical := append(json.RawMessage(nil), compact.Bytes()...)
	return configuration, canonical, nil
}

func validateURL(value jsonValue, configuration *HTTPConfiguration, result *[][2]string) {
	const field = "checkConfiguration.url"
	if value.kind != stringKind {
		*result = append(*result, failure(field, "The value must be a string."))
		return
	}
	if utf16Length(value.text) > MaxURLLength {
		*result = append(*result, failure(field, "The URL must not exceed 2048 characters."))
		return
	}
	parsed, err := url.Parse(value.text)
	if err != nil || (!parsed.IsAbs() && !strings.HasPrefix(value.text, "//")) {
		*result = append(*result, failure(field, "The value must be an absolute URL."))
		return
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		*result = append(*result, failure(field, "The URL scheme must be 'http' or 'https'."))
	}
	if (scheme == "http" || scheme == "https") && parsed.Host == "" {
		*result = append(*result, failure(field, "The value must be an absolute URL."))
		return
	}
	if parsed.User != nil {
		*result = append(*result, failure(field, "The URL must not carry user information."))
	}
	if strings.Contains(value.text, "#") {
		*result = append(*result, failure(field, "The URL must not carry a fragment."))
	}
	configuration.URL = value.text
}

func validateMethod(value jsonValue, configuration *HTTPConfiguration, result *[][2]string) {
	const field = "checkConfiguration.method"
	if value.kind != stringKind {
		*result = append(*result, failure(field, "The value must be a string."))
		return
	}
	if _, ok := allowedMethods[value.text]; !ok {
		*result = append(*result, failure(field, "The method must be one of: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS."))
		return
	}
	configuration.Method = value.text
}

func validateExpectedStatusCodes(value jsonValue, configuration *HTTPConfiguration, result *[][2]string) {
	const field = "checkConfiguration.expectedStatusCodes"
	if value.kind != arrayKind {
		*result = append(*result, failure(field, "The value must be an array of integers."))
		return
	}
	if len(value.array) > MaxExpectedStatusCodes {
		*result = append(*result, failure(field, "At most 20 status codes are allowed."))
		return
	}
	seen := make(map[int]struct{}, len(value.array))
	parsed := make([]int, 0, len(value.array))
	for index, entry := range value.array {
		entryField := fmt.Sprintf("%s[%d]", field, index)
		statusCode, ok := integer32(entry)
		if !ok {
			*result = append(*result, failure(entryField, "The value must be an integer."))
			continue
		}
		if statusCode < 100 || statusCode > 599 {
			*result = append(*result, failure(entryField, "Status codes must be between 100 and 599."))
			continue
		}
		if _, duplicate := seen[statusCode]; duplicate {
			*result = append(*result, failure(entryField, fmt.Sprintf("Status code %d is listed more than once.", statusCode)))
			continue
		}
		seen[statusCode] = struct{}{}
		parsed = append(parsed, statusCode)
	}
	configuration.ExpectedStatusCodes = parsed
}

func validateBoundedInteger(value jsonValue, field string, minimum, maximum int, result *[][2]string) (int, bool) {
	number, ok := integer32(value)
	if !ok {
		*result = append(*result, failure(field, "The value must be an integer."))
		return 0, false
	}
	if number < minimum || number > maximum {
		*result = append(*result, failure(field, fmt.Sprintf("The value must be between %d and %d.", minimum, maximum)))
		return 0, false
	}
	return number, true
}

func validateHeaders(value jsonValue, configuration *HTTPConfiguration, result *[][2]string) {
	const field = "checkConfiguration.headers"
	if value.kind != arrayKind {
		*result = append(*result, failure(field, "The value must be an array of name/value objects."))
		return
	}
	if len(value.array) > MaxHeaders {
		*result = append(*result, failure(field, "At most 20 headers are allowed."))
		return
	}
	seen := make(map[string]struct{}, len(value.array))
	parsed := make([]Header, 0, len(value.array))
	for index, entry := range value.array {
		entryField := fmt.Sprintf("%s[%d]", field, index)
		header, ok := validateHeader(entry, entryField, seen, result)
		if ok {
			parsed = append(parsed, header)
		}
	}
	configuration.Headers = parsed
}

func validateHeader(entry jsonValue, entryField string, seen map[string]struct{}, result *[][2]string) (Header, bool) {
	if entry.kind != objectKind {
		*result = append(*result, failure(entryField, "Each header must be an object with 'name' and 'value'."))
		return Header{}, false
	}
	var name, headerValue string
	hasName, hasValue := false, false
	for _, property := range entry.object {
		switch property.name {
		case "name":
			if property.value.kind != stringKind {
				*result = append(*result, failure(entryField+".name", "The value must be a string."))
				return Header{}, false
			}
			name, hasName = property.value.text, true
		case "value":
			if property.value.kind != stringKind {
				*result = append(*result, failure(entryField+".value", "The value must be a string."))
				return Header{}, false
			}
			headerValue, hasValue = property.value.text, true
			validateHeaderValue(headerValue, entryField+".value", result)
		default:
			*result = append(*result, failure(entryField+"."+property.name, "The field is not part of a header entry."))
			return Header{}, false
		}
	}
	if !hasName || !hasValue {
		*result = append(*result, failure(entryField, "Each header must be an object with 'name' and 'value'."))
		return Header{}, false
	}
	if !validateHeaderName(name, entryField+".name", seen, result) {
		return Header{}, false
	}
	return Header{Name: name, Value: headerValue}, true
}

func validateHeaderName(name, field string, seen map[string]struct{}, result *[][2]string) bool {
	if len(name) == 0 || len(name) > MaxHeaderNameLength {
		*result = append(*result, failure(field, "Header names must be HTTP tokens of at most 128 characters."))
		return false
	}
	for index := 0; index < len(name); index++ {
		if !isTokenCharacter(name[index]) {
			*result = append(*result, failure(field, "Header names must be HTTP tokens of at most 128 characters."))
			return false
		}
	}
	normalized := strings.ToLower(name)
	if _, forbidden := forbiddenHeaderNames[normalized]; forbidden {
		*result = append(*result, failure(field, fmt.Sprintf("The header '%s' cannot be set by check configuration.", name)))
		return false
	}
	if _, duplicate := seen[normalized]; duplicate {
		*result = append(*result, failure(field, fmt.Sprintf("The header '%s' is listed more than once.", name)))
		return false
	}
	seen[normalized] = struct{}{}
	return true
}

func validateHeaderValue(value, field string, result *[][2]string) {
	if utf16Length(value) > MaxHeaderValueLength {
		*result = append(*result, failure(field, "Header values must not exceed 1024 characters."))
		return
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			*result = append(*result, failure(field, "Header values must not contain control characters."))
			return
		}
	}
}

func isTokenCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))
}

func integer32(value jsonValue) (int, bool) {
	if value.kind != numberKind {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value.text, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

func failure(field, message string) [2]string  { return [2]string{field, message} }
func failures(values ...[2]string) [][2]string { return values }

type jsonKind uint8

const (
	nullKind jsonKind = iota
	objectKind
	arrayKind
	stringKind
	numberKind
	boolKind
)

type jsonProperty struct {
	name  string
	value jsonValue
}

type jsonValue struct {
	kind    jsonKind
	object  []jsonProperty
	array   []jsonValue
	text    string
	boolean bool
}

func decodeDocument(raw []byte) (jsonValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return jsonValue{}, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		if err == nil {
			return jsonValue{}, errorsNewTrailingValue
		}
		return jsonValue{}, err
	}
	return value, nil
}

var errorsNewTrailingValue = fmt.Errorf("JSON contains a trailing value")

func decodeValue(decoder *json.Decoder) (jsonValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return jsonValue{}, err
	}
	switch typed := token.(type) {
	case nil:
		return jsonValue{kind: nullKind}, nil
	case bool:
		return jsonValue{kind: boolKind, boolean: typed}, nil
	case string:
		return jsonValue{kind: stringKind, text: typed}, nil
	case json.Number:
		return jsonValue{kind: numberKind, text: typed.String()}, nil
	case json.Delim:
		switch typed {
		case '{':
			value := jsonValue{kind: objectKind}
			for decoder.More() {
				nameToken, tokenErr := decoder.Token()
				if tokenErr != nil {
					return jsonValue{}, tokenErr
				}
				name, ok := nameToken.(string)
				if !ok {
					return jsonValue{}, fmt.Errorf("object property name is not a string")
				}
				child, childErr := decodeValue(decoder)
				if childErr != nil {
					return jsonValue{}, childErr
				}
				value.object = append(value.object, jsonProperty{name: name, value: child})
			}
			if _, err = decoder.Token(); err != nil {
				return jsonValue{}, err
			}
			return value, nil
		case '[':
			value := jsonValue{kind: arrayKind}
			for decoder.More() {
				child, childErr := decodeValue(decoder)
				if childErr != nil {
					return jsonValue{}, childErr
				}
				value.array = append(value.array, child)
			}
			if _, err = decoder.Token(); err != nil {
				return jsonValue{}, err
			}
			return value, nil
		}
	}
	return jsonValue{}, fmt.Errorf("unsupported JSON token")
}
