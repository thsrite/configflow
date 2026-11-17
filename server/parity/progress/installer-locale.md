# Installer shell-test locale fix

Scope: branch `go-fix-installer-locale`, created from local `go` at
`8489da36`. Do not change `go-port-r07`; no pushes or subagents.
Only fix the test environment. The coordinator explicitly kept generated scripts
identical to Python and deferred any real-installer interpreter workaround.

## Cause and change

- Original .32 non-login bash run, LANG/LANGUAGE/LC_* unset:
  `go test -count=1 -timeout=20m ./internal/installer` failed in 251.467s.
  Six dash subtests failed: SummaryKeepsDetailsPrivateAndInteractionVisible,
  OriginalErrexitExitStatusAndCleanupArePreserved,
  UnavailableLogFallsBackBeforeDependencyInstall,
  MetadataFailureDoesNotAbortInstaller,
  VerboseOptionReplaysDetailsOnceAndFinalResultIsLast,
  ExternalNestedInstallerReusesLogAndDefersCompletion (all prefixed Test).
- Root cause is the dash interpreter, not Go rendering: even
  `f() { a="$1"; printf '%s\n' "$a"; }; f '安装与维护'`
  corrupts the bytes in C locale. .32 has Arch dash 0.5.13.4-1; .64 initially
  lacked dash, so installed 0.5.13.5-1 before validation. Both reproduce on
  startup without locale. Runtime `export LC_ALL=C.UTF-8` repairs .13.5 but
  does not repair .13.4. A trial script locale guard was completely reverted.
- `runShell` now appends `LANG=C.UTF-8` and `LC_ALL=C.UTF-8` **after**
  constructing either inherited or replacement child environment. Caller
  environments containing LC_ALL=C cannot defeat the harness requirement;
  the Go test process environment is not changed by the harness.
- New `TestRunShellLocaleDoesNotDependOnCaller`: 18 shell/mode cases
  (parent absent/C/UTF-8 × sh/dash/bash × inherit/replace), with explicitly
  conflicting child locale options; asserts exact child locale, UTF-8
  variable-assignment bytes, exit status and empty stderr.
- No production scripts, generators, fingerprints, recordings or expected
  renderings changed. Existing Python byte comparisons remain strict.

## Known real-environment limit and Debian 12

- This is shared with the original Python-generated installer. On affected dash,
  absent LANG/LC_* (or explicit LC_ALL=C) can write corrupted Chinese bytes to
  **report logs as well as the terminal**.
- A sandbox probe used the real Python output wrapper after asserting both
  embedded report helpers equal the Python files. It copied the actual generated
  Agent JSON heredoc, with `config_path=/etc/中文/config.yaml`, into a temporary
  config. On .32 dash .13.4, terminal/log Chinese were corrupted for absent/C
  locale; the generated **ASCII-escaped Agent JSON remained byte-identical** in
  all three environments. This is evidence for that config path only, not a
  complete installation/configuration-safety claim.
- Debian's [package page](https://packages.debian.org/bookworm/amd64/dash)
  identifies Bookworm's dash as **0.5.12-2**. Downloaded its official amd64 .deb;
  verified SHA256
  `33ea40061da2f1a861ec46212b2b6a34f0776a049b1a3f0abce2fb8cb994258f`
  against the [official download page](https://packages.debian.org/bookworm/amd64/dash/download).
  Extracted, not installed/replaced on the host. Its binary passed the same
  Chinese terminal/log and Agent JSON probe for absent/C/C.UTF-8 locale.
  This ran on .32's libc; L1's independent Debian 12 guest installation with
  LANG unset remains the actual guest acceptance. No guest was accessed here.
- No configuration corruption has been confirmed; if L1 finds it, open a
  separate defect as instructed. Production dash workarounds remain deferred.

## Validation

Done: .64 installer full package passed twice (absent locale 127.809s;
inherited environment 128.478s). .32 harness regression passed all 18 cases
(0.033s); .64 regression passed again on the final harness (0.034s).
.64 gofmt of internal/ and tools/ had no output; `go vet ./...` passed.
In progress: .32 full installer absent/UTF-8 runs, then rebase and all-package
suite on .64 with normal UTF-8 environment. No implementation work remains.

## Exact reproduction/probe commands

Initial failing reproduction (.32, exit 1; last line `FAIL configflow/internal/installer 251.467s`):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -c 'cd server && for key in ${!LC_@} LANG LANGUAGE; do unset "$key"; done; go test -count=1 -timeout=20m ./internal/installer > /tmp/installer-locale-before.log 2>&1; result=$?; tail -70 /tmp/installer-locale-before.log; exit "$result"'
RT_HOST=sunbroke@192.168.0.64 rt -- paru -S --needed --noconfirm dash
```

The second command installed `dash-0.5.13.5-1`. The independent probe command
below is run from the git root on .32 by `rt -- bash -c '<body>'` (single quotes
in the body shell-escaped). It only writes/removes its own temporary directories;
it does not install Debian packages or execute full installers.

```bash
set -eu
probe_dir=$(mktemp -d /tmp/cf-debian-dash.XXXXXX)
trap 'rm -rf "$probe_dir"' EXIT
curl -fsSL --retry 2 --connect-timeout 10 https://deb.debian.org/debian/pool/main/d/dash/dash_0.5.12-2_amd64.deb -o "$probe_dir/dash.deb"
printf '%s  %s\n' 33ea40061da2f1a861ec46212b2b6a34f0776a049b1a3f0abce2fb8cb994258f "$probe_dir/dash.deb" | sha256sum -c -
ar p "$probe_dir/dash.deb" data.tar.xz | tar -xJ -C "$probe_dir"
pacman -Q dash
~/cf-test/.venv/bin/python - /usr/bin/dash "$probe_dir/bin/dash" <<'PY'
import json, pathlib, re, subprocess, sys, tempfile
from backend.agents.install_output import with_install_output, CLI_OUTPUT_HELPERS, INSTALL_OUTPUT_HELPERS
from backend.agents.go_install_script import generate_go_agent_install_script
for name, text in [('cli-output.sh', CLI_OUTPUT_HELPERS), ('install-output.sh', INSTALL_OUTPUT_HELPERS)]:
    assert pathlib.Path('server/internal/installer/scripts', name).read_text() == text
full = generate_go_agent_install_script(server_url='https://config.example', agent_name='locale-probe',
    service_type='mihomo', config_path='/etc/中文/config.yaml')
payload = re.search(r"cat > \"\$CONFIG_FILE.new\" <<'CONFIGFLOW_JSON_EOF'\n(.*?)\nCONFIGFLOW_JSON_EOF", full, re.S)[1]
assert payload.isascii()
for shell in sys.argv[1:]:
    for mode in ['unset', 'C', 'C.UTF-8']:
        with tempfile.TemporaryDirectory(prefix='cf-locale-probe-') as tmp:
            root = pathlib.Path(tmp)
            source = "#!/bin/sh\nset -eu\ncf_step '下载核心'\ncat > config.json <<'JSON_EOF'\n" + payload + "\nJSON_EOF\ncf_fact service_mihomo 'Mihomo：运行中'\ncf_note probe '没有节点，当前直连'\ncf_complete '安装完成'\n"
            script = root / 'install.sh'
            script.write_text(with_install_output(source), encoding='utf-8')
            env = {'PATH': '/usr/bin:/bin', 'CONFIGFLOW_INSTALL_LOG_DIR': tmp}
            if mode != 'unset':
                env.update(LANG=mode, LC_ALL=mode)
            result = subprocess.run([shell, str(script)], env=env, cwd=tmp, capture_output=True, timeout=10)
            log = next(root.glob('configflow-install.*/install.log')).read_bytes()
            terminal_ok = 'Mihomo：运行中'.encode() in result.stdout
            log_ok = 'Mihomo：运行中'.encode() in log
            config_ok = (root/'config.json').read_bytes() == (payload+'\n').encode()
            assert result.returncode == 0 and config_ok
            print(json.dumps(dict(shell=shell, locale=mode, terminal_utf8=terminal_ok,
                log_utf8=log_ok, ascii_agent_config_intact=config_ok), sort_keys=True))
PY
```

Probe output (all JSON keys shown above were compared by the probe):

```text
/tmp/cf-debian-dash.*/dash.deb: OK
dash 0.5.13.4-1
/usr/bin/dash  unset: terminal_utf8=false log_utf8=false ascii_agent_config_intact=true
/usr/bin/dash  C: terminal_utf8=false log_utf8=false ascii_agent_config_intact=true
/usr/bin/dash  C.UTF-8: terminal_utf8=true log_utf8=true ascii_agent_config_intact=true
Debian dash    unset: terminal_utf8=true log_utf8=true ascii_agent_config_intact=true
Debian dash    C: terminal_utf8=true log_utf8=true ascii_agent_config_intact=true
Debian dash    C.UTF-8: terminal_utf8=true log_utf8=true ascii_agent_config_intact=true
```

Full installer environment validation body, run with `RT_HOST=sunbroke@192.168.0.32 rt -- bash -c '<body>'`:

```bash
set -eu
cd server
command -v dash
for mode in unset normal; do
    echo "INSTALLER_MODE=$mode"
    if [ "$mode" = unset ]; then
        (
            for key in ${!LC_@} LANG LANGUAGE; do unset "$key"; done
            go test -count=1 -timeout=30m ./internal/installer
        )
    else
        LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 go test -count=1 -timeout=30m ./internal/installer
    fi
done
echo INSTALLER_BOTH_MODES_PASS
```

