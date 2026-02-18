package cmd

import (
	"strings"
	"testing"
)

func TestMakeBrief(t *testing.T) {
	out, err := executeCommand("make", "brief")
	if err != nil {
		t.Fatalf("make brief should succeed: %v", err)
	}
	if !strings.Contains(out, "Generating brief composition") {
		t.Error("output should indicate brief generation")
	}
	if !strings.Contains(out, "## Summary") {
		t.Error("output should contain Summary section")
	}
	if !strings.Contains(out, "## Key Points") {
		t.Error("output should contain Key Points section")
	}
}

func TestMakePlan(t *testing.T) {
	out, err := executeCommand("make", "plan")
	if err != nil {
		t.Fatalf("make plan should succeed: %v", err)
	}
	if !strings.Contains(out, "Generating plan composition") {
		t.Error("output should indicate plan generation")
	}
}

func TestMakeSummary(t *testing.T) {
	out, err := executeCommand("make", "summary")
	if err != nil {
		t.Fatalf("make summary should succeed: %v", err)
	}
	if !strings.Contains(out, "Generating summary composition") {
		t.Error("output should indicate summary generation")
	}
}

func TestMakeDraft(t *testing.T) {
	out, err := executeCommand("make", "draft")
	if err != nil {
		t.Fatalf("make draft should succeed: %v", err)
	}
	if !strings.Contains(out, "Generating draft composition") {
		t.Error("output should indicate draft generation")
	}
}

func TestMakeWithFilters(t *testing.T) {
	out, err := executeCommand("make", "brief", "--tag", "ux,onboarding", "--mention", "@project.signup", "--since", "2025-01-01")
	if err != nil {
		t.Fatalf("make with filters should succeed: %v", err)
	}
	if !strings.Contains(out, "Generating brief composition") {
		t.Error("output should indicate brief generation")
	}
}

func TestMakeNoTypeError(t *testing.T) {
	_, err := executeCommand("make")
	if err == nil {
		t.Error("make without type argument should fail")
	}
}

func TestMakeHelp(t *testing.T) {
	out, err := executeCommand("make", "--help")
	if err != nil {
		t.Fatalf("make --help should succeed: %v", err)
	}
	for _, flag := range []string{"--mention", "--tag", "--since", "-o"} {
		if !strings.Contains(out, flag) {
			t.Errorf("make help should list flag %s", flag)
		}
	}
	for _, typ := range []string{"brief", "plan", "summary", "draft"} {
		if !strings.Contains(out, typ) {
			t.Errorf("make help should mention type %q", typ)
		}
	}
}
