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
