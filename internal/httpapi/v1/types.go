// Package v1 defines ProbeHive's deliberately versioned HTTP wire types.
package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

type SetupStatusResponse struct {
	SetupComplete bool `json:"setupComplete"`
}

type AntiforgeryTokenResponse struct {
	HeaderName   string `json:"headerName"`
	RequestToken string `json:"requestToken"`
}

type SessionResponse struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type UserResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ProjectResponse struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	IsDefault      bool      `json:"isDefault"`
	CreatedAt      time.Time `json:"createdAt"`
}

type OrganizationResponse struct {
	ID             string          `json:"id"`
	Slug           string          `json:"slug"`
	DisplayName    string          `json:"displayName"`
	CreatedAt      time.Time       `json:"createdAt"`
	DefaultProject ProjectResponse `json:"defaultProject"`
}

type MonitorResponse struct {
	ID                   string    `json:"id"`
	OrganizationID       string    `json:"organizationId"`
	ProjectID            string    `json:"projectId"`
	Name                 string    `json:"name"`
	CheckType            string    `json:"checkType"`
	State                string    `json:"state"`
	LatestRevisionNumber int       `json:"latestRevisionNumber"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type MonitorRevisionResponse struct {
	ID                 string          `json:"id"`
	MonitorID          string          `json:"monitorId"`
	RevisionNumber     int             `json:"revisionNumber"`
	CheckType          string          `json:"checkType"`
	CheckSchemaVersion int             `json:"checkSchemaVersion"`
	CheckConfiguration json.RawMessage `json:"checkConfiguration"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type CreateFirstAdministratorRequest struct {
	Email       *string `json:"email"`
	DisplayName *string `json:"displayName"`
	Password    *string `json:"password"`
}

type LoginRequest struct {
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

type CreateOrganizationRequest struct {
	Slug        *string `json:"slug"`
	DisplayName *string `json:"displayName"`
}

type CreateMonitorRequest struct {
	Name      *string `json:"name"`
	CheckType *string `json:"checkType"`
}

type RenameMonitorRequest struct {
	Name *string `json:"name"`
}

type ChangeMonitorStateRequest struct {
	State *string `json:"state"`
}

// Integer accepts only JSON integers. A missing field retains the zero value,
// while an explicit JSON null remains a malformed request like the legacy host.
type Integer int

func (value *Integer) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("expected an integer, found null")
	}
	var decoded int
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*value = Integer(decoded)
	return nil
}

type CreateMonitorRevisionRequest struct {
	CheckSchemaVersion Integer         `json:"checkSchemaVersion"`
	CheckConfiguration json.RawMessage `json:"checkConfiguration"`
}

type ProblemDetails struct {
	Type   string              `json:"type"`
	Title  string              `json:"title"`
	Status int                 `json:"status"`
	Detail string              `json:"detail,omitempty"`
	Errors map[string][]string `json:"errors,omitempty"`
}
