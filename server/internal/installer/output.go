package installer

import (
	"configflow/internal/textx"
	"strings"
)

// Port of backend/agents/install_output.py.

// CLIOutputHelpers and InstallOutputHelpers are CLI_OUTPUT_HELPERS and
// INSTALL_OUTPUT_HELPERS.
var (
	CLIOutputHelpers     = cliOutputHelpers
	InstallOutputHelpers = installOutputHelpers
)

// WithRootPrivileges is with_root_privileges(script): keep privilege handling
// in the downloaded file, preserving stdin/args.
func WithRootPrivileges(script string) string {
	lines := textx.SplitLines(script, true)
	split := 1
	for split < len(lines) && strings.HasPrefix(lines[split], "# ConfigFlow") {
		split++
	}
	if split > len(lines) {
		split = len(lines)
	}
	return strings.Join(lines[:split], "") + `if [ "$(id -u)" -ne 0 ]; then
    if ! command -v sudo >/dev/null 2>&1; then
        printf '%s\n' '请使用 root 运行安装脚本，或先安装并配置 sudo。' >&2
        exit 1
    fi
    exec sudo sh "$0" "$@"
fi
` + strings.Join(lines[split:], "")
}

// WithInstallOutput is with_install_output(script). The original body keeps
// its own errexit and traps in an unconditional subshell.
func WithInstallOutput(script string) string { return withInstallOutput(script, "") }

// compensationWait keeps the outer report shell of an Agent installer alive on
// HUP/INT/TERM. A terminal interrupt reaches the whole job; without a trap the
// outer shell died at once while the body's cleanup kept restoring the previous
// Agent unseen, its result only in the log. Traps run after the foreground body
// exits, so the result and the log path are reported first (L1 review).
const compensationWait = "trap ':' HUP INT TERM\n"

// withCompensatingInstallOutput is WithInstallOutput for installers whose body
// restores the previous Agent on failure (single-service and AIO).
func withCompensatingInstallOutput(script string) string {
	return withInstallOutput(script, compensationWait)
}

func withInstallOutput(script, beforeBody string) string {
	lines := textx.SplitLines(script, true)
	header := ""
	if len(lines) > 0 {
		header = lines[0]
	} else {
		// There is no executable body to wrap.
		return ""
	}
	body := strings.Join(lines[1:], "")
	if len(lines) > 1 && strings.HasPrefix(lines[1], "# ConfigFlow") {
		header += lines[1]
		body = strings.Join(lines[2:], "")
	}
	return header + "set +e\n" + CLIOutputHelpers + "\n" + InstallOutputHelpers + `
cf_verbose_options "$@"
cf_init "安装与维护"
` + beforeBody + `(
` + trimScriptTail(body) + `
)
cf_installer_exit_status=$?
cf_finish "$cf_installer_exit_status"
exit "$cf_installer_exit_status"
`
}
