package yunxiao

import "strings"

// Repository is a Codeup repository as returned by the
// repositories/{repositoryId} endpoint.
type Repository struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"pathWithNamespace"`
	DefaultBranch     string `json:"defaultBranch"`
	WebURL            string `json:"webUrl"`
	SSHURL            string `json:"sshUrlToRepo"`
	HTTPURL           string `json:"httpUrlToRepo"`
	AccessLevel       string `json:"accessLevel"`
	AllowPush         bool   `json:"allowPush"`
}

// ChangeRequest is a Codeup merge request. Status is the raw Codeup state
// (UNDER_DEV, UNDER_REVIEW, TO_BE_MERGED, MERGED, CLOSED, ...); callers map it
// to the tracker's opened/merged/closed vocabulary.
type ChangeRequest struct {
	LocalID      int    `json:"localId"`
	ProjectID    int    `json:"projectId"`
	SourceBranch string `json:"sourceBranch"`
	TargetBranch string `json:"targetBranch"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	// State is returned by the list endpoint, while single-record endpoints
	// return Status. RawStatus returns whichever one the server supplied.
	State               string `json:"state"`
	WebURL              string `json:"webUrl"`
	SourceProjectID     int    `json:"sourceProjectId"`
	TargetProjectID     int    `json:"targetProjectId"`
	AllRequirementsPass bool   `json:"allRequirementsPass"`
	ConflictCheckStatus string `json:"conflictCheckStatus"`
	MergedRevision      string `json:"mergedRevision"`
	CreateTime          string `json:"createTime"`
	UpdateTime          string `json:"updateTime"`
}

// RawStatus returns the state field used by the list endpoint or the status
// field used by single-record endpoints.
func (c ChangeRequest) RawStatus() string {
	if strings.TrimSpace(c.Status) != "" {
		return c.Status
	}
	return c.State
}

// CreateChangeRequestInput is the body of a create-change-request call.
// SourceProjectID and TargetProjectID are filled with the repository ID by
// CreateChangeRequest when left zero.
type CreateChangeRequestInput struct {
	SourceBranch    string   `json:"sourceBranch"`
	SourceProjectID int      `json:"sourceProjectId"`
	TargetBranch    string   `json:"targetBranch"`
	TargetProjectID int      `json:"targetProjectId"`
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	ReviewerUserIDs []string `json:"reviewerUserIds,omitempty"`
	// WorkItemIDs is a comma-separated string in the Codeup API.
	WorkItemIDs string `json:"workItemIds,omitempty"`
}

// Merge change types accepted by the Codeup merge endpoint.
const (
	MergeTypeFFOnly        = "ff-only"
	MergeTypeNoFastForward = "no-fast-forward"
	MergeTypeSquash        = "squash"
	MergeTypeRebase        = "rebase"
)

// MergeChangeRequestInput is the body of a merge-change-request call.
type MergeChangeRequestInput struct {
	MergeType          string `json:"mergeType"`
	MergeMessage       string `json:"mergeMessage,omitempty"`
	RemoveSourceBranch bool   `json:"removeSourceBranch,omitempty"`
}

// User is a Projex user reference (creator, assignee, reviewer, ...).
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WorkitemStatus is the Projex workflow status of a workitem.
type WorkitemStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	NameEn string `json:"nameEn"`
}

// WorkitemType is the Projex category type (Req, Bug, Task, ...).
type WorkitemType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CustomField is one entry of a workitem's customFields.
type CustomField struct {
	FieldID      string `json:"fieldId"`
	FieldName    string `json:"fieldName"`
	Value        string `json:"value"`
	DisplayValue string `json:"displayValue"`
}

// CustomFieldValue is the create/update form of a custom field. The server
// expects fieldId plus the concrete value.
type CustomFieldValue struct {
	FieldID string `json:"fieldId"`
	Value   string `json:"value"`
}

// Workitem is a Projex workitem. CreatedAt/UpdatedAt carry the raw
// gmtCreate/gmtModified strings; callers parse them as ISO 8601.
type Workitem struct {
	ID                string         `json:"id"`
	Subject           string         `json:"subject"`
	Description       string         `json:"description"`
	FormatType        string         `json:"formatType"`
	CategoryID        string         `json:"categoryId"`
	SerialNumber      string         `json:"serialNumber"`
	Status            WorkitemStatus `json:"status"`
	AssignedTo        User           `json:"assignedTo"`
	WorkitemType      WorkitemType   `json:"workitemType"`
	CustomFieldValues []CustomField  `json:"customFieldValues"`
	CreatedAt         string         `json:"gmtCreate"`
	UpdatedAt         string         `json:"gmtModified"`
}

// SearchWorkitemsInput is the body of a workitems:search call.
type SearchWorkitemsInput struct {
	Category   string `json:"category,omitempty"`
	SpaceID    string `json:"spaceId,omitempty"`
	SpaceType  string `json:"spaceType,omitempty"`
	Conditions string `json:"conditions,omitempty"`
	Page       int    `json:"page,omitempty"`
	PerPage    int    `json:"perPage,omitempty"`
	OrderBy    string `json:"orderBy,omitempty"`
	Sort       string `json:"sort,omitempty"`
}

// CreateWorkitemInput is the body of a create-workitem call. AssignedTo,
// SpaceID, WorkitemTypeID and Subject are required by the API.
type CreateWorkitemInput struct {
	AssignedTo        string             `json:"assignedTo"`
	SpaceID           string             `json:"spaceId"`
	WorkitemTypeID    string             `json:"workitemTypeId"`
	Subject           string             `json:"subject"`
	Description       string             `json:"description,omitempty"`
	FormatType        string             `json:"formatType,omitempty"`
	CustomFieldValues []CustomFieldValue `json:"customFieldValues,omitempty"`
}

// Organization is a Yunxiao organization from the central-station platform API.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
