package tracker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentflow/internal/domain"
)

func yunxiaoFixture(serverURL string) (projectAuthFixture, *Service) {
	fixture := projectAuthFixture{
		projects: map[string]domain.Project{
			"p": {
				ID:                       "p",
				Name:                     "Project",
				OwnerID:                  1,
				YunxiaoSpaceID:           "space-1",
				YunxiaoWorkItemTypes:     map[string]string{"Req": "type-req", "Bug": "type-bug"},
				YunxiaoDefaultAssigneeID: "user-1",
			},
		},
		users: map[int64]domain.User{
			1: {ID: 1, LoginName: "owner", ChineseName: "负责人", YunxiaoToken: "pat-1", YunxiaoUserID: "user-1"},
		},
		repos: []domain.Repository{{
			ID: "repo-1", ProjectID: "p", Provider: domain.RepositoryProviderYunxiao,
			Name: "repo", GongfengProjectID: "codeup:org/repo", YunxiaoRepoID: 42,
			WebURL: "https://codeup.aliyun.com/org/repo", CloneURL: "git@codeup.aliyun.com:org/repo.git",
			DefaultBranch: "master",
		}},
	}
	svc := NewService()
	svc.ConfigureWithStore(domain.SyncConfig{YunxiaoAPIBase: serverURL}, fixture)
	return fixture, svc
}

func TestYunxiaoPullWorkitemsMapsAndPreservesLocalState(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oapi/v1/projex/workitems:search" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-yunxiao-token"); got != "pat-1" {
			t.Fatalf("token = %q", got)
		}
		calls++
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":           "wi-1",
			"subject":      "修复登录",
			"description":  "描述",
			"categoryId":   "Bug",
			"serialNumber": "BUG-1",
			"status":       map[string]any{"id": "s1", "name": "处理中", "nameEn": "IN_PROGRESS"},
			"assignedTo":   map[string]any{"id": "user-1", "name": "负责人"},
			"workitemType": map[string]any{"id": "type-bug", "name": "缺陷"},
			"customFieldValues": []map[string]any{{
				"fieldId": "priority", "fieldName": "优先级", "value": "p1", "displayValue": "高",
			}},
			"gmtCreate": "2026-11-16T10:00:00Z",
		}})
	}))
	defer server.Close()

	_, svc := yunxiaoFixture(server.URL)
	store := &memIssueStore{
		projects: map[string]domain.Project{"p": {ID: "p", Name: "Project", OwnerID: 1, YunxiaoSpaceID: "space-1", YunxiaoWorkItemTypes: map[string]string{"Bug": "type-bug"}}},
		users:    []domain.User{{ID: 1, LoginName: "owner", YunxiaoUserID: "user-1"}},
	}
	n, err := svc.pullYunxiaoWorkitems(store, store.projects["p"])
	if err != nil || n != 1 {
		t.Fatalf("pull = %d, %v", n, err)
	}
	issue, ok := store.Issue("yx-p-wi-1")
	if !ok {
		t.Fatalf("missing issue: %+v", store.issues)
	}
	if issue.Source != "yunxiao" || issue.Kind != "issue" || issue.Priority != "high" || issue.Status != "in_progress" || issue.AssigneeUserID != 1 {
		t.Fatalf("mapped issue = %+v", issue)
	}
	if issue.ExternalRef == nil || issue.ExternalRef.ExternalID != "wi-1" || issue.ExternalRef.ExternalType != "Bug" {
		t.Fatalf("external ref = %+v", issue.ExternalRef)
	}

	issue.Status = "in_review"
	issue.Clarification.Status = "ready"
	if err := store.PutIssue(issue); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.pullYunxiaoWorkitems(store, store.projects["p"]); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := store.Issue("yx-p-wi-1")
	if refreshed.Status != "in_review" || refreshed.Clarification.Status != "ready" {
		t.Fatalf("local state was overwritten: %+v", refreshed)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestYunxiaoCreateWorkitemAndComment(t *testing.T) {
	var created map[string]any
	var comment map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oapi/v1/projex/workitems":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &created)
			_, _ = w.Write([]byte(`{"id":"wi-2"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/oapi/v1/projex/workitems/wi-2":
			_, _ = w.Write([]byte(`{"id":"wi-2","subject":"新需求","description":"body","categoryId":"Req","status":{"id":"s1","name":"待处理","nameEn":"TODO"},"assignedTo":{"id":"user-1","name":"负责人"},"workitemType":{"id":"type-req","name":"需求"},"gmtCreate":"2026-11-16T10:00:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/oapi/v1/projex/workitems/wi-2/comments":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &comment)
			_, _ = w.Write([]byte(`{"id":"c-1"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	_, svc := yunxiaoFixture(server.URL)
	store := &memIssueStore{
		projects: map[string]domain.Project{"p": {ID: "p", Name: "Project", OwnerID: 1, YunxiaoSpaceID: "space-1", YunxiaoWorkItemTypes: map[string]string{"Req": "type-req"}}},
		users:    []domain.User{{ID: 1, LoginName: "owner", YunxiaoUserID: "user-1"}},
	}
	createdIssue, err := svc.CreateYunxiaoWorkitem(store, domain.Issue{ID: "issue-temp", ProjectID: "p", Title: "新需求", Description: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if created["spaceId"] != "space-1" || created["workitemTypeId"] != "type-req" || created["assignedTo"] != "user-1" {
		t.Fatalf("create payload = %+v", created)
	}
	if createdIssue.ID != "yx-p-wi-2" || createdIssue.ExternalRef == nil || createdIssue.ExternalRef.ExternalID != "wi-2" {
		t.Fatalf("created issue = %+v", createdIssue)
	}
	if _, ok := store.Issue("issue-temp"); ok {
		t.Fatal("temporary issue id should be removed")
	}
	if err := svc.WriteBackClarification(store, createdIssue, "澄清完成"); err != nil {
		t.Fatal(err)
	}
	if comment["content"] != "澄清完成" || comment["formatType"] != "MARKDOWN" {
		t.Fatalf("comment payload = %+v", comment)
	}
}

func TestYunxiaoFindAndMergeBranchMR(t *testing.T) {
	var merged bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/oapi/v1/codeup/changeRequests":
			_, _ = w.Write([]byte(`[{"localId":9,"projectId":42,"sourceBranch":"feature/x","targetBranch":"main","title":"MR","status":"UNDER_REVIEW","state":"UNDER_REVIEW","webUrl":"https://codeup.aliyun.com/org/repo/change/9"}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/changeRequests/9/merge"):
			merged = true
			_, _ = w.Write([]byte(`{"localId":9,"projectId":42,"sourceBranch":"feature/x","targetBranch":"main","status":"MERGED","webUrl":"https://codeup.aliyun.com/org/repo/change/9","updateTime":"2026-11-16T12:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	_, svc := yunxiaoFixture(server.URL)
	mr, found, err := svc.FindBranchMR("codeup:org/repo", "feature/x")
	if err != nil || !found || mr.IID != 9 || mr.URL == "" || mr.State != "opened" {
		t.Fatalf("find MR = %+v %v %v", mr, found, err)
	}
	ref, err := svc.EnsureAndMergeBranchMR("codeup:org/repo", "feature/x", "main", "title", "body")
	if err != nil || ref != "#9" || !merged {
		t.Fatalf("merge = %q %v merged=%v", ref, err, merged)
	}
}
