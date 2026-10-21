package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"marketplace-hw2/internal/api"
	"marketplace-hw2/internal/auth"

	"github.com/google/uuid"
)

type contextKey string

const (
	identityKey contextKey = "identity"
	metaKey     contextKey = "request-meta"
)

type requestMeta struct {
	RequestID string
	UserID    *string
	Body      any
}

func authMiddleware(tokens *auth.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/auth/") {
				next.ServeHTTP(w, r)
				return
			}
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, http.StatusUnauthorized, api.ErrorResponse{
					ErrorCode: api.TOKENINVALID, Message: "Bearer access token is required",
				})
				return
			}
			identity, err := tokens.ParseAccess(parts[1])
			if err != nil {
				body, _ := apiError(tokenFault(err))
				writeError(w, http.StatusUnauthorized, body)
				return
			}
			if meta, ok := r.Context().Value(metaKey).(*requestMeta); ok {
				userID := identity.UserID.String()
				meta.UserID = &userID
			}
			ctx := context.WithValue(r.Context(), identityKey, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func identityFromContext(ctx context.Context) (auth.Identity, bool) {
	identity, ok := ctx.Value(identityKey).(auth.Identity)
	return identity, ok
}

func maxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogMiddleware() func(http.Handler) http.Handler {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return requestLogMiddlewareWithLogger(logger)
}

func requestLogMiddlewareWithLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			meta := &requestMeta{RequestID: uuid.NewString()}
			w.Header().Set("X-Request-Id", meta.RequestID)
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			ctx := context.WithValue(r.Context(), metaKey, meta)
			r = r.WithContext(ctx)
			defer func() {
				if recovered := recover(); recovered != nil {
					internalErrorHandler(recorder, r, nil)
					logger.Error("handler_panic", "request_id", meta.RequestID)
				}
				logRequest(logger, r, meta, started, recorder.status)
			}()
			if isMutating(r.Method) && r.Body != nil {
				body, err := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if err != nil {
					meta.Body = "<unreadable-body>"
					writeError(recorder, http.StatusBadRequest, validationBody("request body could not be read", map[string]any{
						"violations": []map[string]string{{"field": "request", "message": "body is unreadable or exceeds the size limit"}},
					}))
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				meta.Body = maskedBody(body)
			}
			next.ServeHTTP(recorder, r)
		})
	}
}

func logRequest(logger *slog.Logger, r *http.Request, meta *requestMeta, started time.Time, status int) {
	fields := []any{
		"request_id", meta.RequestID,
		"method", r.Method,
		"endpoint", r.URL.Path,
		"status_code", status,
		"duration_ms", time.Since(started).Milliseconds(),
		"user_id", meta.UserID,
		"timestamp", started.UTC().Format(time.RFC3339Nano),
	}
	if isMutating(r.Method) {
		fields = append(fields, "request_body", meta.Body)
	}
	logger.Info("api_request", fields...)
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	if status >= 200 {
		r.wroteHeader = true
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func isMutating(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

func maskedBody(body []byte) any {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "<invalid-json>"
	}
	maskSecrets(value)
	return value
}

func maskSecrets(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			switch strings.ToLower(key) {
			case "password", "access_token", "refresh_token":
				current[key] = "***"
			default:
				maskSecrets(nested)
			}
		}
	case []any:
		for _, nested := range current {
			maskSecrets(nested)
		}
	}
}
