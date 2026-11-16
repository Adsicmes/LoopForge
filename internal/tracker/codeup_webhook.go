package tracker

import (
	"log"
	"strings"
	"time"

	"agentflow/internal/domain"
)

// RefreshCodeupRepositoryDeliveries re-reads the delivery state of every merge
// request that belongs to one repository, ignoring the staleness window the
// polling loop uses.
//
// It backs the Codeup webhook: an event on a repository is a strong hint that
// the state just changed, so waiting for the next poll would leave the platform
// showing a merged request as open for minutes.
func (s *Service) RefreshCodeupRepositoryDeliveries(store MRDeliveryStore, repositoryID string) {
	if s == nil || store == nil {
		return
	}
	repository, ok := repositoryByID(store.Repositories(), strings.TrimSpace(repositoryID))
	if !ok || !isCodeupProject(repository.GongfengProjectID) {
		return
	}
	// A far-future cutoff returns every tracked request, including the ones the
	// poller would consider fresh.
	rows, err := store.MRDeliveryStatesNeedingRefresh(time.Now().UTC().Add(365 * 24 * time.Hour))
	if err != nil {
		log.Printf("codeup webhook: list delivery states: %v", err)
		return
	}
	for _, row := range rows {
		if strings.TrimSpace(row.MRProject) != strings.TrimSpace(repository.GongfengProjectID) {
			continue
		}
		if err := s.refreshCodeupDelivery(store, row.MRProject, row.MRIID); err != nil {
			log.Printf("codeup webhook: refresh %s!%d: %v", row.MRProject, row.MRIID, err)
		}
	}
}

func repositoryByID(repositories []domain.Repository, id string) (domain.Repository, bool) {
	for _, repository := range repositories {
		if repository.ID == id {
			return repository, true
		}
	}
	return domain.Repository{}, false
}
