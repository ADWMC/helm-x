package codexcfg

import (
	"os"
	"path/filepath"
	"strings"
)

// 配置文件名与备份后缀。后缀沿用旧版，保证从旧版升级的用户其备份仍被识别。
const (
	ConfigName = "config.toml"

	// BakSuffix 是首次注入前的完整备份（INV-6 的还原依据）。
	BakSuffix = ".helmx-bak"
	// ProxyBakSuffix 是 base_url 的还原点。与完整备份分开，
	// 因为代理启停只改 base_url，不应覆盖完整备份。
	ProxyBakSuffix = ".helmx-proxy-bak"
)

// Home 是一个已定位的 codex 配置目录。
type Home struct {
	Dir string // 目录绝对路径
}

// ConfigPath 返回 config.toml 的完整路径。
func (h Home) ConfigPath() string { return filepath.Join(h.Dir, ConfigName) }

// BakPath 返回完整备份路径。
func (h Home) BakPath() string { return h.ConfigPath() + BakSuffix }

// ProxyBakPath 返回 base_url 还原点路径。
func (h Home) ProxyBakPath() string { return h.ConfigPath() + ProxyBakSuffix }

// Find 定位 codex 配置目录。
//
// 查找顺序（与旧版一致，见旧 config.cpp:24-39）：
//  1. $CODEX_HOME（仅当其中存在 config.toml）
//  2. ~/.codex
//  3. ~/codex
//
// 找不到返回 ErrHomeNotFound；调用方不应据此 panic，
// 而应提示用户"未找到 config.toml，请先运行一次 codex"。
func Find() (Home, error) {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		if fileExists(filepath.Join(v, ConfigName)) {
			abs, err := filepath.Abs(v)
			if err != nil {
				abs = v
			}
			return Home{Dir: abs}, nil
		}
	}

	base, err := os.UserHomeDir()
	if err != nil || base == "" {
		return Home{}, ErrHomeNotFound
	}
	for _, sub := range []string{".codex", "codex"} {
		dir := filepath.Join(base, sub)
		if fileExists(filepath.Join(dir, ConfigName)) {
			return Home{Dir: dir}, nil
		}
	}
	return Home{}, ErrHomeNotFound
}

// FindOrCreate 与 Find 相同，但在全都不存在时返回 ~/.codex（不创建目录）。
// 供 UI 展示"将写入何处"使用。
func FindOrCreate() (Home, error) {
	if h, err := Find(); err == nil {
		return h, nil
	}
	base, err := os.UserHomeDir()
	if err != nil || base == "" {
		return Home{}, ErrHomeNotFound
	}
	return Home{Dir: filepath.Join(base, ".codex")}, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// detectEOL 探测文件的行尾风格。
//
// 旧版按「第一个 \r\n 是否存在」判定整个文件（config.cpp:318），
// 在混合行尾文件上会写错。这里改为统计多数派，且对空文件默认 LF。
func detectEOL(raw []byte) string {
	crlf := strings.Count(string(raw), "\r\n")
	lf := strings.Count(string(raw), "\n") - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}
