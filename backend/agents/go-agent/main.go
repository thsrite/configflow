package main

import (
	"agent/clioutput"
	"agent/preflight"
	"agent/routes"
	"agent/upgrade"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(runAgent())
}

func runAgent() int {
	// 使用命令行参数指定配置文件路径
	// Go的flag包支持单横线(-)和双横线(--)两种格式的参数
	configPath := flag.String("config", "", "Path to the configuration file")
	printVersion := flag.Bool("version", false, "Print Agent version and exit")
	cleanupOnly := flag.Bool("cleanup", false, "Reclaim completed deployment payloads and oversized Agent logs, then exit")
	upgradeWorker := flag.String("upgrade-worker", "", "Run a durable Agent update outside the Agent service")
	checkDeployments := flag.Bool("check-deployments", false, "Check deployment journals without changing files or services; do not register or listen")
	requireCleanDeployments := flag.Bool("require-clean-deployments", false, "Read-only installation preflight; exit nonzero if deployment or native Agent update journals require attention")
	recoverOnly := flag.Bool("recover-only", false, "Deprecated compatibility alias for -check-deployments; does not restore files")
	recoverAndExit := flag.Bool("recover-and-exit", false, "Recover interrupted deployments with service lifecycle, then exit without registration")
	validateService := flag.Bool("validate-service", false, "Validate OxiDNS service working directory without registration or mutation")
	checkEnvironment := flag.Bool("check-environment", false, "Inspect native installation TUN, permissions, IPv4 forwarding and port occupancy without changing the system")
	prepareTUN := flag.Bool("prepare-tun", false, "Prepare enabled TUN for native AIO: load the local driver and persist it, or explain required container host changes")
	assumeTUN := flag.Bool("assume-tun-enabled", false, "With -prepare-tun: prepare TUN even when the bootstrap config has TUN disabled (gateway installer)")
	environmentPeerConfig := flag.String("environment-peer-config", "", "Peer Agent candidate configuration for a joint native AIO environment check")
	verbose := flag.Bool("verbose", false, "Expand one-shot command diagnostics; full details are always saved to a private log")
	haCheck := flag.Bool("ha-check", false, "keepalived health and managed handoff check; no registration, listening or writes")

	// 解析命令行参数
	flag.Parse()
	if *haCheck {
		// keepalived runs this every second: no logs, no migration, no side effects.
		return routes.RunOxiDNSHACheck(*configPath)
	}
	if *assumeTUN && !*prepareTUN {
		fmt.Fprintln(os.Stderr, "-assume-tun-enabled 只能与 -prepare-tun 一起使用。")
		return 2
	}
	if *prepareTUN && (*configPath == "" || *printVersion || *upgradeWorker != "" || *cleanupOnly ||
		*checkDeployments || *requireCleanDeployments || *recoverOnly || *recoverAndExit ||
		*validateService || *checkEnvironment || *environmentPeerConfig != "") {
		fmt.Fprintln(os.Stderr, "-prepare-tun 必须明确指定 -config，不能与其他操作参数一起使用。")
		return 2
	}
	if *printVersion {
		fmt.Println(upgrade.Version)
		return 0
	}
	if *upgradeWorker != "" {
		if err := upgrade.Run(*upgradeWorker); err != nil {
			log.Printf("Agent update: %v", err)
			return 1
		}
		return 0
	}
	oneShot := *prepareTUN || *checkEnvironment || *checkDeployments || *requireCleanDeployments || *recoverOnly || *recoverAndExit || *cleanupOnly || *validateService
	var output *clioutput.Session
	if oneShot {
		operation := "check-deployments"
		switch {
		case *prepareTUN:
			operation = "prepare-tun"
		case *checkEnvironment:
			operation = "environment"
		case *validateService:
			operation = "validate-service"
		case *cleanupOnly:
			operation = "cleanup"
		case *recoverAndExit:
			operation = "recover-deployments"
		}
		output = clioutput.New(operation, *verbose)
		defer output.Close()
	}
	fail := func(title string, err error, fix string) int {
		log.Printf("%s: %v", title, err)
		if output != nil {
			output.Result("error", title, err.Error(), fix)
		}
		return 1
	}

	// 如果没有通过命令行参数指定配置文件路径，则使用自动检测
	if *configPath == "" {
		// 获取agent配置目录，优先使用环境变量，否则使用默认路径
		agentDirs := []string{"/opt/configflow-agent", "/opt/sublink-agent"}
		if envAgentDir := os.Getenv("AGENT_DIR"); envAgentDir != "" {
			agentDirs = []string{envAgentDir}
		}
		configFiles := []string{"config-mihomo.json", "config-mosdns.json", "config-oxidns.json", "config.json"}
		for _, agentDir := range agentDirs {
			for _, configFile := range configFiles {
				fullPath := filepath.Join(agentDir, configFile)
				if _, err := os.Stat(fullPath); err == nil {
					*configPath = fullPath
					break
				}
			}
			if *configPath != "" {
				break
			}
		}
		if *configPath == "" {
			*configPath = filepath.Join(agentDirs[0], "config.json")
		}
	}

	log.Printf("Using config file: %s", *configPath)
	if *prepareTUN {
		candidate, err := preflight.LoadConfig(*configPath)
		var report preflight.Report
		if err != nil {
			report = preflight.Report{{Name: "安装配置", Level: "error", Message: err.Error(), Fix: "核对 Mihomo 安装参数并重新生成脚本。"}}
		} else if *assumeTUN {
			report = preflight.PrepareTUNAssumeEnabled(candidate)
		} else {
			report = preflight.PrepareTUN(candidate)
		}
		report.PrintWithOptions(output.Out, output.Options, true)
		if report.HasErrors() {
			return 1
		}
		return 0
	}
	if *checkEnvironment {
		paths := []string{*configPath}
		if *environmentPeerConfig != "" {
			paths = append(paths, *environmentPeerConfig)
		}
		configs := make([]preflight.Config, 0, len(paths))
		for _, path := range paths {
			candidate, err := preflight.LoadConfig(path)
			if err != nil {
				preflight.Report{{Name: "安装配置", Level: "error", Message: err.Error(), Fix: "核对两个核心的安装参数并重新生成脚本。"}}.PrintWithOptions(output.Out, output.Options, false)
				return 1
			}
			configs = append(configs, candidate)
		}
		report := preflight.Run(configs)
		report.PrintWithOptions(output.Out, output.Options, false)
		if report.HasErrors() {
			return 1
		}
		return 0
	}
	if !*checkDeployments && !*requireCleanDeployments && !*recoverOnly && !*recoverAndExit && !*cleanupOnly && !*validateService {
		binary, err := os.Executable()
		if err != nil {
			return fail("无法确定 Agent 程序路径", err, "")
		}
		if err = upgrade.EnsureInstalled(*configPath, binary); err != nil {
			return fail("Agent migration", err, "")
		}
	}

	// 1. 加载配置
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return fail("Agent 配置加载失败", err, "检查 -config 指定的配置路径与读取权限。")
	}
	log.Println("Configuration loaded successfully.")
	if *validateService {
		output.Title("核心服务配置检查")
		if err := routes.ValidateOxiDNSServiceDirectory(toRoutesConfig(cfg)); err != nil {
			return fail("核心服务配置检查未通过", err, "核对 OxiDNS 服务的工作目录及启动参数。")
		}
		output.Result("pass", "核心服务配置检查通过", "OxiDNS 服务目录校验完成；未更改文件或服务。", "")
		return 0
	}
	if *cleanupOnly {
		output.Title("清理 Agent 历史文件")
		if err := cleanupDisk(cfg); err != nil {
			return fail("Agent 清理未完成", err, "检查目录权限、磁盘空间与详细日志。")
		}
		log.Println("Disk cleanup completed.")
		output.Result("pass", "Agent 历史文件清理完成", "已回收符合条件的已完成发布载荷与超限 Agent 日志。", "")
		return 0
	}

	// Recovery changes live files and can stop the core, so only an explicit
	// operator request may run it. Old installed boot gates still invoke
	// -recover-only during upgrades; keep that entry point read-only as well.
	if *recoverAndExit {
		output.Title("恢复中断的发布")
		if err := routes.RecoverDeployments(toRoutesConfig(cfg), true); err != nil {
			return fail("发布恢复未完成", err, "检查发布日志中的失败步骤，处理后显式重试 -recover-and-exit。")
		}
		log.Println("Deployment recovery completed.")
		output.Result("pass", "发布恢复完成", "已完成显式请求的发布恢复及相应服务操作。", "")
		return 0
	}
	if output != nil {
		output.Title("发布记录检查（只读）")
	}
	if *requireCleanDeployments && cfg.NativeAIO && upgrade.Pending(filepath.Join(filepath.Dir(cfg.filePath), "configflow-agent")) {
		log.Print("Native AIO Agent update requires completion or recovery; installation preflight left files and services unchanged.")
		output.Result("error", "Agent 更新记录需要处理", "检测到未完成或不可读取的 Agent 更新记录。", "先完成或恢复上次 Agent 更新，再重新安装。文件与服务未更改。")
		return 1
	}
	if err := routes.CheckDeployments(toRoutesConfig(cfg)); err != nil {
		log.Printf("Deployment check requires attention: %v. Files and services were left unchanged; inspect the journal and use -recover-and-exit to recover explicitly.", err)
		if output != nil {
			level := "warn"
			if *requireCleanDeployments {
				level = "error"
			}
			output.Result(level, "发布记录需要处理", err.Error(), "检查发布日志；需要恢复时显式运行 -recover-and-exit。本次未更改文件或服务。")
		}
		if *requireCleanDeployments {
			return 1
		}
	} else {
		log.Println("Deployment check completed; no recovery required.")
		if output != nil {
			output.Result("pass", "发布记录检查通过", "没有需要恢复的发布记录；未更改文件或服务。", "")
		}
	}
	if *checkDeployments || *requireCleanDeployments || *recoverOnly {
		return 0
	}

	// 2. 如果未注册，执行注册流程
	if cfg.AgentID == "" || cfg.Token == "" {
		if err := cfg.RegisterAgent(); err != nil {
			return fail("Fatal: Agent registration failed", err, "")
		}
		// 注册成功后，配置已更新并保存
	} else {
		log.Printf("Agent already registered with ID: %s", cfg.AgentID)
	}

	// 3. 在后台启动心跳循环
	startDiskCleanup(cfg)
	StartHeartbeatLoop(cfg)
	routes.ReapplyReturnRouting(toRoutesConfig(cfg))
	routes.ReapplyLinuxGateway(toRoutesConfig(cfg))
	routes.ReapplyIPv6Readiness(toRoutesConfig(cfg))
	routes.ReapplyForwardingBoot(toRoutesConfig(cfg))
	routes.ReapplyOxiDNSHA(toRoutesConfig(cfg))
	routes.StartDNSWatchdog(toRoutesConfig(cfg))

	// 4. 在前台启动 API 服务器 (这是一个阻塞操作)
	StartAPIServer(cfg)
	return 0
}
