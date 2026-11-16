package tracker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

const (
	testCodeupOrg   = "60de7a6852743a5162b5f957"
	testCodeupPath  = testCodeupOrg + "/group/demo"
	testCodeupToken = "pt-project-owner"
)

// newCodeupFixture wires a store whose project owner holds a Yunxiao token and
// one Codeup repository bound to the project.
func newCodeupFixture(t *testing.T) (*Service, *repo.Store, domain.Repository) {
	t.Helper()
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "codeup-owner", GitToken: testCodeupToken}); err != nil {
		t.Fatal(err)
	}
	u, _ := st.UserByLoginName("codeup-owner")
	if err := st.PutProject(domain.Project{ID: "proj", Name: "Proj", OwnerID: u.ID}); err != nil {
		t.Fatal(err)
	}
	r := domain.Repository{
		ID:                "codeup-repo",
		ProjectID:         "proj",
		Provider:          domain.CodeupProvider,
		Name:              "demo",
		GongfengProjectID: "codeup:" + testCodeupPath,
		DefaultBranch:     "master",
		WebURL:            "https://codeup.aliyun.com/" + testCodeupPath,
		CloneURL:          "https://codeup.aliyun.com/" + testCodeupPath + ".git",
	}
	if err := st.PutRepository(r); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	return s, st, r
}

func TestCodeupMergeCreatesMergesAndConfirms(t *testing.T) {
	s, _, r := newCodeupFixture(t)
	var created bool
	var merged bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := req.Header.Get("x-yunxiao-token"); got != testCodeupToken {
			t.Errorf("token header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/repositories/"+testCodeupPath:
			_, _ = w.Write([]byte(`{"id":42,"pathWithNamespace":"` + testCodeupPath + `","defaultBranch":"master"}`))
		case req.Method == http.MethodGet && req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/changeRequests":
			if req.URL.Query().Get("projectIds") != "42" {
				t.Errorf("projectIds = %s", req.URL.Query().Get("projectIds"))
			}
			if !created {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"localId":9,"status":"UNDER_REVIEW","sourceBranch":"feature/work","targetBranch":"master"}]`))
		case req.Method == http.MethodPost && req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/repositories/42/changeRequests":
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["sourceBranch"] != "feature/work" || body["targetBranch"] != "master" {
				t.Errorf("create body = %v", body)
			}
			created = true
			_, _ = w.Write([]byte(`{"localId":9,"status":"UNDER_REVIEW","sourceBranch":"feature/work","targetBranch":"master","detailUrl":"https://codeup.aliyun.com/` + testCodeupPath + `/change/9"}`))
		case req.Method == http.MethodPost && req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/repositories/42/changeRequests/9/merge":
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["mergeType"] != "no-fast-forward" {
				t.Errorf("mergeType = %v", body["mergeType"])
			}
			merged = true
			_, _ = w.Write([]byte(`{}`))
		case req.Method == http.MethodGet && req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/repositories/42/changeRequests/9":
			status := "UNDER_REVIEW"
			if merged {
				status = "MERGED"
			}
			_, _ = w.Write([]byte(`{"localId":9,"status":"` + status + `","sourceBranch":"feature/work","targetBranch":"master"}`))
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s.codeupAPIBaseURL = srv.URL

	ref, err := s.EnsureAndMergeBranchMR(r.GongfengProjectID, "feature/work", "master", "", "")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if ref != "#9" {
		t.Fatalf("ref = %q", ref)
	}
	if !created || !merged {
		t.Fatalf("created=%v merged=%v", created, merged)
	}
}

func TestCodeupMergeWithoutTargetNeedsOpenMR(t *testing.T) {
	s, _, r := newCodeupFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.URL.Path == "/oapi/v1/codeup/organizations/"+testCodeupOrg+"/repositories/"+testCodeupPath:
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	s.codeupAPIBaseURL = srv.URL
	if _, err := s.EnsureAndMergeBranchMR(r.GongfengProjectID, "feature/work", "", "", ""); err != ErrNoOpenMR {
		t.Fatalf("err = %v, want ErrNoOpenMR", err)
	}
}

func TestCodeupFindBranchMatchesSourceBranch(t *testing.T) {
	s, _, r := newCodeupFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/oapi/v1/codeup/organizations/" + testCodeupOrg + "/repositories/" + testCodeupPath:
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			_, _ = w.Write([]byte(`[{"localId":3,"status":"TO_BE_MERGED","sourceBranch":"other","targetBranch":"master"},{"localId":11,"status":"UNDER_DEV","sourceBranch":"feature/work","targetBranch":"master","detailUrl":"https://codeup.aliyun.com/` + testCodeupPath + `/change/11"}]`))
		}
	}))
	defer srv.Close()
	s.codeupAPIBaseURL = srv.URL
	mr, ok, err := s.FindBranchMR(r.GongfengProjectID, "feature/work")
	if err != nil || !ok {
		t.Fatalf("find: %+v %v %v", mr, ok, err)
	}
	if mr.IID != 11 || mr.State != "opened" || mr.TargetBranch != "master" {
		t.Fatalf("mr = %+v", mr)
	}
	if mr.URL != "https://codeup.aliyun.com/"+testCodeupPath+"/change/11" {
		t.Fatalf("url = %q", mr.URL)
	}
}

func TestCodeupClientRequiresProjectToken(t *testing.T) {
	// A project owner without a Yunxiao token must be reported before any
	// anonymous OpenAPI call is attempted.
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutUser(domain.User{LoginName: "no-token-owner"}); err != nil {
		t.Fatal(err)
	}
	u, _ := st.UserByLoginName("no-token-owner")
	if err := st.PutProject(domain.Project{ID: "proj2", Name: "Proj", OwnerID: u.ID}); err != nil {
		t.Fatal(err)
	}
	r := domain.Repository{ID: "codeup-repo-2", ProjectID: "proj2", Provider: domain.CodeupProvider, Name: "demo", GongfengProjectID: "codeup:" + testCodeupPath}
	if err := st.PutRepository(r); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	s.ConfigureWithStore(domain.SyncConfig{}, st)
	if _, err := s.codeupClient(r.GongfengProjectID); err == nil {
		t.Fatal("missing token must be reported")
	}
}

func TestCodeupMRStateMapping(t *testing.T) {
	cases := map[string]string{
		"UNDER_DEV":    "opened",
		"UNDER_REVIEW": "opened",
		"TO_BE_MERGED": "opened",
		"MERGED":       "merged",
		"CLOSED":       "closed",
		"somethingnew": "",
	}
	for in, want := range cases {
		if got := codeupMRState(in); got != want {
			t.Fatalf("%s => %q want %q", in, got, want)
		}
	}
}

func TestCodeupMRURLFallsBackToCodeupHost(t *testing.T) {
	got := codeupMRURL("codeup:"+testCodeupPath, 12)
	want := "https://codeup.aliyun.com/" + testCodeupPath + "/change/12"
	if got != want {
		t.Fatalf("url = %q want %q", got, want)
	}
}
