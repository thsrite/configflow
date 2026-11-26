package preflight

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"agent/internal/atomicfile"
)

const tunModulesFile = "/etc/modules-load.d/99-configflow-tun.conf"
const tunModulesContent = "# Managed by ConfigFlow Shell AIO TUN preparation v1\ntun\n"

// tunPrepareProbe isolates all host access so preparation tests cannot load a
// module, edit /etc, or enable a service on the machine running the tests.
type tunPrepareProbe struct {
	goos     string
	readFile func(string) ([]byte, error)
	stat     func(string) (os.FileInfo, error)
	lstat    func(string) (os.FileInfo, error)
	readlink func(string) (string, error)
	tun      func(string) (uint32, error)
	lookPath func(string) (string, error)
	run      func(string, ...string) ([]byte, error)
	persist  func(string, []byte, []byte, bool) error
	pause    func(time.Duration)
}

func defaultTUNPrepareProbe() tunPrepareProbe {
	return tunPrepareProbe{
		goos: runtime.GOOS, readFile: os.ReadFile, stat: os.Stat, lstat: os.Lstat, readlink: os.Readlink,
		tun: systemTunFeatures, lookPath: exec.LookPath, persist: persistTUNFile, pause: time.Sleep,
		run: func(name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if ctx.Err() != nil {
				return output, ctx.Err()
			}
			return output, err
		},
	}
}

// PrepareTUN is an explicit mutating operation for the ordinary native AIO
// installer. Run and -check-environment remain read-only. It never starts a
// core, creates an interface, changes device permissions, or unloads a module.
func PrepareTUN(cfg Config) Report {
	return prepareTUNWithProbe(cfg, defaultTUNPrepareProbe())
}

// PrepareTUNAssumeEnabled prepares TUN for a configuration that will enable it
// later. The gateway installer bootstraps with TUN disabled so installation
// cannot take over traffic before rules are published. Invalid or ambiguous
// TUN entries are still rejected before any host change.
func PrepareTUNAssumeEnabled(cfg Config) Report {
	return prepareTUNWithMode(cfg, defaultTUNPrepareProbe(), true)
}

func tunPreparationError(message, fix string) Report {
	return Report{{Name: "TUN 自动准备", Level: "error", Message: message, Fix: fix}}
}

func prepareTUNWithProbe(cfg Config, probe tunPrepareProbe) Report {
	return prepareTUNWithMode(cfg, probe, false)
}

func prepareTUNWithMode(cfg Config, probe tunPrepareProbe, assume bool) Report {
	if cfg.ServiceType != "mihomo" {
		return Report{{Name: "TUN 自动准备", Level: "pass", Message: "此核心无需准备 Mihomo TUN，未修改系统。"}}
	}
	doc, err := loadCoreConfig(cfg)
	if err != nil {
		return tunPreparationError(err.Error(), "修复核心配置后重新运行安装脚本。")
	}
	state, configurationChecks := configuredMihomoTUN(doc)
	// Read-only diagnostics may still require TUN when another entry is invalid.
	// Preparation must not mutate the host until every relevant entry is clear.
	if len(configurationChecks) != 0 {
		report := make(Report, 0, len(configurationChecks))
		for _, check := range configurationChecks {
			check.Level = "error"
			check.Message += " 未修改系统。"
			report = append(report, check)
		}
		return report
	}
	if state == tunUnknown {
		return tunPreparationError("无法确定当前配置的 TUN 状态，未修改系统。", "修复 tun 和 listeners 配置后重新运行安装脚本。")
	}
	if assume && state == tunDisabled {
		state = tunEnabled
	}
	if state != tunEnabled {
		return Report{{Name: "TUN 自动准备", Level: "pass", Message: "当前配置未启用 TUN，跳过模块加载与开机配置。"}}
	}
	if !cfg.NativeAIO || (cfg.ServiceManager != "systemd" && cfg.ServiceManager != "openrc") {
		return tunPreparationError("TUN 自动准备仅支持普通 Shell 同机安装的 systemd/OpenRC 服务。", "使用普通 Shell 同机安装入口；其他部署由管理员准备 TUN。")
	}
	if probe.goos != "linux" {
		return tunPreparationError("TUN 自动准备只能在目标 Linux 中执行，未修改系统。", "在运行 Mihomo 的 Linux/虚拟机内重新运行安装脚本。")
	}
	container, err := detectTUNContainer(probe)
	if err != nil {
		return tunPreparationError("无法可靠区分本机与容器，未加载模块或写入开机配置："+err.Error(), "确保可读取容器标识及 /proc/1 信息，或提供可用的 systemd-detect-virt 后重试。")
	}
	deviceErr := usableTUNDevice(probe)
	if container != "" {
		if deviceErr != nil {
			return tunPreparationError("检测到 "+container+" 容器，TUN 不可用："+deviceErr.Error()+"；未在容器内加载模块或修改宿主机配置。", containerTUNFix(container))
		}
		return Report{{Name: "TUN 自动准备", Level: "pass", Message: "检测到 " + container + " 容器，已验证映射的 TUN 字符设备及 TUNGETFEATURES；模块加载和开机配置由宿主机管理，未修改。"}}
	}
	loaded := false
	if deviceErr != nil {
		if _, err := probe.lookPath("modprobe"); err != nil {
			return tunPreparationError("本机 TUN 不可用且缺少 modprobe："+deviceErr.Error(), "安装与当前内核匹配的模块及 modprobe 工具后重试；未创建设备节点或放宽权限。")
		}
		if output, err := probe.run("modprobe", "tun"); err != nil {
			return tunPreparationError("本机 modprobe tun 失败："+tunCommandError(output, err), "检查当前内核是否提供 TUN 模块、模块版本及 CAP_SYS_MODULE；未修改开机配置。")
		}
		loaded = true
		// Device managers may briefly lag module registration. Bound the delay
		// to 800ms and only inspect the device; never run udevadm or create it.
		var deviceErr error
		for attempt := 0; attempt < 5; attempt++ {
			deviceErr = usableTUNDevice(probe)
			if deviceErr == nil {
				break
			}
			if attempt < 4 {
				probe.pause(200 * time.Millisecond)
			}
		}
		if err := deviceErr; err != nil {
			return tunPreparationError("modprobe tun 已执行，但 TUN 仍不可用："+err.Error(), "检查设备管理器与 /dev/net/tun 的访问策略；未创建接口、设备节点或修改权限，也未卸载模块。")
		}
	}
	builtin, err := tunBuiltIntoKernel(probe)
	if err != nil {
		return tunPreparationError("TUN 当前已可用，但无法确认开机加载方式："+err.Error(), "补齐当前内核的 modules.builtin/模块索引或 modinfo 工具后重试；未卸载已加载的 TUN。")
	}
	if builtin {
		return Report{{Name: "TUN 自动准备", Level: "pass", Message: "已验证 TUN 可用且驱动内置于当前内核，无需模块开机加载文件。"}}
	}
	if err := persistTUNModule(probe, cfg.ServiceManager); err != nil {
		prefix := "TUN 当前已可用，但未完成开机持久化："
		if loaded {
			prefix = "本次已加载并验证 TUN，但未完成开机持久化："
		}
		return tunPreparationError(prefix+err.Error(), "按具体错误处理持久化文件或模块加载服务后重试；本次不会卸载已加载模块，也不会重启核心。")
	}
	return Report{{Name: "TUN 自动准备", Level: "pass", Message: "已验证 TUN 字符设备及 TUNGETFEATURES，并确认模块开机加载配置；未创建接口或重启核心。"}}
}

func usableTUNDevice(probe tunPrepareProbe) error {
	info, err := probe.stat("/dev/net/tun")
	if err != nil {
		return fmt.Errorf("无法访问 /dev/net/tun：%w", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("/dev/net/tun 不是字符设备")
	}
	flags, err := probe.tun("/dev/net/tun")
	if err != nil {
		return fmt.Errorf("打开设备或读取 TUNGETFEATURES 失败：%w", err)
	}
	if flags&1 == 0 {
		return fmt.Errorf("驱动未报告 IFF_TUN 支持")
	}
	return nil
}

func containerName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, name := range []string{"podman", "docker", "lxc", "openvz", "systemd-nspawn", "kubepods", "containerd"} {
		if strings.Contains(value, name) {
			return name
		}
	}
	return "未知类型"
}

func detectTUNContainer(probe tunPrepareProbe) (string, error) {
	var uncertain []string
	// OpenVZ exposes /proc/vz on both sides, but /proc/bc only on its host.
	if _, err := probe.lstat("/proc/vz"); err == nil {
		if _, err := probe.lstat("/proc/bc"); os.IsNotExist(err) {
			return "openvz", nil
		} else if err != nil {
			uncertain = append(uncertain, "/proc/bc 无法检查")
		}
	} else if !os.IsNotExist(err) {
		uncertain = append(uncertain, "/proc/vz 无法检查")
	}
	for _, path := range []string{"/run/systemd/container", "/run/host/container-manager"} {
		data, err := probe.readFile(path)
		if err == nil {
			return containerName(string(data)), nil
		}
		if !os.IsNotExist(err) {
			uncertain = append(uncertain, path+" 不可读")
		}
	}
	for _, marker := range []struct{ path, name string }{{"/.dockerenv", "docker"}, {"/run/.containerenv", "podman"}} {
		if _, err := probe.lstat(marker.path); err == nil {
			return marker.name, nil
		} else if !os.IsNotExist(err) {
			uncertain = append(uncertain, marker.path+" 无法检查")
		}
	}
	procReadable := true
	for _, path := range []string{"/proc/1/environ", "/proc/1/cgroup", "/proc/self/cgroup"} {
		data, err := probe.readFile(path)
		if err != nil {
			procReadable = false
			continue
		}
		if path == "/proc/1/environ" {
			for _, field := range strings.Split(string(data), "\x00") {
				if strings.HasPrefix(field, "container=") && len(field) > len("container=") {
					return containerName(strings.TrimPrefix(field, "container=")), nil
				}
			}
		} else if name := containerName(string(data)); name != "未知类型" {
			return name, nil
		}
	}
	// Like systemd's detect_container(), check the initial PID namespace inode
	// (0xEFFFFFFC since Linux 3.8). A privileged container can retain SYS_MODULE
	// and hide cgroup/marker evidence; capability alone never identifies a host.
	namespace, namespaceErr := probe.readlink("/proc/self/ns/pid")
	if namespaceErr == nil && namespace != "pid:[4026531836]" {
		if strings.HasPrefix(namespace, "pid:[") && strings.HasSuffix(namespace, "]") {
			return "未知类型（独立 PID 命名空间）", nil
		}
		namespaceErr = fmt.Errorf("PID 命名空间标识无效")
	}
	if namespaceErr != nil {
		return "", fmt.Errorf("无法核实初始 PID 命名空间：%w", namespaceErr)
	}
	if _, err := probe.lookPath("systemd-detect-virt"); err == nil {
		output, runErr := probe.run("systemd-detect-virt", "--container")
		value := strings.TrimSpace(string(output))
		if runErr == nil && value != "" && value != "none" {
			return containerName(value), nil
		}
		var status interface{ ExitCode() int }
		if errors.As(runErr, &status) && status.ExitCode() == 1 && value == "none" && len(uncertain) == 0 {
			return "", nil
		}
		return "", fmt.Errorf("systemd-detect-virt 未给出可靠结果：%s", tunCommandError(output, runErr))
	}
	// Lack of systemd tools is normal on OpenRC. Initial PID namespace, readable
	// proc evidence and capability must all agree before loading a host module.
	data, err := probe.readFile("/proc/self/status")
	mask, maskErr := capabilityMask(data, "CapEff")
	if procReadable && len(uncertain) == 0 && err == nil && maskErr == nil && mask&(1<<16) != 0 {
		return "", nil
	}
	return "", fmt.Errorf("未发现明确容器标识，但 /proc 信息或 CAP_SYS_MODULE 不足，不能安全认定为虚拟机/物理机")
}

func containerTUNFix(kind string) string {
	switch kind {
	case "lxc":
		return "由 LXC 宿主机管理员加载 tun，并按容器管理器映射 /dev/net/tun、保留 CAP_NET_ADMIN；仅当宿主机是 PVE 时，检查 pct config <CTID> 的空闲 devN 槽，再使用 pct set <CTID> --devN /dev/net/tun，完整停止并启动容器。不要改成特权容器。"
	case "docker", "podman", "containerd", "kubepods":
		return "由宿主机管理员加载 tun，并通过实际容器运行时提供 /dev/net/tun 和 NET_ADMIN；Docker/Podman 使用 --device=/dev/net/tun --cap-add=NET_ADMIN 后重新创建容器，不要使用 --privileged。"
	default:
		return "由宿主机管理员加载 tun，并按实际容器管理器映射 /dev/net/tun、授予所需 NET_ADMIN；本安装器不会在容器内加载宿主机模块或放宽隔离。"
	}
}

func tunCommandError(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if len(text) > 500 {
		text = text[:500] + "…"
	}
	if err == nil {
		return text
	}
	if text == "" {
		return err.Error()
	}
	return err.Error() + "（" + text + "）"
}

func tunBuiltIntoKernel(probe tunPrepareProbe) (bool, error) {
	if data, err := probe.readFile("/proc/modules"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "tun" {
				return false, nil
			}
		}
	}
	if data, err := probe.readFile("/proc/sys/kernel/osrelease"); err == nil {
		version := strings.TrimSpace(string(data))
		if version != "" && version != "." && version != ".." && !strings.ContainsAny(version, "/\\\x00\n\r") {
			if modules, err := probe.readFile("/lib/modules/" + version + "/modules.builtin"); err == nil {
				for _, line := range strings.Split(string(modules), "\n") {
					if filepath.Base(strings.TrimSpace(line)) == "tun.ko" {
						return true, nil
					}
				}
			}
		}
	}
	if _, err := probe.lookPath("modinfo"); err == nil {
		output, err := probe.run("modinfo", "-F", "filename", "tun")
		if err == nil {
			name := strings.TrimSpace(string(output))
			if name == "(builtin)" || name == "builtin" {
				return true, nil
			}
			for _, suffix := range []string{".ko", ".ko.gz", ".ko.xz", ".ko.zst"} {
				if filepath.IsAbs(name) && strings.HasSuffix(name, suffix) {
					return false, nil
				}
			}
		}
	}
	return false, fmt.Errorf("/proc/modules、当前内核 modules.builtin 与 modinfo 均未确认 tun 的类型")
}

func moduleListContainsTUN(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if strings.HasPrefix(line, ";") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "tun" {
			return true
		}
	}
	return false
}

func dedicatedTUNModuleFile(data []byte) (present bool, err error) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		// systemd treats the entire non-comment line as the module name. Do not
		// accept legacy modprobe arguments or an inline comment in this format.
		if line != "tun" {
			return false, fmt.Errorf("%s 含其他模块或无效 tun 行，拒绝覆盖", tunModulesFile)
		}
		present = true
	}
	return present, nil
}

func readTUNPersistence(probe tunPrepareProbe, path string) ([]byte, bool, error) {
	parent := filepath.Dir(path)
	if info, err := probe.lstat(parent); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("持久化目录不是普通目录：%s", parent)
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("无法检查持久化目录 %s：%w", parent, err)
	}
	info, err := probe.lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("无法检查 %s：%w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s 必须是普通文件，不能是符号链接或其他设备", path)
	}
	data, err := probe.readFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("无法读取 %s：%w", path, err)
	}
	return data, true, nil
}

func persistTUNModule(probe tunPrepareProbe, manager string) error {
	path := tunModulesFile
	var openrcScript []byte
	if manager == "openrc" {
		// A currently loaded module can make /dev/net/tun usable even when
		// the command needed by OpenRC's modules service is absent at boot.
		if _, err := probe.lookPath("modprobe"); err != nil {
			return fmt.Errorf("OpenRC modules 开机加载需要 modprobe，请安装 kmod 或修复 PATH 后重试")
		}
		var err error
		openrcScript, err = probe.readFile("/etc/init.d/modules")
		if err != nil {
			return fmt.Errorf("无法读取 OpenRC modules 服务：%w", err)
		}
		if !strings.Contains(string(openrcScript), "/etc/modules-load.d") {
			if !strings.Contains(string(openrcScript), "/etc/modules") {
				return fmt.Errorf("OpenRC modules 服务未声明支持 modules-load.d 或 /etc/modules")
			}
			path = "/etc/modules"
		}
		if path == tunModulesFile {
			// Alpine's modules service gives /run precedence over /etc, unlike
			// systemd. Do not silently claim persistence with a temporary override.
			if _, err := probe.lstat("/run/modules-load.d/99-configflow-tun.conf"); err == nil || !os.IsNotExist(err) {
				return fmt.Errorf("/run/modules-load.d/99-configflow-tun.conf 可能覆盖持久化配置，请先处理临时配置")
			}
		}
	} else {
		output, err := probe.run("systemctl", "show", "systemd-modules-load.service", "--property=LoadState", "--value")
		if err != nil || strings.TrimSpace(string(output)) != "loaded" {
			return fmt.Errorf("systemd-modules-load.service 不可用或已屏蔽：%s", tunCommandError(output, err))
		}
	}
	before, exists, err := readTUNPersistence(probe, path)
	if err != nil {
		return err
	}
	after := before
	containsTUN := moduleListContainsTUN(before)
	if path == tunModulesFile {
		containsTUN, err = dedicatedTUNModuleFile(before)
		if err != nil {
			return err
		}
	}
	if !containsTUN {
		if path == tunModulesFile {
			if exists && len(bytes.TrimSpace(before)) != 0 {
				return fmt.Errorf("%s 已有其他内容，拒绝覆盖", path)
			}
			after = []byte(tunModulesContent)
		} else {
			after = append([]byte(nil), before...)
			if len(after) > 0 && after[len(after)-1] != '\n' {
				after = append(after, '\n')
			}
			after = append(after, []byte("# ConfigFlow Shell AIO: load TUN at boot\ntun\n")...)
		}
		if err := probe.persist(path, before, after, exists); err != nil {
			return fmt.Errorf("写入 %s 失败：%w", path, err)
		}
	}
	verified, present, err := readTUNPersistence(probe, path)
	if err != nil || !present || !bytes.Equal(verified, after) {
		return fmt.Errorf("%s 写入后核对失败", path)
	}
	if manager == "openrc" {
		boot := "/etc/runlevels/boot/modules"
		data, err := probe.readFile(boot)
		if err == nil && bytes.Equal(data, openrcScript) {
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("OpenRC modules 开机入口已存在但内容无法核对，请检查 %s", boot)
		}
		if output, err := probe.run("rc-update", "add", "modules", "boot"); err != nil {
			return fmt.Errorf("模块配置已写入，但启用 OpenRC modules boot 失败：%s", tunCommandError(output, err))
		}
		data, err = probe.readFile(boot)
		if err != nil || !bytes.Equal(data, openrcScript) {
			return fmt.Errorf("模块配置已写入，但未能确认 OpenRC modules 服务已加入 boot")
		}
	}
	return nil
}

// persistTUNFile preserves a legacy module list and uses exclusive creation for
// a new file. Never follow a symlink or overwrite a file changed since reading.
func persistTUNFile(path string, before, after []byte, existed bool) error {
	dir := filepath.Dir(path)
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("持久化目录不是普通目录：%s", dir)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	check := func() error {
		info, err := os.Lstat(path)
		if !existed {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("目标文件已被其他操作创建：%s", path)
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("目标文件类型已变化：%s", path)
		}
		mode = info.Mode().Perm()
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, before) {
			return fmt.Errorf("目标文件内容已变化：%s", path)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	return atomicfile.WriteWith(path, after, mode, atomicfile.Options{
		Pattern:      ".configflow-tun-*",
		Exclusive:    !existed,
		BeforeCommit: check,
	})
}
