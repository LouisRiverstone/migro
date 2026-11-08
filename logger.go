package migro

import "gorm.io/gorm/logger"

func silentLogger() logger.Interface { return logger.Default.LogMode(logger.Silent) }
