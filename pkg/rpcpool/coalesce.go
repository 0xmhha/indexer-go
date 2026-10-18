package rpcpool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

const (
	readQueueCapacity = 128
	readBatchMembers  = 64
	readEnvelopeBytes = 1 << 20
	readWindow        = 10 * time.Millisecond
	maintenanceWindow = 500 * time.Millisecond
)

type rpcObject map[string]json.RawMessage

// Explicit allowlist: notifications, writes and unknown methods never enter
// the dispatcher. Existing batches are flattened only when every member is a
// read. Params and block references are left untouched, including EIP-1898.
func readEnvelope(req *http.Request, body []byte) ([]rpcObject, bool) {
	if req.Method != http.MethodPost || len(body) > readEnvelopeBytes {
		return nil, false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, false
	}
	batch := body[0] == '['
	var parts []rpcObject
	if batch {
		if json.Unmarshal(body, &parts) != nil {
			return nil, false
		}
	} else {
		var part rpcObject
		if json.Unmarshal(body, &part) != nil {
			return nil, false
		}
		parts = []rpcObject{part}
	}
	if len(parts) == 0 || len(parts) > readBatchMembers {
		return nil, false
	}
	for _, part := range parts {
		var method, version string
		if json.Unmarshal(part["method"], &method) != nil || json.Unmarshal(part["jsonrpc"], &version) != nil || version != "2.0" {
			return nil, false
		}
		id := bytes.TrimSpace(part["id"])
		if len(id) == 0 || bytes.Equal(id, []byte("null")) {
			return nil, false
		}
		var value any
		if json.Unmarshal(id, &value) != nil {
			return nil, false
		}
		switch value.(type) {
		case string, float64:
		default:
			return nil, false
		}
		switch method {
		case "eth_chainId", "eth_blockNumber", "eth_getBlockByNumber", "eth_getBlockByHash", "eth_getBlockReceipts", "eth_getTransactionReceipt", "eth_getBalance", "eth_getCode", "eth_call", "eth_getStorageAt", "eth_getTransactionByHash", "eth_getLogs":
		default:
			return nil, false
		}
	}
	return parts, batch
}

type readReply struct {
	response *http.Response
	err      error
}
type readRequest struct {
	req         *http.Request
	parts       []rpcObject
	batch       bool
	reply       chan readReply
	traceID     uint64
	maintenance bool
}

var readTraceID atomic.Uint64

type readCoalescer struct {
	pool        *Pool
	ctx         context.Context
	cancel      context.CancelFunc
	jobs        chan *readRequest
	done        chan struct{}
	unsupported atomic.Bool
}

func newReadCoalescer(pool *Pool) *readCoalescer {
	ctx, cancel := context.WithCancel(context.Background())
	c := &readCoalescer{pool: pool, ctx: ctx, cancel: cancel, jobs: make(chan *readRequest, readQueueCapacity), done: make(chan struct{})}
	go c.run()
	return c
}

func (c *readCoalescer) close() { c.cancel(); <-c.done }

func (c *readCoalescer) roundTrip(req *http.Request, parts []rpcObject, batch bool) (*http.Response, error) {
	region := trace.StartRegion(req.Context(), "rpc.read")
	defer region.End()
	job := &readRequest{req: req.Clone(req.Context()), parts: parts, batch: batch, reply: make(chan readReply, 1), maintenance: port.IsRPCMaintenance(req.Context())}
	if trace.IsEnabled() {
		job.traceID = readTraceID.Add(1)
		trace.Logf(req.Context(), "rpc.readID", "%d", job.traceID)
		role := "foreground"
		if job.maintenance {
			role = "maintenance"
		}
		trace.Log(req.Context(), "rpc.readRole", role)
	}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-c.ctx.Done():
		return nil, errors.New("rpcpool: closed")
	case c.jobs <- job:
	}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-c.ctx.Done():
		return nil, errors.New("rpcpool: closed")
	case reply := <-job.reply:
		return reply.response, reply.err
	}
}

// Reserve one physical HTTP token, then collect reads that arrived while
// waiting. Up to four groups may be in flight; a slow response must not hold
// the next token. The bounded channel provides cancellation-aware backpressure.
func (c *readCoalescer) run() {
	defer close(c.done)
	var workers sync.WaitGroup
	defer workers.Wait()
	slots := make(chan struct{}, 4)
	var carry *readRequest
	for {
		first := carry
		carry = nil
		if first == nil {
			select {
			case <-c.ctx.Done():
				return
			case first = <-c.jobs:
			}
		}
		if first.req.Context().Err() != nil {
			continue
		}
		select {
		case <-c.ctx.Done():
			return
		case slots <- struct{}{}:
		}
		window := trace.StartRegion(c.ctx, "rpc.window")
		if trace.IsEnabled() {
			trace.Logf(c.ctx, "rpc.first", "%d", first.traceID)
		}
		group := readGroup{jobs: []*readRequest{first}, members: len(first.parts)}
		delay := c.maintenanceDelay(first)
		timer := time.NewTimer(delay)
		if delay > readWindow {
			if c.waitMaintenance(&group, timer.C) != nil {
				timer.Stop()
				window.End()
				return
			}
		} else {
			select {
			case <-c.ctx.Done():
				timer.Stop()
				window.End()
				return
			case <-timer.C:
			}
		}
		timer.Stop()
		window.End()
		if c.pool.limiter != nil {
			if c.pool.waitLimit(c.ctx) != nil {
				return
			}
		}
	collect:
		for !c.unsupported.Load() && group.members < readBatchMembers && group.carry == nil {
			select {
			case job := <-c.jobs:
				if !group.add(job) {
					break collect
				}
			default:
				break collect
			}
		}
		carry = group.carry
		active := group.jobs[:0]
		for _, job := range group.jobs {
			if job.req.Context().Err() == nil {
				active = append(active, job)
			}
		}
		if len(active) == 0 {
			<-slots
			continue
		}
		workers.Add(1)
		go func(group []*readRequest) {
			defer workers.Done()
			defer func() { <-slots }()
			c.dispatch(group)
		}(active)
	}
}

func sameReadHeaders(a, b *http.Request) bool {
	return a.URL.String() == b.URL.String() && a.Host == b.Host && reflect.DeepEqual(a.Header, b.Header)
}

func originalBody(job *readRequest) []byte {
	if job.batch {
		data, _ := json.Marshal(job.parts)
		return data
	}
	data, _ := json.Marshal(job.parts[0])
	return data
}

func (c *readCoalescer) dispatch(group []*readRequest) {
	region := trace.StartRegion(c.ctx, "rpc.dispatch")
	defer region.End()
	if trace.IsEnabled() {
		for _, job := range group {
			trace.Logf(c.ctx, "rpc.member", "%d", job.traceID)
		}
	}
	// A singleton preserves the original envelope and caller deadline.
	if len(group) == 1 {
		job := group[0]
		ctx, cancel := context.WithCancel(job.req.Context())
		stop := context.AfterFunc(c.ctx, cancel)
		defer stop()
		defer cancel()
		resp, err := c.pool.send(ctx, job.req, originalBody(job), true)
		job.reply <- readReply{resp, err}
		return
	}
	var combined []rpcObject
	type owner struct {
		job    int
		member int
		id     json.RawMessage
	}
	owners := make(map[string]owner)
	for j, job := range group {
		for m, part := range job.parts {
			copyPart := make(rpcObject, len(part))
			for key, value := range part {
				copyPart[key] = value
			}
			id := fmt.Sprintf("read-%d", len(combined))
			owners[id] = owner{j, m, part["id"]}
			copyPart["id"], _ = json.Marshal(id)
			combined = append(combined, copyPart)
		}
	}
	body, _ := json.Marshal(combined)
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	var remaining atomic.Int32
	remaining.Store(int32(len(group)))
	stops := make([]func() bool, 0, len(group))
	for _, job := range group {
		stops = append(stops, context.AfterFunc(job.req.Context(), func() {
			if remaining.Add(-1) == 0 {
				cancel()
			}
		}))
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	resp, err := c.pool.send(ctx, group[0].req, body, true)
	if err != nil {
		replyAll(group, nil, err)
		return
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		replyAll(group, nil, err)
		return
	}
	// Only an explicit envelope-level invalid-request error permits retrying
	// original envelopes. Member errors (including reverts/pruned state) remain
	// errors of that member and never trigger duplicate calls or approximation.
	var envelope struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &envelope) == nil && envelope.Error != nil && envelope.Error.Code == -32600 && (len(envelope.ID) == 0 || bytes.Equal(bytes.TrimSpace(envelope.ID), []byte("null"))) {
		c.unsupported.Store(true)
		for _, job := range group {
			if job.req.Context().Err() != nil {
				continue
			}
			ctx, cancel := context.WithCancel(job.req.Context())
			stop := context.AfterFunc(c.ctx, cancel)
			response, sendErr := c.pool.send(ctx, job.req, originalBody(job), false)
			stop()
			cancel()
			job.reply <- readReply{response, sendErr}
		}
		return
	}
	var replies []rpcObject
	if resp.StatusCode != http.StatusOK || json.Unmarshal(data, &replies) != nil || len(replies) != len(owners) {
		replyAll(group, nil, errors.New("rpcpool: invalid coalesced response"))
		return
	}
	results := make([][]rpcObject, len(group))
	for j, job := range group {
		results[j] = make([]rpcObject, len(job.parts))
	}
	for _, part := range replies {
		var id string
		if json.Unmarshal(part["id"], &id) != nil {
			replyAll(group, nil, errors.New("rpcpool: invalid coalesced response id"))
			return
		}
		owner, ok := owners[id]
		if !ok || results[owner.job][owner.member] != nil {
			replyAll(group, nil, errors.New("rpcpool: unknown or duplicate coalesced response id"))
			return
		}
		part["id"] = owner.id
		results[owner.job][owner.member] = part
	}
	for j, job := range group {
		var payload []byte
		if job.batch {
			payload, _ = json.Marshal(results[j])
		} else {
			payload, _ = json.Marshal(results[j][0])
		}
		response := *resp
		response.Header = resp.Header.Clone()
		response.Header.Del("Content-Encoding")
		response.Header.Set("Content-Length", fmt.Sprint(len(payload)))
		response.Body = io.NopCloser(bytes.NewReader(payload))
		response.ContentLength = int64(len(payload))
		response.Request = job.req
		job.reply <- readReply{&response, nil}
	}
}

func replyAll(group []*readRequest, resp *http.Response, err error) {
	for _, job := range group {
		job.reply <- readReply{resp, err}
	}
}
