package domain

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// DeliveryMR is one merge request delivered for a work item. A multi-repository
// SOP delivers at most one MR per run repository.
type DeliveryMR struct {
	RepositoryID string `json:"repositoryId,omitempty"`
	Repository   string `json:"repository,omitempty"`
	Project      string `json:"project,omitempty"`
	IID          int    `json:"iid,omitempty"`
	URL          string `json:"url"`
	// State is the classified delivery state (merged, integrated, opened, closed, unknown).
	State        string `json:"state,omitempty"`
	TargetBranch string `json:"targetBranch,omitempty"`
	// GongfengState is the raw Gongfeng state from a live lookup; empty when
	// the MR was only recovered from artifact text.
	GongfengState string `json:"-"`
}

// YunxiaoWebBase is the web base used to build Codeup merge request links.
// Region deployments override it, e.g. https://<org>.devops.aliyuncs.com.
var YunxiaoWebBase = "https://" + CodeupHost

func init() {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTFLOW_YUNXIAO_WEB_BASE")), "/"); base != "" {
		YunxiaoWebBase = base
	}
}

// MRURL returns the canonical web URL of a hosted merge request. provider is
// the repository provider (github, yunxiao, ...); the project identity alone
// decides when provider is empty or generic.
func MRURL(provider, project string, iid int) string {
	project = strings.Trim(strings.TrimSpace(project), "/")
	if project == "" || iid <= 0 {
		return ""
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == RepositoryProviderYunxiao || strings.HasPrefix(project, "codeup:") {
		path := strings.TrimPrefix(project, "codeup:")
		base := strings.TrimRight(strings.TrimSpace(YunxiaoWebBase), "/")
		if base == "" {
			base = "https://" + CodeupHost
		}
		return fmt.Sprintf("%s/%s/change/%d", base, path, iid)
	}
	if provider == "github" || strings.HasPrefix(project, "github:") {
		return fmt.Sprintf("https://github.com/%s/pull/%d", strings.TrimPrefix(project, "github:"), iid)
	}
	return fmt.Sprintf("https://%s/%s/-/merge_requests/%d", GongfengRepoHost, project, iid)
}

// GongfengMRURL is the compatibility wrapper of MRURL for callers that only
// know the project identity.
func GongfengMRURL(project string, iid int) string {
	return MRURL("", project, iid)
}

// AggregateDeliveryMRState folds per-MR states into the work item label state.
// Closed MRs only win when every MR is closed; otherwise the item is merged
// when all live MRs reached the default branch, integrated when all reached at
// least the integrate baseline, and the first still-open state otherwise.
func AggregateDeliveryMRState(mrs []DeliveryMR) string {
	if len(mrs) == 0 {
		return ""
	}
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return "unknown"
		}
		return s
	}
	if len(mrs) == 1 {
		return norm(mrs[0].State)
	}
	live, merged, integrated := 0, 0, 0
	pending := ""
	for _, mr := range mrs {
		switch st := norm(mr.State); st {
		case "closed":
			continue
		case "merged":
			merged++
		case "integrated":
			integrated++
		default:
			if pending == "" || (pending == "unknown" && st != "unknown") {
				pending = st
			}
		}
		live++
	}
	switch {
	case live == 0:
		return "closed"
	case merged == live:
		return "merged"
	case merged+integrated == live:
		return "integrated"
	default:
		return pending
	}
}

// DeliveryMRList returns the work item's delivery MRs. Items written before
// multi-MR support only carry DeliveryMRURL / DeliveryMRState.
func (i Issue) DeliveryMRList() []DeliveryMR {
	if len(i.DeliveryMRs) > 0 {
		return append([]DeliveryMR(nil), i.DeliveryMRs...)
	}
	url := strings.TrimSpace(i.DeliveryMRURL)
	if url == "" {
		return nil
	}
	mr := DeliveryMR{URL: url, State: strings.TrimSpace(i.DeliveryMRState)}
	if project, iid, _, ok := ParseGongfengMR(url); ok {
		mr.Project, mr.IID = project, iid
	}
	return []DeliveryMR{mr}
}

// HasDeliveryMR reports whether url is one of the work item's delivery MRs.
func (i Issue) HasDeliveryMR(url string) bool {
	for _, mr := range i.DeliveryMRList() {
		if SameGongfengMR(mr.URL, url) {
			return true
		}
	}
	return false
}

// SetDeliveryMRs replaces the delivery MRs and keeps the legacy single-MR
// fields in sync: DeliveryMRURL is the first MR, DeliveryMRState the aggregate.
func (i *Issue) SetDeliveryMRs(mrs []DeliveryMR) {
	out := make([]DeliveryMR, 0, len(mrs))
	for _, mr := range mrs {
		mr.URL = strings.TrimSpace(mr.URL)
		if mr.URL == "" {
			continue
		}
		dup := false
		for _, cur := range out {
			if SameGongfengMR(cur.URL, mr.URL) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, mr)
		}
	}
	if len(out) == 0 {
		i.DeliveryMRs = nil
		i.DeliveryMRURL = ""
		i.DeliveryMRState = ""
		return
	}
	i.DeliveryMRs = out
	i.DeliveryMRURL = out[0].URL
	i.DeliveryMRState = AggregateDeliveryMRState(out)
}

// SetDeliveryMRState updates one delivery MR's state. A work item without
// any MR adopts url as its only MR. It reports false when url belongs to a
// different MR than the ones already recorded.
func (i *Issue) SetDeliveryMRState(url, state string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}
	list := i.DeliveryMRList()
	if len(list) == 0 {
		mr := DeliveryMR{URL: url, State: state}
		if project, iid, _, ok := ParseGongfengMR(url); ok {
			mr.Project, mr.IID = project, iid
		}
		i.SetDeliveryMRs([]DeliveryMR{mr})
		return true
	}
	for idx := range list {
		if SameGongfengMR(list[idx].URL, url) {
			list[idx].State = state
			i.SetDeliveryMRs(list)
			return true
		}
	}
	return false
}

// ResolveRunDeliveryMRs picks the delivery MR of every Gongfeng repository in
// the run. byBranch is the authoritative lookup (Gongfeng MR whose source is
// the repository work branch); MR-bearing artifacts are only a fallback and
// must belong to the same Gongfeng project. Runs without any Gongfeng
// repository fall back to the best MR per project in artifacts, then telemetry URLs.
func ResolveRunDeliveryMRs(run Run, byBranch func(RepositoryContext) (DeliveryMR, bool)) []DeliveryMR {
	candidates := runArtifactMRCandidates(run)
	repos := []RepositoryContext{}
	for _, rc := range run.RepositoryContexts() {
		if strings.TrimSpace(rc.GongfengProjectID) != "" && !rc.AlignOnly {
			repos = append(repos, rc)
		}
	}
	if len(repos) == 0 {
		out := []DeliveryMR{}
		seen := map[string]bool{}
		add := func(mr DeliveryMR) {
			project := strings.ToLower(mr.Project)
			if !seen[project] {
				seen[project] = true
				out = append(out, mr)
			}
		}
		if len(candidates) > 0 {
			for _, candidate := range candidates {
				add(candidate.mr)
			}
			return out
		}
		for _, obs := range run.Telemetry {
			if url, ok := obs.Attributes["url"].(string); ok {
				if project, iid, parsed, ok := ParseGongfengMR(url); ok {
					add(DeliveryMR{Project: project, IID: iid, URL: parsed})
				}
			}
		}
		return out
	}
	out := []DeliveryMR{}
	for _, rc := range repos {
		project := strings.TrimSpace(rc.GongfengProjectID)
		var picked DeliveryMR
		found := false
		if byBranch != nil && strings.TrimSpace(rc.WorkBranch) != "" {
			picked, found = byBranch(rc)
		}
		if !found {
			for _, c := range candidates {
				if strings.EqualFold(c.mr.Project, project) {
					picked, found = c.mr, true
					break
				}
			}
		}
		if !found {
			continue
		}
		if picked.Project == "" {
			picked.Project = project
		}
		if picked.URL == "" {
			picked.URL = GongfengMRURL(picked.Project, picked.IID)
		}
		picked.RepositoryID = rc.RepositoryID
		picked.Repository = rc.Name
		out = append(out, picked)
	}
	return out
}

type artifactMRCandidate struct {
	mr       DeliveryMR
	priority int
	order    int
}

// runArtifactMRCandidates lists MRs quoted by MR-bearing artifacts, best first:
// higher MRArtifactPriority, then later artifacts (a rework's newest MR).
func runArtifactMRCandidates(run Run) []artifactMRCandidate {
	best := map[string]artifactMRCandidate{}
	for idx, art := range run.Artifacts {
		pri := MRArtifactPriority(art.Name, art.Path, art.NodeID)
		if pri <= 0 {
			continue
		}
		for _, ref := range ParseGongfengMRs(art.Content) {
			key := fmt.Sprintf("%s#%d", strings.ToLower(ref.Project), ref.IID)
			if cur, ok := best[key]; ok && cur.priority >= pri {
				continue
			}
			best[key] = artifactMRCandidate{mr: DeliveryMR{Project: ref.Project, IID: ref.IID, URL: ref.URL}, priority: pri, order: idx}
		}
	}
	out := make([]artifactMRCandidate, 0, len(best))
	for _, c := range best {
		out = append(out, c)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].priority != out[b].priority {
			return out[a].priority > out[b].priority
		}
		if out[a].order != out[b].order {
			return out[a].order > out[b].order
		}
		return out[a].mr.IID > out[b].mr.IID
	})
	return out
}
