package observability

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry        *prometheus.Registry
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	serviceName     string
}

func NewMetrics(serviceName string) *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	factory := promauto.With(registry)
	return &Metrics{
		registry:    registry,
		serviceName: serviceName,
		requestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests handled by the API.",
		}, []string{"service", "method", "route", "status"}),
		requestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"service", "method", "route", "status"}),
	}
}

func (m *Metrics) Handler() gin.HandlerFunc {
	return gin.WrapH(promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
}

func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		status := strconv.Itoa(c.Writer.Status())

		m.requestsTotal.WithLabelValues(m.serviceName, c.Request.Method, route, status).Inc()
		m.requestDuration.WithLabelValues(m.serviceName, c.Request.Method, route, status).Observe(time.Since(start).Seconds())
	}
}
