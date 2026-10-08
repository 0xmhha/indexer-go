package graphql

import (
	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/graphql-go/graphql"
)

// WithNotificationService sets the notification service for the schema
func (b *SchemaBuilder) WithNotificationService(service notifications.Service) *SchemaBuilder {
	b.schema.notificationService = service
	return b
}

// WithNotificationQueries adds notification management queries and mutations
func (b *SchemaBuilder) WithNotificationQueries() *SchemaBuilder {
	s := b.schema

	// Queries
	b.queries["notificationSettings"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationSettingType))),
		Description: "List all notification settings",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        notificationSettingsFilterInputType,
				Description: "Filter options",
			},
		},
		Resolve: s.resolveNotificationSettings,
	}
	b.queries["notificationSetting"] = &graphql.Field{
		Type:        notificationSettingType,
		Description: "Get a notification setting by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveNotificationSetting,
	}
	b.queries["notifications"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationType))),
		Description: "List notifications with optional filtering",
		Args: graphql.FieldConfigArgument{
			"filter": &graphql.ArgumentConfig{
				Type:        notificationsFilterInputType,
				Description: "Filter options",
			},
		},
		Resolve: s.resolveNotifications,
	}
	b.queries["notification"] = &graphql.Field{
		Type:        notificationType,
		Description: "Get a notification by ID",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveNotification,
	}
	b.queries["notificationStats"] = &graphql.Field{
		Type:        notificationStatsType,
		Description: "Get statistics for a notification setting",
		Args: graphql.FieldConfigArgument{
			"settingId": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveNotificationStats,
	}
	b.queries["deliveryHistory"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(deliveryHistoryType))),
		Description: "Get delivery history for a notification",
		Args: graphql.FieldConfigArgument{
			"notificationId": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveDeliveryHistory,
	}

	// Mutations
	b.mutations["createNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(notificationSettingType),
		Description: "Create a new notification setting",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(createNotificationSettingInputType),
				Description: "Setting configuration",
			},
		},
		Resolve: s.resolveCreateNotificationSetting,
	}
	b.mutations["updateNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(notificationSettingType),
		Description: "Update an existing notification setting",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
			"input": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(updateNotificationSettingInputType),
				Description: "Fields to update",
			},
		},
		Resolve: s.resolveUpdateNotificationSetting,
	}
	b.mutations["deleteNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Delete a notification setting",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveDeleteNotificationSetting,
	}
	b.mutations["testNotificationSetting"] = &graphql.Field{
		Type:        graphql.NewNonNull(deliveryResultType),
		Description: "Test a notification setting by sending a sample notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Setting identifier",
			},
		},
		Resolve: s.resolveTestNotificationSetting,
	}
	b.mutations["retryNotification"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Retry a failed notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveRetryNotification,
	}
	b.mutations["cancelNotification"] = &graphql.Field{
		Type:        graphql.NewNonNull(graphql.Boolean),
		Description: "Cancel a pending notification",
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.ID),
				Description: "Notification identifier",
			},
		},
		Resolve: s.resolveCancelNotification,
	}

	return b
}
