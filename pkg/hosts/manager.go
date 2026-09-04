package hosts

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HostsManager hosts文件管理器
type HostsManager struct {
	hostsPath string
	entries   map[string]string // 域名 -> IP
}

// NewHostsManager 创建hosts管理器
func NewHostsManager(domains []string) *HostsManager {
	// Windows hosts文件路径
	hostsPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")

	manager := &HostsManager{
		hostsPath: hostsPath,
		entries:   make(map[string]string),
	}

	// 添加域名到映射
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain != "" {
			// 处理通配符域名
			if strings.HasPrefix(domain, "*.") {
				baseDomain := strings.TrimPrefix(domain, "*.")
				manager.entries[domain] = "127.0.0.1"
				manager.entries[baseDomain] = "127.0.0.1"
			} else {
				manager.entries[domain] = "127.0.0.1"
			}
		}
	}

	return manager
}

// CheckAndFix 检查并修复hosts文件
func (m *HostsManager) CheckAndFix() error {
	// 检查hosts文件是否存在
	if _, err := os.Stat(m.hostsPath); os.IsNotExist(err) {
		return fmt.Errorf("hosts文件不存在: %s", m.hostsPath)
	}

	// 读取现有hosts文件
	existingEntries, err := m.readHostsFile()
	if err != nil {
		return fmt.Errorf("读取hosts文件失败: %v", err)
	}

	// 检查哪些域名需要添加
	missingDomains := make([]string, 0)
	for domain := range m.entries {
		if !m.isDomainConfigured(existingEntries, domain) {
			missingDomains = append(missingDomains, domain)
		}
	}

	if len(missingDomains) == 0 {
		fmt.Println("[HOSTS] ✅ 所有域名已配置")
		return nil
	}

	fmt.Printf("[HOSTS] ⚠️ 发现 %d 个域名未配置，正在添加...\n", len(missingDomains))
	for _, domain := range missingDomains {
		fmt.Printf("[HOSTS]   - %s → 127.0.0.1\n", domain)
	}

	// 添加缺失的域名
	if err := m.addDomains(missingDomains); err != nil {
		return fmt.Errorf("添加域名失败: %v", err)
	}

	fmt.Printf("[HOSTS] ✅ 成功添加 %d 个域名到hosts文件\n", len(missingDomains))
	return nil
}

// readHostsFile 读取hosts文件
func (m *HostsManager) readHostsFile() (map[string]bool, error) {
	file, err := os.Open(m.hostsPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	existingEntries := make(map[string]bool)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析行: IP 域名1 域名2 ...
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// 第一个是IP，后面的是域名
		ip := parts[0]
		for _, domain := range parts[1:] {
			domain = strings.TrimSpace(domain)
			if domain != "" && ip == "127.0.0.1" {
				existingEntries[domain] = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return existingEntries, nil
}

// isDomainConfigured 检查域名是否已配置
func (m *HostsManager) isDomainConfigured(existingEntries map[string]bool, domain string) bool {
	// 精确匹配
	if existingEntries[domain] {
		return true
	}

	// 通配符匹配
	if strings.HasPrefix(domain, "*.") {
		baseDomain := strings.TrimPrefix(domain, "*.")
		// 检查基础域名是否配置
		if existingEntries[baseDomain] {
			return true
		}
	}

	return false
}

// addDomains 添加域名到hosts文件
func (m *HostsManager) addDomains(domains []string) error {
	// 以追加模式打开文件
	file, err := os.OpenFile(m.hostsPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// 添加新行
	for _, domain := range domains {
		line := fmt.Sprintf("\n127.0.0.1 %s", domain)
		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}

	return nil
}
