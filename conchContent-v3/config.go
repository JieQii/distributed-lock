package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type NodeInfo struct {
	ID         string `toml:"-"`           // 运行时填充
	Root       string `toml:"root"`        // 节点的根目录
	IP         string `toml:"ip,omitempty"`
	LockServer string `toml:"lock_server"` // 分布式锁 server 地址，例如 http://127.0.0.1:8080
}

type Config struct {
	CurrentNodeID string              `toml:"current_node"`
	SocketPath    string              `toml:"socket_path,omitempty"`
	Nodes         map[string]NodeInfo `toml:"nodes"`

	CurrentNode NodeInfo
	AllNodes    []NodeInfo
}

// ParseConfig 从指定路径加载配置文件
func ParseConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取配置文件 %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.CurrentNodeID == "" {
		return nil, fmt.Errorf("配置中缺少 current_node 字段")
	}

	node, exists := cfg.Nodes[cfg.CurrentNodeID]
	if !exists {
		return nil, fmt.Errorf("current_node=%s 未在 [nodes] 中定义", cfg.CurrentNodeID)
	}

	// 填充运行时字段
	node.ID = cfg.CurrentNodeID
	cfg.CurrentNode = node

	// 构建 AllNodes 列表（按 ID 排序）
	var allNodes []NodeInfo
	for id, info := range cfg.Nodes {
		allNodes = append(allNodes, NodeInfo{
			ID:         id,
			Root:       info.Root,
			IP:         info.IP,
			LockServer: info.LockServer,
		})
	}
	sort.Slice(allNodes, func(i, j int) bool {
		return allNodes[i].ID < allNodes[j].ID
	})
	cfg.AllNodes = allNodes

	// 自动生成 socket path（如果未设置）
	if cfg.SocketPath == "" {
		cfg.SocketPath = fmt.Sprintf("/run/containerd-content-%s.sock", strings.ToLower(cfg.CurrentNodeID))
	}

	return &cfg, nil
}

// ensureDirectories 初始化当前节点的 host / merged 目录，并挂载 mergerfs
func ensureDirectories(cfg *Config) error {
	hostDir := filepath.Join(cfg.CurrentNode.Root, "host")
	mergedDir := filepath.Join(cfg.CurrentNode.Root, "merged")

	// 创建本地目录
	dirs := []string{
		filepath.Join(hostDir, "blobs", "sha256"),
		filepath.Join(hostDir, "ingest"),
		filepath.Join(mergedDir, "blobs", "sha256"),
		filepath.Join(mergedDir, "ingest"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}

	// 收集所有节点的 host/blobs/sha256 路径
	var srcPaths []string
	for _, node := range cfg.AllNodes {
		blobPath := filepath.Join(node.Root, "host", "blobs", "sha256")
		if err := os.MkdirAll(blobPath, 0755); err != nil {
			return fmt.Errorf("无法创建源 blob 目录 %s: %w", blobPath, err)
		}
		srcPaths = append(srcPaths, blobPath)
	}
	mergerSrc := strings.Join(srcPaths, ":")

	mergeTarget := filepath.Join(mergedDir, "blobs", "sha256")
	_ = exec.Command("fusermount", "-u", mergeTarget).Run()

	cmd := exec.Command(
		"mergerfs",
		"-o", "defaults,allow_other,category.create=mfs,func.getattr=newest,cache.files=off,cache.readdir=off",
		mergerSrc,
		mergeTarget,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mergerfs 挂载失败: %w, output: %s", err, string(output))
	}

	fmt.Printf("MergerFS 挂载成功 (当前节点: %s)\n", cfg.CurrentNode.ID)
	fmt.Printf("  源: %s\n", mergerSrc)
	fmt.Printf("  目标: %s\n", mergeTarget)
	return nil
}


