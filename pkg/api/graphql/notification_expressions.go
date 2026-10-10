package graphql

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/notifications"
)

// Dry run of notification expressions (subscriptions design phase 5b).

var (
	expressionSampleLogInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "NotificationSampleLogInput",
		Description: "A log as eth_getLogs gives it",
		Fields: graphql.InputObjectConfigFieldMap{
			"address":         &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
			"topics":          &graphql.InputObjectFieldConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
			"data":            &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "0x hex"},
			"index":           &graphql.InputObjectFieldConfig{Type: graphql.Int, Description: "Log index in the block"},
			"transactionHash": &graphql.InputObjectFieldConfig{Type: graphql.String},
		},
	})
	expressionSampleTxInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "NotificationSampleTransactionInput",
		Description: "A transaction",
		Fields: graphql.InputObjectConfigFieldMap{
			"hash":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
			"from":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
			"to":    &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "Empty for a contract creation"},
			"value": &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "Decimal wei"},
		},
	})
	expressionSampleBlockInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "NotificationSampleBlockInput",
		Description: "The block of the sample event",
		Fields: graphql.InputObjectConfigFieldMap{
			"number": &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "Decimal"},
			"time":   &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "Unix seconds, decimal"},
			"hash":   &graphql.InputObjectFieldConfig{Type: graphql.String},
		},
	})
	expressionCheckInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "NotificationExpressionCheckInput",
		Description: "Expressions of a notification setting and, optionally, one sample log or transaction to evaluate them over",
		Fields: graphql.InputObjectConfigFieldMap{
			"event":       &graphql.InputObjectFieldConfig{Type: graphql.String, Description: "Filter event signature with argument names (filter.event)"},
			"condition":   &graphql.InputObjectFieldConfig{Type: graphql.String},
			"payload":     &graphql.InputObjectFieldConfig{Type: graphql.String},
			"log":         &graphql.InputObjectFieldConfig{Type: expressionSampleLogInputType},
			"transaction": &graphql.InputObjectFieldConfig{Type: expressionSampleTxInputType},
			"block":       &graphql.InputObjectFieldConfig{Type: expressionSampleBlockInputType},
		},
	})
	expressionCheckType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "NotificationExpressionCheck",
		Description: "The outcome of a dry run of notification expressions",
		Fields: graphql.Fields{
			"valid":   &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Description: "The expressions would be accepted at registration"},
			"error":   &graphql.Field{Type: graphql.String, Description: "Why they would not, or the sample's evaluation error, or why the sample is not the filter event"},
			"notify":  &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Description: "The sample would be notified"},
			"result":  &graphql.Field{Type: graphql.String, Description: "The payload's value for the sample, as JSON"},
			"decoded": &graphql.Field{Type: graphql.String, Description: "The sample's filter event arguments, as a JSON object"},
		},
	})
)

func inputString(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

func inputUint(m map[string]interface{}, key string) (uint64, error) {
	s := inputString(m, key)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a decimal number", key, s)
	}
	return n, nil
}

// parseExpressionCheck reads the query's input.
func parseExpressionCheck(in map[string]interface{}) (notifications.ExpressionCheck, error) {
	c := notifications.ExpressionCheck{Event: inputString(in, "event"), Condition: inputString(in, "condition"), Payload: inputString(in, "payload")}
	if l, ok := in["log"].(map[string]interface{}); ok {
		c.Log = &notifications.SampleLog{Address: inputString(l, "address"), Data: inputString(l, "data"), TransactionHash: inputString(l, "transactionHash")}
		if i, ok := l["index"].(int); ok && i >= 0 {
			c.Log.Index = uint64(i)
		}
		if topics, ok := l["topics"].([]interface{}); ok {
			for _, t := range topics {
				s, _ := t.(string)
				c.Log.Topics = append(c.Log.Topics, s)
			}
		}
	}
	if tx, ok := in["transaction"].(map[string]interface{}); ok {
		c.Transaction = &notifications.SampleTransaction{Hash: inputString(tx, "hash"), From: inputString(tx, "from"), To: inputString(tx, "to"), Value: inputString(tx, "value")}
	}
	if b, ok := in["block"].(map[string]interface{}); ok {
		c.Block = &notifications.SampleBlock{Hash: inputString(b, "hash")}
		var err error
		if c.Block.Number, err = inputUint(b, "number"); err != nil {
			return c, fmt.Errorf("block: %w", err)
		}
		if c.Block.Time, err = inputUint(b, "time"); err != nil {
			return c, fmt.Errorf("block: %w", err)
		}
	}
	return c, nil
}

// expressionChecker is the notification service's dry run.
type expressionChecker interface {
	CheckExpressions(notifications.ExpressionCheck) (*notifications.ExpressionCheckResult, error)
}

func (s *Schema) resolveCheckNotificationExpressions(p graphql.ResolveParams) (interface{}, error) {
	checker, ok := s.notificationService.(expressionChecker)
	if !ok {
		return nil, fmt.Errorf("notification service not enabled")
	}
	in, _ := p.Args["input"].(map[string]interface{})
	c, err := parseExpressionCheck(in)
	if err != nil {
		return nil, err
	}
	out, err := checker.CheckExpressions(c)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"valid": out.Valid, "notify": out.Notify}
	if out.Error != "" {
		result["error"] = out.Error
	}
	if out.Result != nil {
		result["result"] = string(out.Result)
	}
	if out.Decoded != nil {
		data, err := json.Marshal(out.Decoded)
		if err != nil {
			return nil, err
		}
		result["decoded"] = string(data)
	}
	return result, nil
}

// addNotificationExpressionCheck adds the dry run query.
func (b *SchemaBuilder) addNotificationExpressionCheck() {
	b.queries["checkNotificationExpressions"] = &graphql.Field{
		Type: graphql.NewNonNull(expressionCheckType),
		Description: "Dry run of a notification setting's condition and payload: whether they would be registered and, over a sample log or " +
			"transaction, whether it would be notified and with what payload. Nothing is stored or sent",
		Args: graphql.FieldConfigArgument{
			"input": &graphql.ArgumentConfig{Type: graphql.NewNonNull(expressionCheckInputType)},
		},
		Resolve: requireAPIKey(b.schema.resolveCheckNotificationExpressions),
	}
}
