package apierror_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/apierror"
)

func TestAPIError_Error(t *testing.T) {
	e := apierror.New(apierror.CodeNotFound, "thing not found", nil)
	if !strings.Contains(e.Error(), "CTXT-9001") {
		t.Errorf("expected code in error string, got %q", e.Error())
	}
	if !strings.Contains(e.Error(), "thing not found") {
		t.Errorf("expected message in error string, got %q", e.Error())
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := apierror.New(apierror.CodeStorageInitFailed, "storage failed", cause)
	if !errors.Is(e, cause) {
		t.Error("errors.Is should find wrapped cause")
	}
}

func TestAPIError_JSONBody(t *testing.T) {
	e := apierror.New(apierror.CodeInvalidInput, "bad input", nil)
	body := e.JSONBody()
	errMap, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("expected 'error' key in JSONBody")
	}
	if errMap["code"] != apierror.CodeInvalidInput {
		t.Errorf("expected code %q, got %q", apierror.CodeInvalidInput, errMap["code"])
	}
	if errMap["message"] != "bad input" {
		t.Errorf("expected message %q, got %q", "bad input", errMap["message"])
	}
}

func TestErrNotFound(t *testing.T) {
	e := apierror.ErrNotFound("pipeline")
	if e.Code != apierror.CodeNotFound {
		t.Errorf("expected %s, got %s", apierror.CodeNotFound, e.Code)
	}
	if !strings.Contains(e.Message, "pipeline") {
		t.Errorf("message should reference resource, got %q", e.Message)
	}
}

func TestErrInvalidInput(t *testing.T) {
	e := apierror.ErrInvalidInput("q is required")
	if e.Code != apierror.CodeInvalidInput {
		t.Errorf("expected %s, got %s", apierror.CodeInvalidInput, e.Code)
	}
}

func TestErrPipelineFailed(t *testing.T) {
	cause := errors.New("step failed")
	e := apierror.ErrPipelineFailed(cause)
	if e.Code != apierror.CodePipelineRunFailed {
		t.Errorf("expected %s, got %s", apierror.CodePipelineRunFailed, e.Code)
	}
	if !errors.Is(e, cause) {
		t.Error("should wrap original error")
	}
}

func TestErrRegistryNotFound(t *testing.T) {
	e := apierror.ErrRegistryNotFound("myregistry")
	if e.Code != apierror.CodeRegistryNotFound {
		t.Errorf("expected %s, got %s", apierror.CodeRegistryNotFound, e.Code)
	}
	if !strings.Contains(e.Message, "myregistry") {
		t.Errorf("message should contain registry name, got %q", e.Message)
	}
}
