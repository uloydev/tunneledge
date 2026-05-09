package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

type Config struct {
	Level  string
	Format string
}

type Logger struct {
	zerolog.Logger
}

func New(cfg Config) *Logger {
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zerolog.DurationFieldInteger = true
	zerolog.ErrorStackMarshaler = func(err error) any {
		return err.Error()
	}

	var writer io.Writer
	if cfg.Format == "console" {
		writer = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	} else {
		writer = os.Stdout
	}

	zl := zerolog.New(writer).With().Timestamp().Logger()

	return &Logger{Logger: zl}
}

func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("component", component).Logger()}
}

func (l *Logger) WithService(service string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("service", service).Logger()}
}

func (l *Logger) WithTunnelID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("tunnel_id", id).Logger()}
}

func (l *Logger) WithStreamID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("stream_id", id).Logger()}
}

func (l *Logger) WithSessionID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("session_id", id).Logger()}
}

func (l *Logger) WithCorrelationID(id string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("correlation_id", id).Logger()}
}

func (l *Logger) WithError(err error) *Logger {
	return &Logger{Logger: l.Logger.With().Err(err).Logger()}
}

func (l *Logger) WithFields(fields map[string]any) *Logger {
	ctx := l.Logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &Logger{Logger: ctx.Logger()}
}
