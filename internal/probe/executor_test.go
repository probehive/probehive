package probe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/probehive/probehive/internal/check"
)

func newDispatchingExecutor(t *testing.T, ports ...uint16) *Executor {
	t.Helper()
	executor, err := NewExecutor(newExecutor(t, DefaultSettings(), nil, ports...))
	if err != nil {
		t.Fatalf("build executor: %v", err)
	}
	return executor
}

func TestExecutorRunsAStoredConfiguration(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	executor := newDispatchingExecutor(t, serverPort(t, server))

	configuration := json.RawMessage(fmt.Sprintf(`{"url":%q,"method":"GET"}`, server.URL))
	observation := executor.Execute(t.Context(), check.HTTPCheckType, check.HTTPCurrentSchemaVersion, configuration)

	if observation.Outcome != OutcomePassed {
		t.Fatalf("outcome = %q with failure %+v", observation.Outcome, observation.Failure)
	}
	if observation.HTTP == nil || observation.HTTP.StatusCode != http.StatusOK {
		t.Fatalf("HTTP result = %+v", observation.HTTP)
	}
}

// A stored revision reaches execution long after it was written. These are the cases where
// the validator this build contains disagrees with what is stored, and each one is an errored
// Run rather than a dropped error (ADR 0020).
func TestExecutorRejectsAConfigurationItCannotExecute(t *testing.T) {
	t.Parallel()
	executor := newDispatchingExecutor(t, 443)

	for _, testCase := range []struct {
		name          string
		checkType     string
		schemaVersion int
		configuration string
		code          string
	}{
		{"unsupported check type", "icmp", 1, `{}`, CodeCheckTypeUnsupported},
		{"unsupported schema version", check.HTTPCheckType, 2, `{"url":"https://example.test/"}`, check.SchemaVersionUnsupportedCode},
		{"missing url", check.HTTPCheckType, 1, `{"method":"GET"}`, check.URLRequiredCode},
		{"unknown field", check.HTTPCheckType, 1, `{"url":"https://example.test/","retries":3}`, check.UnknownFieldCode},
		{"not an object", check.HTTPCheckType, 1, `[]`, check.ConfigurationNotObjectCode},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			observation := executor.Execute(t.Context(), testCase.checkType, testCase.schemaVersion, json.RawMessage(testCase.configuration))

			requireFailure(t, observation, OutcomeErrored, testCase.code)
			if observation.HTTP != nil {
				t.Fatalf("HTTP result = %+v for a configuration that never ran", observation.HTTP)
			}
			if !observation.StartedAt.Equal(testInstant) || !observation.FinishedAt.Equal(testInstant) {
				t.Fatalf("instants %s..%s do not come from the injected clock", observation.StartedAt, observation.FinishedAt)
			}
		})
	}
}

func TestExecutorReportsWhatItSupports(t *testing.T) {
	t.Parallel()
	executor := newDispatchingExecutor(t, 443)

	if !executor.Supports(check.HTTPCheckType) {
		t.Fatalf("the %q check type is not supported", check.HTTPCheckType)
	}
	if executor.Supports("dns") {
		t.Fatal("an unimplemented check type reports as supported")
	}
}

func TestNewExecutorRequiresAnHTTPExecutor(t *testing.T) {
	t.Parallel()
	if _, err := NewExecutor(nil); err == nil {
		t.Fatal("a missing HTTP executor was accepted")
	}
}
