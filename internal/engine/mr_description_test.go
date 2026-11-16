package engine

import (
	"strings"
	"testing"

	"agentflow/internal/domain"
)

func TestIntegrateMRDescriptionLinksTheRun(t *testing.T) {
	t.Setenv("AGENTFLOW_PUBLIC_URL", "https://loopforge.example.com/")
	r := domain.Run{ID: "run-42"}
	body := integrateMRDescription(r, "feature/x", "develop")
	if !strings.Contains(body, "https://loopforge.example.com/#/runs/run-42") {
		t.Fatalf("description should carry a clickable run link:\n%s", body)
	}
	if !strings.Contains(body, "[run-42]") {
		t.Fatalf("the run id should stay readable as link text:\n%s", body)
	}
}

func TestIntegrateMRDescriptionWithoutPublicURLKeepsPlainRunID(t *testing.T) {
	t.Setenv("AGENTFLOW_PUBLIC_URL", "")
	r := domain.Run{ID: "run-43"}
	body := integrateMRDescription(r, "feature/x", "develop")
	if strings.Contains(body, "](http") {
		t.Fatalf("no link should be invented without a public URL:\n%s", body)
	}
	if !strings.Contains(body, "- run: run-43") {
		t.Fatalf("plain run id missing:\n%s", body)
	}
}

func TestAlignOnlyDescriptionAlsoLinksTheRun(t *testing.T) {
	t.Setenv("AGENTFLOW_PUBLIC_URL", "https://loopforge.example.com")
	r := domain.Run{ID: "run-44", Repository: domain.RepositoryContext{AlignOnly: true}}
	body := integrateMRDescription(r, "main", "develop")
	if !strings.Contains(body, "https://loopforge.example.com/#/runs/run-44") {
		t.Fatalf("alignment MR should link the run too:\n%s", body)
	}
	if !strings.Contains(body, "- source: `main`") {
		t.Fatalf("source line missing:\n%s", body)
	}
}

func TestRunLinkEscapesTheRunID(t *testing.T) {
	t.Setenv("AGENTFLOW_PUBLIC_URL", "https://loopforge.example.com")
	if got := runLink("run/1 2"); !strings.HasSuffix(got, "run%2F1%202") {
		t.Fatalf("run link = %q", got)
	}
	if got := runLink("  "); got != "" {
		t.Fatalf("blank run id must not produce a link: %q", got)
	}
}
