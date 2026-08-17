// Package overview owns the bounded Organization-level operational read model.
package overview

import (
	"context"
	"errors"
	"time"
)

const ActiveIncidentPreviewLimit = 5

type MonitorCounts struct {
	Total    int
	Draft    int
	Active   int
	Paused   int
	Archived int
}

type HealthCounts struct {
	NotEvaluated int
	Unknown      int
	Healthy      int
	Degraded     int
	Down         int
}

type IncidentCounts struct {
	Active       int
	Open         int
	Acknowledged int
}

type IntegrationCounts struct {
	Total   int
	Enabled int
}

type StatusPageState struct {
	Configured bool
	Published  bool
}

type ActiveIncident struct {
	ID          string
	ProjectID   string
	MonitorID   string
	MonitorName string
	State       string
	UpdatedAt   time.Time
}

type Summary struct {
	OrganizationID           string
	Monitors                 MonitorCounts
	Health                   HealthCounts
	Incidents                IncidentCounts
	ActiveIncidents          []ActiveIncident
	ActiveIncidentsTruncated bool
	Integrations             IntegrationCounts
	StatusPage               StatusPageState
}

type Store interface {
	GetOverview(context.Context, string, int) (Summary, bool, error)
}

type Service struct{ store Store }

func NewService(store Store) *Service {
	if store == nil {
		panic("overview.Service requires a store")
	}
	return &Service{store: store}
}

func (service *Service) Get(ctx context.Context, organizationID string) (Summary, bool, error) {
	if organizationID == "" {
		return Summary{}, false, errors.New("an Organization overview requires Organization identity")
	}
	value, found, err := service.store.GetOverview(ctx, organizationID, ActiveIncidentPreviewLimit)
	if err != nil || !found {
		return Summary{}, found, err
	}
	if value.ActiveIncidents == nil {
		value.ActiveIncidents = []ActiveIncident{}
	}
	if err := value.validate(); err != nil {
		return Summary{}, false, err
	}
	return value, true, nil
}

func (value Summary) validate() error {
	if value.OrganizationID == "" {
		return errors.New("an Organization overview requires Organization identity")
	}
	if value.Monitors.Total < 0 || value.Monitors.Draft < 0 || value.Monitors.Active < 0 ||
		value.Monitors.Paused < 0 || value.Monitors.Archived < 0 ||
		value.Monitors.Total != value.Monitors.Draft+value.Monitors.Active+value.Monitors.Paused+value.Monitors.Archived {
		return errors.New("Organization overview Monitor counts are inconsistent")
	}
	if value.Health.NotEvaluated < 0 || value.Health.Unknown < 0 || value.Health.Healthy < 0 ||
		value.Health.Degraded < 0 || value.Health.Down < 0 ||
		value.Monitors.Active != value.Health.NotEvaluated+value.Health.Unknown+value.Health.Healthy+value.Health.Degraded+value.Health.Down {
		return errors.New("Organization overview health counts are inconsistent")
	}
	if value.Incidents.Active < 0 || value.Incidents.Open < 0 || value.Incidents.Acknowledged < 0 ||
		value.Incidents.Active != value.Incidents.Open+value.Incidents.Acknowledged {
		return errors.New("Organization overview Incident counts are inconsistent")
	}
	if value.Integrations.Total < 0 || value.Integrations.Enabled < 0 || value.Integrations.Enabled > value.Integrations.Total {
		return errors.New("Organization overview Integration counts are inconsistent")
	}
	if value.StatusPage.Published && !value.StatusPage.Configured {
		return errors.New("an Organization overview cannot publish an unconfigured status page")
	}
	if len(value.ActiveIncidents) > ActiveIncidentPreviewLimit ||
		value.ActiveIncidentsTruncated != (value.Incidents.Active > len(value.ActiveIncidents)) {
		return errors.New("Organization overview active Incident preview is inconsistent")
	}
	for _, incident := range value.ActiveIncidents {
		if incident.ID == "" || incident.ProjectID == "" || incident.MonitorID == "" || incident.MonitorName == "" ||
			(incident.State != "open" && incident.State != "acknowledged") || !isUTC(incident.UpdatedAt) {
			return errors.New("Organization overview contains an invalid active Incident")
		}
	}
	return nil
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
