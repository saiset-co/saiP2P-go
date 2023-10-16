package utils

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func InStringArray(slice []string, str string, partly bool) bool {
	for _, s := range slice {
		if partly && strings.Contains(str, s) {
			return true
		} else if s == str {
			return true
		}
	}

	return false
}

func IsEmptyStringArray(slice []string) bool {
	return len(slice) <= 0
}

// Remove port from whole p2p peer addr (bug with storing pubkey (different ports))
func GetIpFromAddr(addr string) (ip string) {
	return strings.TrimSuffix(addr, ":")
}

// Remove ip from whole p2p peer addr (bug with storing pubkey (different ports))
func GetPortFromAddr(addr string) (ip string) {
	return strings.TrimPrefix(addr, ":")
}

// Build logger
func BuildLogger(debugMode bool) (logger *zap.Logger, err error) {
	switch debugMode {
	case true:
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		logger, _ = config.Build()
	case false:
		config := zap.NewProductionConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		logger, _ = config.Build()

	}
	return logger, nil
}
