package repo

import (
	"agentflow/internal/domain"
	"strings"
)

// Reprice a copy for analytics; raw adapter amounts and persisted Run/Node rows
// remain intact. A missing node/model/token split makes coverage incomplete.
func analyticsPricedRun(run domain.Run) domain.Run {
	if run.IsWorkspace() {
		// Conversation workspaces have no nodes; AddConversationRunUsage
		// prices each turn with its model when it is recorded.
		return run
	}
	nodes := make(map[string]*domain.NodeRun, len(run.Nodes))
	var input, output, read, write, total int64
	var cost float64
	known := true
	included, billable := 0, 0
	for id, original := range run.Nodes {
		if original == nil {
			continue
		}
		node := *original
		usage := node.Usage
		zeroUsage := usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0 && usage.CostUSD == 0
		if !zeroUsage {
			billable++
			model := strings.ToLower(strings.TrimSpace(node.Model))
			inclusive := node.Provider == "codex" || node.Provider == "openai" || strings.HasPrefix(model, "gpt-")
			if domain.IsIncludedAuthMode(node.AuthMode) {
				node.Usage = domain.IncludedUsage(node.Usage)
				included++
			} else {
				node.Usage = domain.AnalyticsPricedUsage(node.Model, node.Usage, inclusive)
			}
			known = known && usageCostKnown(node.Usage)
		}
		nodes[id] = &node
		input += node.Usage.InputTokens
		output += node.Usage.OutputTokens
		read += node.Usage.CacheReadTokens
		write += node.Usage.CacheWriteTokens
		total += node.Usage.TotalTokens
		cost += node.Usage.CostUSD
	}
	u := run.Usage
	if input != u.InputTokens || output != u.OutputTokens || read != u.CacheReadTokens || write != u.CacheWriteTokens || total != u.TotalTokens {
		known = false
	}
	if len(nodes) == 0 && (u.CostUSD != 0 || u.ReasoningTokens != 0) {
		known = false
	}
	run.Nodes = nodes
	run.Usage.CostUSD = cost
	switch {
	case !known:
		run.Usage.CostStatus = "unknown"
	case billable > 0 && included == billable:
		run.Usage.CostStatus = "included"
	default:
		run.Usage.CostStatus = "estimated"
	}
	return run
}

func analyticsInsightPricedUsage(u domain.InsightUsage) domain.TokenUsage {
	usage := domain.TokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, TotalTokens: u.InputTokens + u.OutputTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, ReasoningTokens: u.ReasoningTokens, CostStatus: u.CostStatus}
	if u.CostUSD != nil {
		usage.CostUSD = *u.CostUSD
	}
	return domain.AnalyticsPricedUsage(u.Model, usage, true)
}

// The legacy USD fields remain USD equivalents; CNY is the reporting currency.
func applyAnalyticsCNY(out *domain.AnalyticsSnapshot) {
	fx := domain.AnalyticsUSDToCNY
	out.Summary.CostCurrency, out.Summary.USDToCNY, out.Summary.CostPricingVersion = "CNY", fx, domain.AnalyticsPricingVersion
	out.Summary.TotalCostCNY = out.Summary.TotalCostUSD * fx
	out.Summary.CostReportedCNY = out.Summary.CostReportedUSD * fx
	out.Summary.ConversationCostCNY = out.Summary.ConversationCostUSD * fx
	out.Delivery.TotalCostCNY = out.Delivery.TotalCostUSD * fx
	for i := range out.Trend {
		out.Trend[i].CostCNY = out.Trend[i].CostUSD * fx
		out.Trend[i].ConversationCostCNY = out.Trend[i].ConversationCostUSD * fx
	}
	for i := range out.WeekHour {
		out.WeekHour[i].CostCNY = out.WeekHour[i].CostUSD * fx
	}
	for _, list := range [][]domain.AnalyticsBreakdown{out.Workflows, out.Agents, out.Executors, out.Models, out.Nodes, out.IssueSources, out.Categories} {
		for i := range list {
			list[i].CostCNY = list[i].CostUSD * fx
		}
	}
	for i := range out.Deliveries {
		out.Deliveries[i].CostCNY = out.Deliveries[i].CostUSD * fx
	}
	for i := range out.WorkflowVersions {
		out.WorkflowVersions[i].CostCNY = out.WorkflowVersions[i].CostUSD * fx
	}
	for i := range out.UserUsage {
		out.UserUsage[i].CostCNY = out.UserUsage[i].CostUSD * fx
	}
	for i := range out.ProjectUsage {
		out.ProjectUsage[i].CostCNY = out.ProjectUsage[i].CostUSD * fx
	}
	for i := range out.RunTraces {
		out.RunTraces[i].CostCNY = out.RunTraces[i].CostUSD * fx
	}
}
