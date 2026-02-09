package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"picpic.render/internal/core/ports"
)

// Publisher consumes Redis PubSub and publishes to MQTT
type Publisher struct {
	client mqtt.Client
	pubsub ports.LogPubSub
	prefix string
}

// NewPublisher initializes the MQTT publisher
func NewPublisher(pubsub ports.LogPubSub, brokerURL string) (*Publisher, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(fmt.Sprintf("picpic-server-%d", time.Now().UnixNano()))
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetAutoReconnect(true)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	log.Printf("Connected to MQTT Broker: %s", brokerURL)
	return &Publisher{
		client: client,
		pubsub: pubsub,
		prefix: "picpic",
	}, nil
}

// Start consumers
func (p *Publisher) Start(ctx context.Context) {
	go p.consumeLogs(ctx)
	go p.consumeJobUpdates(ctx)
	go p.consumeResources(ctx)
}

func (p *Publisher) consumeLogs(ctx context.Context) {
	ch, err := p.pubsub.Subscribe(ctx, "")
	if err != nil {
		log.Printf("Failed to subscribe to logs: %v", err)
		return
	}

	log.Println("MQTT: Started log consumer")

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			// Topic: picpic/logs/{job_id}
			topic := fmt.Sprintf("%s/logs/%s", p.prefix, entry.JobID)
			payload, _ := json.Marshal(entry)
			p.client.Publish(topic, 0, false, payload)
		}
	}
}

func (p *Publisher) consumeJobUpdates(ctx context.Context) {
	ch, err := p.pubsub.SubscribeJobUpdates(ctx)
	if err != nil {
		log.Printf("Failed to subscribe to job updates: %v", err)
		return
	}

	log.Println("MQTT: Started job update consumer")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			// Wrap in event format expected by frontend
			// Redis msg is {"job_id": "...", "status": "..."}
			var innerPayload map[string]interface{}
			if err := json.Unmarshal([]byte(msg), &innerPayload); err != nil {
				log.Printf("Failed to parse job update: %v", err)
				continue
			}

			jobID, ok := innerPayload["job_id"].(string)
			if !ok || jobID == "" {
				log.Printf("Job update missing job_id: %v", innerPayload)
				continue
			}

			event := map[string]interface{}{
				"type":    "job_update",
				"payload": innerPayload,
			}
			data, _ := json.Marshal(event)

			// Publish to job specific topic
			topic := fmt.Sprintf("%s/job/%s", p.prefix, jobID)
			p.client.Publish(topic, 0, false, data)
		}
	}
}

func (p *Publisher) consumeResources(ctx context.Context) {
	ch, err := p.pubsub.SubscribeResources(ctx)
	if err != nil {
		log.Printf("Failed to subscribe to resources: %v", err)
		return
	}

	log.Println("MQTT: Started resource consumer")

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}

			event := map[string]interface{}{
				"type":    "resource_update",
				"payload": entry,
			}
			data, _ := json.Marshal(event)

			// Publish to global events topic
			topic := fmt.Sprintf("%s/events", p.prefix)
			p.client.Publish(topic, 0, false, data)
		}
	}
}
