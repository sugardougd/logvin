package logvin

import (
	"github.com/sirupsen/logrus"
	"testing"
)

func TestInfo(t *testing.T) {
	configFile = "./logger_test.yaml"
	RegisterFormatter(CorvinFormatterName, func(config *LoggerConfig) logrus.Formatter {
		return &CorvinFormatter{
			Config: config,
		}
	})

	root := New("root")
	root.Info("hello world", " ", "root")
	root.Error("hello world", " ", "root")

	logger := New("logvin")
	logger.Info("hello world", " ", "logvin")
	logger.Error("hello world", " ", "logvin")
}
