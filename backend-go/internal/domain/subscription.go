package domain

const DefaultTopic = "default"

type Subscription struct {
	Endpoint string   `json:"endpoint"`
	P256DH   string   `json:"p256dh"`
	Auth     string   `json:"auth"`
	Topics   []string `json:"topics"`
}

func NormalizeTopic(topic string) string {
	if topic == "" {
		return DefaultTopic
	}
	return topic
}
