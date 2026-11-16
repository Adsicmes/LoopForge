package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

const testCodeupOrgID = "60de7a6852743a5162b5f957"

// codeupListHarness builds a request against the Codeup repository picker for a
// signed-in user whose Git credential slot holds a Yunxiao token.
func codeupListHarness(t *testing.T, query, token string, handler http.HandlerFunc) (*httptest.ResponseRecorder, *httptest.Server) {
	t.Helper()
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "codeup-picker", GitToken: token}); err != nil {
		t.Fatal(err)
	}
	user, ok := st.UserByLoginName("codeup-picker")
	if !ok {
		t.Fatal("user was not stored")
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv(codeupAPIBaseEnv, srv.URL)

	ctx := context.WithValue(context.Background(), requestUserContextKey{}, domainUserToAuth(user))
	req := httptest.NewRequest(http.MethodGet, "/api/codeup/repositories"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	h := &usersHandlers{deps: &deps{repo: st}}
	h.codeupRepositories(c)
	return rec, srv
}

func TestCodeupRepositoriesListsNormalizedRepositories(t *testing.T) {
	rec, _ := codeupListHarness(t, "?org="+testCodeupOrgID+"&search=demo", "pt-picker", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-yunxiao-token"); got != "pt-picker" {
			t.Errorf("token header = %q", got)
		}
		if !strings.Contains(r.URL.Path, "/organizations/"+testCodeupOrgID+"/repositories") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("search") != "demo" {
			t.Errorf("search = %s", r.URL.Query().Get("search"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 2,
			"result": []map[string]any{
				{"id": 1, "name": "demo", "pathWithNamespace": testCodeupOrgID + "/group/demo", "defaultBranch": "master", "visibilityLevel": "private"},
				{"id": 2, "name": "", "pathWithNamespace": testCodeupOrgID + "/group/blank-name"},
			},
		})
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Repositories []struct {
			Project       string `json:"project"`
			Name          string `json:"name"`
			WebURL        string `json:"webUrl"`
			CloneURL      string `json:"cloneUrl"`
			DefaultBranch string `json:"defaultBranch"`
		} `json:"repositories"`
		Organization string `json:"organization"`
		Total        int    `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body: %v (%s)", err, rec.Body.String())
	}
	if payload.Organization != testCodeupOrgID || payload.Total != 2 {
		t.Fatalf("payload = %+v", payload)
	}
	if len(payload.Repositories) != 2 {
		t.Fatalf("repositories = %+v", payload.Repositories)
	}
	first := payload.Repositories[0]
	if first.Project != "codeup:"+testCodeupOrgID+"/group/demo" || first.DefaultBranch != "master" {
		t.Fatalf("first = %+v", first)
	}
	if first.CloneURL != "https://codeup.aliyun.com/"+testCodeupOrgID+"/group/demo.git" {
		t.Fatalf("clone url = %q", first.CloneURL)
	}
	// The API can omit the display name; the picker still needs a label.
	if payload.Repositories[1].Name != "blank-name" {
		t.Fatalf("fallback name = %q", payload.Repositories[1].Name)
	}
}

func TestCodeupRepositoriesTakesOrganizationFromAddress(t *testing.T) {
	rec, _ := codeupListHarness(t, "?repo=https%3A%2F%2Fcodeup.aliyun.com%2F"+testCodeupOrgID+"%2Fgroup%2Fdemo.git", "pt-picker", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"result":[]}`))
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), testCodeupOrgID) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCodeupRepositoriesRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		token  string
		status int
		want   string
	}{
		{name: "no organization", query: "", token: "pt", status: http.StatusBadRequest, want: "组织 ID"},
		{name: "malformed organization", query: "?org=not-an-org", token: "pt", status: http.StatusBadRequest, want: "格式不正确"},
		{name: "no token", query: "?org=" + testCodeupOrgID, token: "", status: http.StatusBadRequest, want: "个人访问令牌"},
	}
	for _, tc := range cases {
		rec, _ := codeupListHarness(t, tc.query, tc.token, func(w http.ResponseWriter, r *http.Request) {
			t.Error("the OpenAPI must not be called for invalid input")
		})
		if rec.Code != tc.status {
			t.Fatalf("%s: status = %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: body = %s", tc.name, rec.Body.String())
		}
	}
}

func TestCodeupRepositoriesReportsRejectedToken(t *testing.T) {
	rec, _ := codeupListHarness(t, "?org="+testCodeupOrgID, "pt-bad", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"token invalid"}`))
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "个人访问令牌") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}
