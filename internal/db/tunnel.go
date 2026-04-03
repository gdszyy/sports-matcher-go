// Package db 提供通过 SSH 隧道访问 MySQL 的能力
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

	SRDb *sql.DB // xp-bet-test
	TSDb *sql.DB // test-thesports-db
}

// NewTunnel 创建并建立 SSH 隧道，返回已连接的 Tunnel
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

	// 读取 SSH 私钥
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 测试环境
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", t.cfg.SSHHost, t.cfg.SSHPort)
	sshClient, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return fmt.Errorf("SSH 连接失败 %s: %w", addr, err)
	}
	t.sshClient = sshClient

	// 注册自定义 dialer，通过 SSH 隧道连接 MySQL
	dialerName := fmt.Sprintf("ssh-tunnel-%d", t.cfg.LocalPort)
	sshClientRef := t.sshClient
	mysqldrv.RegisterDialContext(dialerName, func(_ context.Context, _ string) (net.Conn, error) {
		return sshClientRef.Dial("tcp", fmt.Sprintf("%s:%d", t.cfg.DBHost, t.cfg.DBPort))
	})

	// 连接 SR 库
	// dialer 名称作为网络地址，占位符 placeholder 不会被使用（真实连接由 dialer 接管）
	srDSN := fmt.Sprintf("%s:%s@%s(placeholder)/xp-bet-test?charset=utf8mb4&parseTime=true&loc=UTC",
		t.cfg.DBUser, t.cfg.DBPassword, dialerName)
	srDb, err := sql.Open("mysql", srDSN)
	if err != nil {
		return fmt.Errorf("打开 SR 数据库失败: %w", err)
	}
	srDb.SetMaxOpenConns(20)
	srDb.SetMaxIdleConns(5)
	srDb.SetConnMaxLifetime(10 * time.Minute)
	srDb.SetConnMaxIdleTime(5 * time.Minute)
	if err := srDb.Ping(); err != nil {
		return fmt.Errorf("SR 数据库 ping 失败: %w", err)
	}
	t.SRDb = srDb

	// 连接 TS 库
	tsDSN := fmt.Sprintf("%s:%s@%s(placeholder)/test-thesports-db?charset=utf8mb4&parseTime=true&loc=UTC",
		t.cfg.DBUser, t.cfg.DBPassword, dialerName)
	tsDb, err := sql.Open("mysql", tsDSN)
	if err != nil {
		return fmt.Errorf("打开 TS 数据库失败: %w", err)
	}
	tsDb.SetMaxOpenConns(20)
	tsDb.SetMaxIdleConns(5)
	tsDb.SetConnMaxLifetime(10 * time.Minute)
	tsDb.SetConnMaxIdleTime(5 * time.Minute)
	if err := tsDb.Ping(); err != nil {
		return fmt.Errorf("TS 数据库 ping 失败: %w", err)
	}
	t.TSDb = tsDb

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
