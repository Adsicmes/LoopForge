package gitwork

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KnownHostCandidate is an exact endpoint and public key, not an assertion of trust.
type KnownHostCandidate struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Marker      string `json:"marker"`
	PublicKey   string `json:"publicKey"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
}

// KnownHostImportItem preserves errors for individual input lines or hosts.
type KnownHostImportItem struct {
	Line      int                 `json:"line"`
	HostInput string              `json:"hostInput,omitempty"`
	Candidate *KnownHostCandidate `json:"candidate,omitempty"`
	Error     string              `json:"error,omitempty"`
}

// ParseKnownHostsImport previews known_hosts content without granting trust.
// Comments and blank lines are ignored; every other unsupported entry is reported.
func ParseKnownHostsImport(content []byte) ([]KnownHostImportItem, error) {
	if len(content) > 1<<20 {
		return nil, fmt.Errorf("known_hosts 内容不能超过 1 MiB")
	}
	var result []KnownHostImportItem
	for i, line := range bytes.Split(content, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		marker, hosts, key, _, rest, err := ssh.ParseKnownHosts(append(bytes.Clone(line), '\n'))
		if err != nil || len(bytes.TrimSpace(rest)) != 0 || key == nil {
			result = append(result, KnownHostImportItem{Line: i + 1, Error: "无法解析 known_hosts 条目"})
			continue
		}
		if marker != "" && marker != "revoked" && marker != "cert-authority" {
			result = append(result, KnownHostImportItem{Line: i + 1, Error: "不支持的标记"})
			continue
		}
		if _, isCert := key.(*ssh.Certificate); isCert {
			result = append(result, KnownHostImportItem{Line: i + 1, Error: "不支持将证书作为主机公钥导入"})
			continue
		}
		switch key.Type() {
		case "ssh-rsa", "ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com":
		default:
			result = append(result, KnownHostImportItem{Line: i + 1, Error: "不支持的公钥算法"})
			continue
		}
		if len(key.Marshal()) > 16<<10 {
			result = append(result, KnownHostImportItem{Line: i + 1, Error: "公钥过大"})
			continue
		}
		for _, raw := range hosts {
			item := KnownHostImportItem{Line: i + 1, HostInput: raw}
			host, port, err := parseKnownHostEndpoint(raw)
			if err != nil {
				item.Error = err.Error()
			} else {
				item.Candidate = &KnownHostCandidate{Host: host, Port: port, Marker: marker,
					PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
					KeyType:   key.Type(), Fingerprint: ssh.FingerprintSHA256(key)}
			}
			result = append(result, item)
		}
	}
	return result, nil
}

func parseKnownHostEndpoint(raw string) (string, int, error) {
	invalid := fmt.Errorf("仅支持精确主机名和端口，不支持哈希、通配符或否定匹配")
	if raw == "" || strings.ContainsAny(raw, "|*?!%/\\ \t\r\n") {
		return "", 0, invalid
	}
	host, port := raw, 22
	if strings.HasPrefix(raw, "[") {
		var p string
		var err error
		host, p, err = net.SplitHostPort(raw)
		if err != nil {
			return "", 0, invalid
		}
		port, err = strconv.Atoi(p)
		if err != nil || port < 1 || port > 65535 {
			return "", 0, invalid
		}
	} else if strings.ContainsAny(raw, "[]:") {
		if ip, err := netip.ParseAddr(raw); err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", 0, invalid
		}
	}
	host = strings.ToLower(host)
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" {
			return "", 0, invalid
		}
		host = ip.String()
	} else {
		if len(host) > 253 || !sshHost.MatchString(host) || strings.HasSuffix(host, ".") {
			return "", 0, invalid
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", 0, invalid
			}
		}
	}
	return host, port, nil
}
