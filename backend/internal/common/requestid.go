package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type requestIDKey struct{}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				requestID = "request-id-unavailable"
			} else {
				requestID = hex.EncodeToString(bytes[:])
			}
		}
		writer.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), requestIDKey{}, requestID)))
	})
}

func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func GetRequestID(request *http.Request) string {
	if request == nil {
		return ""
	}
	return RequestIDFromContext(request.Context())
}
