package worker

import (
	_ "example.com/m/internal/delivery/postgres"
	_ "example.com/m/internal/worker/sandbox"
	_ "gorm.io/gorm"
)
