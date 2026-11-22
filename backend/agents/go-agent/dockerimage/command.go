package dockerimage

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"agent/svc"
)

// Command is the subcommand name (configflow-agent docker-config ...).
const Command = "docker-config"

// AgentDir is the persistent Agent directory of the image.
const AgentDir = "/opt/configflow-agent"

var cores = map[string]bool{"mihomo": true, "mosdns": true, "oxidns": true}

// Run executes docker-config and returns the exit code.
//
//	docker-config agent SERVICE NAME PORT CONFIG_PATH
//	docker-config services [-macvlan-ha] CORE[=CONFIG_PATH] ...
//	docker-config companion
func Run(args []string, env Environment, stderr io.Writer) int {
	return run(args, env, svc.DefaultLayout, AgentDir, stderr)
}

func run(args []string, env Environment, layout svc.Layout, agentDir string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, msgUsage)
		return 2
	}
	var err error
	switch args[0] {
	case "agent":
		if len(args) != 5 || !cores[args[1]] {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		err = WriteAgentConfig(AgentConfig{Path: filepath.Join(agentDir, "config-"+args[1]+".json"),
			Service: args[1], Name: args[2], Port: args[3], ConfigPath: args[4]}, env)
	case "services":
		flags := flag.NewFlagSet("services", flag.ContinueOnError)
		flags.SetOutput(stderr)
		macvlanHA := flags.Bool("macvlan-ha", false, "stop keepalived before the Agent and core at shutdown")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		var plan svc.Plan
		if plan, err = mainPlan(flags.Args(), *macvlanHA); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		err = svc.Setup(layout, plan)
	case "companion":
		if len(args) != 1 {
			fmt.Fprintln(stderr, msgUsage)
			return 2
		}
		err = svc.Setup(layout, svc.CompanionPlan())
	default:
		fmt.Fprintln(stderr, msgUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, msgFailed+"\n", err)
		return 1
	}
	return 0
}

// mainPlan enables each named core with its Agent; CORE=PATH also runs the
// core with that configuration file.
func mainPlan(args []string, macvlanHA bool) (svc.Plan, error) {
	var enabled []string
	argv := map[string][]string{}
	seen := map[string]bool{}
	for _, arg := range args {
		core, config, hasConfig := strings.Cut(arg, "=")
		if !cores[core] || seen[core] || (hasConfig && !filepath.IsAbs(config)) {
			return svc.Plan{}, fmt.Errorf(msgBadCore, arg)
		}
		seen[core] = true
		enabled = append(enabled, core, "agent-"+core)
		if hasConfig {
			argv[core] = svc.CoreArgv(core, config)
		}
	}
	return svc.MainPlan(enabled, argv, macvlanHA), nil
}

// LookupEnv is the container environment.
var LookupEnv Environment = os.LookupEnv
