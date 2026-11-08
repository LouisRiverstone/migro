package migro

import (
	"io"
	"log"

	"gorm.io/gorm/logger"
)

// silentLogger discards everything, even for drivers that force Debug() on internal queries.
func silentLogger() logger.Interface {
	return logger.New(log.New(io.Discard, "", 0), logger.Config{LogLevel: logger.Silent})
}
