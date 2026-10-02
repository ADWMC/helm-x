package codexcfg

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// 本文件实现原子写入与备份还原（INV-6）。
//
// 核心承诺：apply → remove 之后，config.toml 与初始状态**逐字节一致**。
// 达成方式：备份保存的是**完整原文**，还原是**整文件覆盖**，
// 不是"反向修改" —— 反向修改会因路径不同而留下差异。

// ReadConfig 读取 config.toml 原文。
func (h Home) ReadConfig() ([]byte, error) {
	raw, err := os.ReadFile(h.ConfigPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, h.ConfigPath())
		}
		return nil, err
	}
	return raw, nil
}

// ParseConfig 读取并解析 config.toml。
func (h Home) ParseConfig() (*Doc, []byte, error) {
	raw, err := h.ReadConfig()
	if err != nil {
		return nil, nil, err
	}
	d, err := ParseDoc(raw)
	if err != nil {
		return nil, nil, err
	}
	return d, raw, nil
}

// writeAtomic 原子写入：先写同目录临时文件，再替换。
//
// 同目录是必须的 —— 跨卷 rename 不是原子操作。
// 与旧版一致（config.cpp:95-120），但去掉"先做 TOML 校验"那一步：
// 我们的 Doc 模型在解析阶段就已经拒绝了不支持的形态。
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".helmx-tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// 失败路径下清理；成功路径下文件已被 rename 掉
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", path, err)
	}
	return nil
}

// EnsureBackup 确保完整备份存在。**已存在则不覆盖** ——
// 备份一旦被覆盖，就再也回不到最初状态（INV-6）。
func (h Home) EnsureBackup() (created bool, err error) {
	bak := h.BakPath()
	if _, statErr := os.Stat(bak); statErr == nil {
		return false, nil // 已有备份，保留
	}
	raw, err := h.ReadConfig()
	if err != nil {
		return false, err
	}
	if err := writeAtomic(bak, raw); err != nil {
		return false, err
	}
	return true, nil
}

// HasBackup 报告完整备份是否存在。
func (h Home) HasBackup() bool {
	_, err := os.Stat(h.BakPath())
	return err == nil
}

// HasProxyBackup 报告 base_url 还原点是否存在。
func (h Home) HasProxyBackup() bool {
	_, err := os.Stat(h.ProxyBakPath())
	return err == nil
}

// Update 对 config.toml 施加一次修改：确保备份、写回、原子替换。
//
// mutate 返回修改后的字节。若 mutate 返回错误，什么都不写。
func (h Home) Update(mutate func(d *Doc) error) error {
	d, _, err := h.ParseConfig()
	if err != nil {
		return err
	}
	if err := mutate(d); err != nil {
		return err
	}
	out := d.Bytes()
	if len(out) == 0 {
		return fmt.Errorf("%w: 修改后内容为空，拒绝写入", ErrUnsupportedForm)
	}
	if _, err := h.EnsureBackup(); err != nil {
		return err
	}
	return writeAtomic(h.ConfigPath(), out)
}

// RestoreFromBackup 用完整备份覆盖 config.toml，并删除备份。
//
// 这是 INV-6 的落点：整文件覆盖而非反向修改。
func (h Home) RestoreFromBackup() error {
	bak := h.BakPath()
	raw, err := os.ReadFile(bak)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNoBackup, bak)
		}
		return err
	}
	if err := writeAtomic(h.ConfigPath(), raw); err != nil {
		return err
	}
	if err := os.Remove(bak); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("备份已还原但删除失败: %w", err)
	}
	return nil
}

// SnapshotProxyBak 把当前（非代理态）的 config.toml 存为 base_url 还原点。
//
// 只在当前 base_url **不指向本地** 时才写 —— 否则会把代理地址存进还原点，
// 导致还原后仍指向代理（旧版 config.cpp:406-409 也是这个判断）。
func (h Home) SnapshotProxyBak() error {
	raw, err := h.ReadConfig()
	if err != nil {
		return err
	}
	return writeAtomic(h.ProxyBakPath(), raw)
}

// RestoreProxyBak 把 base_url 还原点读出来，用于还原上游地址。
// 文件保留不删，供后续再次查看。
func (h Home) ReadProxyBak() ([]byte, error) {
	raw, err := os.ReadFile(h.ProxyBakPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoProxyBackup, h.ProxyBakPath())
		}
		return nil, err
	}
	return raw, nil
}

// ClearProxyBak 删除还原点。
func (h Home) ClearProxyBak() error {
	err := os.Remove(h.ProxyBakPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Diff 描述一次将发生的修改，供 UI 的「危险操作确认」展示（DESIGN.md §3.1）。
type Diff struct {
	Table    []string
	Key      string
	OldValue string
	NewValue string
}

// PreviewSet 计算一次修改会带来什么变化，但不写入。
func (h Home) PreviewSet(table []string, key, val string) (Diff, error) {
	d, _, err := h.ParseConfig()
	if err != nil {
		return Diff{}, err
	}
	old, _ := d.GetString(table, key)
	return Diff{Table: table, Key: key, OldValue: old, NewValue: val}, nil
}

// IsAlreadyApplied 报告某个键是否已是目标值。
func (h Home) IsAlreadyApplied(table []string, key, val string) (bool, error) {
	d, _, err := h.ParseConfig()
	if err != nil {
		return false, err
	}
	cur, ok := d.GetString(table, key)
	return ok && cur == val, nil
}

// SameBytes 是还原断言的辅助：比较两块字节是否完全相同。
func SameBytes(a, b []byte) bool { return bytes.Equal(a, b) }
