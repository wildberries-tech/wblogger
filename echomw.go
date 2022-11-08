package wblogger

import (
	"time"

	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func EchoMWLogger(ignoreUrls []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)
			if err != nil {
				c.Error(err)
			}

			req := c.Request()
			res := c.Response()

			id := req.Header.Get(echo.HeaderXRequestID)
			if id == "" {
				id = res.Header().Get(echo.HeaderXRequestID)
			}

			fields := []zapcore.Field{
				zap.Int("status", res.Status),
				zap.Float64("latency", time.Since(start).Seconds()),
				zap.String("id", id),
				zap.String("method", req.Method),
				zap.String("uri", req.RequestURI),
				zap.String("host", req.Host),
				zap.String("remote_ip", c.RealIP()),
			}

			n := res.Status
			switch {
			case n >= 500:
				logger.Error("httpserver_server_error", fields...)
			case n >= 400:
				logger.Warn("httpserver_client_error", fields...)
			case n >= 300:
				logger.Debug("httpserver_redirection", fields...)
			default:
				for _, url := range ignoreUrls {
					if req.URL.Path == url {
						return nil
					}
				}
				logger.Debug("httpserver_success", fields...)
			}

			return nil
		}
	}
}
