package requirement

import "strings"

const (
	LifecycleStatusDraft    = "draft"
	LifecycleStatusApproved = "approved"
	LifecycleStatusBlocked  = "blocked"
	LifecycleStatusWaived   = "waived"

	TaskStateTodo       = "todo"
	TaskStateInProgress = "in_progress"
	TaskStateDone       = "done"
	TaskStateBlocked    = "blocked"
	TaskStateWaived     = "waived"
)

var lifecycleStatusValues = []string{
	LifecycleStatusDraft,
	LifecycleStatusApproved,
	LifecycleStatusBlocked,
	LifecycleStatusWaived,
}

var taskStateValues = []string{
	TaskStateTodo,
	TaskStateInProgress,
	TaskStateDone,
	TaskStateBlocked,
	TaskStateWaived,
}

func normalizeLifecycleStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isLifecycleStatus(value string) bool {
	return containsStatusValue(lifecycleStatusValues, value)
}

func lifecycleStatusValuesText() string {
	return strings.Join(lifecycleStatusValues, ", ")
}

func normalizeTaskState(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isTaskState(value string) bool {
	return containsStatusValue(taskStateValues, value)
}

func taskStateValuesText() string {
	return strings.Join(taskStateValues, ", ")
}

func containsStatusValue(values []string, value string) bool {
	for _, allowed := range values {
		if value == allowed {
			return true
		}
	}
	return false
}
