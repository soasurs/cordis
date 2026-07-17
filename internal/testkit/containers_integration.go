//go:build integration

// Package testkit starts isolated infrastructure dependencies for integration
// tests. The helpers intentionally use fixed image versions so that local and
// CI runs exercise the same server behaviour.
package testkit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	etcdcontainer "github.com/testcontainers/testcontainers-go/modules/etcd"
	kafkacontainer "github.com/testcontainers/testcontainers-go/modules/kafka"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	postgresImage = "postgres:17.5-alpine"
	redisImage    = "redis:7.4.2-alpine"
	kafkaImage    = "confluentinc/confluent-local:7.5.0"
	etcdImage     = "gcr.io/etcd-development/etcd:v3.5.21"
)

// Postgres exposes the connection string of an isolated PostgreSQL container.
type Postgres struct {
	Container *postgrescontainer.PostgresContainer
	DSN       string
}

// Redis exposes the host:port address of an isolated Redis container.
type Redis struct {
	Container *rediscontainer.RedisContainer
	Address   string
}

// Kafka exposes the bootstrap broker address of an isolated KRaft Kafka
// container.
type Kafka struct {
	Container *kafkacontainer.KafkaContainer
	Address   string
}

// Etcd exposes the host:port client endpoint of an isolated etcd container.
type Etcd struct {
	Container *etcdcontainer.EtcdContainer
	Address   string
}

// StartPostgres starts PostgreSQL and registers cleanup with t.
func StartPostgres(t *testing.T) *Postgres {
	t.Helper()
	ctx := containerContext(t)
	container, err := postgrescontainer.Run(ctx, postgresImage,
		postgrescontainer.WithDatabase("cordis"),
		postgrescontainer.WithUsername("cordis"),
		postgrescontainer.WithPassword("cordis"),
		testcontainers.WithWaitStrategy(wait.ForSQL("5432/tcp", "postgres", func(host string, port network.Port) string {
			return fmt.Sprintf("postgres://cordis:cordis@%s:%s/cordis?sslmode=disable", host, port.Port())
		}).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { terminate(t, container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	return &Postgres{Container: container, DSN: dsn}
}

// StartRedis starts Redis and registers cleanup with t.
func StartRedis(t *testing.T) *Redis {
	t.Helper()
	ctx := containerContext(t)
	container, err := rediscontainer.Run(ctx, redisImage)
	if err != nil {
		t.Fatalf("start Redis container: %v", err)
	}
	t.Cleanup(func() { terminate(t, container) })

	address, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("get Redis connection string: %v", err)
	}
	return &Redis{Container: container, Address: strings.TrimPrefix(address, "redis://")}
}

// StartKafka starts a single-node KRaft Kafka broker and registers cleanup
// with t.
func StartKafka(t *testing.T) *Kafka {
	t.Helper()
	ctx := containerContext(t)
	container, err := kafkacontainer.Run(ctx, kafkaImage)
	if err != nil {
		t.Fatalf("start Kafka container: %v", err)
	}
	t.Cleanup(func() { terminate(t, container) })

	address, err := container.PortEndpoint(ctx, "9093/tcp", "")
	if err != nil {
		t.Fatalf("get Kafka bootstrap address: %v", err)
	}
	return &Kafka{Container: container, Address: address}
}

// StartEtcd starts a single-node etcd server and registers cleanup with t.
func StartEtcd(t *testing.T) *Etcd {
	t.Helper()
	ctx := containerContext(t)
	container, err := etcdcontainer.Run(ctx, etcdImage)
	if err != nil {
		t.Fatalf("start etcd container: %v", err)
	}
	t.Cleanup(func() { terminate(t, container) })

	address, err := container.ClientEndpoint(ctx)
	if err != nil {
		t.Fatalf("get etcd client endpoint: %v", err)
	}
	return &Etcd{Container: container, Address: strings.TrimPrefix(address, "http://")}
}

// CreateKafkaTopic creates a single-partition topic suitable for an isolated
// integration test.
func CreateKafkaTopic(t *testing.T, client *kgo.Client, topic string) {
	t.Helper()
	req := kmsg.NewPtrCreateTopicsRequest()
	topicReq := kmsg.NewCreateTopicsRequestTopic()
	topicReq.Topic = topic
	topicReq.NumPartitions = 1
	topicReq.ReplicationFactor = 1
	req.Topics = append(req.Topics, topicReq)

	response, err := client.Request(t.Context(), req)
	if err != nil {
		t.Fatalf("create Kafka topic %q: %v", topic, err)
	}
	created, ok := response.(*kmsg.CreateTopicsResponse)
	if !ok || len(created.Topics) != 1 || created.Topics[0].ErrorCode != 0 {
		t.Fatalf("create Kafka topic %q: unexpected response %#v", topic, response)
	}
}

func containerContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func terminate(t *testing.T, container testcontainers.Container) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := container.Terminate(ctx); err != nil {
		t.Errorf("terminate integration container: %v", err)
	}
}
