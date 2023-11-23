package config

type AWSConfig struct {
	ACCESS_ID         string
	ACCESS_SECRET_KEY string
	URL               string
	Region            string
	QueueName         string
	DelayStep         int32
}
