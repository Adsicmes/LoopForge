package repo

import (
	"crypto/sha256"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"agentflow/internal/gitwork"

	"golang.org/x/crypto/ssh/knownhosts"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SSHHostsRepo owns administrator-confirmed platform SSH host keys.
type SSHHostsRepo struct{ *deps }

type sshHostRow struct {
	ID          uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	Host        string `gorm:"size:253;not null;uniqueIndex:idx_ssh_host_key" json:"host"`
	Port        int    `gorm:"not null;uniqueIndex:idx_ssh_host_key" json:"port"`
	Marker      string `gorm:"size:32;not null;uniqueIndex:idx_ssh_host_key" json:"marker"`
	Fingerprint string `gorm:"size:64;not null" json:"fingerprint"`
	KeyDigest   string `gorm:"size:64;not null;uniqueIndex:idx_ssh_host_key" json:"-"`
	KeyType     string `gorm:"size:128;not null" json:"keyType"`
	PublicKey   string `gorm:"type:text;not null" json:"publicKey"`
}

func (sshHostRow) TableName() string { return "ssh_trusted_hosts" }

// SSHHost is a stored, exact host-key assertion.
type SSHHost = sshHostRow

func (r *SSHHostsRepo) SSHHosts() ([]SSHHost, error) {
	var rows []sshHostRow
	err := r.DB.Order("host, port, id").Find(&rows).Error
	return rows, err
}

// PutSSHHosts validates the whole selection before atomically inserting it.
func (r *SSHHostsRepo) PutSSHHosts(items []gitwork.KnownHostCandidate) error {
	if len(items) == 0 || len(items) > 1000 {
		return fmt.Errorf("请选择 1 至 1000 个主机公钥")
	}
	rows := make([]sshHostRow, 0, len(items))
	for _, item := range items {
		if item.Port < 1 || item.Port > 65535 || strings.ContainsAny(item.PublicKey+item.Host+item.Marker, "\r\n") || strings.ContainsAny(item.Host, "[]") {
			return fmt.Errorf("主机公钥无效")
		}
		marker := ""
		if item.Marker != "" {
			marker = "@" + item.Marker + " "
		}
		line := marker + "[" + item.Host + "]:" + strconv.Itoa(item.Port) + " " + item.PublicKey
		parsed, err := gitwork.ParseKnownHostsImport([]byte(line))
		if err != nil || len(parsed) != 1 || parsed[0].Candidate == nil {
			return fmt.Errorf("主机公钥无效")
		}
		v := parsed[0].Candidate
		digest := sha256.Sum256([]byte(v.PublicKey))
		rows = append(rows, sshHostRow{Host: v.Host, Port: v.Port, Marker: v.Marker, Fingerprint: v.Fingerprint, KeyDigest: fmt.Sprintf("%x", digest), KeyType: v.KeyType, PublicKey: v.PublicKey})
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
	})
}

func (r *SSHHostsRepo) DeleteSSHHost(id uint64) error {
	return r.DB.Delete(&sshHostRow{}, id).Error
}

// SSHHostSnapshot selects only exact endpoints and fails closed on read errors.
// Endpoints trusted by the builtin pins (github.com, upstream 48cdf0c A2) get
// their builtin lines without an administrator entry; an @revoked snapshot
// entry still overrides the builtin key (sshHostKeys decides first-wins).
func (r *SSHHostsRepo) SSHHostSnapshot(remotes []string) ([]byte, error) {
	allowed := make(map[string]bool)
	for _, raw := range remotes {
		remote, err := gitwork.ParseSSHRemote(raw)
		if err != nil {
			return nil, err
		}
		allowed[remote.Endpoint()] = false
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	rows, err := r.SSHHosts()
	if err != nil {
		return nil, fmt.Errorf("读取 SSH 可信主机失败")
	}
	var out strings.Builder
	for _, row := range rows {
		endpoint := net.JoinHostPort(row.Host, strconv.Itoa(row.Port))
		if _, ok := allowed[endpoint]; !ok {
			continue
		}
		if row.Marker != "" {
			fmt.Fprintf(&out, "@%s ", row.Marker)
		}
		fmt.Fprintf(&out, "%s %s\n", knownhosts.Normalize(endpoint), row.PublicKey)
		if row.Marker != "revoked" {
			allowed[endpoint] = true
		}
	}
	endpoints := make([]string, 0, len(allowed))
	for endpoint, ok := range allowed {
		if ok {
			continue
		}
		endpoints = append(endpoints, knownhosts.Normalize(endpoint))
	}
	if len(endpoints) > 0 {
		sort.Strings(endpoints)
		lines, covered := gitwork.BuiltinKnownHostsFor(endpoints)
		out.Write(lines)
		for _, endpoint := range endpoints {
			if !covered[endpoint] {
				return nil, fmt.Errorf("SSH 主机 %s 未获信任，请平台管理员核对并添加公钥", endpoint)
			}
		}
	}
	return []byte(out.String()), nil
}
