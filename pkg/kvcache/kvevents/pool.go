package kvevents

import (
	"context"
	"hash/fnv"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/llm-d/llm-d-kv-cache-manager/pkg/kvcache/kvblock"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils"
	"github.com/llm-d/llm-d-kv-cache-manager/pkg/utils/logging"
)

// Config holds the configuration for the event processing pool.
type Config struct {
	// ZMQEndpoint is the ZMQ address to connect to (e.g., "tcp://indexer:5557").
	ZMQEndpoint string
	// TopicFilter is the ZMQ subscription filter (e.g., "kv.").
	TopicFilter string
	// Concurrency is the number of parallel workers to run.
	Concurrency int
}

// DefaultConfig returns a default configuration for the event processing pool.
func DefaultConfig() *Config {
	return &Config{
		ZMQEndpoint: "tcp://indexer:5557",
		TopicFilter: "kv@",
		Concurrency: 4,
	}
}

// Message represents a message that is read from a ZMQ topic.
type Message struct {
	Topic   string
	Payload []byte
	// Sequence number of the message
	Seq uint64
	// PodIdentifier is the identifier of the pod that sent the event.
	// This will be extracted from the ZMQ topic.
	PodIdentifier string
	// ModelName is the name of the model that is associated with this event.
	ModelName string
}

// Pool is a sharded worker pool that processes events from a ZMQ subscriber.
// It ensures that events for the same PodIdentifier are processed in order.
type Pool struct {
	queues      []workqueue.TypedRateLimitingInterface[*Message]
	concurrency int // can replace use with len(queues)
	subscriber  *zmqSubscriber
	index       kvblock.Index
	wg          sync.WaitGroup
}

// NewPool creates a Pool with a sharded worker setup.
func NewPool(cfg *Config, index kvblock.Index) *Pool {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	p := &Pool{
		queues:      make([]workqueue.TypedRateLimitingInterface[*Message], cfg.Concurrency),
		concurrency: cfg.Concurrency,
		index:       index,
	}

	for i := 0; i < p.concurrency; i++ {
		p.queues[i] = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[*Message]())
	}

	p.subscriber = newZMQSubscriber(p, cfg.ZMQEndpoint, cfg.TopicFilter)
	return p
}

// Start begins the worker pool and the ZMQ subscriber.
// It is non-blocking.
func (p *Pool) Start(ctx context.Context) {
	logger := klog.FromContext(ctx)
	logger.Info("Starting sharded event processing pool", "workers", p.concurrency)

	p.wg.Add(p.concurrency)
	for i := 0; i < p.concurrency; i++ {
		// Each worker is given its own dedicated queue shard.
		go p.worker(ctx, i)
	}

	go p.subscriber.Start(ctx)
}

// Shutdown gracefully stops the pool and its subscriber.
func (p *Pool) Shutdown(ctx context.Context) {
	logger := klog.FromContext(ctx)
	logger.Info("Shutting down event processing pool...")

	for _, queue := range p.queues {
		queue.ShutDown()
	}

	p.wg.Wait()
	logger.Info("event processing pool shut down.")
}

// AddTask is called by the subscriber to add a message to the processing queue.
// It hashes the PodIdentifier to select a queue, ensuring messages for the
// same pod always go to the same worker (ordered queue).
func (p *Pool) AddTask(task *Message) {
	// Use an FNV-1a hash to deterministically select a queue.
	// TODO: round-robin or simpler approach could be good enough
	h := fnv.New32a()
	_, err := h.Write([]byte(task.PodIdentifier))
	if err != nil {
		return
	}

	//nolint:gosec // if concurrency overflows then the world is in trouble anyway
	queueIndex := h.Sum32() % uint32(p.concurrency)
	p.queues[queueIndex].Add(task)
}

// worker is the main processing loop for a single worker goroutine.
// It processes messages from its dedicated queue using the workqueue pattern.
// TODO: profile and benchmark cases like backpressure, slow processing (profile), etc.
func (p *Pool) worker(ctx context.Context, workerIndex int) {
	defer p.wg.Done()
	queue := p.queues[workerIndex]
	for {
		task, shutdown := queue.Get()
		if shutdown {
			return
		}

		// Use a nested func to ensure Done is always called.
		func(task *Message) {
			defer queue.Done(task)
			p.processEvent(ctx, task)
			// Task succeeded, remove it from the queue.
			queue.Forget(task)
		}(task)

		// Check if context was cancelled after processing a task.
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// processEvent deserializes the message payload and calls the appropriate
// index method based on the event type. It returns an error to trigger retries.
func (p *Pool) processEvent(ctx context.Context, msg *Message) {
	debugLogger := klog.FromContext(ctx).V(logging.DEBUG)
	debugLogger.Info("Processing event", "topic", msg.Topic, "seq", msg.Seq)

	var eventBatch EventBatch
	if err := msgpack.Unmarshal(msg.Payload, &eventBatch); err != nil {
		// This is likely a "poison pill" message that can't be unmarshalled.
		// We log the error but return nil to prevent it from being retried indefinitely.
		debugLogger.Error(err, "Failed to unmarshal event batch, dropping message")
		return
	}

	events := make([]event, 0, len(eventBatch.Events))
	for _, rawEvent := range eventBatch.Events {
		var taggedUnion []msgpack.RawMessage
		if err := msgpack.Unmarshal(rawEvent, &taggedUnion); err != nil {
			debugLogger.Error(err, "Failed to unmarshal tagged union, skipping event")
			continue
		}

		// Handle array_like tagged union: re-marshall tail parts into a payload array
		if len(taggedUnion) < 1 {
			debugLogger.Error(nil, "Malformed tagged union, no tag element", "parts", len(taggedUnion))
			continue
		}
		payloadBytes, err := msgpack.Marshal(taggedUnion[1:])
		if err != nil {
			debugLogger.Error(err, "Failed to re-marshal payload parts, skipping event")
			continue
		}

		var tag string
		if err := msgpack.Unmarshal(taggedUnion[0], &tag); err != nil {
			debugLogger.Error(err, "Failed to unmarshal tag from tagged union, skipping event")
			continue
		}

		var event event
		var unmarshalErr error
		switch tag {
		case "BlockStored":
			var bs BlockStored
			unmarshalErr = msgpack.Unmarshal(payloadBytes, &bs)
			event = bs
		case "BlockRemoved":
			var br BlockRemoved
			unmarshalErr = msgpack.Unmarshal(payloadBytes, &br)
			event = br
		case "AllBlocksCleared":
			var ac AllBlocksCleared
			unmarshalErr = msgpack.Unmarshal(payloadBytes, &ac)
			event = ac
		default:
			debugLogger.Info("Unknown event tag", "tag", tag)
			continue
		}

		if unmarshalErr != nil {
			debugLogger.Error(unmarshalErr, "Failed to unmarshal event value", "tag", tag)
			continue
		}
		events = append(events, event)
	}

	podIdentifier := msg.PodIdentifier
	modelName := msg.ModelName
	entries := []kvblock.PodEntry{{PodIdentifier: podIdentifier, DeviceTier: "gpu"}}
	p.digestEvents(ctx, podIdentifier, modelName, events, entries)
}

func (p *Pool) digestEvents(ctx context.Context, podIdentifier, modelName string,
	events []event, podEntries []kvblock.PodEntry,
) {
	debugLogger := klog.FromContext(ctx).V(logging.DEBUG)
	debugLogger.Info("Digesting events", "count", len(events))

	// Process each event in the batch
	for _, event := range events {
		switch ev := event.(type) {
		case BlockStored:
			keys := utils.SliceMap(ev.BlockHashes, func(hash uint64) kvblock.Key {
				return kvblock.Key{ModelName: modelName, ChunkHash: hash}
			})

			if err := p.index.Add(ctx, keys, podEntries); err != nil {
				debugLogger.Error(err, "Failed to add event to index",
					"podIdentifier", podIdentifier, "event", ev)

				continue // Continue processing other events even if one fails
			}
		case BlockRemoved:
			for _, hash := range ev.BlockHashes {
				key := kvblock.Key{ModelName: modelName, ChunkHash: hash}
				if err := p.index.Evict(ctx, key, podEntries); err != nil {
					debugLogger.Error(err, "Failed to remove event from index",
						"podIdentifier", podIdentifier, "event", ev)
					continue // Continue processing other events even if one fails
				}
			}
		case AllBlocksCleared:
			continue
		default:
			debugLogger.Info("Unknown event", "podIdentifier", podIdentifier, "event", ev)
		}
	}
}
