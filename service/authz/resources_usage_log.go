package authz

const (
	ResourceUsageLog  = "usage_log"
	ActionCaptureView = "capture_view"
)

var UsageLogCaptureView = Permission{Resource: ResourceUsageLog, Action: ActionCaptureView}

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceUsageLog,
		LabelKey: "Usage Logs",
		Actions: []ActionDefinition{
			{
				Action:         ActionCaptureView,
				LabelKey:       "View diagnostic log content",
				DescriptionKey: "Preview and download the diagnostic capture file for a usage log.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
