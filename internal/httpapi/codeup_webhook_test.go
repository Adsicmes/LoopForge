package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

const codeupWebhookOrg = "60de7a6852743a5162b5f957"

func webhookHarness(t *testing.T) (*catalogHandlers, *repo.Store, domain.Repository) {
	t.Helper()
	st, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.PutProject(domain.Project{ID: "proj", Name: "Proj"}); err != nil {
		t.Fatal(err)
	}
	repository := domain.Repository{
		ID: "repo-codeup", ProjectID: "proj", Provider: domain.CodeupProvider,
		Name: "demo", GongfengProjectID: "codeup:" + codeupWebhookOrg + "/group/demo",
		WebhookSecret: "s3cret",
	}
	if err := st.PutRepository(repository); err != nil {
		t.Fatal(err)
	}
	return &catalogHandlers{deps: &deps{repo: st}}, st, repository
}

func webhookRequest(t *testing.T, h *catalogHandlers, id, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/codeup/"+id, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Codeup-Token", token)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: id}}
	h.codeupWebhook(c)
	return rec
}

func TestCodeupWebhookRejectsWrongSecret(t *testing.T) {
	h, _, _ := webhookHarness(t)
	for _, token := range []string{"", "wrong", "s3cret "} {
		rec := webhookRequest(t, h, "repo-codeup", token, `{"object_kind":"merge_request"}`)
		if rec.Code == http.StatusOK {
			t.Fatalf("token %q must not be accepted: %s", token, rec.Body.String())
		}
	}
}

func TestCodeupWebhookRejectsRepositoryWithoutSecret(t *testing.T) {
	h, st, _ := webhookHarness(t)
	if err := st.PutRepository(domain.Repository{ID: "repo-plain", ProjectID: "proj", Provider: domain.CodeupProvider, Name: "plain", GongfengProjectID: "codeup:" + codeupWebhookOrg + "/group/plain"}); err != nil {
		t.Fatal(err)
	}
	rec := webhookRequest(t, h, "repo-plain", "s3cret", `{}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCodeupWebhookAcceptsTheConfiguredSecret(t *testing.T) {
	h, _, _ := webhookHarness(t)
	rec := webhookRequest(t, h, "repo-codeup", "s3cret", `{"object_kind":"merge_request","event_name":"mr_merged"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// No sync service in this harness, so the handler must say it did nothing
	// rather than pretend the refresh ran.
	if !strings.Contains(rec.Body.String(), "ignored") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCodeupWebhookReportsUnknownRepository(t *testing.T) {
	h, _, _ := webhookHarness(t)
	rec := webhookRequest(t, h, "missing", "s3cret", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCodeupWebhookSecretIsNotSerialized(t *testing.T) {
	_, st, repository := webhookHarness(t)
	stored, ok := st.Repository(repository.ID)
	if !ok || stored.WebhookSecret != "s3cret" {
		t.Fatalf("stored secret = %+v", stored)
	}
	encoded, err := json.Marshal(domain.PublicRepository(stored))
	if err != nil {
		t.Fatal(err)
	}
	if body := string(encoded); strings.Contains(body, "s3cret") {
		t.Fatalf("public repository leaked the secret: %s", body)
	}
}

func TestCodeupWebhookSecretSurvivesRepositoryEdit(t *testing.T) {
	h, st, repository := webhookHarness(t)
	// An ordinary edit comes back without the secret, exactly as the UI sends it.
	payload := `{"id":"repo-codeup","projectId":"proj","provider":"codeup","name":"renamed","gongfengProjectId":"` + repository.GongfengProjectID + `","defaultBranch":"master"}`
	req := httptest.NewRequest(http.MethodPut, "/api/repositories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	h.repositories(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, _ := st.Repository("repo-codeup")
	if stored.WebhookSecret != "s3cret" {
		t.Fatalf("edit dropped the webhook secret: %+v", stored)
	}
	if stored.Name != "renamed" {
		t.Fatalf("edit was not applied: %+v", stored)
	}
}

func TestCodeupEventSummaryPrefersKnownFields(t *testing.T) {
	cases := map[string]string{
		`{"object_kind":"merge_request"}`:                   "merge_request",
		`{"event_name":"push"}`:                             "push",
		`{"objectType":"merge_request","eventType":"open"}`: "open",
		`{}`:       "",
		`not json`: "",
	}
	for body, want := range cases {
		if got := codeupEventSummary([]byte(body)); got != want {
			t.Fatalf("body %s => %q, want %q", body, got, want)
		}
	}
}
