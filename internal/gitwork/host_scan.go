package gitwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var scanSlots = make(chan struct{}, 4)

var blockedScanPrefixes = func() []netip.Prefix {
	values := []string{"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "::ffff:0:0/96", "64:ff9b::/96", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fe80::/10", "ff00::/8"}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func allowedScanIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() {
		return false
	}
	// IPv4-mapped addresses must be checked using the IPv4 policy.
	ip = ip.Unmap()
	for _, prefix := range blockedScanPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	// Private IPv6 is an ordinary internal target, not a special service.
	return ip.Is4() || ip.IsPrivate() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

// ScanSSHHost returns untrusted candidates. It connects to resolved IP literals,
// never to the original name after validation, to prevent DNS rebinding.
func ScanSSHHost(ctx context.Context, host string, port int) ([]KnownHostCandidate, error) {
	return scanSSHHost(ctx, host, port, net.DefaultResolver.LookupNetIP, func(ctx context.Context, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
	})
}

func scanSSHHost(ctx context.Context, host string, port int, resolve func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string) (net.Conn, error)) ([]KnownHostCandidate, error) {
	if port < 1 || port > 65535 || strings.ContainsAny(host, "[]\r\n") {
		return nil, errors.New("主机或端口无效")
	}
	normalized, parsedPort, err := parseKnownHostEndpoint("[" + host + "]:" + strconv.Itoa(port))
	if err != nil || parsedPort != port {
		return nil, errors.New("主机或端口无效")
	}
	select {
	case scanSlots <- struct{}{}:
		defer func() { <-scanSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, errors.New("扫描繁忙，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ips, err := resolve(ctx, "ip", normalized)
	if err != nil || len(ips) == 0 || len(ips) > 32 {
		return nil, errors.New("主机解析失败或地址过多")
	}
	for _, ip := range ips {
		if !allowedScanIP(ip) {
			return nil, errors.New("主机解析到禁止扫描的特殊地址")
		}
	}
	var candidates []KnownHostCandidate
	seen := map[string]bool{}
	for _, algorithm := range ssh.SupportedAlgorithms().HostKeys {
		if strings.Contains(algorithm, "-cert-") {
			continue
		}
		for _, ip := range ips {
			conn, err := dial(ctx, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
			if err != nil {
				continue
			}
			deadline := time.Now().Add(2 * time.Second)
			if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
				deadline = d
			}
			_ = conn.SetDeadline(deadline)
			config := &ssh.ClientConfig{User: "host-key-scan", HostKeyAlgorithms: []string{algorithm}, HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				if _, cert := key.(*ssh.Certificate); !cert && len(key.Marshal()) <= 16<<10 && len(candidates) < 32 {
					fingerprint := ssh.FingerprintSHA256(key)
					if !seen[fingerprint] {
						seen[fingerprint] = true
						candidates = append(candidates, KnownHostCandidate{Host: normalized, Port: port, KeyType: key.Type(), PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), Fingerprint: fingerprint})
					}
				}
				return errors.New("candidate captured; authentication disabled")
			}}
			_, _, _, _ = ssh.NewClientConn(conn, net.JoinHostPort(normalized, strconv.Itoa(port)), config)
			_ = conn.Close()
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("未发现候选公钥；请检查连接或通过可信渠道手动导入")
	}
	return candidates, nil
}
