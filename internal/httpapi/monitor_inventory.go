package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
)

const (
	monitorInventorySearchInvalidCode      = "monitor.inventory.search.invalid"
	monitorInventoryStateInvalidCode       = "monitor.inventory.state.invalid"
	monitorInventoryHealthInvalidCode      = "monitor.inventory.health.invalid"
	monitorInventoryRunInvalidCode         = "monitor.inventory.runOutcome.invalid"
	monitorInventoryMaintenanceInvalidCode = "monitor.inventory.maintenance.invalid"
	monitorInventorySortInvalidCode        = "monitor.inventory.sort.invalid"
	monitorInventoryDirectionInvalidCode   = "monitor.inventory.direction.invalid"
	monitorInventoryPageInvalidCode        = "monitor.inventory.page.invalid"
	monitorInventoryPageSizeInvalidCode    = "monitor.inventory.pageSize.invalid"
)

var monitorInventoryQueryMessages = map[string]string{
	monitorInventorySearchInvalidCode:      "search must be from 1 through 100 characters after trimming.",
	monitorInventoryStateInvalidCode:       "state must be one of: draft, active, paused, archived.",
	monitorInventoryHealthInvalidCode:      "health must be one of: notEvaluated, unknown, healthy, degraded, down.",
	monitorInventoryRunInvalidCode:         "runOutcome must be one of: notRun, inProgress, passed, failed, errored, timedout, cancelled, skipped.",
	monitorInventoryMaintenanceInvalidCode: "maintenance must be one of: none, upcoming, active.",
	monitorInventorySortInvalidCode:        "sort must be one of: name, createdAt, updatedAt.",
	monitorInventoryDirectionInvalidCode:   "direction must be one of: asc, desc.",
	monitorInventoryPageInvalidCode:        "page must be one integer from 1 through 1000.",
	monitorInventoryPageSizeInvalidCode:    "pageSize must be one integer from 1 through 100.",
}

func (server *Server) monitorInventoryRoot(w http.ResponseWriter, r *http.Request) {
	organizationID, projectID, ok := projectScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.readMonitors(w, r, organizationID); !ok {
		return
	}
	query, failures := parseMonitorInventoryQuery(r.URL.Query())
	if len(failures) != 0 {
		writeValidationProblem(w, failures)
		return
	}
	page, found, err := server.monitorInventory.List(r.Context(), organizationID, projectID, query)
	if err != nil {
		server.internalError(w, r, "list Monitor inventory", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.MonitorInventoryItemResponse, len(page.Items))
	for index, item := range page.Items {
		items[index] = toMonitorInventoryItemResponse(item)
	}
	writeJSON(w, http.StatusOK, api.MonitorInventoryPageResponse{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

func parseMonitorInventoryQuery(values url.Values) (monitor.InventoryQuery, [][3]string) {
	query := monitor.InventoryQuery{Sort: monitor.InventorySortName, Direction: monitor.InventoryDirectionAscending, Page: 1, PageSize: monitor.DefaultInventoryPageSize}
	var failures [][3]string
	if value, present, valid := oneQueryValue(values, "search"); present {
		if normalized, ok := monitor.NormalizeInventorySearch(value); !valid || !ok {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventorySearchInvalidCode, "search"))
		} else {
			query.Search = normalized
		}
	}
	if value, present, valid := oneQueryValue(values, "state"); present {
		if !valid || !validMonitorInventoryState(value) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryStateInvalidCode, "state"))
		} else {
			query.State = monitor.State(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "health"); present {
		if !valid || !monitor.ValidInventoryHealth(value) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryHealthInvalidCode, "health"))
		} else {
			query.Health = monitor.InventoryHealthFilter(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "runOutcome"); present {
		if !valid || !monitor.ValidInventoryRun(value) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryRunInvalidCode, "runOutcome"))
		} else {
			query.RunOutcome = monitor.InventoryRunFilter(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "maintenance"); present {
		if !valid || !monitor.ValidInventoryMaintenance(value) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryMaintenanceInvalidCode, "maintenance"))
		} else {
			query.Maintenance = monitor.InventoryMaintenanceFilter(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "sort"); present {
		if !valid || !monitor.ValidInventorySort(value) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventorySortInvalidCode, "sort"))
		} else {
			query.Sort = monitor.InventorySort(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "direction"); present {
		if !valid || (value != string(monitor.InventoryDirectionAscending) && value != string(monitor.InventoryDirectionDescending)) {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryDirectionInvalidCode, "direction"))
		} else {
			query.Direction = monitor.InventoryDirection(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "page"); present {
		if parsed, err := strconv.Atoi(value); !valid || err != nil || parsed < 1 || parsed > monitor.MaxInventoryPage {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryPageInvalidCode, "page"))
		} else {
			query.Page = parsed
		}
	}
	if value, present, valid := oneQueryValue(values, "pageSize"); present {
		if parsed, err := strconv.Atoi(value); !valid || err != nil || parsed < 1 || parsed > monitor.MaxInventoryPageSize {
			failures = append(failures, monitorInventoryQueryFailure(monitorInventoryPageSizeInvalidCode, "pageSize"))
		} else {
			query.PageSize = parsed
		}
	}
	return query, failures
}

func monitorInventoryQueryFailure(code, field string) [3]string {
	return [3]string{code, field, monitorInventoryQueryMessages[code]}
}

func validMonitorInventoryState(value string) bool {
	switch monitor.State(value) {
	case monitor.StateDraft, monitor.StateActive, monitor.StatePaused, monitor.StateArchived:
		return true
	default:
		return false
	}
}

func toMonitorInventoryItemResponse(value monitor.InventoryItem) api.MonitorInventoryItemResponse {
	response := api.MonitorInventoryItemResponse{
		Monitor:     toMonitorResponse(value.Monitor),
		Maintenance: api.MonitorInventoryMaintenanceResponse{State: string(value.Maintenance.State)},
	}
	if value.Health != nil {
		response.Health = &api.MonitorInventoryHealthResponse{State: value.Health.State, UpdatedAt: value.Health.UpdatedAt}
	}
	if value.LastRun != nil {
		outcome := value.LastRun.Outcome
		if outcome == "" {
			outcome = string(monitor.InventoryRunInProgress)
		}
		response.LastRun = &api.MonitorInventoryRunResponse{ID: value.LastRun.ID, Outcome: outcome, ScheduledFor: value.LastRun.ScheduledFor}
	}
	if value.Maintenance.WindowID != "" {
		response.Maintenance.WindowID = optionalString(value.Maintenance.WindowID)
		response.Maintenance.StartsAt = optionalInstant(value.Maintenance.StartsAt)
		response.Maintenance.EndsAt = optionalInstant(value.Maintenance.EndsAt)
	}
	return response
}
