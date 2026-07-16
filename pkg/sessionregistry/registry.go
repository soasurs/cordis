// Package sessionregistry stores the live Session-node directory in etcd.
package sessionregistry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	defaultPrefix      = "/cordis/session/nodes"
	defaultDialTimeout = 5 * time.Second

	StatusReady    = "ready"
	StatusDraining = "draining"
)

var (
	ErrNodeNotFound = errors.New("session node not found")
	ErrNodeNotReady = errors.New("session node not ready")
)

type Config struct {
	Hosts              []string
	Prefix             string `json:",default=/cordis/session/nodes"`
	User               string `json:",optional"`
	Pass               string `json:",optional"`
	CertFile           string `json:",optional"`
	CertKeyFile        string `json:",optional=CertFile"`
	CACertFile         string `json:",optional=CertFile"`
	InsecureSkipVerify bool   `json:",optional"`
	DialTimeoutSeconds int    `json:",default=5"`
}

func (c Config) KeyPrefix() string {
	prefix := strings.TrimSpace(c.Prefix)
	if prefix == "" {
		return defaultPrefix
	}
	return "/" + strings.Trim(prefix, "/")
}

func (c Config) DialTimeout() time.Duration {
	if c.DialTimeoutSeconds <= 0 {
		return defaultDialTimeout
	}
	return time.Duration(c.DialTimeoutSeconds) * time.Second
}

type Node struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	RPCAddress string `json:"rpc_address"`
	Status     string `json:"status"`
}

type Directory interface {
	Register(ctx context.Context, node Node, ttl time.Duration) error
	Ready(ctx context.Context) ([]Node, error)
	Resolve(ctx context.Context, nodeID, generation string) (Node, error)
	Close() error
}

type EtcdDirectory struct {
	client *clientv3.Client
	prefix string

	mu      sync.Mutex
	leaseID clientv3.LeaseID
	nodeKey string
}

func New(cfg Config) (*EtcdDirectory, error) {
	if len(cfg.Hosts) == 0 {
		return nil, errors.New("session registry etcd hosts are required")
	}
	tlsConfig, err := loadTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Hosts,
		Username:    cfg.User,
		Password:    cfg.Pass,
		DialTimeout: cfg.DialTimeout(),
		TLS:         tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("create session registry client: %w", err)
	}
	return &EtcdDirectory{client: client, prefix: cfg.KeyPrefix()}, nil
}

func (d *EtcdDirectory) Register(ctx context.Context, node Node, ttl time.Duration) error {
	if err := validateNode(node); err != nil {
		return err
	}
	if ttl <= 0 {
		return errors.New("session node ttl must be positive")
	}
	payload, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("marshal session node: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	key := d.key(node.ID)
	if d.nodeKey != "" && d.nodeKey != key {
		return errors.New("session registry cannot register multiple node ids")
	}
	if d.leaseID != 0 {
		if _, err := d.client.KeepAliveOnce(ctx, d.leaseID); err != nil {
			d.leaseID = 0
		}
	}
	if d.leaseID == 0 {
		lease, err := d.client.Grant(ctx, max(int64(ttl/time.Second), 1))
		if err != nil {
			return fmt.Errorf("grant session node lease: %w", err)
		}
		d.leaseID = lease.ID
	}
	if _, err := d.client.Put(ctx, key, string(payload), clientv3.WithLease(d.leaseID)); err != nil {
		d.leaseID = 0
		return fmt.Errorf("register session node: %w", err)
	}
	d.nodeKey = key
	return nil
}

func (d *EtcdDirectory) Ready(ctx context.Context) ([]Node, error) {
	resp, err := d.client.Get(ctx, d.prefix+"/", clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("list session nodes: %w", err)
	}
	nodes := make([]Node, 0, len(resp.Kvs))
	for _, item := range resp.Kvs {
		node, err := decodeNode(item.Value)
		if err != nil || node.Status != StatusReady {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (d *EtcdDirectory) Resolve(ctx context.Context, nodeID, generation string) (Node, error) {
	if !validNodeID(nodeID) || strings.TrimSpace(generation) == "" {
		return Node{}, ErrNodeNotFound
	}
	resp, err := d.client.Get(ctx, d.key(nodeID))
	if err != nil {
		return Node{}, fmt.Errorf("resolve session node: %w", err)
	}
	if len(resp.Kvs) != 1 {
		return Node{}, ErrNodeNotFound
	}
	node, err := decodeNode(resp.Kvs[0].Value)
	if err != nil || node.ID != nodeID || node.Generation != generation {
		return Node{}, ErrNodeNotFound
	}
	if node.Status != StatusReady {
		return Node{}, ErrNodeNotReady
	}
	return node, nil
}

func (d *EtcdDirectory) Close() error {
	d.mu.Lock()
	leaseID := d.leaseID
	d.leaseID = 0
	d.mu.Unlock()
	if leaseID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = d.client.Revoke(ctx, leaseID)
		cancel()
	}
	return d.client.Close()
}

func (d *EtcdDirectory) key(nodeID string) string {
	return path.Join(d.prefix, nodeID)
}

func decodeNode(value []byte) (Node, error) {
	var node Node
	if err := json.Unmarshal(value, &node); err != nil {
		return Node{}, err
	}
	if err := validateNode(node); err != nil {
		return Node{}, err
	}
	return node, nil
}

func validateNode(node Node) error {
	if strings.TrimSpace(node.ID) == "" {
		return errors.New("session node id is required")
	}
	if !validNodeID(node.ID) {
		return errors.New("session node id is invalid")
	}
	if strings.TrimSpace(node.Generation) == "" {
		return errors.New("session node generation is required")
	}
	if strings.TrimSpace(node.RPCAddress) == "" {
		return errors.New("session node rpc address is required")
	}
	if node.Status != StatusReady && node.Status != StatusDraining {
		return errors.New("session node status is invalid")
	}
	return nil
}

func validNodeID(nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	return nodeID != "" && nodeID != "." && nodeID != ".." && !strings.Contains(nodeID, "/")
}

func loadTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.CertFile == "" && cfg.CertKeyFile == "" && cfg.CACertFile == "" && !cfg.InsecureSkipVerify {
		return nil, nil
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if cfg.CertFile != "" || cfg.CertKeyFile != "" {
		if cfg.CertFile == "" || cfg.CertKeyFile == "" {
			return nil, errors.New("session registry cert file and key file must be set together")
		}
		certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.CertKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load session registry client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if cfg.CACertFile != "" {
		ca, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read session registry ca certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("parse session registry ca certificate")
		}
		tlsConfig.RootCAs = pool
	}
	return tlsConfig, nil
}
