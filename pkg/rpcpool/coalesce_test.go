package rpcpool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type readTransport func(*http.Request) (*http.Response, error)

func (f readTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func readResponse(data []byte) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}
}

// Queue the whole group before starting the dispatcher, so tests establish
// concurrency by a gate rather than depending on scheduler timing.
func stagedReadPool(t *testing.T, transport http.RoundTripper) (*Pool, func(int)) {
	t.Helper()
	p, err := New(Config{Endpoints: []string{"http://node"}, Timeout: time.Second, Transport: transport})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	p.reads = &readCoalescer{pool: p, ctx: ctx, cancel: cancel, jobs: make(chan *readRequest, readQueueCapacity), done: make(chan struct{})}
	var started atomic.Bool
	t.Cleanup(func() {
		if !started.Load() {
			go p.reads.run()
		}
		p.Close()
	})
	return p, func(n int) {
		require.Eventually(t, func() bool { return len(p.reads.jobs) == n }, time.Second, time.Millisecond)
		started.Store(true)
		go p.reads.run()
	}
}

func submitRead(p *Pool, ctx context.Context, body string) <-chan readReply {
	out := make(chan readReply, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, "POST", p.Primary(), bytes.NewBufferString(body))
		resp, err := p.RoundTrip(req)
		out <- readReply{resp, err}
	}()
	return out
}

func replyJSON(t *testing.T, ch <-chan readReply) json.RawMessage {
	t.Helper()
	select {
	case reply := <-ch:
		require.NoError(t, reply.err)
		defer func() { _ = reply.response.Body.Close() }()
		data, err := io.ReadAll(reply.response.Body)
		require.NoError(t, err)
		return data
	case <-time.After(2 * time.Second):
		t.Fatal("read did not finish")
		return nil
	}
}

func TestCoalescedReadsRestoreIDsShapesAndMemberErrors(t *testing.T) {
	var calls atomic.Int32
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		var parts []rpcObject
		require.NoError(t, json.NewDecoder(req.Body).Decode(&parts))
		require.Len(t, parts, 3)
		var replies []rpcObject
		for i := len(parts) - 1; i >= 0; i-- {
			part := parts[i]
			if string(part["method"]) == `"eth_call"` {
				replies = append(replies, rpcObject{"jsonrpc": json.RawMessage(`"2.0"`), "id": part["id"], "error": json.RawMessage(`{"code":3,"message":"execution reverted"}`)})
			} else {
				replies = append(replies, rpcObject{"jsonrpc": json.RawMessage(`"2.0"`), "id": part["id"], "result": part["params"]})
			}
		}
		data, _ := json.Marshal(replies)
		return readResponse(data), nil
	}))
	a := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x12",false]}`)
	b := submitRead(p, context.Background(), `[{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["acct",{"blockHash":"hash","requireCanonical":true}]},{"jsonrpc":"2.0","id":"caller","method":"eth_call","params":[]}]`)
	start(2)
	var single rpcObject
	require.NoError(t, json.Unmarshal(replyJSON(t, a), &single))
	require.Equal(t, `1`, string(single["id"]))
	require.JSONEq(t, `["0x12",false]`, string(single["result"]))
	var batch []rpcObject
	require.NoError(t, json.Unmarshal(replyJSON(t, b), &batch))
	require.Len(t, batch, 2)
	require.Equal(t, `1`, string(batch[0]["id"]))
	require.JSONEq(t, `["acct",{"blockHash":"hash","requireCanonical":true}]`, string(batch[0]["result"]))
	require.Equal(t, `"caller"`, string(batch[1]["id"]))
	require.NotNil(t, batch[1]["error"])
	require.EqualValues(t, 1, calls.Load())
}

func TestCoalescedReadsRejectMissingDuplicateAndUnknownReplies(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
				var parts []rpcObject
				require.NoError(t, json.NewDecoder(req.Body).Decode(&parts))
				for _, part := range parts {
					delete(part, "method")
					delete(part, "params")
					part["result"] = json.RawMessage(`"ok"`)
				}
				switch kind {
				case "missing":
					parts = parts[:1]
				case "duplicate":
					parts[1]["id"] = parts[0]["id"]
				case "unknown":
					parts[1]["id"] = json.RawMessage(`"unknown"`)
				}
				data, _ := json.Marshal(parts)
				return readResponse(data), nil
			}))
			a := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
			b := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`)
			start(2)
			require.Error(t, (<-a).err)
			require.Error(t, (<-b).err)
		})
	}
}

func TestCoalescedReadsCancellationDoesNotCancelPeers(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		var parts []rpcObject
		require.NoError(t, json.NewDecoder(req.Body).Decode(&parts))
		close(entered)
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-release:
		}
		for _, part := range parts {
			delete(part, "method")
			part["result"] = json.RawMessage(`"0x1"`)
		}
		data, _ := json.Marshal(parts)
		return readResponse(data), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := submitRead(p, ctx, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	b := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}`)
	start(2)
	<-entered
	cancel()
	require.ErrorIs(t, (<-a).err, context.Canceled)
	close(release)
	require.NotEmpty(t, replyJSON(t, b))
	p.Close()
	p.Close()
}

func TestCoalescedReadsCloseJoinsInflightAndQueued(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-req.Context().Done()
		return nil, req.Context().Err()
	}))
	a := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	start(1)
	<-entered
	b := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}`)
	p.Close()
	require.Error(t, (<-a).err)
	require.Error(t, (<-b).err)
}

func TestCoalescedReadsCancelWholeGroup(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		close(exited)
		return nil, req.Context().Err()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := submitRead(p, ctx, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	b := submitRead(p, ctx, `{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}`)
	start(2)
	<-entered
	cancel()
	require.ErrorIs(t, (<-a).err, context.Canceled)
	require.ErrorIs(t, (<-b).err, context.Canceled)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("canceled group still in flight")
	}
}

func TestCoalescedReadsBoundMembersAndPreserveCredentials(t *testing.T) {
	var calls atomic.Int32
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var parts []rpcObject
		batch := data[0] == '['
		if batch {
			require.NoError(t, json.Unmarshal(data, &parts))
		} else {
			var part rpcObject
			require.NoError(t, json.Unmarshal(data, &part))
			parts = []rpcObject{part}
		}
		require.LessOrEqual(t, len(parts), readBatchMembers)
		for _, part := range parts {
			require.Equal(t, `["`+req.Header.Get("Authorization")+`"]`, string(part["params"]))
			delete(part, "method")
			part["result"] = json.RawMessage(`"ok"`)
		}
		if batch {
			data, _ = json.Marshal(parts)
		} else {
			data, _ = json.Marshal(parts[0])
		}
		return readResponse(data), nil
	}))
	channels := make([]<-chan readReply, 0, 3)
	for _, credential := range []string{"a", "b", "a"} {
		parts := make([]rpcObject, readBatchMembers)
		for i := range parts {
			parts[i] = rpcObject{"jsonrpc": json.RawMessage(`"2.0"`), "id": json.RawMessage(`1`), "method": json.RawMessage(`"eth_getBalance"`), "params": json.RawMessage(`["` + credential + `"]`)}
		}
		body, _ := json.Marshal(parts)
		out := make(chan readReply, 1)
		channels = append(channels, out)
		go func() {
			req, _ := http.NewRequest("POST", p.Primary(), bytes.NewReader(body))
			req.Header.Set("Authorization", credential)
			resp, err := p.RoundTrip(req)
			out <- readReply{resp, err}
		}()
	}
	start(3)
	for _, out := range channels {
		replyJSON(t, out)
	}
	require.EqualValues(t, 3, calls.Load())
}

// Use small envelopes here: the member bound must not itself separate the
// requests and accidentally mask a broken credential-compatibility check.
func TestCoalescedReadsDifferentCredentialsStaySeparate(t *testing.T) {
	var calls atomic.Int32
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		var part rpcObject
		require.NoError(t, json.NewDecoder(req.Body).Decode(&part), "different credentials must not form a batch")
		require.JSONEq(t, `["`+req.Header.Get("Authorization")+`"]`, string(part["params"]))
		delete(part, "method")
		part["result"] = json.RawMessage(`"ok"`)
		data, _ := json.Marshal(part)
		return readResponse(data), nil
	}))
	var replies []<-chan readReply
	for _, credential := range []string{"a", "b"} {
		out := make(chan readReply, 1)
		replies = append(replies, out)
		go func() {
			req, _ := http.NewRequest("POST", p.Primary(), bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["`+credential+`"]}`))
			req.Header.Set("Authorization", credential)
			resp, err := p.RoundTrip(req)
			out <- readReply{resp, err}
		}()
	}
	start(2)
	for _, out := range replies {
		replyJSON(t, out)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestCoalescedReadsUnsupportedEnvelopeRetriesOriginals(t *testing.T) {
	var calls atomic.Int32
	p, start := stagedReadPool(t, readTransport(func(req *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		calls.Add(1)
		if data[0] == '[' {
			return readResponse([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch unsupported"}}`)), nil
		}
		var part rpcObject
		require.NoError(t, json.Unmarshal(data, &part))
		delete(part, "method")
		part["result"] = json.RawMessage(`"ok"`)
		data, _ = json.Marshal(part)
		return readResponse(data), nil
	}))
	a := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`)
	b := submitRead(p, context.Background(), `{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}`)
	start(2)
	replyJSON(t, a)
	replyJSON(t, b)
	require.True(t, p.reads.unsupported.Load())
	require.EqualValues(t, 3, calls.Load())
}

func TestReadCoalescingExcludesWritesAndNotifications(t *testing.T) {
	req, _ := http.NewRequest("POST", "http://node", nil)
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction"}`, `{"jsonrpc":"2.0","method":"eth_blockNumber"}`, `[{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},{"jsonrpc":"2.0","id":2,"method":"admin_addPeer"}]`} {
		parts, _ := readEnvelope(req, []byte(body))
		require.Empty(t, parts)
	}
}

func TestRateLimitChargesEachFailoverAttempt(t *testing.T) {
	var times []time.Time
	p, err := New(Config{Endpoints: []string{"http://primary", "http://fallback"}, RateLimit: 100, Burst: 1, Transport: readTransport(func(req *http.Request) (*http.Response, error) {
		times = append(times, time.Now())
		if req.URL.Host == "primary" {
			return nil, errors.New("offline")
		}
		return readResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)), nil
	})})
	require.NoError(t, err)
	req, _ := http.NewRequest("POST", p.Primary(), bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
	resp, err := p.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), 8*time.Millisecond)
}
