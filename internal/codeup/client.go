// Package codeup is a small client for the Aliyun Yunxiao (云效) OpenAPI as it
// applies to Codeup repositories: personal-access-token auth, repository
// lookup, and merge requests (change requests).
//
// Auth is a personal access token sent in the x-yunxiao-token header. The
// token is never placed in a URL, so it cannot leak through git remotes or
// logs. Every request is bounded by a timeout and a response size limit.
package codeup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the central-edition (中心版) OpenAPI endpoint. Region
// editions use their own domain, for example openapi.cn-hangzhou.aliyuncs.com;
// callers pass it through Config.APIBase.
const DefaultAPIBase = "https://openapi-rdc.aliyuncs.com"

const (
	maxResponseBytes = 8 << 20
	// MR states as the OpenAPI reports them.
	StateUnderDev    = "UNDER_DEV"
	StateUnderReview = "UNDER_REVIEW"
	StateToBeMerged  = "TO_BE_MERGED"
	StateClosed      = "CLOSED"
	StateMerged      = "MERGED"
)

// Config configures one Yunxiao organization's client.
type Config struct {
	// APIBase is the OpenAPI host root, without /oapi/v1. Empty means the
	// central edition.
	APIBase string
	// OrganizationID is required by the central edition and ignored by the
	// region edition.
	OrganizationID string
	// Token is the personal access token (pt-…).
	Token string
	// HTTP is optional; a bounded client is built when it is nil.
	HTTP *http.Client
}

// Client talks to one Yunxiao organization.
type Client struct {
	base string
	org  string
	tok  string
	http *http.Client
}

// New builds a client. An empty token is an error: the OpenAPI has no
// anonymous mode, and failing here keeps the message close to the cause.
func New(cfg Config) (*Client, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("未配置云效个人访问令牌（PAT）")
	}
	base := strings.TrimSpace(cfg.APIBase)
	if base == "" {
		base = DefaultAPIBase
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return nil, fmt.Errorf("云效 API 地址必须以 http(s):// 开头：%s", base)
	}
	client := cfg.HTTP
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			// The OpenAPI answers with 3xx only on misconfiguration; never
			// follow a redirect that would replay the token header.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &Client{base: base, org: strings.TrimSpace(cfg.OrganizationID), tok: token, http: client}, nil
}

// APIError is a non-2xx OpenAPI response.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	switch {
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		return fmt.Sprintf("云效拒绝了个人访问令牌（HTTP %d）：请在 云效 → 个人设置 → 个人访问令牌 确认令牌未过期，并授予代码管理「合并请求/代码库」读写权限", e.Status)
	case e.Status == http.StatusNotFound:
		return fmt.Sprintf("云效返回 404：组织 ID、代码库或合并请求不存在，或令牌没有该资源的权限（%s）", e.detail())
	default:
		return fmt.Sprintf("云效返回 HTTP %d：%s", e.Status, e.detail())
	}
}

func (e *APIError) detail() string {
	switch {
	case e.Message != "" && e.Code != "":
		return e.Code + " " + e.Message
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	default:
		return "无错误详情"
	}
}

// IsNotFound reports a 404 from the OpenAPI.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// IsAuth reports a rejected token.
func IsAuth(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden)
}

// orgPath builds the organization-scoped prefix. The region edition omits it.
func (c *Client) orgPath(group string) string {
	if c.org == "" {
		return "/oapi/v1/" + group
	}
	return "/oapi/v1/" + group + "/organizations/" + url.PathEscape(c.org)
}

// repoPath accepts either a numeric repository id or a path with namespace;
// the OpenAPI expects the path URL-encoded as a single segment.
func repoPath(identity string) string {
	trimmed := strings.TrimSpace(identity)
	if _, err := strconv.Atoi(trimmed); err == nil {
		return trimmed
	}
	return url.PathEscape(strings.Trim(trimmed, "/"))
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-yunxiao-token", c.tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接云效 OpenAPI：%w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("读取云效响应失败：%w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		apiErr := &APIError{Status: res.StatusCode}
		var envelope struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil {
			apiErr.Code = fmt.Sprint(envelope.Code)
			if apiErr.Code == "<nil>" {
				apiErr.Code = ""
			}
			apiErr.Message = strings.TrimSpace(envelope.Message)
			if apiErr.Message == "" {
				apiErr.Message = strings.TrimSpace(envelope.Error)
			}
		}
		return apiErr
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析云效响应失败：%w", err)
	}
	return nil
}

// User is the token owner as /platform/user reports it.
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// GetUser resolves the personal access token to its owner; this is the token
// verification call.
func (c *Client) GetUser(ctx context.Context) (User, error) {
	var u User
	if err := c.do(ctx, http.MethodGet, "/oapi/v1/platform/user", nil, &u); err != nil {
		return User{}, err
	}
	if strings.TrimSpace(u.ID) == "" && strings.TrimSpace(u.Username) == "" {
		return User{}, errors.New("云效令牌校验返回空用户，请确认这是云效个人访问令牌")
	}
	return u, nil
}

// Repository is one Codeup repository.
type Repository struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"pathWithNamespace"`
	WebURL            string `json:"webUrl"`
	HTTPURL           string `json:"httpUrlToRepository"`
	SSHURL            string `json:"sshUrlToRepository"`
	DefaultBranch     string `json:"defaultBranch"`
	Visibility        string `json:"visibilityLevel"`
	NamespaceID       int64  `json:"namespaceId"`
}

// ListRepositories returns one page of repositories the token can see.
func (c *Client) ListRepositories(ctx context.Context, search string, page, perPage int) ([]Repository, int, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(perPage)}, "orderBy": {"last_activity_at"}, "sort": {"desc"}}
	if s := strings.TrimSpace(search); s != "" {
		q.Set("search", s)
	}
	var out struct {
		Total  int          `json:"total"`
		Result []Repository `json:"result"`
	}
	// The central edition routes repositories through the organization; the
	// region edition keeps the same shape without the organization segment.
	if err := c.do(ctx, http.MethodGet, c.orgPath("codeup")+"/repositories?"+q.Encode(), nil, &out); err != nil {
		return nil, 0, err
	}
	return out.Result, out.Total, nil
}

// GetRepository resolves one repository by id or by path with namespace.
func (c *Client) GetRepository(ctx context.Context, identity string) (Repository, error) {
	var repo Repository
	err := c.do(ctx, http.MethodGet, c.orgPath("codeup")+"/repositories/"+repoPath(identity), nil, &repo)
	return repo, err
}

// ChangeRequest is one merge request (新建合并请求).
type ChangeRequest struct {
	LocalID             int64  `json:"localId"`
	ProjectID           int64  `json:"projectId"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	SourceBranch        string `json:"sourceBranch"`
	TargetBranch        string `json:"targetBranch"`
	Status              string `json:"status"`
	DetailURL           string `json:"detailUrl"`
	CreateTime          string `json:"createTime"`
	MergedRevision      string `json:"mergedRevision"`
	AllRequirementsPass bool   `json:"allRequirementsPass"`
}

// Merged reports whether Yunxiao considers the change request merged.
func (cr ChangeRequest) Merged() bool { return strings.EqualFold(cr.Status, StateMerged) }

// Closed reports whether it can no longer be merged.
func (cr ChangeRequest) Closed() bool {
	return strings.EqualFold(cr.Status, StateClosed)
}

// ListChangeRequests returns change requests across the organization. The
// OpenAPI filters by project id, not by repository path and not by branch, so
// callers pass projectIDs (numeric ids) and filter branches themselves.
func (c *Client) ListChangeRequests(ctx context.Context, projectIDs []string, state string, page, perPage int) ([]ChangeRequest, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "perPage": {strconv.Itoa(perPage)}, "orderBy": {"updated_at"}, "sort": {"desc"}}
	if len(projectIDs) > 0 {
		q.Set("projectIds", strings.Join(projectIDs, ","))
	}
	if state != "" {
		q.Set("state", state)
	}
	var out []ChangeRequest
	if err := c.do(ctx, http.MethodGet, c.orgPath("codeup")+"/changeRequests?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetChangeRequest reads one change request by its local id.
func (c *Client) GetChangeRequest(ctx context.Context, repositoryIdentity string, localID int64) (ChangeRequest, error) {
	var cr ChangeRequest
	path := c.orgPath("codeup") + "/repositories/" + repoPath(repositoryIdentity) + "/changeRequests/" + strconv.FormatInt(localID, 10)
	err := c.do(ctx, http.MethodGet, path, nil, &cr)
	return cr, err
}

// CreateChangeRequestInput is the payload CreateChangeRequest accepts. Yunxiao
// requires both the source and target project id even for a same-repository
// merge request.
type CreateChangeRequestInput struct {
	RepositoryID    string
	SourceProjectID int64
	TargetProjectID int64
	SourceBranch    string
	TargetBranch    string
	Title           string
	Description     string
	ReviewerUserIDs []string
	WorkItemIDs     string
}

// CreateChangeRequest opens a merge request and returns it.
func (c *Client) CreateChangeRequest(ctx context.Context, in CreateChangeRequestInput) (ChangeRequest, error) {
	body := map[string]any{
		"sourceProjectId": in.SourceProjectID,
		"targetProjectId": in.TargetProjectID,
		"sourceBranch":    in.SourceBranch,
		"targetBranch":    in.TargetBranch,
		"title":           in.Title,
		"description":     in.Description,
		"createFrom":      "WEB",
	}
	if len(in.ReviewerUserIDs) > 0 {
		body["reviewerUserIds"] = in.ReviewerUserIDs
	}
	if strings.TrimSpace(in.WorkItemIDs) != "" {
		body["workItemIds"] = in.WorkItemIDs
	}
	var cr ChangeRequest
	path := c.orgPath("codeup") + "/repositories/" + repoPath(in.RepositoryID) + "/changeRequests"
	if err := c.do(ctx, http.MethodPost, path, body, &cr); err != nil {
		return ChangeRequest{}, err
	}
	return cr, nil
}

// Merge types the OpenAPI accepts.
const (
	MergeTypeNoFastForward = "no-fast-forward"
	MergeTypeFFOnly        = "ff-only"
	MergeTypeSquash        = "squash"
	MergeTypeRebase        = "rebase"
)

// MergeChangeRequest merges one change request. Yunxiao merges asynchronously
// in some projects; callers re-read the request to confirm the final state.
func (c *Client) MergeChangeRequest(ctx context.Context, repositoryIdentity string, localID int64, mergeType string) error {
	if strings.TrimSpace(mergeType) == "" {
		mergeType = MergeTypeNoFastForward
	}
	path := c.orgPath("codeup") + "/repositories/" + repoPath(repositoryIdentity) + "/changeRequests/" + strconv.FormatInt(localID, 10) + "/merge"
	return c.do(ctx, http.MethodPost, path, map[string]any{"mergeType": mergeType}, nil)
}

// WorkItem is one Yunxiao Projex work item (需求/缺陷/任务).
type WorkItem struct {
	Identifier     string `json:"identifier"`
	Subject        string `json:"subject"`
	Description    string `json:"description"`
	WorkitemTypeID string `json:"workitemTypeIdentifier"`
	Status         string `json:"status"`
	SpaceID        string `json:"spaceIdentifier"`
	GmtCreate      string `json:"gmtCreate"`
}

// SearchWorkItems lists Projex work items in one project or project set. It is
// the issue-import source for a Yunxiao-backed project. category is a work
// item type identifier such as Req (需求) or Bug (缺陷); several types are
// comma-separated. The OpenAPI windows pagination at page*perPage <= 10000.
func (c *Client) SearchWorkItems(ctx context.Context, spaceID, category, subject string, page, perPage int) ([]WorkItem, error) {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 200 {
		perPage = 200
	}
	body := map[string]any{"category": category, "page": page, "perPage": perPage}
	if s := strings.TrimSpace(subject); s != "" {
		conditions, err := json.Marshal(map[string]any{"conditionGroups": [][]map[string]any{{
			{"fieldIdentifier": "subject", "operator": "CONTAINS", "value": []string{s}, "toValue": nil, "className": "string", "format": "input"},
		}}})
		if err != nil {
			return nil, err
		}
		body["conditions"] = string(conditions)
	}
	var out []WorkItem
	path := c.orgPath("projex") + "/workitems:search"
	if strings.TrimSpace(spaceID) != "" {
		path += "?spaceId=" + url.QueryEscape(spaceID)
	}
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
