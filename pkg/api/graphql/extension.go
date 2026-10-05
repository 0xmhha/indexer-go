package graphql

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/graphql-go/graphql"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// Extensions let packages outside the API, such as chain-specific code
// under pkg/chains/<chain>/, add queries, mutations and subscriptions
// without the API depending on them. They register in init; every schema
// built afterwards includes them, in name order.

// Extension is what a registered extension receives while a schema is
// built.
type Extension struct {
	b *SchemaBuilder
}

var (
	extMu         sync.RWMutex
	extensions    = map[string]func(*Extension){}
	subscriptions = map[string]SubscriptionSpec{}
)

// RegisterExtension adds an extension. Registering a name twice panics: it
// is a wiring bug.
func RegisterExtension(name string, fn func(*Extension)) {
	extMu.Lock()
	defer extMu.Unlock()
	if _, dup := extensions[name]; dup {
		panic(fmt.Sprintf("graphql: extension %q registered twice", name))
	}
	extensions[name] = fn
}

// Storage returns the schema's storage.
func (e *Extension) Storage() storage.Storage { return e.b.schema.storage }

// Logger returns the schema's logger.
func (e *Extension) Logger() *zap.Logger { return e.b.schema.logger }

// AddQuery adds a query field. A name already used panics.
func (e *Extension) AddQuery(name string, f *graphql.Field) { addField(e.b.queries, "query", name, f) }

// AddMutation adds a mutation field. A name already used panics.
func (e *Extension) AddMutation(name string, f *graphql.Field) {
	addField(e.b.mutations, "mutation", name, f)
}

// AddSubscription adds a subscription field; its delivery is described by
// RegisterSubscription under the same name.
func (e *Extension) AddSubscription(name string, f *graphql.Field) {
	addField(e.b.subscriptions, "subscription", name, f)
}

func addField(fields graphql.Fields, kind, name string, f *graphql.Field) {
	if _, dup := fields[name]; dup {
		panic(fmt.Sprintf("graphql: %s %q defined twice", kind, name))
	}
	fields[name] = f
}

// applyExtensions runs the registered extensions on the builder, once.
func (b *SchemaBuilder) applyExtensions() {
	if b.extended {
		return
	}
	b.extended = true
	extMu.RLock()
	names := make([]string, 0, len(extensions))
	for n := range extensions {
		names = append(names, n)
	}
	extMu.RUnlock()
	sort.Strings(names)
	for _, n := range names {
		extMu.RLock()
		fn := extensions[n]
		extMu.RUnlock()
		fn(&Extension{b: b})
	}
}

// SubscriptionSpec describes how a subscription field registered by an
// extension is served over /graphql/ws.
type SubscriptionSpec struct {
	// EventType is the event bus type the subscription listens to.
	EventType events.EventType
	// Filter builds an event filter from the subscription's variables
	// (optional).
	Filter func(variables map[string]interface{}) (*events.Filter, error)
	// Payload maps an event to the field's value; false skips the event.
	Payload func(events.Event) (interface{}, bool)
}

// RegisterSubscription describes the delivery of a subscription field.
// Registering a name twice panics.
func RegisterSubscription(name string, spec SubscriptionSpec) {
	extMu.Lock()
	defer extMu.Unlock()
	if _, dup := subscriptions[name]; dup {
		panic(fmt.Sprintf("graphql: subscription %q registered twice", name))
	}
	subscriptions[name] = spec
}

func registeredSubscription(name string) (SubscriptionSpec, bool) {
	extMu.RLock()
	defer extMu.RUnlock()
	spec, ok := subscriptions[name]
	return spec, ok
}

// registeredSubscriptionIn returns the registered subscription named in a
// query, preferring the longest name (so "consensusBlock" wins over a
// shorter name it contains).
func registeredSubscriptionIn(query string) string {
	extMu.RLock()
	defer extMu.RUnlock()
	best := ""
	for name := range subscriptions {
		if len(name) > len(best) && strings.Contains(query, name) {
			best = name
		}
	}
	return best
}

// Shared scalar types for extensions (the schema encodes them as strings).
var (
	BigIntType  = bigIntType
	HashType    = hashType
	AddressType = addressType
	BytesType   = bytesType
)

// PaginationInputType is the input type of pagination arguments.
func PaginationInputType() *graphql.InputObject { return paginationInputType }

// PageInfoType is the page information type of connections.
func PageInfoType() *graphql.Object { return pageInfoType }

// Pagination reads the pagination argument of a field, capping the limit
// at maxLimit (0: the default maximum).
func Pagination(p graphql.ResolveParams, maxLimit int) (limit, offset int) {
	pp := parsePaginationParams(p, maxLimit)
	return pp.Limit, pp.Offset
}
