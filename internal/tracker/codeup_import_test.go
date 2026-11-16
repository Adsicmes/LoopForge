package tracker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

// codeupImportFixture stores a project whose Codeup repository is the import
// source.
func codeupImportFixture(t *testing.T) (*Service, *repo.Store, domain.Repository) {
	t.Helper()
	s, st, r := newCodeupFixture(t)
	return s, st, r
}

func TestImportCodeupWorkItemStoresTheRequirement(t *testing.T) {
	s, st, r := codeupImportFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/oapi/v1/projex/organizations/"+testCodeupOrg+"/workitems:search" {
			t.Errorf("unexpected path %s", req.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		if conditions, _ := body["conditions"].(string); !strings.Contains(conditions, "identifier") {
			t.Errorf("search should filter on the identifier: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"identifier": "DEMO-11", "subject": "另一个需求", "spaceIdentifier": "space-1"},
			{"identifier": "DEMO-12", "subject": "登录接口加验证码", "description": "详见设计稿。", "spaceIdentifier": "space-1"},
		})
	}))
	defer srv.Close()
	s.codeupAPIBaseURL = srv.URL

	issue, err := s.ImportCodeupWorkItem(st, "proj", r.ID, "demo-12", "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if issue.ProjectID != "proj" || issue.RepositoryID != r.ID {
		t.Fatalf("issue = %+v", issue)
	}
	if issue.Title != "登录接口加验证码" || issue.Description != "详见设计稿。" {
		t.Fatalf("title/description = %q / %q", issue.Title, issue.Description)
	}
	if issue.Source != domain.CodeupProvider || issue.ExternalRef == nil || issue.ExternalRef.ExternalID != "DEMO-12" {
		t.Fatalf("source ref = %+v", issue.ExternalRef)
	}
	if !strings.Contains(issue.ExternalRef.ExternalURL, "devops.aliyun.com/projex") {
		t.Fatalf("external url = %q", issue.ExternalRef.ExternalURL)
	}
	if issue.Status != "backlog" || issue.Kind != "requirement" {
		t.Fatalf("status/kind = %s/%s", issue.Status, issue.Kind)
	}
}

func TestImportCodeupWorkItemReportsMissingItem(t *testing.T) {
	s, st, r := codeupImportFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	s.codeupAPIBaseURL = srv.URL
	if _, err := s.ImportCodeupWorkItem(st, "proj", r.ID, "DEMO-404", ""); err == nil || !strings.Contains(err.Error(), "没有找到工作项") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportCodeupWorkItemRefusesNonCodeupRepository(t *testing.T) {
	s, st, _ := codeupImportFixture(t)
	if err := st.PutRepository(domain.Repository{ID: "plain-repo", ProjectID: "proj", Provider: "gongfeng", Name: "plain", GongfengProjectID: "1001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCodeupWorkItem(st, "proj", "plain-repo", "DEMO-12", ""); err == nil || !strings.Contains(err.Error(), "不是云效") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportCodeupWorkItemRefusesForeignRepository(t *testing.T) {
	s, st, r := codeupImportFixture(t)
	if err := st.PutProject(domain.Project{ID: "other-project", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportCodeupWorkItem(st, "other-project", r.ID, "DEMO-12", ""); err == nil || !strings.Contains(err.Error(), "不属于项目") {
		t.Fatalf("err = %v", err)
	}
}
