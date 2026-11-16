// Package yunxiao is a minimal client for the Aliyun Yunxiao (云效) OpenAPI.
// It covers the Codeup repository and change-request endpoints and the Projex
// workitem endpoints used by LoopForge, and depends only on the standard
// library.
package yunxiao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the central-station (中心站) API host.
	DefaultBaseURL = "https://openapi-rdc.aliyuncs.com"
	// DefaultTimeout bounds every HTTP call, including redirects and retries.
	DefaultTimeout = 25 * time.Second

	// maxResponseBytes caps how much of any response body is read.
	maxResponseBytes = 8 << 20
	// maxRetries is the number of retries after the first attempt for 429/5xx.
	maxRetries = 2
	// maxErrorMessage bounds the response excerpt embedded in an error.
	maxErrorMessage = 500
	// defaultMaxPages bounds ListAll* when the caller does not pass a limit.
	defaultMaxPages = 50
)

// Client talks to one Yunxiao station with one personal access token. The
// token is never written to logs or error messages.
type Client struct {
	baseURL        string
	organizationID string
	token          string
	http           *http.Client
	central        bool
}

// New builds a client for base (DefaultBaseURL when empty), organization org
// and personal access token token. A base containing /oapi/v1 or the central
// host openapi-rdc.aliyuncs.com is treated as the central station; anything
// else is treated as a region station (e.g. https://<org>.devops.aliyuncs.com).
func New(base, org, token string) *Client {
	base = strings.TrimSpace(base)
	if base == "" {
		base = DefaultBaseURL
	}
	c := &Client{
		baseURL:        apiPrefix(base),
		organizationID: strings.TrimSpace(org),
		token:          strings.TrimSpace(token),
		central:        isCentralBase(base),
	}
	c.http = &http.Client{
		Timeout: DefaultTimeout,
		// Tokens travel in a header; never replay a request to another host.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return c
}

// isCentralBase reports whether base addresses the central station.
func isCentralBase(base string) bool {
	lowered := strings.ToLower(base)
	if strings.Contains(lowered, "/oapi/v1") {
		return true
	}
	return strings.Contains(lowered, "openapi-rdc.aliyuncs.com")
}

// apiPrefix normalizes base to the versioned API root.
func apiPrefix(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.Contains(base, "/oapi/v1") {
		return base
	}
	return base + "/oapi/v1"
}

// codeupRoot is the station-specific root of the Codeup API.
func (c *Client) codeupRoot() string {
	root := c.baseURL + "/codeup"
	if c.central {
		root += "/organizations/" + url.PathEscape(c.organizationID)
	}
	return root
}

// projexRoot is the station-specific root of the Projex API.
func (c *Client) projexRoot() string {
	root := c.baseURL + "/projex"
	if c.central {
		root += "/organizations/" + url.PathEscape(c.organizationID)
	}
	return root
}

// CodeupPath joins parts under the Codeup API root, escaping each part.
func (c *Client) CodeupPath(parts ...string) string {
	return joinPath(c.codeupRoot(), parts...)
}

// ProjexPath joins parts under the Projex API root, escaping each part.
func (c *Client) ProjexPath(parts ...string) string {
	return joinPath(c.projexRoot(), parts...)
}

// repositoryPath builds repositories/{repositoryID}/<parts> under the Codeup
// root. repositoryID may be a numeric ID or an already URL-encoded full path.
func (c *Client) repositoryPath(repositoryID string, parts ...string) string {
	base := c.codeupRoot() + "/repositories/" + escapeRepositoryID(repositoryID)
	return joinPath(base, parts...)
}

// escapeRepositoryID keeps a pre-encoded full path intact and percent-encodes
// a raw path such as group/sub/repo into a single path segment.
func escapeRepositoryID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "%") {
		return id
	}
	return url.PathEscape(id)
}

func joinPath(base string, parts ...string) string {
	for _, part := range parts {
		if part == "" {
			continue
		}
		base += "/" + url.PathEscape(part)
	}
	return base
}

func withQuery(target string, query url.Values) string {
	if len(query) == 0 {
		return target
	}
	return target + "?" + query.Encode()
}

// PageInfo is the pagination state reported by the Yunxiao response headers.
type PageInfo struct {
	Total      int
	TotalPages int
	NextPage   int
}

// HasNext reports whether another page is available.
func (p PageInfo) HasNext() bool { return p.NextPage > 0 }

func pageInfoFrom(header http.Header, page int) PageInfo {
	info := PageInfo{
		Total:      headerInt(header, "x-total"),
		TotalPages: headerInt(header, "x-total-pages"),
		NextPage:   headerInt(header, "x-next-page"),
	}
	if info.NextPage == 0 && page > 0 && info.TotalPages > page {
		info.NextPage = page + 1
	}
	return info
}

func headerInt(header http.Header, key string) int {
	value := strings.TrimSpace(header.Get(key))
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// APIError is a non-2xx Yunxiao response. Message is the truncated, token-free
// response excerpt; Hint is an actionable hint for auth failures.
type APIError struct {
	StatusCode int
	Message    string
	Hint       string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("云效接口返回 HTTP %d", e.StatusCode)
	if e.Message != "" {
		msg += "：" + e.Message
	}
	if e.Hint != "" {
		msg += "（" + e.Hint + "）"
	}
	return msg
}

func (c *Client) get(ctx context.Context, target string, query url.Values) ([]byte, http.Header, error) {
	return c.do(ctx, http.MethodGet, withQuery(target, query), nil)
}

func (c *Client) post(ctx context.Context, target string, payload any) ([]byte, http.Header, error) {
	return c.do(ctx, http.MethodPost, target, payload)
}

// do sends one logical request, retrying 429 and 5xx at most maxRetries times.
// Authentication failures are never retried.
func (c *Client) do(ctx context.Context, method, target string, payload any) ([]byte, http.Header, error) {
	if c.token == "" {
		return nil, nil, fmt.Errorf("未配置云效访问令牌，请先在个人配置中添加 PAT")
	}
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("序列化云效请求失败：%w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
		if err != nil {
			return nil, nil, fmt.Errorf("构造云效请求失败：%w", err)
		}
		req.Header.Set("x-yunxiao-token", c.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("无法连接云效：%s", redact(err.Error(), c.token))
		}
		data, readErr := readBody(resp)
		resp.Body.Close()
		if readErr != nil {
			return nil, nil, readErr
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return data, resp.Header, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, nil, c.apiError(resp.StatusCode, data)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			if attempt >= maxRetries {
				return nil, nil, c.apiError(resp.StatusCode, data)
			}
			if err := sleepContext(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, c.apiError(resp.StatusCode, data)
		}
	}
}

func (c *Client) apiError(status int, data []byte) *APIError {
	return &APIError{
		StatusCode: status,
		Message:    c.excerpt(data),
		Hint:       authHint(status),
	}
}

func authHint(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "云效访问令牌无效或已过期，请重新配置 PAT"
	case http.StatusForbidden:
		return "云效访问令牌无权访问该资源，请检查仓库或空间权限"
	default:
		return ""
	}
}

// excerpt turns a response body into a single-line, token-free, bounded
// message safe to embed in an error.
func (c *Client) excerpt(data []byte) string {
	msg := redact(string(data), c.token)
	msg = strings.Join(strings.Fields(msg), " ")
	return truncate(msg, maxErrorMessage)
}

func readBody(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取云效响应失败：%w", err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("云效响应超过 %d MiB 上限", maxResponseBytes>>20)
	}
	return data, nil
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if delay, ok := parseRetryAfter(retryAfter); ok && delay > 0 {
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		return delay
	}
	base := 100 * time.Millisecond * time.Duration(1<<attempt)
	jitter := time.Duration(rand.Int64N(int64(50 * time.Millisecond)))
	return base + jitter
}

func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay < 0 {
			delay = 0
		}
		return delay, true
	}
	return 0, false
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func redact(message, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "***")
}

func truncate(message string, limit int) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= limit {
		return message
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// GetRepository resolves one repository by numeric ID or URL-encoded full
// path (group/sub/repo).
func (c *Client) GetRepository(ctx context.Context, repositoryID string) (Repository, error) {
	data, _, err := c.get(ctx, c.repositoryPath(repositoryID), nil)
	if err != nil {
		return Repository{}, err
	}
	return decodeOne[Repository](data)
}

// ListOrganizations lists the organizations of the token's user. The endpoint
// exists on the central station only.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	if !c.central {
		return nil, fmt.Errorf("云效 Region 站不支持组织列表接口，请使用中心站 %s", DefaultBaseURL)
	}
	data, _, err := c.get(ctx, c.baseURL+"/platform/organizations", nil)
	if err != nil {
		return nil, err
	}
	return decodeList[Organization](data)
}

// ListChangeRequests returns one page of change requests. state is opened,
// merged or closed; empty means no state filter. page and perPage are sent
// only when positive.
func (c *Client) ListChangeRequests(ctx context.Context, projectIDs []int, state string, page, perPage int) ([]ChangeRequest, error) {
	items, _, err := c.ListChangeRequestsPage(ctx, projectIDs, state, page, perPage)
	return items, err
}

// ListChangeRequestsPage also returns the pagination headers.
func (c *Client) ListChangeRequestsPage(ctx context.Context, projectIDs []int, state string, page, perPage int) ([]ChangeRequest, PageInfo, error) {
	query := url.Values{}
	if len(projectIDs) > 0 {
		ids := make([]string, 0, len(projectIDs))
		for _, id := range projectIDs {
			ids = append(ids, strconv.Itoa(id))
		}
		query.Set("projectIds", strings.Join(ids, ","))
	}
	if state != "" {
		query.Set("state", state)
	}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		query.Set("perPage", strconv.Itoa(perPage))
	}

	data, header, err := c.get(ctx, c.CodeupPath("changeRequests"), query)
	if err != nil {
		return nil, PageInfo{}, err
	}
	items, err := decodeList[ChangeRequest](data)
	if err != nil {
		return nil, PageInfo{}, err
	}
	return items, pageInfoFrom(header, page), nil
}

// ListAllChangeRequests follows pagination up to maxPages (default 50).
func (c *Client) ListAllChangeRequests(ctx context.Context, projectIDs []int, state string, perPage, maxPages int) ([]ChangeRequest, error) {
	if perPage <= 0 {
		perPage = 100
	}
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}
	var all []ChangeRequest
	page := 1
	for fetched := 0; page > 0 && fetched < maxPages; fetched++ {
		items, info, err := c.ListChangeRequestsPage(ctx, projectIDs, state, page, perPage)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next := info.NextPage
		if next <= page {
			if len(items) >= perPage {
				next = page + 1
			} else {
				next = 0
			}
		}
		page = next
	}
	return all, nil
}

// GetChangeRequest fetches one change request by repository and local ID.
func (c *Client) GetChangeRequest(ctx context.Context, repoID, localID int) (ChangeRequest, error) {
	target := c.repositoryPath(strconv.Itoa(repoID), "changeRequests", strconv.Itoa(localID))
	data, _, err := c.get(ctx, target, nil)
	if err != nil {
		return ChangeRequest{}, err
	}
	return decodeOne[ChangeRequest](data)
}

// CreateChangeRequest opens a change request. Zero SourceProjectID and
// TargetProjectID are filled with repoID, matching the Codeup requirement that
// both sides point at the same repository for branch-to-branch merges.
func (c *Client) CreateChangeRequest(ctx context.Context, repoID int, in CreateChangeRequestInput) (ChangeRequest, error) {
	if strings.TrimSpace(in.SourceBranch) == "" || strings.TrimSpace(in.TargetBranch) == "" {
		return ChangeRequest{}, fmt.Errorf("创建云效合并请求需要 sourceBranch 和 targetBranch")
	}
	if strings.TrimSpace(in.Title) == "" {
		return ChangeRequest{}, fmt.Errorf("创建云效合并请求需要 title")
	}
	if in.SourceProjectID == 0 {
		in.SourceProjectID = repoID
	}
	if in.TargetProjectID == 0 {
		in.TargetProjectID = repoID
	}
	data, _, err := c.post(ctx, c.repositoryPath(strconv.Itoa(repoID), "changeRequests"), in)
	if err != nil {
		return ChangeRequest{}, err
	}
	return decodeOne[ChangeRequest](data)
}

// MergeChangeRequest merges one change request. An empty MergeType defaults to
// no-fast-forward so the merge commit is kept, matching Gongfeng behaviour.
func (c *Client) MergeChangeRequest(ctx context.Context, repoID, localID int, in MergeChangeRequestInput) (ChangeRequest, error) {
	if in.MergeType == "" {
		in.MergeType = MergeTypeNoFastForward
	}
	target := c.repositoryPath(strconv.Itoa(repoID), "changeRequests", strconv.Itoa(localID), "merge")
	data, _, err := c.post(ctx, target, in)
	if err != nil {
		return ChangeRequest{}, err
	}
	return decodeOne[ChangeRequest](data)
}

// SearchWorkitems returns one page of Projex workitems.
func (c *Client) SearchWorkitems(ctx context.Context, in SearchWorkitemsInput) ([]Workitem, error) {
	items, _, err := c.SearchWorkitemsPage(ctx, in)
	return items, err
}

// SearchWorkitemsPage also returns the pagination headers.
func (c *Client) SearchWorkitemsPage(ctx context.Context, in SearchWorkitemsInput) ([]Workitem, PageInfo, error) {
	page := in.Page
	if page <= 0 {
		page = 1
		in.Page = page
	}
	data, header, err := c.post(ctx, c.ProjexPath("workitems:search"), in)
	if err != nil {
		return nil, PageInfo{}, err
	}
	items, err := decodeList[Workitem](data)
	if err != nil {
		return nil, PageInfo{}, err
	}
	return items, pageInfoFrom(header, page), nil
}

// ListAllWorkitems follows pagination up to maxPages (default 50).
func (c *Client) ListAllWorkitems(ctx context.Context, in SearchWorkitemsInput, maxPages int) ([]Workitem, error) {
	if in.PerPage <= 0 {
		in.PerPage = 200
	}
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	var all []Workitem
	page := in.Page
	for fetched := 0; page > 0 && fetched < maxPages; fetched++ {
		in.Page = page
		items, info, err := c.SearchWorkitemsPage(ctx, in)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next := info.NextPage
		if next <= page {
			if len(items) >= in.PerPage {
				next = page + 1
			} else {
				next = 0
			}
		}
		page = next
	}
	return all, nil
}

// GetWorkitem fetches one Projex workitem by ID.
func (c *Client) GetWorkitem(ctx context.Context, id string) (Workitem, error) {
	data, _, err := c.get(ctx, c.ProjexPath("workitems", id), nil)
	if err != nil {
		return Workitem{}, err
	}
	return decodeOne[Workitem](data)
}

// CreateWorkitem creates a Projex workitem and returns its ID. The API only
// returns the ID; callers use GetWorkitem to read the full record.
func (c *Client) CreateWorkitem(ctx context.Context, in CreateWorkitemInput) (string, error) {
	switch {
	case strings.TrimSpace(in.Subject) == "":
		return "", fmt.Errorf("创建云效工作项需要 subject")
	case strings.TrimSpace(in.SpaceID) == "":
		return "", fmt.Errorf("创建云效工作项需要 spaceId")
	case strings.TrimSpace(in.WorkitemTypeID) == "":
		return "", fmt.Errorf("创建云效工作项需要 workitemTypeId")
	case strings.TrimSpace(in.AssignedTo) == "":
		return "", fmt.Errorf("创建云效工作项需要 assignedTo（负责人）")
	}
	data, _, err := c.post(ctx, c.ProjexPath("workitems"), in)
	if err != nil {
		return "", err
	}
	var created struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", fmt.Errorf("解析云效工作项响应失败：%w", err)
	}
	id := jsonID(created.ID)
	if id == "" {
		return "", fmt.Errorf("云效没有返回工作项 ID")
	}
	return id, nil
}

// CreateWorkitemComment appends a Markdown comment to a workitem and returns
// the comment ID.
func (c *Client) CreateWorkitemComment(ctx context.Context, workitemID, content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("云效工作项评论内容不能为空")
	}
	payload := struct {
		Content    string `json:"content"`
		FormatType string `json:"formatType"`
	}{Content: content, FormatType: "MARKDOWN"}

	data, _, err := c.post(ctx, c.ProjexPath("workitems", workitemID, "comments"), payload)
	if err != nil {
		return "", err
	}
	var created struct {
		ID        json.RawMessage `json:"id"`
		CommentID json.RawMessage `json:"commentId"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		return "", fmt.Errorf("解析云效评论响应失败：%w", err)
	}
	id := jsonID(created.ID)
	if id == "" {
		id = jsonID(created.CommentID)
	}
	if id == "" {
		return "", fmt.Errorf("云效没有返回评论 ID")
	}
	return id, nil
}

func decodeOne[T any](data []byte) (T, error) {
	var value T
	if len(bytes.TrimSpace(data)) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("解析云效响应失败：%w", err)
	}
	return value, nil
}

// decodeList accepts either a bare JSON array or an object wrapping the array
// under items/result/data/organizations, since Yunxiao endpoints differ.
func decodeList[T any](data []byte) ([]T, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []T
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("解析云效响应失败：%w", err)
		}
		return items, nil
	}
	var wrapper struct {
		Items         []T `json:"items"`
		Result        []T `json:"result"`
		Data          []T `json:"data"`
		Organizations []T `json:"organizations"`
	}
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return nil, fmt.Errorf("解析云效响应失败：%w", err)
	}
	switch {
	case wrapper.Items != nil:
		return wrapper.Items, nil
	case wrapper.Result != nil:
		return wrapper.Result, nil
	case wrapper.Data != nil:
		return wrapper.Data, nil
	case wrapper.Organizations != nil:
		return wrapper.Organizations, nil
	}
	return nil, nil
}

// jsonID accepts both a quoted and a bare JSON scalar ID.
func jsonID(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return s
	}
	return string(raw)
}
