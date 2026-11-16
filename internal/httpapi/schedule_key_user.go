package httpapi

import (
	"strings"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

// scheduleKeyStore is what scheduleKeyUser needs from the repo. user resolves
// the stored user; the shared-key lookups borrow the platform shared records.
// *repo.Store satisfies it through storeKeyStore; callers wrap with
// asScheduleKeyStore.
type scheduleKeyStore interface {
	SharedCodeBuddyKey() (repo.SharedCodeBuddyKey, bool)
	SharedOpenCodeKey() (repo.SharedOpenCodeKey, bool)
	User(id int64) (domain.User, bool)
}

// storeKeyStore adapts a *repo.Store to scheduleKeyStore.
type storeKeyStore struct{ store *repo.Store }

func (s storeKeyStore) SharedCodeBuddyKey() (repo.SharedCodeBuddyKey, bool) {
	if s.store == nil {
		return repo.SharedCodeBuddyKey{}, false
	}
	return s.store.SharedCodeBuddyKey()
}

func (s storeKeyStore) SharedOpenCodeKey() (repo.SharedOpenCodeKey, bool) {
	if s.store == nil {
		return repo.SharedOpenCodeKey{}, false
	}
	return s.store.SharedOpenCodeKey()
}

func (s storeKeyStore) User(id int64) (domain.User, bool) {
	if s.store == nil {
		return domain.User{}, false
	}
	return s.store.User(id)
}

// asScheduleKeyStore wraps a store; nil stays nil so callers can pass
// h.repo directly.
func asScheduleKeyStore(store *repo.Store) scheduleKeyStore {
	if store == nil {
		return nil
	}
	return storeKeyStore{store: store}
}

// scheduleKeyUser is user with the key a scheduled run for engine uses: their
// own, else a platform shared key (whose allowed models are returned as
// shared). OpenCode borrows the shared OpenCode credential, CodeBuddy the
// shared CodeBuddy key; every other CLI has no shared fallback. Everything
// but the key stays the user's own. ok is false when neither exists.
func scheduleKeyUser(r scheduleKeyStore, user domain.User, engine string) (domain.User, []string, bool) {
	if key, _ := assistantEngineCredential(user, engine); key != "" {
		return user, nil, true
	}
	switch engine {
	case assistantEngineCodeBuddy:
		if r == nil {
			return user, nil, false
		}
		key, found := r.SharedCodeBuddyKey()
		if !found || strings.TrimSpace(key.APIKey) == "" || len(key.Models) == 0 || (key.SponsorID != 0 && key.SponsorID == user.ID) {
			return user, nil, false
		}
		user.CodeBuddyAPIKey = key.APIKey
		user.CodeBuddyInternetEnvironment = key.InternetEnvironment
		return user, key.Models, true
	case assistantEngineOpenCode:
		if r == nil {
			return user, nil, false
		}
		credential, ok := openCodeUserCredential(r, user.ID)
		if !ok {
			return user, nil, false
		}
		user = withOpenCodeCredential(user, credential)
		var shared []string
		if credential.Sponsored {
			if key, found := r.SharedOpenCodeKey(); found && len(key.Models) > 0 {
				shared = key.Models
			}
		}
		return user, shared, true
	}
	return user, nil, false
}

// openCodeUserCredential is openCodeProviderCredential for a scheduleKeyStore:
// the user's own OpenCode credential, else the platform shared one.
func openCodeUserCredential(r scheduleKeyStore, userID int64) (runtime.ProviderCredential, bool) {
	if r == nil || userID <= 0 {
		return runtime.ProviderCredential{}, false
	}
	if user, ok := r.User(userID); ok {
		cred := user.AgentCLICredentials[runtime.AgentCLIOpenCode]
		if strings.TrimSpace(cred.APIKey) != "" {
			return runtime.ProviderCredential{
				APIKey: cred.APIKey, BaseURL: cred.BaseURL, Protocol: cred.Protocol, Models: cred.Models,
			}, true
		}
	}
	shared, ok := r.SharedOpenCodeKey()
	if !ok {
		return runtime.ProviderCredential{}, false
	}
	credential := runtime.ProviderCredential{
		APIKey: shared.APIKey, BaseURL: shared.BaseURL, Protocol: shared.ProtocolOrDefault(), Sponsored: true,
	}
	for _, id := range shared.Models {
		credential.Models = append(credential.Models, domain.CLIModel{ID: id, Name: id})
	}
	return credential, true
}

// assistantSponsoredModelFor limits shared-key turns to the allowed models.
// An empty request picks the default; any other model is refused.
func assistantSponsoredModelFor(shared []string, requested string) (string, bool) {
	return repo.SharedCodeBuddyKey{Models: shared}.Model(requested)
}
