package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var leaseRenewedSeconds = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "mgmt_leader_lease_renewed_seconds",
	Help: "Unix timestamp of the last successful management-plane lease acquire or renew.",
})

type gaugeSink struct{}

func (gaugeSink) SetLeaseRenewedSeconds(v float64) { leaseRenewedSeconds.Set(v) }
