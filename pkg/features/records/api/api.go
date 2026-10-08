// Package api serves the records of declared tables over GraphQL
// (refactoring plan R6-1). It is a GraphQL extension, linked in with the
// feature.
package api

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	gql "github.com/graphql-go/graphql"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/features/records"
)

func init() { graphql.RegisterExtension("records", register) }

var (
	fieldType = gql.NewObject(gql.ObjectConfig{
		Name: "RecordField",
		Fields: gql.Fields{
			"name":  {Type: gql.NewNonNull(gql.String)},
			"value": {Type: gql.NewNonNull(gql.String), Description: "Addresses and bytes as lower-case 0x hex, integers in decimal"},
		},
	})
	recordType = gql.NewObject(gql.ObjectConfig{
		Name:        "Record",
		Description: "A log of a declared table (features.records)",
		Fields: gql.Fields{
			"table":           {Type: gql.NewNonNull(gql.String)},
			"blockNumber":     {Type: gql.NewNonNull(graphql.BigIntType)},
			"blockTime":       {Type: gql.NewNonNull(graphql.BigIntType), Description: "Unix seconds"},
			"transactionHash": {Type: gql.NewNonNull(graphql.HashType)},
			"logIndex":        {Type: gql.NewNonNull(gql.Int)},
			"address":         {Type: gql.NewNonNull(graphql.AddressType), Description: "The contract that emitted the log"},
			"fields":          {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(fieldType))), Description: "The event's arguments, in order"},
			"field": {
				Type: gql.String, Description: "One argument's value, null when the event has no such argument",
				Args: gql.FieldConfigArgument{"name": {Type: gql.NewNonNull(gql.String)}},
				Resolve: func(p gql.ResolveParams) (interface{}, error) {
					m, _ := p.Source.(map[string]interface{})
					values, _ := m["values"].(map[string]string)
					name, _ := p.Args["name"].(string)
					if v, ok := values[name]; ok {
						return v, nil
					}
					return nil, nil
				},
			},
		},
	})
	recordConnection = gql.NewObject(gql.ObjectConfig{
		Name: "RecordConnection",
		Fields: gql.Fields{
			"nodes":    {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(recordType)))},
			"pageInfo": {Type: gql.NewNonNull(graphql.PageInfoType())},
		},
	})
	whereType = gql.NewInputObject(gql.InputObjectConfig{
		Name:        "RecordFieldInput",
		Description: "A field's value; the fields given must be one of the table's declared keys",
		Fields: gql.InputObjectConfigFieldMap{
			"field": {Type: gql.NewNonNull(gql.String)},
			"value": {Type: gql.NewNonNull(gql.String)},
		},
	})
)

var errUnsupported = errors.New("no declared tables are served (features.records)")

func register(e *graphql.Extension) {
	store := e.Storage()
	reader, _ := store.(port.RecordReader)
	e.AddQuery("records", &gql.Field{
		Type: gql.NewNonNull(recordConnection),
		Description: "A declared table's records, oldest first: every record, or with where those whose values match one of " +
			"the table's declared keys",
		Args: gql.FieldConfigArgument{
			"table":      {Type: gql.NewNonNull(gql.String)},
			"where":      {Type: gql.NewList(gql.NewNonNull(whereType))},
			"pagination": {Type: graphql.PaginationInputType()},
		},
		Resolve: func(p gql.ResolveParams) (interface{}, error) {
			plan := records.Lookup(store)
			if plan == nil || reader == nil {
				return nil, errUnsupported
			}
			name, _ := p.Args["table"].(string)
			table, ok := plan.Table(name)
			if !ok {
				return nil, fmt.Errorf("no table %q is declared", name)
			}
			pg := graphql.Page(p, 0)
			where, _ := p.Args["where"].([]interface{})
			var (
				items []*port.Record
				next  string
				err   error
			)
			if len(where) == 0 {
				items, next, err = reader.ListRecords(p.Context, table.Name, pg)
			} else {
				var key port.RecordKey
				if key, err = keyOf(table, where); err != nil {
					return nil, err
				}
				items, next, err = reader.ListRecordsByKey(p.Context, table.Name, key, pg)
			}
			if err != nil {
				return nil, err
			}
			nodes := make([]map[string]interface{}, len(items))
			for i, r := range items {
				nodes[i] = recordMap(table, r)
			}
			return map[string]interface{}{"nodes": nodes, "pageInfo": graphql.CursorPageInfo(pg, next)}, nil
		},
	})
}

// keyOf finds the declared key with the where fields and normalizes the
// values as records store them.
func keyOf(table *declared.TablePlan, where []interface{}) (port.RecordKey, error) {
	given := map[string]string{}
	names := make([]string, 0, len(where))
	for _, w := range where {
		m, _ := w.(map[string]interface{})
		field, _ := m["field"].(string)
		value, _ := m["value"].(string)
		if _, dup := given[field]; dup {
			return port.RecordKey{}, fmt.Errorf("field %q is given twice", field)
		}
		given[field] = value
		names = append(names, field)
	}
	fields, ok := table.Key(names)
	if !ok {
		sort.Strings(names)
		return port.RecordKey{}, fmt.Errorf("table %q declares no key %v (keys: %v)", table.Name, names, table.Keys)
	}
	key := port.RecordKey{ID: declared.KeyID(fields), Values: make([]string, len(fields))}
	for i, f := range fields {
		for _, arg := range table.Event.Inputs {
			if arg.Name == f {
				v, err := declared.FormatInput(arg, given[f])
				if err != nil {
					return port.RecordKey{}, err
				}
				key.Values[i] = v
			}
		}
	}
	return key, nil
}

func recordMap(table *declared.TablePlan, r *port.Record) map[string]interface{} {
	fields := make([]map[string]interface{}, 0, len(r.Fields))
	for _, name := range table.Fields() {
		if v, ok := r.Fields[name]; ok {
			fields = append(fields, map[string]interface{}{"name": name, "value": v})
		}
	}
	return map[string]interface{}{
		"table": r.Table, "blockNumber": strconv.FormatUint(r.BlockNumber, 10), "blockTime": strconv.FormatUint(r.BlockTime, 10),
		"transactionHash": r.TxHash.Hex(), "logIndex": int(r.LogIndex), "address": r.Address.Hex(),
		"fields": fields, "values": r.Fields,
	}
}
