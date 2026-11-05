package postgres

import "time"

// eventEntity is a row of outbox.events. It carries GORM's mapping so
// pkg/events does not.
type eventEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	Type           string     `gorm:"column:type"`
	OrganizationID *string    `gorm:"column:organization_id"`
	AggregateType  string     `gorm:"column:aggregate_type"`
	AggregateID    string     `gorm:"column:aggregate_id"`
	CorrelationID  string     `gorm:"column:correlation_id"`
	CausationID    *string    `gorm:"column:causation_id"`
	OccurredAt     time.Time  `gorm:"column:occurred_at"`
	Payload        []byte     `gorm:"column:payload;type:jsonb"`
	RecordedAt     time.Time  `gorm:"column:recorded_at;autoCreateTime:false"`
	PublishedAt    *time.Time `gorm:"column:published_at"`
	Attempts       int        `gorm:"column:attempts"`
	NextAttemptAt  *time.Time `gorm:"column:next_attempt_at"`
	ClaimedUntil   *time.Time `gorm:"column:claimed_until"`
	DeadLetteredAt *time.Time `gorm:"column:dead_lettered_at"`
	LastError      *string    `gorm:"column:last_error"`
}

func (eventEntity) TableName() string { return "outbox.events" }
