package yunxiao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testToken = "pat-secret-token"

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func centralClient(srv *httptest.Server) *Client {
	return New(srv.URL+"/oapi/v1", "org-1", testToken)
}

func regionClient(srv *httptest.Server) *Client {
	return New(srv.URL, "org-1", testToken)
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, body)
}

func TestValidateBaseURL(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://openapi-rdc.aliyuncs.com",
		"https://acme.devops.aliyuncs.com",
		"http://127.0.0.1:1234",
		"http://localhost:1234",
	} {
		if err := ValidateBaseURL(raw); err != nil {
			t.Fatalf("ValidateBaseURL(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://evil.example",
		"https://evil.example",
		"https://user:pass@openapi-rdc.aliyuncs.com",
		"https://openapi-rdc.aliyuncs.com/?redirect=evil",
	} {
		if err := ValidateBaseURL(raw); err == nil {
			t.Fatalf("ValidateBaseURL(%q) accepted an unsafe host", raw)
		}
	}
}

func TestStationPaths(t *testing.T) {
	tests := []struct {
		name   string
		region bool
		repo   string
		want   string
	}{
		{"central numeric", false, "42", "/oapi/v1/codeup/organizations/org-1/repositories/42"},
		{"region numeric", true, "42", "/oapi/v1/codeup/repositories/42"},
		{"central encoded path", false, "group%2Fsub%2Frepo", "/oapi/v1/codeup/organizations/org-1/repositories/group%2Fsub%2Frepo"},
		{"region raw path", true, "group/sub", "/oapi/v1/codeup/repositories/group%2Fsub"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.EscapedPath()
				writeJSON(w, `{"id":42}`)
			})
			client := centralClient(srv)
			if tt.region {
				client = regionClient(srv)
			}
			if _, err := client.GetRepository(context.Background(), tt.repo); err != nil {
				t.Fatalf("GetRepository: %v", err)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthHeaderAndContentType(t *testing.T) {
	var token, contentType, accept string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		token = r.Header.Get("x-yunxiao-token")
		contentType = r.Header.Get("Content-Type")
		accept = r.Header.Get("Accept")
		writeJSON(w, `{"localId":1}`)
	})
	client := centralClient(srv)
	_, err := client.CreateChangeRequest(context.Background(), 7, CreateChangeRequestInput{
		SourceBranch: "feature/a",
		TargetBranch: "main",
		Title:        "title",
	})
	if err != nil {
		t.Fatalf("CreateChangeRequest: %v", err)
	}
	if token != testToken {
		t.Fatalf("x-yunxiao-token = %q, want %q", token, testToken)
	}
	if contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if accept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", accept)
	}
}

func TestGetRepository(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		writeJSON(w, `{
			"id": 42,
			"name": "demo",
			"path": "demo",
			"pathWithNamespace": "group/demo",
			"defaultBranch": "main",
			"webUrl": "https://codeup.aliyun.com/group/demo",
			"sshUrlToRepo": "git@codeup.aliyun.com:group/demo.git",
			"httpUrlToRepo": "https://codeup.aliyun.com/group/demo.git",
			"accessLevel": "40",
			"allowPush": true
		}`)
	})
	repo, err := centralClient(srv).GetRepository(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.ID != 42 || repo.Name != "demo" || repo.PathWithNamespace != "group/demo" {
		t.Fatalf("unexpected repository: %+v", repo)
	}
	if repo.DefaultBranch != "main" || !repo.AllowPush || repo.AccessLevel != "40" {
		t.Fatalf("unexpected repository fields: %+v", repo)
	}
	if repo.SSHURL != "git@codeup.aliyun.com:group/demo.git" {
		t.Fatalf("SSHURL = %q", repo.SSHURL)
	}
}

func TestListChangeRequests(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/codeup/organizations/org-1/changeRequests" {
			t.Errorf("path = %q", got)
		}
		query := r.URL.Query()
		if got := query.Get("projectIds"); got != "42,43" {
			t.Errorf("projectIds = %q, want 42,43", got)
		}
		if got := query.Get("state"); got != "opened" {
			t.Errorf("state = %q", got)
		}
		if got := query.Get("page"); got != "2" {
			t.Errorf("page = %q", got)
		}
		if got := query.Get("perPage"); got != "100" {
			t.Errorf("perPage = %q", got)
		}
		w.Header().Set("x-total", "3")
		w.Header().Set("x-total-pages", "2")
		w.Header().Set("x-next-page", "2")
		writeJSON(w, `[
			{"localId": 11, "projectId": 42, "sourceBranch": "feature/a", "targetBranch": "main", "status": "UNDER_REVIEW"},
			{"localId": 12, "projectId": 43, "title": "second", "status": "MERGED"}
		]`)
	})
	items, info, err := centralClient(srv).ListChangeRequestsPage(context.Background(), []int{42, 43}, "opened", 2, 100)
	if err != nil {
		t.Fatalf("ListChangeRequestsPage: %v", err)
	}
	if len(items) != 2 || items[0].LocalID != 11 || items[1].Title != "second" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if info.Total != 3 || info.TotalPages != 2 || info.NextPage != 2 || !info.HasNext() {
		t.Fatalf("unexpected page info: %+v", info)
	}
}

func TestListAllChangeRequests(t *testing.T) {
	var requests int
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("x-next-page", "2")
			writeJSON(w, `[{"localId":1},{"localId":2}]`)
		case "2":
			w.Header().Set("x-total-pages", "2")
			writeJSON(w, `[{"localId":3}]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
			writeJSON(w, `[]`)
		}
	})
	items, err := centralClient(srv).ListAllChangeRequests(context.Background(), []int{42}, "opened", 2, 10)
	if err != nil {
		t.Fatalf("ListAllChangeRequests: %v", err)
	}
	if len(items) != 3 || requests != 2 {
		t.Fatalf("items = %d, requests = %d", len(items), requests)
	}
}

func TestGetChangeRequest(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/codeup/organizations/org-1/repositories/42/changeRequests/9" {
			t.Errorf("path = %q", got)
		}
		writeJSON(w, `{
			"localId": 9,
			"projectId": 42,
			"sourceBranch": "feature/a",
			"targetBranch": "main",
			"title": "Add feature",
			"description": "body",
			"status": "TO_BE_MERGED",
			"webUrl": "https://codeup.aliyun.com/group/demo/change/9",
			"sourceProjectId": 42,
			"targetProjectId": 42,
			"allRequirementsPass": true,
			"conflictCheckStatus": "NO_CONFLICT",
			"mergedRevision": "abc123",
			"createTime": "2026-11-01T10:00:00Z",
			"updateTime": "2026-11-02T11:00:00Z"
		}`)
	})
	cr, err := centralClient(srv).GetChangeRequest(context.Background(), 42, 9)
	if err != nil {
		t.Fatalf("GetChangeRequest: %v", err)
	}
	if cr.LocalID != 9 || cr.Status != "TO_BE_MERGED" || !cr.AllRequirementsPass {
		t.Fatalf("unexpected change request: %+v", cr)
	}
	if cr.ConflictCheckStatus != "NO_CONFLICT" || cr.MergedRevision != "abc123" {
		t.Fatalf("unexpected merge fields: %+v", cr)
	}
	if cr.CreateTime != "2026-11-01T10:00:00Z" || cr.WebURL == "" {
		t.Fatalf("unexpected timestamps/webUrl: %+v", cr)
	}
}

func TestCreateChangeRequest(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/oapi/v1/codeup/organizations/org-1/repositories/42/changeRequests" {
			t.Errorf("path = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writeJSON(w, `{"localId":9,"projectId":42,"title":"Add feature","status":"UNDER_DEV"}`)
	})
	in := CreateChangeRequestInput{
		SourceBranch: "feature/a",
		TargetBranch: "main",
		Title:        "Add feature",
		Description:  "desc",
		WorkItemIDs:  "wi-1",
	}
	cr, err := centralClient(srv).CreateChangeRequest(context.Background(), 42, in)
	if err != nil {
		t.Fatalf("CreateChangeRequest: %v", err)
	}
	if cr.LocalID != 9 || cr.Status != "UNDER_DEV" {
		t.Fatalf("unexpected change request: %+v", cr)
	}
	if body["sourceBranch"] != "feature/a" || body["targetBranch"] != "main" || body["title"] != "Add feature" {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["sourceProjectId"] != float64(42) || body["targetProjectId"] != float64(42) {
		t.Fatalf("project IDs were not defaulted from repoID: %v", body)
	}
	if body["description"] != "desc" {
		t.Fatalf("description = %v", body["description"])
	}
}

func TestMergeChangeRequest(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/oapi/v1/codeup/organizations/org-1/repositories/42/changeRequests/9/merge" {
			t.Errorf("path = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writeJSON(w, `{"localId":9,"status":"MERGED","mergedRevision":"abc123"}`)
	})
	cr, err := centralClient(srv).MergeChangeRequest(context.Background(), 42, 9, MergeChangeRequestInput{
		MergeType:          MergeTypeSquash,
		MergeMessage:       "squash it",
		RemoveSourceBranch: true,
	})
	if err != nil {
		t.Fatalf("MergeChangeRequest: %v", err)
	}
	if cr.Status != "MERGED" || cr.MergedRevision != "abc123" {
		t.Fatalf("unexpected change request: %+v", cr)
	}
	if body["mergeType"] != "squash" || body["mergeMessage"] != "squash it" || body["removeSourceBranch"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestMergeChangeRequestDefaultsToNoFastForward(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		writeJSON(w, `{"localId":9,"status":"MERGED"}`)
	})
	if _, err := centralClient(srv).MergeChangeRequest(context.Background(), 42, 9, MergeChangeRequestInput{}); err != nil {
		t.Fatalf("MergeChangeRequest: %v", err)
	}
	if body["mergeType"] != MergeTypeNoFastForward {
		t.Fatalf("mergeType = %v, want %s", body["mergeType"], MergeTypeNoFastForward)
	}
}

func TestSearchWorkitems(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/oapi/v1/projex/organizations/org-1/workitems:search" {
			t.Errorf("path = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("x-total-pages", "5")
		w.Header().Set("x-next-page", "3")
		writeJSON(w, `[{
			"id": "wi-1",
			"subject": "Fix bug",
			"description": "details",
			"formatType": "MARKDOWN",
			"categoryId": "Bug",
			"serialNumber": "DEMO-1",
			"status": {"id": "s1", "name": "处理中", "nameEn": "IN_PROGRESS"},
			"assignedTo": {"id": "u1", "name": "张三"},
			"workitemType": {"id": "t1", "name": "Bug"},
			"customFieldValues": [{"fieldId": "priority", "fieldName": "优先级", "value": "high", "displayValue": "高"}],
			"gmtCreate": "2026-11-01T10:00:00Z",
			"gmtModified": "2026-11-02T11:00:00Z"
		}]`)
	})
	in := SearchWorkitemsInput{
		Category:   "Req,Bug",
		SpaceID:    "space-1",
		Conditions: "status = 'TODO'",
		Page:       2,
		PerPage:    200,
		OrderBy:    "gmtModified",
		Sort:       "desc",
	}
	items, info, err := centralClient(srv).SearchWorkitemsPage(context.Background(), in)
	if err != nil {
		t.Fatalf("SearchWorkitemsPage: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	item := items[0]
	if item.ID != "wi-1" || item.Subject != "Fix bug" || item.SerialNumber != "DEMO-1" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Status.NameEn != "IN_PROGRESS" || item.AssignedTo.Name != "张三" || item.WorkitemType.Name != "Bug" {
		t.Fatalf("unexpected nested fields: %+v", item)
	}
	if len(item.CustomFieldValues) != 1 || item.CustomFieldValues[0].DisplayValue != "高" {
		t.Fatalf("unexpected custom fields: %+v", item.CustomFieldValues)
	}
	if item.CreatedAt != "2026-11-01T10:00:00Z" || item.UpdatedAt != "2026-11-02T11:00:00Z" {
		t.Fatalf("unexpected timestamps: %+v", item)
	}
	if info.TotalPages != 5 || info.NextPage != 3 {
		t.Fatalf("unexpected page info: %+v", info)
	}
	if body["category"] != "Req,Bug" || body["spaceId"] != "space-1" || body["conditions"] != "status = 'TODO'" {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["page"] != float64(2) || body["perPage"] != float64(200) {
		t.Fatalf("unexpected pagination body: %v", body)
	}
	if body["orderBy"] != "gmtModified" || body["sort"] != "desc" {
		t.Fatalf("unexpected sort body: %v", body)
	}
}

func TestListAllWorkitems(t *testing.T) {
	var pages []int
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body SearchWorkitemsInput
		json.NewDecoder(r.Body).Decode(&body)
		pages = append(pages, body.Page)
		switch body.Page {
		case 1:
			w.Header().Set("x-next-page", "2")
			writeJSON(w, `[{"id":"wi-1"},{"id":"wi-2"}]`)
		case 2:
			writeJSON(w, `[{"id":"wi-3"}]`)
		default:
			writeJSON(w, `[]`)
		}
	})
	items, err := centralClient(srv).ListAllWorkitems(context.Background(), SearchWorkitemsInput{
		Category: "Bug",
		SpaceID:  "space-1",
		PerPage:  2,
	}, 10)
	if err != nil {
		t.Fatalf("ListAllWorkitems: %v", err)
	}
	if len(items) != 3 || len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("items = %d, pages = %v", len(items), pages)
	}
}

func TestCreateWorkitem(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/projex/organizations/org-1/workitems" {
			t.Errorf("path = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writeJSON(w, `{"id": 12345}`)
	})
	in := CreateWorkitemInput{
		AssignedTo:     "u1",
		SpaceID:        "space-1",
		WorkitemTypeID: "t1",
		Subject:        "New bug",
		Description:    "desc",
		FormatType:     "MARKDOWN",
	}
	id, err := centralClient(srv).CreateWorkitem(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateWorkitem: %v", err)
	}
	if id != "12345" {
		t.Fatalf("id = %q, want 12345", id)
	}
	if body["assignedTo"] != "u1" || body["spaceId"] != "space-1" || body["workitemTypeId"] != "t1" {
		t.Fatalf("unexpected body: %v", body)
	}
	if body["subject"] != "New bug" || body["formatType"] != "MARKDOWN" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestCreateWorkitemValidation(t *testing.T) {
	var requests int32
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		writeJSON(w, `{}`)
	})
	client := centralClient(srv)
	cases := []CreateWorkitemInput{
		{SpaceID: "s", WorkitemTypeID: "t", AssignedTo: "u"},
		{Subject: "x", WorkitemTypeID: "t", AssignedTo: "u"},
		{Subject: "x", SpaceID: "s", AssignedTo: "u"},
		{Subject: "x", SpaceID: "s", WorkitemTypeID: "t"},
	}
	for _, in := range cases {
		if _, err := client.CreateWorkitem(context.Background(), in); err == nil {
			t.Fatalf("expected validation error for %+v", in)
		}
	}
	if requests != 0 {
		t.Fatalf("validation failures issued %d requests", requests)
	}
}

func TestCreateWorkitemComment(t *testing.T) {
	var body map[string]any
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/projex/organizations/org-1/workitems/wi-1/comments" {
			t.Errorf("path = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writeJSON(w, `{"id":"comment-1"}`)
	})
	id, err := centralClient(srv).CreateWorkitemComment(context.Background(), "wi-1", "澄清说明")
	if err != nil {
		t.Fatalf("CreateWorkitemComment: %v", err)
	}
	if id != "comment-1" {
		t.Fatalf("id = %q", id)
	}
	if body["content"] != "澄清说明" || body["formatType"] != "MARKDOWN" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestGetWorkitem(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/projex/organizations/org-1/workitems/wi-1" {
			t.Errorf("path = %q", got)
		}
		writeJSON(w, `{"id":"wi-1","subject":"Fix bug"}`)
	})
	item, err := centralClient(srv).GetWorkitem(context.Background(), "wi-1")
	if err != nil {
		t.Fatalf("GetWorkitem: %v", err)
	}
	if item.ID != "wi-1" || item.Subject != "Fix bug" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestListOrganizations(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/oapi/v1/platform/organizations" {
			t.Errorf("path = %q", got)
		}
		writeJSON(w, `[{"id":"org-1","name":"示例组织"}]`)
	})
	orgs, err := centralClient(srv).ListOrganizations(context.Background())
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != "org-1" || orgs[0].Name != "示例组织" {
		t.Fatalf("unexpected organizations: %+v", orgs)
	}
}

func TestListOrganizationsUnsupportedOnRegion(t *testing.T) {
	client := New("https://org-1.devops.aliyuncs.com", "org-1", testToken)
	if _, err := client.ListOrganizations(context.Background()); err == nil {
		t.Fatal("expected region station to reject ListOrganizations")
	} else if !strings.Contains(err.Error(), "Region") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthFailuresAreNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests int32
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requests, 1)
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"invalid token `+testToken+`"}`)
			})
			_, err := centralClient(srv).ListChangeRequests(context.Background(), []int{42}, "opened", 1, 10)
			if err == nil {
				t.Fatal("expected auth error")
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1 (no retry)", requests)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("expected APIError with %d, got %v", status, err)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatalf("token leaked in error: %v", err)
			}
			if apiErr.Hint == "" {
				t.Fatal("expected an actionable hint")
			}
		})
	}
}

func TestRetriesOn429And500(t *testing.T) {
	var requests int32
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&requests, 1) {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, "rate limited")
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, "boom")
		default:
			writeJSON(w, `[]`)
		}
	})
	items, err := centralClient(srv).ListChangeRequests(context.Background(), []int{42}, "opened", 1, 10)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	if len(items) != 0 || requests != 3 {
		t.Fatalf("items = %d, requests = %d, want 0 and 3", len(items), requests)
	}
}

func TestRetriesExhausted(t *testing.T) {
	var requests int32
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "upstream down")
	})
	_, err := centralClient(srv).ListChangeRequests(context.Background(), []int{42}, "opened", 1, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream down") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorRedactionAndLimit(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "token="+testToken+" "+strings.Repeat("x", 4000))
	})
	_, err := centralClient(srv).ListChangeRequests(context.Background(), []int{42}, "opened", 1, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, testToken) {
		t.Fatalf("token leaked in error: %v", msg)
	}
	if len(msg) > 900 {
		t.Fatalf("error message not truncated: %d bytes", len(msg))
	}
	if !strings.Contains(msg, "400") {
		t.Fatalf("missing status: %v", msg)
	}
}

func TestNoRedirectFollowing(t *testing.T) {
	var targetHits int32
	target := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		writeJSON(w, `[]`)
	})
	redirect := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	})
	_, err := centralClient(redirect).ListOrganizations(context.Background())
	if err == nil {
		t.Fatal("expected redirect to be rejected")
	}
	if targetHits != 0 {
		t.Fatalf("redirect target was called %d times", targetHits)
	}
}

func TestMissingToken(t *testing.T) {
	var requests int32
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		writeJSON(w, `[]`)
	})
	client := New(srv.URL, "org-1", "")
	if _, err := client.ListChangeRequests(context.Background(), []int{42}, "opened", 1, 10); err == nil {
		t.Fatal("expected missing token error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestDecodeListWrappers(t *testing.T) {
	for _, body := range []string{
		`[{"id":"org-1","name":"one"}]`,
		`{"items":[{"id":"org-1","name":"one"}]}`,
		`{"result":[{"id":"org-1","name":"one"}]}`,
		`{"data":[{"id":"org-1","name":"one"}]}`,
		`{"organizations":[{"id":"org-1","name":"one"}]}`,
	} {
		orgs, err := decodeList[Organization]([]byte(body))
		if err != nil {
			t.Fatalf("decodeList(%s): %v", body, err)
		}
		if len(orgs) != 1 || orgs[0].ID != "org-1" {
			t.Fatalf("decodeList(%s) = %+v", body, orgs)
		}
	}
}

func TestPageInfoHasNextFalseWithoutHeaders(t *testing.T) {
	if (PageInfo{}).HasNext() {
		t.Fatal("empty PageInfo must not report a next page")
	}
}
