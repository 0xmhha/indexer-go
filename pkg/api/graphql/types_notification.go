package graphql

import "github.com/graphql-go/graphql"

// Notification type enum
var notificationTypeEnumType = graphql.NewEnum(graphql.EnumConfig{
	Name:        "NotificationType",
	Description: "Notification delivery type",
	Values: graphql.EnumValueConfigMap{
		"WEBHOOK": &graphql.EnumValueConfig{
			Value:       "webhook",
			Description: "Webhook delivery",
		},
		"EMAIL": &graphql.EnumValueConfig{
			Value:       "email",
			Description: "Email delivery",
		},
		"SLACK": &graphql.EnumValueConfig{
			Value:       "slack",
			Description: "Slack delivery",
		},
	},
})

// Notification event type enum
var notificationEventTypeEnumType = graphql.NewEnum(graphql.EnumConfig{
	Name:        "NotificationEventType",
	Description: "Event type that triggers notification",
	Values: graphql.EnumValueConfigMap{
		"BLOCK": &graphql.EnumValueConfig{
			Value:       "block",
			Description: "New block event",
		},
		"TRANSACTION": &graphql.EnumValueConfig{
			Value:       "transaction",
			Description: "New transaction event",
		},
		"LOG": &graphql.EnumValueConfig{
			Value:       "log",
			Description: "Log event",
		},
		"CONTRACT_CREATION": &graphql.EnumValueConfig{
			Value:       "contract_creation",
			Description: "Contract creation event",
		},
		"TOKEN_TRANSFER": &graphql.EnumValueConfig{
			Value:       "token_transfer",
			Description: "Token transfer event",
		},
	},
})

// Notification status enum
var notificationStatusEnumType = graphql.NewEnum(graphql.EnumConfig{
	Name:        "NotificationStatus",
	Description: "Notification delivery status",
	Values: graphql.EnumValueConfigMap{
		"PENDING": &graphql.EnumValueConfig{
			Value:       "pending",
			Description: "Pending delivery",
		},
		"RETRYING": &graphql.EnumValueConfig{
			Value:       "retrying",
			Description: "Retrying after failure",
		},
		"SENT": &graphql.EnumValueConfig{
			Value:       "sent",
			Description: "Successfully delivered",
		},
		"FAILED": &graphql.EnumValueConfig{
			Value:       "failed",
			Description: "Delivery failed",
		},
		"CANCELLED": &graphql.EnumValueConfig{
			Value:       "cancelled",
			Description: "Cancelled by user",
		},
	},
})

// NotificationDestination type
var notificationDestinationType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "NotificationDestination",
	Description: "Notification delivery destination configuration",
	Fields: graphql.Fields{
		"webhookURL": &graphql.Field{
			Type:        graphql.String,
			Description: "Webhook URL for delivery",
		},
		"webhookSecret": &graphql.Field{
			Type:        graphql.String,
			Description: "Webhook HMAC secret (masked)",
		},
		"emailTo": &graphql.Field{
			Type:        graphql.NewList(graphql.String),
			Description: "Email recipients",
		},
		"emailSubject": &graphql.Field{
			Type:        graphql.String,
			Description: "Email subject template",
		},
		"slackWebhookURL": &graphql.Field{
			Type:        graphql.String,
			Description: "Slack webhook URL",
		},
		"slackChannel": &graphql.Field{
			Type:        graphql.String,
			Description: "Slack channel",
		},
		"slackUsername": &graphql.Field{
			Type:        graphql.String,
			Description: "Slack bot username",
		},
	},
})

// NotificationFilter type
var notificationFilterType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "NotificationFilter",
	Description: "Filter configuration for notifications",
	Fields: graphql.Fields{
		"addresses": &graphql.Field{
			Type:        graphql.NewList(addressType),
			Description: "Filter by contract/account addresses",
		},
		"topics": &graphql.Field{
			Type:        graphql.NewList(graphql.NewList(hashType)),
			Description: "Filter by log topics",
		},
		"minValue": &graphql.Field{
			Type:        bigIntType,
			Description: "Minimum transaction value filter",
		},
	},
})

// NotificationSetting type
var notificationSettingType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "NotificationSetting",
	Description: "Notification setting configuration",
	Fields: graphql.Fields{
		"id": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Setting unique identifier",
		},
		"name": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Setting name",
		},
		"type": &graphql.Field{
			Type:        graphql.NewNonNull(notificationTypeEnumType),
			Description: "Notification type",
		},
		"enabled": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Boolean),
			Description: "Whether the setting is enabled",
		},
		"eventTypes": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationEventTypeEnumType))),
			Description: "Event types that trigger notifications",
		},
		"filter": &graphql.Field{
			Type:        notificationFilterType,
			Description: "Optional filter configuration",
		},
		"destination": &graphql.Field{
			Type:        graphql.NewNonNull(notificationDestinationType),
			Description: "Delivery destination configuration",
		},
		"createdAt": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Creation timestamp",
		},
		"updatedAt": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Last update timestamp",
		},
	},
})

// NotificationStats type
var notificationStatsType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "NotificationStats",
	Description: "Notification statistics for a setting",
	Fields: graphql.Fields{
		"settingId": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Setting identifier",
		},
		"totalSent": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Total notifications sent",
		},
		"totalFailed": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Total notifications failed",
		},
		"successRate": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Float),
			Description: "Success rate percentage",
		},
		"avgDeliveryMs": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Float),
			Description: "Average delivery time in milliseconds",
		},
		"lastSentAt": &graphql.Field{
			Type:        graphql.String,
			Description: "Last successful delivery timestamp",
		},
		"lastFailedAt": &graphql.Field{
			Type:        graphql.String,
			Description: "Last failed delivery timestamp",
		},
	},
})

// Notification type
var notificationType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "Notification",
	Description: "Notification record",
	Fields: graphql.Fields{
		"id": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Notification unique identifier",
		},
		"settingId": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Associated setting identifier",
		},
		"type": &graphql.Field{
			Type:        graphql.NewNonNull(notificationTypeEnumType),
			Description: "Notification type",
		},
		"eventType": &graphql.Field{
			Type:        graphql.NewNonNull(notificationEventTypeEnumType),
			Description: "Event type that triggered the notification",
		},
		"status": &graphql.Field{
			Type:        graphql.NewNonNull(notificationStatusEnumType),
			Description: "Delivery status",
		},
		"retryCount": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Number of retry attempts",
		},
		"error": &graphql.Field{
			Type:        graphql.String,
			Description: "Error message if failed",
		},
		"blockNumber": &graphql.Field{
			Type:        bigIntType,
			Description: "Associated block number",
		},
		"blockHash": &graphql.Field{
			Type:        hashType,
			Description: "Associated block hash",
		},
		"createdAt": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Creation timestamp",
		},
		"sentAt": &graphql.Field{
			Type:        graphql.String,
			Description: "Delivery timestamp",
		},
		"nextRetry": &graphql.Field{
			Type:        graphql.String,
			Description: "Next retry timestamp",
		},
	},
})

// DeliveryHistory type
var deliveryHistoryType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "DeliveryHistory",
	Description: "Notification delivery history record",
	Fields: graphql.Fields{
		"notificationId": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Notification identifier",
		},
		"settingId": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.ID),
			Description: "Setting identifier",
		},
		"attempt": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Attempt number",
		},
		"success": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Boolean),
			Description: "Whether delivery succeeded",
		},
		"statusCode": &graphql.Field{
			Type:        graphql.Int,
			Description: "HTTP status code",
		},
		"error": &graphql.Field{
			Type:        graphql.String,
			Description: "Error message if failed",
		},
		"durationMs": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Delivery duration in milliseconds",
		},
		"timestamp": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Delivery timestamp",
		},
	},
})

// DeliveryResult type (for test results)
var deliveryResultType = graphql.NewObject(graphql.ObjectConfig{
	Name:        "DeliveryResult",
	Description: "Notification delivery result",
	Fields: graphql.Fields{
		"success": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Boolean),
			Description: "Whether delivery succeeded",
		},
		"statusCode": &graphql.Field{
			Type:        graphql.Int,
			Description: "HTTP status code",
		},
		"error": &graphql.Field{
			Type:        graphql.String,
			Description: "Error message if failed",
		},
		"durationMs": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Delivery duration in milliseconds",
		},
	},
})

// Input types for mutations

// NotificationDestinationInput type
var notificationDestinationInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "NotificationDestinationInput",
	Description: "Input for notification destination configuration",
	Fields: graphql.InputObjectConfigFieldMap{
		"webhookURL": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Webhook URL for delivery",
		},
		"webhookSecret": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Webhook HMAC secret",
		},
		"emailTo": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(graphql.String),
			Description: "Email recipients",
		},
		"emailSubject": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Email subject template",
		},
		"slackWebhookURL": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Slack webhook URL",
		},
		"slackChannel": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Slack channel",
		},
		"slackUsername": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Slack bot username",
		},
	},
})

// NotificationFilterInput type
var notificationFilterInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "NotificationFilterInput",
	Description: "Input for notification filter configuration",
	Fields: graphql.InputObjectConfigFieldMap{
		"addresses": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(addressType),
			Description: "Filter by contract/account addresses",
		},
		"topics": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(graphql.NewList(graphql.String)),
			Description: "Filter by log topics",
		},
		"minValue": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Minimum transaction value filter",
		},
	},
})

// CreateNotificationSettingInput type
var createNotificationSettingInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "CreateNotificationSettingInput",
	Description: "Input for creating a notification setting",
	Fields: graphql.InputObjectConfigFieldMap{
		"name": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "Setting name",
		},
		"type": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(notificationTypeEnumType),
			Description: "Notification type",
		},
		"enabled": &graphql.InputObjectFieldConfig{
			Type:         graphql.Boolean,
			DefaultValue: true,
			Description:  "Whether the setting is enabled",
		},
		"eventTypes": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(notificationEventTypeEnumType))),
			Description: "Event types that trigger notifications",
		},
		"filter": &graphql.InputObjectFieldConfig{
			Type:        notificationFilterInputType,
			Description: "Optional filter configuration",
		},
		"destination": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewNonNull(notificationDestinationInputType),
			Description: "Delivery destination configuration",
		},
	},
})

// UpdateNotificationSettingInput type
var updateNotificationSettingInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "UpdateNotificationSettingInput",
	Description: "Input for updating a notification setting",
	Fields: graphql.InputObjectConfigFieldMap{
		"name": &graphql.InputObjectFieldConfig{
			Type:        graphql.String,
			Description: "Setting name",
		},
		"enabled": &graphql.InputObjectFieldConfig{
			Type:        graphql.Boolean,
			Description: "Whether the setting is enabled",
		},
		"eventTypes": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(graphql.NewNonNull(notificationEventTypeEnumType)),
			Description: "Event types that trigger notifications",
		},
		"filter": &graphql.InputObjectFieldConfig{
			Type:        notificationFilterInputType,
			Description: "Optional filter configuration",
		},
		"destination": &graphql.InputObjectFieldConfig{
			Type:        notificationDestinationInputType,
			Description: "Delivery destination configuration",
		},
	},
})

// NotificationSettingsFilter input type
var notificationSettingsFilterInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "NotificationSettingsFilterInput",
	Description: "Filter for listing notification settings",
	Fields: graphql.InputObjectConfigFieldMap{
		"types": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(notificationTypeEnumType),
			Description: "Filter by notification types",
		},
		"enabled": &graphql.InputObjectFieldConfig{
			Type:        graphql.Boolean,
			Description: "Filter by enabled status",
		},
		"limit": &graphql.InputObjectFieldConfig{
			Type:         graphql.Int,
			DefaultValue: 100,
			Description:  "Maximum results",
		},
		"offset": &graphql.InputObjectFieldConfig{
			Type:         graphql.Int,
			DefaultValue: 0,
			Description:  "Pagination offset",
		},
	},
})

// NotificationsFilter input type
var notificationsFilterInputType = graphql.NewInputObject(graphql.InputObjectConfig{
	Name:        "NotificationsFilterInput",
	Description: "Filter for listing notifications",
	Fields: graphql.InputObjectConfigFieldMap{
		"settingId": &graphql.InputObjectFieldConfig{
			Type:        graphql.ID,
			Description: "Filter by setting ID",
		},
		"status": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(notificationStatusEnumType),
			Description: "Filter by status",
		},
		"eventTypes": &graphql.InputObjectFieldConfig{
			Type:        graphql.NewList(notificationEventTypeEnumType),
			Description: "Filter by event types",
		},
		"limit": &graphql.InputObjectFieldConfig{
			Type:         graphql.Int,
			DefaultValue: 100,
			Description:  "Maximum results",
		},
		"offset": &graphql.InputObjectFieldConfig{
			Type:         graphql.Int,
			DefaultValue: 0,
			Description:  "Pagination offset",
		},
	},
})
