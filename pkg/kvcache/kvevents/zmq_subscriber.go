package kvevents

import (
	"context"
	"encoding/binary"
	"log"
	"strings"
	"time"

	zmq "github.com/pebbe/zmq4"
)

const (
	// How long to wait before retrying to connect.
	retryInterval = 5 * time.Second
	// How often the poller should time out to check for context cancellation.
	pollTimeout = 250 * time.Millisecond
)

// zmqSubscriber connects to a ZMQ publisher and forwards messages to a pool.
type zmqSubscriber struct {
	pool        *Pool
	endpoint    string
	topicFilter string
}

// newZMQSubscriber creates a new ZMQ subscriber.
func newZMQSubscriber(pool *Pool, endpoint, topicFilter string) *zmqSubscriber {
	return &zmqSubscriber{
		pool:        pool,
		endpoint:    endpoint,
		topicFilter: topicFilter,
	}
}

// Start connects to a ZMQ PUB socket as a SUB, receives messages,
// wraps them in Message structs, and pushes them into the pool.
// This loop will run until the provided context is canceled.
func (z *zmqSubscriber) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("ZMQ subscriber shutting down.")
			return
		default:
			// We run the subscriber in a separate function to handle socket
			// setup/teardown and connection retries cleanly.
			z.runSubscriber(ctx)
			// wait before retrying, unless the context has been canceled.
			select {
			case <-time.After(retryInterval):
				log.Printf("ZMQ subscriber disconnected. Retrying in %v...", retryInterval)
			case <-ctx.Done():
				log.Println("ZMQ subscriber shutting down.")
				return
			}
		}
	}
}

func (z *zmqSubscriber) runSubscriber(ctx context.Context) {
	sub, err := zmq.NewSocket(zmq.SUB)
	if err != nil {
		log.Printf("Failed to create ZMQ SUB socket: %v", err)
		return
	}
	defer sub.Close()

	if err := sub.Connect(z.endpoint); err != nil {
		log.Printf("Failed to connect ZMQ SUB socket to %s: %v", z.endpoint, err)
		return
	}
	log.Printf("ZMQ subscriber connected to %s", z.endpoint)

	if err := sub.SetSubscribe(z.topicFilter); err != nil {
		log.Printf("Failed to subscribe to topic '%s': %v", z.topicFilter, err)
		return
	}

	poller := zmq.NewPoller()
	poller.Add(sub, zmq.POLLIN)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		polled, err := poller.Poll(pollTimeout)
		if err != nil {
			log.Printf("Error polling ZMQ socket: %v", err)
			break // Exit on poll error to reconnect
		}

		if len(polled) > 0 {
			parts, err := sub.RecvMessageBytes(0)
			if err != nil {
				log.Printf("Error receiving ZMQ message: %v", err)
				break // Exit on receive error to reconnect
			}
			if len(parts) != 3 {
				log.Printf("Unexpected message parts count: got %d, want 3", len(parts))
				continue
			}
			topic := string(parts[0])
			seqBytes := parts[1]
			payload := parts[2]

			seq := binary.BigEndian.Uint64(seqBytes)

			// Extract pod identifier from topic, assuming "kv.<pod-id>" format
			topicParts := strings.Split(topic, ".")
			var podIdentifier string
			if len(topicParts) > 1 {
				podIdentifier = topicParts[1]
			} else {
				log.Printf("Could not extract pod identifier from topic: %s", topic)
				return // Useless if we can't extract pod identifier
			}

			z.pool.AddTask(&Message{
				Topic:         topic,
				Payload:       payload,
				Seq:           seq,
				PodIdentifier: podIdentifier,
			})
		}
	}
}
