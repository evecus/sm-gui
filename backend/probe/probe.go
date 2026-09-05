// Package probe 测量单个节点的真连接延迟与下载速度。
//
// 参考 v2rayN 的做法：为被测节点生成一份临时的 sing-box 配置
// （mixed inbound 监听随机端口 + 节点 outbound），拉起一个独立的 sing-box
// 进程，通过该本地代理发起真实 HTTP 请求，测完立即杀掉进程并清理临时目录。
// 延迟 = 经代理完整请求 http://www.gstatic.com/generate_204 的耗时；
// 速度 = 经代理下载固定大小文件的吞吐（Mbps，最长 10 秒）。
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"sm-gui/backend/config"
	"sm-gui/backend/node"
)

const (
	latencyURL   = "http://www.gstatic.com/generate_204"
	latencyTO    = 5 * time.Second
	speedURL     = "https://speed.cloudflare.com/__down?bytes=26214400" // 25 MB
	speedMaxTime = 10 * time.Second
)

// proxyClient 构造经本地 mixed inbound 的 HTTP 客户端。
func proxyClient(proxyPort int, timeout time.Duration) *http.Client {
	proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", proxyPort)}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
}

// TestLatency 返回真连接延迟（毫秒）。节点不可达 / 超时返回 error。
func TestLatency(coreBin string, n node.Node) (int, error) {
	proxyPort, stop, err := startTestInstance(coreBin, n)
	if err != nil {
		return 0, err
	}
	defer stop()

	client := proxyClient(proxyPort, latencyTO)
	start := time.Now()
	resp, err := client.Get(latencyURL)
	if err != nil {
		return 0, fmt.Errorf("连接失败: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 399 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return int(time.Since(start).Milliseconds()), nil
}

// TestSpeed 返回下载速度（Mbps）。节点不可达 / 下载失败返回 error。
func TestSpeed(coreBin string, n node.Node) (float64, error) {
	proxyPort, stop, err := startTestInstance(coreBin, n)
	if err != nil {
		return 0, err
	}
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), speedMaxTime)
	defer cancel()
	client := proxyClient(proxyPort, 0)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, speedURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("下载失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	start := time.Now()
	nBytes, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)
	if err != nil && nBytes == 0 {
		return 0, fmt.Errorf("下载失败: %v", err)
	}
	if elapsed <= 0 {
		elapsed = time.Millisecond
	}
	// 部分实现提前超时截断Body读流，此时 nBytes 仍有效，按已有数据计算
	return float64(nBytes) * 8 / elapsed.Seconds() / 1e6, nil
}

// startTestInstance 拉起一个临时的 sing-box 实例（mixed inbound + 节点 outbound）。
// 返回本地代理端口与停止清理函数。
func startTestInstance(coreBin string, n node.Node) (int, func(), error) {
	if st, err := os.Stat(coreBin); err != nil || st.IsDir() {
		return 0, nil, fmt.Errorf("未找到 sing-box 内核: %s（测试延迟/速度需要 bin/sing-box.exe）", coreBin)
	}
	outbound, err := config.SingBoxOutboundForNode(n)
	if err != nil {
		return 0, nil, err
	}
	outbound["tag"] = "proxy"

	port, err := freePort()
	if err != nil {
		return 0, nil, err
	}
	cfg := map[string]interface{}{
		"log": map[string]interface{}{"level": "error"},
		"inbounds": []interface{}{
			map[string]interface{}{
				"type": "mixed", "tag": "mixed-in",
				"listen": "127.0.0.1", "listen_port": port,
			},
		},
		"outbounds": []interface{}{
			outbound,
			map[string]interface{}{"type": "direct", "tag": "direct"},
		},
		"route": map[string]interface{}{"final": "proxy"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return 0, nil, err
	}

	tmpDir, err := os.MkdirTemp("", "smgui-probe-*")
	if err != nil {
		return 0, nil, err
	}
	cfgPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return 0, nil, err
	}

	cmd := exec.Command(coreBin, "run", "-c", cfgPath, "-D", tmpDir)
	hideWindow(cmd)

	stop := func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
		os.RemoveAll(tmpDir)
	}

	if err := cmd.Start(); err != nil {
		os.RemoveAll(tmpDir)
		return 0, nil, fmt.Errorf("启动测试进程失败: %v", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// 等待 mixed inbound 就绪（最多 5 秒）；进程提前退出则报错
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			stop()
			return 0, nil, fmt.Errorf("测试进程提前退出（节点配置可能不被当前内核支持）")
		}
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return port, stop, nil
		}
		if time.Now().After(deadline) {
			stop()
			return 0, nil, fmt.Errorf("测试实例启动超时")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
