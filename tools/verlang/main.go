// Command verlang 修正 exe 里 RT_VERSION 资源的语言标识，让 Windows 能读到版本号。
//
// 背景（2026-11-22 实测）：wails3 generate syso -info info.json 写出的版本资源
// 完全合法 —— 手工解析确认 wLength=548、签名 0xfeef04bd、file_version=0.3.0.0 ——
// 但 FileVersionInfo 读出来是空。用 Win32 逐条探针定位到真正原因：
//
//	VerQueryValue("\")                                       → OK（len=52）
//	VerQueryValue("\StringFileInfo\000004B0\ProductVersion") → "0.3.0.0"   ← 能取到
//	VerQueryValue("\StringFileInfo\040904B0\ProductVersion") → MISS (1813)
//
// 版本块内部 StringFileInfo / VarFileInfo 的键是 000004B0（语言 0x0000 + 代码页
// 0x04b0）。Windows 按目录项语言找块、再按块内键取字符串；两处都被写成中性 0x0000，
// 属性页于是显示空。
//
// 本工具做两处**等长**改写（不动偏移、不动长度）：
//  1. 语言目录项的 ID：0x0000 → 0x0409
//  2. 版本块内所有 000004B0 文本键 → 040904B0
//
// 用法：go run ./tools/verlang -file bin/helmx.exe
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

const (
	rtVersion   = 16
	codePage    = 0x04b0
	resDirEntry = 112 + 16 // 数据目录第 3 项（资源表）相对 OptionalHeader 的偏移
)

func main() {
	file := flag.String("file", "bin/helmx.exe", "要改的 exe")
	lang := flag.Uint("lang", 0x0409, "目标语言 ID（默认 0x0409）")
	flag.Parse()

	if *lang > 0xFFFF {
		fail("语言 ID 超出范围: %#x", *lang)
	}

	b, err := os.ReadFile(*file)
	if err != nil {
		fail("读取失败: %v", err)
	}

	langOff, blobOff, blobLen, err := locate(b)
	if err != nil {
		fail("%v", err)
	}

	oldLang := binary.LittleEndian.Uint16(b[langOff:])
	binary.LittleEndian.PutUint16(b[langOff:], uint16(*lang))

	oldKey := fmt.Sprintf("%04X%04X", 0x0000, codePage)
	newKey := fmt.Sprintf("%04X%04X", uint16(*lang), codePage)
	patched := replaceUTF16(b[blobOff:blobOff+blobLen], oldKey, newKey)

	// VarFileInfo\Translation 是**二进制** DWORD：lang 在高 16 位、codepage 在低 16 位。
	// .NET 的 FileVersionInfo 正是用它来定位 StringFileInfo 语言块 —— 只改文本键不够，
	// 这里把 0x04B00000（codepage 0x04B0 + lang 0x0000）改成 0x04B0<lang>。
	oldTrans := uint32(codePage)<<16 | 0x0000
	newTrans := uint32(codePage)<<16 | uint32(*lang)
	transPatched := replaceU32(b[blobOff:blobOff+blobLen], oldTrans, newTrans)

	// VS_FIXEDFILEINFO 里的 dwProductVersionMS/LS 被 wails 写成 0（属性页"产品版本"为空）。
	// 签名 0xFEEF04BD 后依次是：dwStrucVersion, dwFileVersionMS/LS, dwProductVersionMS/LS。
	// 把产品版本对齐到文件版本 —— 本项目的两者本来就是同一个值。
	prodFixed := syncProductVersion(b[blobOff : blobOff+blobLen])

	if err := os.WriteFile(*file, b, 0o644); err != nil {
		fail("写回失败: %v", err)
	}
	fmt.Printf("RT_VERSION 语言：%#04x → %#04x（目录偏移 %#x）；文本键 %s → %s 改写 %d 处；Translation %d 处；产品版本 %v\n",
		oldLang, uint16(*lang), langOff, oldKey, newKey, patched, transPatched, prodFixed)
}

// locate 返回：语言项 ID 字段偏移 / 版本块偏移 / 版本块长度。
func locate(b []byte) (langOff, blobOff, blobLen int, err error) {
	if len(b) < 0x40 {
		return 0, 0, 0, fmt.Errorf("文件太小，不是 PE")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	if pe <= 0 || pe+24 > len(b) {
		return 0, 0, 0, fmt.Errorf("PE 头偏移异常")
	}
	nsec := int(binary.LittleEndian.Uint16(b[pe+6:]))
	optSize := int(binary.LittleEndian.Uint16(b[pe+20:]))
	optOff := pe + 24
	secOff := optOff + optSize

	rsrcRVA := binary.LittleEndian.Uint32(b[optOff+resDirEntry:])
	rsrcRaw := -1
	for i := 0; i < nsec; i++ {
		o := secOff + i*40
		va := binary.LittleEndian.Uint32(b[o+12:])
		rawSize := binary.LittleEndian.Uint32(b[o+16:])
		rawPtr := binary.LittleEndian.Uint32(b[o+20:])
		if rsrcRVA >= va && rsrcRVA < va+maxU32(rawSize, 1) {
			rsrcRaw = int(rawPtr + (rsrcRVA - va))
		}
	}
	if rsrcRaw < 0 || rsrcRaw >= len(b) {
		return 0, 0, 0, fmt.Errorf("找不到 .rsrc 节")
	}

	n1 := int(binary.LittleEndian.Uint16(b[rsrcRaw+12:])) + int(binary.LittleEndian.Uint16(b[rsrcRaw+14:]))
	for i := 0; i < n1; i++ {
		e1 := rsrcRaw + 16 + i*8
		if binary.LittleEndian.Uint32(b[e1:]) != rtVersion {
			continue
		}
		d2 := rsrcRaw + int(binary.LittleEndian.Uint32(b[e1+4:])&0x7FFFFFFF)

		n2 := int(binary.LittleEndian.Uint16(b[d2+12:])) + int(binary.LittleEndian.Uint16(b[d2+14:]))
		if n2 == 0 {
			return 0, 0, 0, fmt.Errorf("RT_VERSION 没有名字条目")
		}
		d3 := rsrcRaw + int(binary.LittleEndian.Uint32(b[d2+20:])&0x7FFFFFFF)

		n3 := int(binary.LittleEndian.Uint16(b[d3+12:])) + int(binary.LittleEndian.Uint16(b[d3+14:]))
		if n3 == 0 {
			return 0, 0, 0, fmt.Errorf("RT_VERSION 没有语言条目")
		}
		langEntry := d3 + 16
		dataEntry := rsrcRaw + int(binary.LittleEndian.Uint32(b[langEntry+4:])&0x7FFFFFFF)
		if dataEntry+16 > len(b) {
			return 0, 0, 0, fmt.Errorf("数据项偏移越界")
		}
		rva := binary.LittleEndian.Uint32(b[dataEntry:])
		size := binary.LittleEndian.Uint32(b[dataEntry+4:])
		if rva < rsrcRVA {
			return 0, 0, 0, fmt.Errorf("版本块 RVA 异常")
		}
		off := int(rva-rsrcRVA) + rsrcRaw
		if size == 0 || off < 0 || off+int(size) > len(b) {
			return 0, 0, 0, fmt.Errorf("版本块越界（size=%d off=%#x）", size, off)
		}
		return langEntry, off, int(size), nil
	}
	return 0, 0, 0, fmt.Errorf("exe 里没有 RT_VERSION（构建时没带 -info？）")
}

// replaceUTF16 在 blob 内做等长 UTF-16 键替换，返回改写次数。
func replaceUTF16(blob []byte, oldKey, newKey string) int {
	if len(oldKey) != len(newKey) {
		return 0
	}
	oldB := encodeUTF16(oldKey)
	newB := encodeUTF16(newKey)
	count := 0
	for i := 0; i+len(oldB) <= len(blob); {
		if !matchFold(blob[i:], oldB) {
			i++
			continue
		}
		copy(blob[i:], newB)
		count++
		i += len(oldB)
	}
	return count
}

// encodeUTF16 把 ASCII 十六进制键编码成大写 UTF-16LE（版本块里的键是大写）。
func encodeUTF16(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c, 0)
	}
	return out
}

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "verlang: "+format+"\n", args...)
	os.Exit(1)
}

// matchFold 比较 UTF-16LE 的 ASCII 键，忽略大小写（版本块里的十六进制大小写不定）。
func matchFold(b, pat []byte) bool {
	if len(b) < len(pat) {
		return false
	}
	for k := 0; k < len(pat); k += 2 {
		c := b[k]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != pat[k] || b[k+1] != 0 {
			return false
		}
	}
	return true
}

// replaceU32 在 blob 内替换等长小端 DWORD，返回改写次数。
func replaceU32(blob []byte, old, new uint32) int {
	pat := make([]byte, 4)
	binary.LittleEndian.PutUint32(pat, old)
	rep := make([]byte, 4)
	binary.LittleEndian.PutUint32(rep, new)
	count := 0
	for i := 0; i+4 <= len(blob); i++ {
		if blob[i] != pat[0] || blob[i+1] != pat[1] || blob[i+2] != pat[2] || blob[i+3] != pat[3] {
			continue
		}
		copy(blob[i:], rep)
		count++
		i += 3
	}
	return count
}

// syncProductVersion 把 VS_FIXEDFILEINFO 的 dwProductVersionMS/LS 设成与文件版本相同。
func syncProductVersion(blob []byte) bool {
	sig := []byte{0xbd, 0x04, 0xef, 0xfe} // 0xFEEF04BD 小端
	for i := 0; i+52 <= len(blob); i++ {
		if blob[i] != sig[0] || blob[i+1] != sig[1] || blob[i+2] != sig[2] || blob[i+3] != sig[3] {
			continue
		}
		// +8/+12 = 文件版本 MS/LS；+16/+20 = 产品版本 MS/LS
		copy(blob[i+16:i+20], blob[i+8:i+12])
		copy(blob[i+20:i+24], blob[i+12:i+16])
		return true
	}
	return false
}
