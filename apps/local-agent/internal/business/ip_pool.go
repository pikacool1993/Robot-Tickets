package business

import (
	"net/http"
	"robot/apps/local-agent/internal/ippool"
)

type poolTransport struct {
	pool *ippool.Manager
	base http.RoundTripper
}

func (t poolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.pool.RoundTrip(r, t.base)
}
func (c *Client) UseIPPool(m *ippool.Manager) { c.http.Transport = poolTransport{m, c.http.Transport} }
