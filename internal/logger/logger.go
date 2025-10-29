package logger

import (
	"os"

	log "github.com/sirupsen/logrus"
)

var Logger *log.Logger

func Init(level log.Level) {
	Logger = log.New()
	Logger.SetOutput(os.Stderr)
	Logger.SetFormatter(&log.JSONFormatter{})
	Logger.SetLevel(level)
}
