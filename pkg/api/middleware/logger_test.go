package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestLogger(t *testing.T) {
	logger := zap.NewNop()
	middleware := Logger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	middleware(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", w.Code)
	}

	body := w.Body.String()
	if body != "test response" {
		t.Errorf("expected 'test response', got %v", body)
	}
}

func TestLoggerWithLevel(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		expectedBody string
	}{
		{
			name:         "2xx success",
			statusCode:   http.StatusOK,
			expectedBody: "success",
		},
		{
			name:         "4xx client error",
			statusCode:   http.StatusBadRequest,
			expectedBody: "client error",
		},
		{
			name:         "5xx server error",
			statusCode:   http.StatusInternalServerError,
			expectedBody: "server error",
		},
	}

	logger := zap.NewNop()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := LoggerWithLevel(logger)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.expectedBody))
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			middleware(handler).ServeHTTP(w, req)

			if w.Code != tt.statusCode {
				t.Errorf("expected status %v, got %v", tt.statusCode, w.Code)
			}

			body := w.Body.String()
			if body != tt.expectedBody {
				t.Errorf("expected '%v', got %v", tt.expectedBody, body)
			}
		})
	}
}

func TestResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	wrapped := wrapResponseWriter(w)

	// Initial status should be 0
	if wrapped.Status() != 0 {
		t.Errorf("expected initial status 0, got %v", wrapped.Status())
	}

	// Write header
	wrapped.WriteHeader(http.StatusOK)

	if wrapped.Status() != http.StatusOK {
		t.Errorf("expected status OK, got %v", wrapped.Status())
	}

	// Writing header again should not change status
	wrapped.WriteHeader(http.StatusBadRequest)

	if wrapped.Status() != http.StatusOK {
		t.Errorf("expected status to remain OK, got %v", wrapped.Status())
	}
}
