package indexing

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

type sourceSearchRequest struct {
	filter capture.SearchFilter
	cursor string
	limit  int
}

func parseSourceSearch(r *http.Request, kind string) (out sourceSearchRequest, err error) {
	if len(r.URL.RawQuery) > 8192 {
		return out, port.ErrInvalidData
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return out, port.ErrInvalidData
	}
	for k, v := range values {
		if len(v) != 1 {
			return out, port.ErrInvalidData
		}
		switch k {
		case "from", "to", "address", "role", "topic0", "topic1", "topic2", "topic3", "from_time", "to_time", "after", "limit":
		default:
			return out, port.ErrInvalidData
		}
	}
	q := capture.SearchFilter{Kind: capture.RowLog}
	if kind == "transactions" {
		q.Kind = capture.RowTransaction
	}
	q.From, e = strconv.ParseUint(values.Get("from"), 10, 64)
	if e != nil {
		return out, port.ErrInvalidData
	}
	q.To, e = strconv.ParseUint(values.Get("to"), 10, 64)
	if e != nil {
		return out, port.ErrInvalidData
	}
	parse := func(s string, n int) ([]byte, error) {
		if len(s) != 2+n*2 || !strings.HasPrefix(s, "0x") {
			return nil, port.ErrInvalidData
		}
		return hex.DecodeString(s[2:])
	}
	if values.Has("address") {
		parts := strings.Split(values.Get("address"), ",")
		if len(parts) > capture.MaxSearchAlternatives {
			return out, port.ErrInvalidData
		}
		for _, part := range parts {
			b, e := parse(part, 20)
			if e != nil {
				return out, port.ErrInvalidData
			}
			q.Addresses = append(q.Addresses, common.BytesToAddress(b))
		}
	}
	q.Role = values.Get("role")
	q.TopicAny = make([][]common.Hash, 4)
	for i := 0; i < 4; i++ {
		key := "topic" + strconv.Itoa(i)
		if values.Has(key) {
			parts := strings.Split(values.Get(key), ",")
			if len(parts) > capture.MaxSearchAlternatives {
				return out, port.ErrInvalidData
			}
			for _, part := range parts {
				b, e := parse(part, 32)
				if e != nil {
					return out, port.ErrInvalidData
				}
				q.TopicAny[i] = append(q.TopicAny[i], common.BytesToHash(b))
			}
		}
	}
	for _, field := range []struct {
		key    string
		target **uint64
	}{{"from_time", &q.FromTime}, {"to_time", &q.ToTime}} {
		if values.Has(field.key) {
			n, e := strconv.ParseUint(values.Get(field.key), 10, 64)
			if e != nil {
				return out, port.ErrInvalidData
			}
			*field.target = &n
		}
	}
	q, e = q.Normalize()
	if e != nil {
		return out, port.ErrInvalidData
	}
	limit := 50
	if values.Has("limit") {
		limit, e = strconv.Atoi(values.Get("limit"))
		if e != nil || limit < 1 || limit > 256 {
			return out, port.ErrInvalidData
		}
	}
	cursor := values.Get("after")
	if values.Has("after") && cursor == "" {
		return out, port.ErrInvalidData
	}
	return sourceSearchRequest{q, cursor, limit}, nil
}
