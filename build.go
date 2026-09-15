// EdKeyBridge 构建脚本（纯 Go，替代原先的 build.sh）。
//
// 用法（仓库根目录执行）：
//
//	go run build.go          # 生成资源 → 编译 → UPX 压缩
//	go run build.go icon     # 重新生成 icon/app.ico 与 icon/preview.png
//
// 构建流程：
//  1. rsrc：把 comctl32 v6 清单 + 应用图标编成 src/rsrc_windows_amd64.syso
//     （必须输出到 src/ —— Go 只链接包目录下的 .syso）
//  2. go build：编译无控制台窗口的 GUI 版 exe，并注入版本号
//  3. upx：压缩（未安装则跳过，产物仍可用）
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "icon" {
		makeIcon()
		return
	}
	build()
}

// ---------------------------------------------------------------- 构建

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GOSUMDB=off")
	fmt.Printf("→ %s %s\n", name, strings.Join(args, " "))
	return cmd.Run()
}

// gitDescribe 取版本号（形如 v0.1.1 或 v0.1.1-3-gabc1234）；失败时返回空串。
func gitDescribe() string {
	out, err := exec.Command("git", "describe", "--tags", "--always").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func build() {
	if _, err := os.Stat("go.mod"); err != nil {
		fmt.Println("请在仓库根目录运行：go run build.go")
		os.Exit(1)
	}

	fmt.Println("[1/3] rsrc: manifest + icon -> src/rsrc_windows_amd64.syso")
	if err := run("go", "run", "github.com/akavel/rsrc@v0.10.2",
		"-manifest", "app.manifest", "-ico", "icon/app.ico",
		"-arch", "amd64", "-o", filepath.Join("src", "rsrc_windows_amd64.syso")); err != nil {
		fmt.Println("rsrc 失败:", err)
		os.Exit(1)
	}

	fmt.Println("[2/3] go build (windowsgui, version stamped)")
	ldflags := "-s -w -H windowsgui"
	if v := gitDescribe(); v != "" {
		// 坑：main 包的 -X 只认 main.<var>，写 import path 会静默失效（版本永远是 dev）
		ldflags += " -X main.version=" + v
	}
	if err := run("go", "build", "-trimpath", "-ldflags", ldflags,
		"-o", "edkeybridge.exe", "./src"); err != nil {
		fmt.Println("go build 失败:", err)
		os.Exit(1)
	}

	fmt.Println("[3/3] upx --best --lzma")
	if _, err := exec.LookPath("upx"); err != nil {
		fmt.Println("未找到 upx，跳过压缩（产物仍可用）")
	} else if err := run("upx", "--best", "--lzma", "edkeybridge.exe"); err != nil {
		fmt.Println("upx 失败（不影响产物）:", err)
	}

	if abs, err := filepath.Abs("edkeybridge.exe"); err == nil {
		fmt.Println("done ->", abs)
	}
}

// ---------------------------------------------------------------- 图标生成
//
// 设计：深蓝圆角徽章 + 暖色键帽（带键柄），读作"键盘按键"。
// 同一份图元同时驱动多尺寸 .ico 与预览 PNG；改 design() 后重跑即可。

const iconW = 256 // 设计坐标系尺寸

var (
	iconBG  = [3]float64{38, 50, 86}    // 深蓝徽章
	iconKey = [3]float64{255, 176, 87}  // 暖色键帽
	iconRim = [3]float64{255, 255, 255} // 极淡白描边（深色任务栏上留边界）
)

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// sdRoundBox 有符号距离函数：圆角矩形。正=外部，负=内部。
func sdRoundBox(px, py, cx, cy, hw, hh, r float64) float64 {
	qx := math.Abs(px-cx) - (hw - r)
	qy := math.Abs(py-cy) - (hh - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

type iconLayer struct {
	sdf   func(px, py float64) float64
	col   [3]float64
	alpha float64
}

func design() []iconLayer {
	return []iconLayer{
		{func(px, py float64) float64 { return sdRoundBox(px, py, 128, 128, 110, 110, 44) }, iconRim, 0.14},
		{func(px, py float64) float64 { return sdRoundBox(px, py, 128, 128, 104, 104, 40) }, iconBG, 1.0},
		// 键柄（上）与键帽（下）重叠，合成一个连贯的"按键"轮廓
		{func(px, py float64) float64 { return sdRoundBox(px, py, 128, 96, 22, 48, 10) }, iconKey, 1.0},
		{func(px, py float64) float64 { return sdRoundBox(px, py, 128, 154, 50, 44, 18) }, iconKey, 1.0},
	}
}

// render 按尺寸栅格化，返回 RGBA 像素（自左上到右下）。
func render(size int) []byte {
	aa := float64(iconW) / float64(size)
	img := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px := (float64(x) + 0.5) * aa
			py := (float64(y) + 0.5) * aa
			var cr, cg, cb, ca float64
			for _, l := range design() {
				d := l.sdf(px, py)
				cov := clamp01(0.5-d/aa) * l.alpha
				if cov <= 0 {
					continue
				}
				na := cov + ca*(1-cov)
				if na <= 0 {
					continue
				}
				cr = (l.col[0]*cov + cr*ca*(1-cov)) / na
				cg = (l.col[1]*cov + cg*ca*(1-cov)) / na
				cb = (l.col[2]*cov + cb*ca*(1-cov)) / na
				ca = na
			}
			i := (y*size + x) * 4
			img[i] = byte(int(cr + 0.5))
			img[i+1] = byte(int(cg + 0.5))
			img[i+2] = byte(int(cb + 0.5))
			img[i+3] = byte(int(ca*255 + 0.5))
		}
	}
	return img
}

// dibBytes 小尺寸用 32bpp DIB（XOR 自下而上 + 1bpp AND 掩码全 0）。
func dibBytes(size int, img []byte) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)             // biSize
	binary.LittleEndian.PutUint32(hdr[4:], uint32(size))   // biWidth
	binary.LittleEndian.PutUint32(hdr[8:], uint32(size*2)) // biHeight（XOR + AND）
	binary.LittleEndian.PutUint16(hdr[12:], 1)             // biPlanes
	binary.LittleEndian.PutUint16(hdr[14:], 32)            // biBitCount
	b.Write(hdr)
	for y := size - 1; y >= 0; y-- { // DIB 自下而上
		b.Write(img[y*size*4 : (y+1)*size*4])
	}
	b.Write(make([]byte, ((size+31)/32)*4*size)) // AND 掩码全 0
	return b.Bytes()
}

// pngBytes 编码 RGBA PNG（用于 128/256 尺寸与预览图）。
func pngBytes(size int, img []byte) []byte {
	m := image.NewRGBA(image.Rect(0, 0, size, size))
	copy(m.Pix, img)
	var b bytes.Buffer
	// 最高压缩：这几档 PNG 会直接进 .rsrc，而 UPX 不压缩 .rsrc，省一点是一点
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&b, m); err != nil {
		panic(err)
	}
	return b.Bytes()
}

type icoEntry struct {
	size int
	data []byte
}

// icoBytes 组装 .ico：ICONDIR + 目录项 + 数据区。
func icoBytes(entries []icoEntry) []byte {
	out := new(bytes.Buffer)
	binary.Write(out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	dir, data := new(bytes.Buffer), new(bytes.Buffer)
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0 // 256 在目录项里用 0 表示
		}
		dir.WriteByte(dim)                                 // width
		dir.WriteByte(dim)                                 // height
		dir.WriteByte(0)                                   // palette
		dir.WriteByte(0)                                   // reserved
		binary.Write(dir, binary.LittleEndian, uint16(1))  // planes
		binary.Write(dir, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(dir, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(dir, binary.LittleEndian, uint32(offset+data.Len()))
		data.Write(e.data)
	}
	out.Write(dir.Bytes())
	out.Write(data.Bytes())
	return out.Bytes()
}

func makeIcon() {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	entries := make([]icoEntry, 0, len(sizes))
	for _, s := range sizes {
		img := render(s)
		if s >= 128 {
			entries = append(entries, icoEntry{s, pngBytes(s, img)})
		} else {
			entries = append(entries, icoEntry{s, dibBytes(s, img)})
		}
		if s == 256 {
			if err := os.WriteFile(filepath.Join("icon", "preview.png"), pngBytes(s, img), 0o644); err != nil {
				panic(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join("icon", "app.ico"), icoBytes(entries), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("图标已生成: icon/app.ico, icon/preview.png 尺寸:", sizes)
}
