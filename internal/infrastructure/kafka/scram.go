package kafka

import (
	"github.com/IBM/sarama"
	"github.com/xdg-go/scram"
)

// scramClient adapts xdg-go/scram to sarama's SCRAMClient. SCRAM-SHA-256 is the
// mechanism Redpanda Cloud requires; PLAIN is rejected by the broker.
type scramClient struct {
	*scram.Client
	*scram.ClientConversation
}

func newSCRAMClient() sarama.SCRAMClient {
	return &scramClient{}
}

func (x *scramClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = scram.SHA256.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

func (x *scramClient) Step(challenge string) (string, error) {
	return x.ClientConversation.Step(challenge)
}

func (x *scramClient) Done() bool {
	return x.ClientConversation.Done()
}
