package wblogger

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const IOCTL_SYNC_ERROR = "sync /dev/stdout: inappropriate ioctl for device"

type logField string

const (
	TraceID  logField = "traceID"
	OrderUID logField = "orderUID"
	Handler  logField = "handler"
	ClientID logField = "clientID"
	UserID   logField = "userID"
	ItemSRID logField = "itemSRID"
	ItemRID  logField = "itemRID"
)

var logFields = []logField{TraceID, UserID, OrderUID, Handler, ClientID, ItemSRID, ItemRID}

var (
	logger        *zap.Logger
	fieldsKey     string
	ctxUserFields []string
)

func init() {
	config := zap.NewProductionConfig()
	lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
	l := os.Getenv("WBLOGGER_LEVEL")
	if l != "" {
		switch l {
		case "DEBUG":
			lvl = zap.NewAtomicLevelAt(zap.DebugLevel)
		case "WARN":
			lvl = zap.NewAtomicLevelAt(zap.WarnLevel)
		case "ERROR":
			lvl = zap.NewAtomicLevelAt(zap.ErrorLevel)
		}
	}
	fieldsKey = "wblogger.fields" + strconv.Itoa(time.Now().Second())
	config.Level = lvl
	config.OutputPaths = []string{"stdout"}
	config.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
	log, err := config.Build(zap.AddCallerSkip(1)) // записываем не logger в каждую запись, а реального вызывающего
	if err != nil {
		panic(err)
	}
	logger = log
}

func CtxField(field string) {
	ctxUserFields = append(ctxUserFields, field)
}

// WithSentry attaches sentry to logger.
// All Error calls will be written to sentry
func WithSentry(dsn, environment, version string) error {
	return sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: environment,
		Release:     version,
	})
}

func WithField(ctx context.Context, key, value string) context.Context {
	var fields []string
	v := ctx.Value(fieldsKey)
	if v != nil {
		fields = v.([]string)
	}
	fields = append(fields, key, value)
	return context.WithValue(ctx, fieldsKey, fields)
}

// Flush flushes all logs to stdout.
func Flush() {
	sentry.Flush(time.Second * 3)
	if err := logger.Sync(); err != nil && err.Error() != IOCTL_SYNC_ERROR {
		fmt.Println("Ologger exit error", err)
	}
}

// Info logs msg with info level.
func Info(ctx context.Context, msg string) {
	fields, _ := getFields(ctx)
	logger.Info(msg, fields...)
}

// Warn logs msg with warn level.
func Warn(ctx context.Context, msg string) {
	fields, _ := getFields(ctx)
	logger.Warn(msg, fields...)
}

// Debug logs msg with debug level if ORDO_LOG_LEVEL set to DEBUG.
func Debug(ctx context.Context, msg string) {
	fields, _ := getFields(ctx)
	logger.Debug(msg, fields...)
}

// Error logs msg with error level. Adds first err as and error field
func Error(ctx context.Context, msg string, err error) {
	fields, tags := getFields(ctx)
	logger.Error(msg, append(fields, zap.Error(err))...)
	sendToSentry(tags, err, msg)
}

func Errorf(ctx context.Context, msg string, err error, fields ...string) {
	field, tags := getFields(ctx, fields...)
	logger.Error(msg, append(field, zap.Error(err))...)
	sendToSentry(tags, err, msg)
}

func Infof(ctx context.Context, msg string, fields ...string) {
	field, _ := getFields(ctx, fields...)
	logger.Info(msg, field...)
}

func Warnf(ctx context.Context, msg string, fields ...string) {
	field, _ := getFields(ctx, fields...)
	logger.Warn(msg, field...)
}

func Debugf(ctx context.Context, msg string, fields ...string) {
	field, _ := getFields(ctx, fields...)
	logger.Debug(msg, field...)
}

// SendError writing error with stack trace to sentry
// TODO при логировании ошибок, имеют тип: errors.errorString (нужны отдельные типы, если хотим красиво) и слегка раздутый stacktrace (если передавать сюда ошибку из пакета github.com/pkg/errors, проблема исчезает)
func SendError(ctx context.Context, err error, fields ...string) {
	_, tags := getFields(ctx, fields...)
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTags(tags)
		scope.SetTag("error", err.Error())
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureException(err)
	})
}

func getFields(ctx context.Context, fields ...string) ([]zap.Field, map[string]string) {
	var ctxFields []string
	v := ctx.Value(fieldsKey)
	if v != nil {
		if f, ok := v.([]string); ok {
			ctxFields = f
		}
	}

	var res []zap.Field
	var m map[string]string

	if len(fields) != 0 && len(fields)%2 == 0 {
		m = make(map[string]string, len(ctxUserFields)+len(ctxFields)+len(fields)/2)
		res = make([]zap.Field, 0, len(ctxUserFields)+len(ctxFields)+len(fields)/2)
		for i := 0; i < len(fields); i += 2 {
			res = append(res, zap.Any(fields[i], fields[i+1]))
			m[fields[i]] = fields[i]
		}
	} else {
		m = make(map[string]string, len(ctxUserFields)+len(ctxFields))
		res = make([]zap.Field, 0, len(ctxUserFields)+len(ctxFields))
	}

	if len(ctxFields)%2 == 0 {
		for i := 0; i < len(ctxFields); i += 2 {
			res = append(res, zap.String(ctxFields[i], ctxFields[i+1]))
			m[ctxFields[i]] = ctxFields[i+1]
		}
	}

	for i := range ctxUserFields {
		if s, ok := ctx.Value(ctxUserFields[i]).(string); ok && s != "" {
			res = append(res, zap.String(ctxUserFields[i], s))
			m[ctxUserFields[i]] = s
		}
	}

	return res, m
}

func sendToSentry(tags map[string]string, err error, msg string) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTags(tags)
		scope.SetTag("error", err.Error())
		scope.SetLevel(sentry.LevelError)
		sentry.CaptureMessage(msg)
	})
}
