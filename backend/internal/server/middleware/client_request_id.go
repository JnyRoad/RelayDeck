package middleware

import (
	"context"
	"strings"

	"github.com/JnyRoad/RelayDeck/internal/pkg/ctxkey"
	"github.com/JnyRoad/RelayDeck/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const clientRequestIDHeader = "X-Client-Request-ID"

// ClientRequestID ensures every request has a unique client_request_id in request.Context().
//
// This is used by the Ops monitoring module for end-to-end request correlation.
func ClientRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		ensureClientRequestID(c)
		c.Next()
	}
}

// ensureClientRequestID 将关联标识写入上下文和响应头，并可被热路径观测中间件提前复用。
func ensureClientRequestID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}

	if value, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(value) != "" {
		var valid bool
		value, valid = normalizeCorrelationID(value)
		if !valid {
			value = uuid.New().String()
		}
		c.Header(clientRequestIDHeader, value)
		ctx := context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, value)
		c.Request = c.Request.WithContext(ctx)
		return value
	}

	id := uuid.New().String()
	c.Header(clientRequestIDHeader, id)
	ctx := context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, id)
	requestLogger := logger.FromContext(ctx).With(zap.String("client_request_id", strings.TrimSpace(id)))
	ctx = logger.IntoContext(ctx, requestLogger)
	c.Request = c.Request.WithContext(ctx)
	return id
}
