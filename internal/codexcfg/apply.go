package codexcfg

import (
	"fmt"
	"strings"
)

// 本文件实现"应用到 codex 配置"的完整操作：
// 注入上下文默认键 + 把激活 provider 的 base_url 指向本地代理。
//
// 与旧版 apply（config.cpp）等价，但基于行级模型，
// 保证未改动的行逐字节不变（INV-6）。

// ApplyOptions 控制 Apply 的行为。
type ApplyOptions struct {
	// ProxyAddr 是本地代理地址（如 "http://127.0.0.1:1800/v1"）。
	// 为空表示只注入上下文默认键，不改 base_url。
	ProxyAddr string
}

// ApplyResult 描述一次应用的结果。
type ApplyResult struct {
	Home        string   `json:"home"`
	ConfigPath  string   `json:"configPath"`
	BackupPath  string   `json:"backupPath"`
	BackupMade  bool     `json:"backupMade"`
	InjectedKey []string `json:"injectedKeys"`
	Provider    string   `json:"provider"`
	OldBaseURL  string   `json:"oldBaseUrl"`
	NewBaseURL  string   `json:"newBaseUrl"`
	ProxyBak    string   `json:"proxyBakPath,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// EnsureInjectKeys 只注入缺失的上下文默认键，不改 base_url。
// 供自愈守护使用：配置被改坏时恢复注入状态。
func (h Home) EnsureInjectKeys() error {
	return h.Update(func(d *Doc) error {
		for _, kv := range ContextDefaultKeys {
			if err := d.InsertTopLevelRaw(kv.Key, []byte(kv.Val)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Apply 执行完整注入。
//
// 步骤：
//  1. 解析并校验（不支持的形态直接拒绝，不猜）
//  2. 注入缺失的上下文默认键
//  3. 若指定了 ProxyAddr，把激活 provider 的 base_url 换成本地代理
//  4. 切 base_url 前先把当前（非代理态）配置存为还原点
//  5. 备份 + 原子写
func (h Home) Apply(opts ApplyOptions) (ApplyResult, error) {
	res := ApplyResult{
		Home:       h.Dir,
		ConfigPath: h.ConfigPath(),
		BackupPath: h.BakPath(),
	}

	probe, perr := h.Probe()
	if perr != nil {
		// provider 信息拿不到不影响注入默认键，但要告诉用户
		res.Warnings = append(res.Warnings, "无法确定激活 provider: "+perr.Error())
	} else {
		res.Provider = probe.Name
		res.OldBaseURL = probe.BaseURL
	}

	// 切 base_url 之前先存还原点（仅当当前不是代理态）
	if opts.ProxyAddr != "" && perr == nil {
		if probe.IsLocalProxy() {
			res.Warnings = append(res.Warnings, "base_url 已指向本地，跳过还原点更新")
		} else {
			if err := h.SnapshotProxyBak(); err != nil {
				return res, fmt.Errorf("保存 base_url 还原点失败: %w", err)
			}
			res.ProxyBak = h.ProxyBakPath()
		}
	}

	// 记录备份是否本来就有，用于报告
	res.BackupMade = !h.HasBackup()

	err := h.Update(func(d *Doc) error {
		// 1. 注入上下文默认键
		for _, kv := range ContextDefaultKeys {
			before := d.Has(nil, kv.Key)
			if err := d.InsertTopLevelRaw(kv.Key, []byte(kv.Val)); err != nil {
				return err
			}
			if !before {
				res.InjectedKey = append(res.InjectedKey, kv.Key)
			}
		}
		// 2. 改 base_url
		if opts.ProxyAddr != "" && perr == nil {
			if !d.HasTable(probe.Table) {
				return fmt.Errorf("%w: [model_providers.%s]", ErrTableNotFound, probe.Name)
			}
			if err := d.SetString(probe.Table, "base_url", opts.ProxyAddr); err != nil {
				return err
			}
			res.NewBaseURL = opts.ProxyAddr
		}
		return nil
	})
	if err != nil {
		return res, err
	}

	// 验证写入确实生效 —— 不验证就报告成功是不诚实的
	if st := h.CheckInjection(); !st.Injected {
		return res, fmt.Errorf("写入后自检未通过，缺少: %s", strings.Join(st.MissingKeys, ", "))
	}
	return res, nil
}

// RemoveOptions 控制 Remove 的行为。
type RemoveOptions struct {
	// KeepProxyBackup 为 true 时不删还原点（默认删除）。
	KeepProxyBackup bool
}

// RemoveResult 描述一次卸载的结果。
type RemoveResult struct {
	Restored     bool   `json:"restored"`
	ConfigPath   string `json:"configPath"`
	ProxyBakKept bool   `json:"proxyBakKept"`
	NothingToDo  bool   `json:"nothingToDo"`
}

// Remove 还原配置：用完整备份覆盖 config.toml。
//
// 【与旧版的差别】旧版 remove_all（config.cpp:513+）会逐项反向修改。
// 新版是**整文件覆盖**，这是 INV-6「逐字节还原」唯一可靠的做法：
// 反向修改只要漏掉一项就还原不干净。
func (h Home) Remove(opts RemoveOptions) (RemoveResult, error) {
	res := RemoveResult{ConfigPath: h.ConfigPath()}

	if !h.HasBackup() {
		res.NothingToDo = true
		return res, nil
	}
	if err := h.RestoreFromBackup(); err != nil {
		return res, err
	}
	res.Restored = true

	if !opts.KeepProxyBackup && h.HasProxyBackup() {
		if err := h.ClearProxyBak(); err != nil {
			return res, fmt.Errorf("配置已还原，但清理 base_url 还原点失败: %w", err)
		}
	} else if h.HasProxyBackup() {
		res.ProxyBakKept = true
	}
	return res, nil
}

// RestoreProxy 只还原 base_url（保留注入），供代理退出时调用。
//
// 与 remove 的区别：不动完整备份，只把 base_url 换回原来的上游。
func (h Home) RestoreProxy() (bool, error) {
	probe, err := h.Probe()
	if err != nil {
		return false, err
	}
	if !probe.IsLocalProxy() {
		return false, nil // 本来就不指向本地，无事可做
	}
	if !h.HasProxyBackup() {
		return false, fmt.Errorf("%w: 无法确定原始上游地址", ErrNoProxyBackup)
	}

	bakRaw, err := h.ReadProxyBak()
	if err != nil {
		return false, err
	}
	bd, err := ParseDoc(bakRaw)
	if err != nil {
		return false, fmt.Errorf("还原点不可解析: %w", err)
	}
	orig, ok := bd.GetString(probe.Table, "base_url")
	if !ok || orig == "" {
		return false, fmt.Errorf("还原点里没有 %s 的 base_url", probe.Name)
	}

	err = h.Update(func(d *Doc) error {
		return d.SetString(probe.Table, "base_url", orig)
	})
	if err != nil {
		return false, err
	}
	_ = h.ClearProxyBak()
	return true, nil
}
