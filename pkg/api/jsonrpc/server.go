package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/0xmhha/indexer-go/pkg/notifications"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"go.uber.org/zap"
)

// Server handles JSON-RPC HTTP requests
type Server struct {
	handler *Handler
	logger  *zap.Logger
}

// NewServer creates a new JSON-RPC server
func NewServer(store storage.Storage, logger *zap.Logger) *Server {
	return &Server{
		handler: NewHandler(store, logger),
		logger:  logger,
	}
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body size to prevent memory exhaustion (2MB)
	const maxRequestBodySize = 2 << 20 // 2 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("failed to read request body", zap.Error(err))
		s.writeErrorResponse(w, nil, NewError(ParseError, "request body too large or unreadable", err.Error()))
		return
	}
	defer r.Body.Close()

	// Check if it's a batch request
	var isBatch bool
	var firstChar byte
	for _, b := range body {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		firstChar = b
		break
	}
	isBatch = firstChar == '['

	if isBatch {
		s.handleBatchRequest(w, r, body)
	} else {
		s.handleSingleRequest(w, r, body)
	}
}

// handleSingleRequest handles a single JSON-RPC request
func (s *Server) handleSingleRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		s.logger.Error("failed to parse request", zap.Error(err))
		s.writeErrorResponse(w, nil, NewError(ParseError, "parse error", err.Error()))
		return
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		s.writeErrorResponse(w, req.ID, NewError(InvalidRequest, "invalid jsonrpc version", nil))
		return
	}

	// Validate method
	if req.Method == "" {
		s.writeErrorResponse(w, req.ID, NewError(InvalidRequest, "missing method", nil))
		return
	}

	// Execute method
	ctx := r.Context()
	result, rpcErr := s.handler.HandleMethod(ctx, req.Method, req.Params)

	// Write response
	if rpcErr != nil {
		s.writeErrorResponse(w, req.ID, rpcErr)
	} else {
		s.writeSuccessResponse(w, req.ID, result)
	}
}

// handleBatchRequest handles a batch of JSON-RPC requests
func (s *Server) handleBatchRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	var batch BatchRequest
	if err := json.Unmarshal(body, &batch); err != nil {
		s.logger.Error("failed to parse batch request", zap.Error(err))
		s.writeErrorResponse(w, nil, NewError(ParseError, "parse error", err.Error()))
		return
	}

	// Empty batch is invalid
	if len(batch) == 0 {
		s.writeErrorResponse(w, nil, NewError(InvalidRequest, "empty batch", nil))
		return
	}

	// Limit batch size to prevent DoS via large batch arrays
	const maxBatchSize = 100
	if len(batch) > maxBatchSize {
		s.logger.Warn("batch request too large",
			zap.Int("batch_size", len(batch)),
			zap.Int("max_batch_size", maxBatchSize))
		s.writeErrorResponse(w, nil, NewError(InvalidRequest, "batch too large (max 100 requests)", nil))
		return
	}

	ctx := r.Context()
	responses := make(BatchResponse, 0, len(batch))

	for _, req := range batch {
		// Validate JSON-RPC version
		if req.JSONRPC != "2.0" {
			responses = append(responses, *NewErrorResponse(req.ID, NewError(InvalidRequest, "invalid jsonrpc version", nil)))
			continue
		}

		// Validate method
		if req.Method == "" {
			responses = append(responses, *NewErrorResponse(req.ID, NewError(InvalidRequest, "missing method", nil)))
			continue
		}

		// Execute method
		result, rpcErr := s.handler.HandleMethod(ctx, req.Method, req.Params)

		if rpcErr != nil {
			responses = append(responses, *NewErrorResponse(req.ID, rpcErr))
		} else {
			responses = append(responses, *NewResponse(req.ID, result))
		}
	}

	// Write batch response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(responses); err != nil {
		s.logger.Error("failed to encode batch response", zap.Error(err))
	}
}

// writeSuccessResponse writes a successful JSON-RPC response
func (s *Server) writeSuccessResponse(w http.ResponseWriter, id interface{}, result interface{}) {
	resp := NewResponse(id, result)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", zap.Error(err))
	}
}

// writeErrorResponse writes an error JSON-RPC response
func (s *Server) writeErrorResponse(w http.ResponseWriter, id interface{}, rpcErr *Error) {
	resp := NewErrorResponse(id, rpcErr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return 200 OK
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode error response", zap.Error(err))
	}
}

// HandleMethodDirect directly handles a method call (for testing)
func (s *Server) HandleMethodDirect(ctx context.Context, method string, params json.RawMessage) (interface{}, *Error) {
	return s.handler.HandleMethod(ctx, method, params)
}

// SetNotificationService sets the notification service for JSON-RPC handlers
func (s *Server) SetNotificationService(service notifications.Service) {
	s.handler.SetNotificationService(service)
}
