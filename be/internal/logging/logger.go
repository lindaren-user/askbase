package logging

import (
	"os"
	"path/filepath"

	"askbase/be/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init 初始化 zap 全局日志：开发用 console 编码写 stdout，生产用 JSON 追加写文件。
func Init(cfg config.LogConfig) error {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var core zapcore.Core
	if cfg.Development {
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		core = zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderCfg),
			zapcore.Lock(os.Stdout),
			zapcore.DebugLevel,
		)
	} else {
		if err := ensureLogDir(cfg.File); err != nil {
			return err
		}
		f, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		core = zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.Lock(f),
			zapcore.InfoLevel,
		)
	}

	zap.ReplaceGlobals(zap.New(core, zap.AddCaller()))
	return nil
}

// L 返回全局 logger 的简写。
func L() *zap.Logger {
	return zap.L()
}

func ensureLogDir(file string) error {
	dir := filepath.Dir(file)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
