package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	ActivePeerCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "furymesh_active_peer_count",
		Help: "Number of active connected peers",
	})

	TransferThroughput = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "furymesh_transfer_throughput_bytes_total",
		Help: "Total bytes transferred over WebRTC",
	})

	DHTLookupLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "furymesh_dht_lookup_latency_seconds",
		Help:    "Latency of DHT lookup operations",
		Buckets: prometheus.DefBuckets,
	})

	EncryptionFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "furymesh_encryption_failures_total",
		Help: "Number of encryption/decryption failures",
	})
)

func Register() {
	prometheus.MustRegister(
		ActivePeerCount,
		TransferThroughput,
		DHTLookupLatency,
		EncryptionFailures,
	)
}
