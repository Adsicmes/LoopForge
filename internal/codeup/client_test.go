package codeup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testOrg = "60de7a6852743a5162b5f957"

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := New(Config{APIBase: srv.URL, OrganizationID: testOrg, Token: "pt-test-token"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, srv
}

func TestNewRequiresToken(t *testing.T) {
	if _, err := New(Config{Token: "  "}); err == nil {
		t.Fatal("empty token must be rejected")
	}
}

func TestNewRejectsNonHTTPBase(t *testing.T) {
	if _, err := New(Config{Token: "pt-x", APIBase: "openapi-rdc.aliyuncs.com"}); err == nil {
		t.Fatal("scheme-less base must be rejected")
	}
}

func TestGetUserSendsTokenHeaderAndResolvesOwner(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oapi/v1/platform/user" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-yunxiao-token"); got != "pt-test-token" {
			t.Errorf("token header = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("token must not travel as a bearer header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(User{ID: "62c795", Username: "codeup-user", Name: "示例用户"})
	})
	user, err := client.GetUser(context.Background())
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Username != "codeup-user" || user.ID != "62c795" {
		t.Fatalf("user = %+v", user)
	}
}

func TestGetUserRejectsEmptyPayload(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := client.GetUser(context.Background()); err == nil {
		t.Fatal("empty user payload must be rejected")
	}
}

func TestAuthFailureExplainsPersonalAccessToken(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"Unauthorized","message":"token invalid"}`))
	})
	_, err := client.GetUser(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsAuth(err) {
		t.Fatalf("IsAuth = false for %v", err)
	}
	if !strings.Contains(err.Error(), "个人访问令牌") {
		t.Fatalf("message should name the token: %v", err)
	}
}

func TestNotFoundIsReported(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"NotFound","message":"no such repository"}`))
	})
	_, err := client.GetRepository(context.Background(), testOrg+"/group/demo")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false for %v", err)
	}
}

func TestListRepositoriesUsesOrganizationScope(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oapi/v1/codeup/organizations/"+testOrg+"/repositories" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("search") != "demo" || r.URL.Query().Get("perPage") != "100" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"result": []map[string]any{{
				"id": 2813489, "name": "demo", "pathWithNamespace": testOrg + "/group/demo",
				"defaultBranch": "master", "webUrl": "https://codeup.aliyun.com/" + testOrg + "/group/demo",
			}},
		})
	})
	repos, total, err := client.ListRepositories(context.Background(), "demo", 1, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(repos) != 1 || repos[0].ID != 2813489 || repos[0].DefaultBranch != "master" {
		t.Fatalf("repos = %+v total=%d", repos, total)
	}
}

func TestGetRepositoryEncodesPathIdentity(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// The OpenAPI accepts the path with namespace as one URL-encoded path
		// segment; net/http decodes it back for us in r.URL.Path.
		want := "/oapi/v1/codeup/organizations/" + testOrg + "/repositories/" + testOrg + "/group/demo"
		if r.URL.Path != want {
			t.Errorf("path = %s want %s", r.URL.Path, want)
		}
		_ = json.NewEncoder(w).Encode(Repository{ID: 42, PathWithNamespace: testOrg + "/group/demo", DefaultBranch: "master"})
	})
	repo, err := client.GetRepository(context.Background(), testOrg+"/group/demo")
	if err != nil {
		t.Fatalf("get repository: %v", err)
	}
	if repo.ID != 42 {
		t.Fatalf("repo = %+v", repo)
	}
}

func TestListChangeRequestsQueriesOrganization(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oapi/v1/codeup/organizations/"+testOrg+"/changeRequests" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("projectIds") != "42,43" || q.Get("state") != "opened" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"localId": 7, "status": "UNDER_REVIEW", "sourceBranch": "feature/work", "targetBranch": "master",
		}})
	})
	items, err := client.ListChangeRequests(context.Background(), []string{"42", "43"}, "opened", 1, 100)
	if err != nil {
		t.Fatalf("list change requests: %v", err)
	}
	if len(items) != 1 || items[0].LocalID != 7 || items[0].Status != StateUnderReview {
		t.Fatalf("items = %+v", items)
	}
}

func TestCreateChangeRequestPostsRequiredFields(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oapi/v1/codeup/organizations/"+testOrg+"/repositories/42/changeRequests" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body: %v", err)
		}
		for _, key := range []string{"sourceProjectId", "targetProjectId", "sourceBranch", "targetBranch", "title", "createFrom"} {
			if _, ok := body[key]; !ok {
				t.Errorf("missing %s in %v", key, body)
			}
		}
		if body["workItemIds"] != "wi-1,wi-2" {
			t.Errorf("work items = %v", body["workItemIds"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"localId": 9, "status": "UNDER_REVIEW", "sourceBranch": "feature/work", "targetBranch": "master"})
	})
	cr, err := client.CreateChangeRequest(context.Background(), CreateChangeRequestInput{
		RepositoryID: "42", SourceProjectID: 42, TargetProjectID: 42,
		SourceBranch: "feature/work", TargetBranch: "master", Title: "t", WorkItemIDs: "wi-1,wi-2",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if cr.LocalID != 9 {
		t.Fatalf("cr = %+v", cr)
	}
}

func TestMergeChangeRequestSendsMergeType(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oapi/v1/codeup/organizations/"+testOrg+"/repositories/42/changeRequests/9/merge" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["mergeType"] != MergeTypeNoFastForward {
			t.Errorf("mergeType = %v", body["mergeType"])
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := client.MergeChangeRequest(context.Background(), "42", 9, ""); err != nil {
		t.Fatalf("merge: %v", err)
	}
}

func TestGetChangeRequestMapsStates(t *testing.T) {
	states := map[string]bool{
		StateMerged:      true,
		StateClosed:      false,
		StateUnderReview: false,
	}
	for state, merged := range states {
		state := state
		client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"localId": 9, "status": state})
		})
		cr, err := client.GetChangeRequest(context.Background(), "42", 9)
		if err != nil {
			t.Fatalf("get %s: %v", state, err)
		}
		if cr.Merged() != merged {
			t.Fatalf("%s merged = %v", state, cr.Merged())
		}
	}
}

func TestSearchWorkItemsPostsProjexQuery(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oapi/v1/projex/organizations/"+testOrg+"/workitems:search" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("spaceId") != "space-1" {
			t.Errorf("spaceId = %s", r.URL.Query().Get("spaceId"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["category"] != "Req" {
			t.Errorf("category = %v", body["category"])
		}
		if conditions, ok := body["conditions"].(string); !ok || !strings.Contains(conditions, "CONTAINS") {
			t.Errorf("conditions = %v", body["conditions"])
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"identifier": "wi-1", "subject": "demo", "status": "28"}})
	})
	items, err := client.SearchWorkItems(context.Background(), "space-1", "Req", "demo", 1, 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 1 || items[0].Identifier != "wi-1" {
		t.Fatalf("items = %+v", items)
	}
}

func TestRegionEditionOmitsOrganizationSegment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oapi/v1/codeup/repositories/42/changeRequests/9" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"localId": 9, "status": "MERGED"})
	}))
	defer srv.Close()
	client, err := New(Config{APIBase: srv.URL, Token: "pt-test"}) // no organization id
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cr, err := client.GetChangeRequest(context.Background(), "42", 9)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !cr.Merged() {
		t.Fatalf("state = %s", cr.Status)
	}
}

func TestConnectionErrorIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	client, err := New(Config{APIBase: url, Token: "pt-test"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = client.GetUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "无法连接云效") {
		t.Fatalf("err = %v", err)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport failure must not be an APIError: %v", err)
	}
}
