package config

type AWSConfig struct {
	ACCESS_ID         string
	ACCESS_SECRET_KEY string
	URL               string
	Region            string
	QueueName         string
	DelayStep         int32
}

/*
* @field User - RabbitMQ User
* @field Password - RabbitMQ Password
* @field Host - RabbitMQ Host
* @field Port - RabbitMQ Port
 */
type RabbitMQDriverConfig struct {
	User     string
	Password string
	Host     string
	Port     uint16
}

/*
* @field Type (default, dlq). @default: default
* @field Name - Queue name, if "", MQ will create unique name
* @field DlxName - Name of exchange for DLQ, if "" - queue will not have DL logic
* @field IsAutoDelete - If true, queue will automatically delete queue
* @field IsDurable - If true, queue will save state if MQ restart
* @field IsExclusive - If true, queue will allow only one client to consume
* @field IsLazyMode - If true, Keep as many messages as possible on disk and as few messages as possible in RAM. REDUCE PERFOMANCE
* @field MaxLength - Sets the maximum number of messages in the queue. If 0 - unlimited
* @field TTL - Value in seconds after which the message will be deleted
 */
type RabbitMQQueueConfig struct {
	Type         string
	Name         string
	DlxName      string
	RoutingKey   string
	IsAutoDelete bool
	IsDurable    bool
	IsExclusive  bool
	IsLazyMode   bool
	MaxLength    int32
	TTL          int32
	MaxRerties   int64
}
