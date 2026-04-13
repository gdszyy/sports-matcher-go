// Package db 提供数据库连接管理（支持 SSH 隧道模式和直连模式）
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gdszyy/sports-matcher/internal/config"
	mysqldrv "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/ssh"
)

// Tunnel 管理 SSH 隧道和数据库连接
type Tunnel struct {
	cfg       *config.Config
	sshClient *ssh.Client
	mu        sync.Mutex

	SRDb *sql.DB // xp-bet-test（SportRadar 数据）
	TSDb *sql.DB // test-thesports-db（TheSports 数据）
	LSDb *sql.DB // test-xp-lsports（LSports 数据）
}

// NewTunnel 创建并建立数据库连接，返回已连接的 Tunnel
// 优先尝试 SSH 隧道模式，失败则回退到直连本地端口模式
func NewTunnel(cfg *config.Config) (*Tunnel, error) {
	t := &Tunnel{cfg: cfg}
	if err := t.connect(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *Tunnel) connect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 尝试 SSH 隧道模式
	if err := t.connectViaSSH(); err == nil {
		return nil
	}

	// SSH 失败，回退到直连本地端口模式
	// 假设已有外部隧道：3308 → xp-bet-test/test-thesports-db，3309 → test-xp-lsports
	return t.connectDirect()
}

// connectViaSSH 通过 SSH 隧道连接数据库
func (t *Tunnel) connectViaSSH() error {
	keyBytes, err := os.ReadFile(t.cfg.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("读取 SSH 私钥失败: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return fmt.Errorf("解析 SSH 私钥失败: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            t.cfg.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", t.cfg.SSHHost, t.cfg.SSHPort)
	sshClient, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return fmt.Errorf("SSH 连接失败 %s: %w", addr, err)
	}
	t.sshClient = sshClient

	dialerName := fmt.Sprintf("ssh-tunnel-%d", t.cfg.LocalPort)
	sshClientRef := t.sshClient
	mysqldrv.RegisterDialContext(dialerName, func(_ context.Context, _ string) (net.Conn, error) {
		return sshClientRef.Dial("tcp", fmt.Sprintf("%s:%d", t.cfg.DBHost, t.cfg.DBPort))
	})

	openDB := func(dbName, label string) (*sql.DB, error) {
		dsn := fmt.Sprintf("%s:%s@%s(placeholder)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
			t.cfg.DBUser, t.cfg.DBPassword, dialerName, dbName)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("打开 %s 数据库失败: %w", label, err)
		}
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(10 * time.Minute)
		db.SetConnMaxIdleTime(5 * time.Minute)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("%s 数据库 ping 失败: %w", label, err)
		}
		return db, nil
	}

	if t.SRDb, err = openDB("xp-bet-test", "SR"); err != nil {
		return err
	}
	if t.TSDb, err = openDB("test-thesports-db", "TS"); err != nil {
		return err
	}
	if t.LSDb, err = openDB("test-xp-lsports", "LS"); err != nil {
		return err
	}

	return nil
}

// connectDirect 直连本地端口（外部 SSH 隧道已建立）
// 端口映射（由外部隧道管理）：
//   - 3308 → test-db:3306（xp-bet-test, test-thesports-db）
//   - 3309 → test-db:3306（test-xp-lsports）
func (t *Tunnel) connectDirect() error {
	openDB := func(host string, port int, dbName, label string) (*sql.DB, error) {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
			t.cfg.DBUser, t.cfg.DBPassword, host, port, dbName)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("打开 %s 数据库失败: %w", label, err)
		}
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(10 * time.Minute)
		db.SetConnMaxIdleTime(5 * time.Minute)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("%s 数据库 ping 失败: %w", label, err)
		}
		return db, nil
	}

	var err error
	// SR 库和 TS 库共用 3308 端口（同一个 RDS 实例）
	if t.SRDb, err = openDB("127.0.0.1", 3308, "xp-bet-test", "SR"); err != nil {
		return err
	}
	if t.TSDb, err = openDB("127.0.0.1", 3308, "test-thesports-db", "TS"); err != nil {
		return err
	}
	// LS 库使用 3309 端口
	if t.LSDb, err = openDB("127.0.0.1", 3309, "test-xp-lsports", "LS"); err != nil {
		return err
	}

	return nil
}

// Close 关闭所有连接
func (t *Tunnel) Close() {
	if t.SRDb != nil {
		t.SRDb.Close()
	}
	if t.TSDb != nil {
		t.TSDb.Close()
	}
	if t.LSDb != nil {
		t.LSDb.Close()
	}
	if t.sshClient != nil {
		t.sshClient.Close()
	}
}

// sshConn 实现 net.Conn，通过 SSH channel 转发
type sshConn struct {
	io.ReadWriteCloser
	localAddr  net.Addr
	remoteAddr net.Addr
}

func (c *sshConn) LocalAddr() net.Addr                { return c.localAddr }
func (c *sshConn) RemoteAddr() net.Addr               { return c.remoteAddr }
func (c *sshConn) SetDeadline(_ time.Time) error      { return nil }
func (c *sshConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *sshConn) SetWriteDeadline(_ time.Time) error { return nil }
