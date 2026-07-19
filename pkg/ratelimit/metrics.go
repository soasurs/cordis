package ratelimit

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var decisionsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "cordis",
		Subsystem: "rate_limit",
		Name:      "decisions_total",
		Help:      "Rate limit decisions by policy and result.",
	},
	[]string{"policy", "result"},
)

var bucketUsageRatio = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "cordis",
		Subsystem: "rate_limit",
		Name:      "bucket_usage_ratio",
		Help:      "Distribution of rate-limit bucket quota usage after decisions.",
		Buckets:   []float64{0.1, 0.25, 0.5, 0.75, 0.9, 1},
	},
	[]string{"policy", "backend"},
)

func recordDecision(policy string, decision Decision) {
	result := "allow"
	if !decision.Allowed {
		result = "reject"
	}
	if decision.Fallback {
		result = "fallback_" + result
	}
	decisionsTotal.WithLabelValues(policy, result).Inc()
}

func recordBucketUsage(policy, backend string, limit, remaining int64) {
	usage := float64(limit-remaining) / float64(limit)
	usage = min(max(usage, 0), 1)
	bucketUsageRatio.WithLabelValues(policy, backend).Observe(usage)
}
