package logging

import (
	"os"

	"askbase/be/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init 初始化 zap 全局日志：开发使用 console 编码，生产使用 JSON，统一写入 stdout。
func Init(cfg config.LogConfig) error {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if cfg.Development {
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}
	level := zapcore.InfoLevel
	if cfg.Development {
		level = zapcore.DebugLevel
	}
	core := zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), level)
	zap.ReplaceGlobals(zap.New(core, zap.AddCaller()))
	return nil
}

// L 返回全局 logger 的简写。
func L() *zap.Logger {
	return zap.L()
}
