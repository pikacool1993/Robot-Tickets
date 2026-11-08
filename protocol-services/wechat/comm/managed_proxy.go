package comm

import (
	"net/url"
	"os"
	"wechatdll/models"
)

// Override older Redis proxy settings only for a client-managed service.
func managedProxy(original models.ProxyInfo) models.ProxyInfo {
	if os.Getenv("ROBOT_PROTOCOL_MANAGED") != "1" {
		return original
	}
	p := models.ProxyInfo{}
	u, err := url.Parse(os.Getenv("ROBOT_PROTOCOL_PROXY"))
	if err != nil || u.Host == "" {
		return p
	}
	p.ProxyIp = u.Scheme + "://" + u.Host
	if u.User != nil {
		p.ProxyUser = u.User.Username()
		p.ProxyPassword, _ = u.User.Password()
	}
	return p
}
