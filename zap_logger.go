package service

import (
	"fmt"

	"go.uber.org/zap"
)

type ZapLogger struct {
	logger *zap.Logger
}

func WrapLogger(logger *zap.Logger) *ZapLogger {
	return &ZapLogger{logger}
}

func (l *ZapLogger) Error(format string, v ...any) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l *ZapLogger) Info(format string, v ...any) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func (l *ZapLogger) Debug(format string, v ...any) {
	l.logger.Debug(fmt.Sprintf(format, v...))
}
