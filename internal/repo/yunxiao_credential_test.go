package repo

import (
	"encoding/json"
	"strings"
	"testing"

	"agentflow/internal/domain"
)

func TestYunxiaoCredentialRoundTrip(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.PutUser(domain.User{LoginName: "yunxiao-owner"}); err != nil {
		t.Fatal(err)
	}
	user, _ := st.UserByLoginName("yunxiao-owner")
	yunxiaoID := "yx-user-1"
	if _, err := st.UpdateUserCredentials(user.ID, UserCredentialUpdate{
		YunxiaoToken: "pat-secret", YunxiaoUserID: &yunxiaoID,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok := st.User(user.ID)
	if !ok || got.YunxiaoToken != "pat-secret" || got.YunxiaoUserID != yunxiaoID {
		t.Fatalf("stored yunxiao credential = %+v", got)
	}
	if credential := st.YunxiaoCredential(user.ID); credential != "pat-secret" {
		t.Fatalf("YunxiaoCredential = %q", credential)
	}
	// A login refresh must preserve the token and user id.
	if err := st.PutUser(domain.User{ID: user.ID, LoginName: "yunxiao-owner", ChineseName: "刷新"}); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := st.User(user.ID)
	if refreshed.YunxiaoToken != "pat-secret" || refreshed.YunxiaoUserID != yunxiaoID {
		t.Fatalf("refresh lost yunxiao credential: %+v", refreshed)
	}
	public := domain.PublicUser(refreshed)
	raw, _ := json.Marshal(public)
	if strings.Contains(string(raw), "pat-secret") || !public.YunxiaoTokenConfigured || public.YunxiaoToken != "" {
		t.Fatalf("public user leaked yunxiao token: %s", raw)
	}
	cleared, err := st.UpdateUserCredentials(user.ID, UserCredentialUpdate{YunxiaoTokenClear: true})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.YunxiaoToken != "" || st.YunxiaoCredential(user.ID) != "" {
		t.Fatalf("clear left token: %+v", cleared)
	}
}
