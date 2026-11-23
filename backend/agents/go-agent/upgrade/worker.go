package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent/internal/atomicfile"
	"golang.org/x/sys/unix"
)

type Request struct {
	ID      string `json:"update_id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}
type Status struct {
	ID              string  `json:"update_id"`
	Status          string  `json:"status"`
	Version         string  `json:"target_version"`
	PreviousVersion string  `json:"previous_version"`
	AgentID         string  `json:"agent_id"`
	Error           string  `json:"error,omitempty"`
	UpdatedAt       string  `json:"updated_at"`
	Message         string  `json:"message,omitempty"`
	Events          []Event `json:"events,omitempty"`
}

// Events describe work actually entered by this worker. Never include request
// URLs, configuration contents or raw errors in the progress timeline.
type Event struct {
	At      string `json:"at"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

const maxEvents = 80

type Job struct {
	Status
	ConfigFile   string      `json:"config_file"`
	Binary       string      `json:"binary"`
	Worker       string      `json:"worker"`
	Digest       string      `json:"digest"`
	Size         int64       `json:"size"`
	Backup       string      `json:"backup"`
	BackupDigest string      `json:"backup_digest"`
	Snapshots    []Snapshot  `json:"snapshots,omitempty"`
	Manager      string      `json:"manager"`
	AgentUnit    string      `json:"agent_unit"`
	WorkerUnit   string      `json:"worker_unit"`
	Mutating     bool        `json:"mutating"`
	BinaryMode   os.FileMode `json:"binary_mode"`
	BinaryUID    int         `json:"binary_uid"`
	BinaryGID    int         `json:"binary_gid"`
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,64}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var displayVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9._+-]{1,64}$`)

func agentVersionLabel(version string) string {
	if displayVersionPattern.MatchString(version) {
		return "Agent " + version
	}
	return "Agent 目标版本"
}

func (j *Job) stageMessage(stage string) string {
	target := agentVersionLabel(j.Version)
	switch stage {
	case "queued":
		return "等待更新进程启动，目标：" + target
	case "downloading":
		arch := runtime.GOARCH
		if arch == "arm" {
			arch = "armv7"
		}
		return fmt.Sprintf("正在下载 %s（configflow-agent-linux-%s，%d 字节），并校验文件大小与 SHA-256", target, arch, j.Size)
	case "verifying":
		return "下载文件大小与 SHA-256 校验通过；正在检查 " + target + " 的可执行性和版本号"
	case "backing_up":
		return "正在备份旧版 Agent 程序、配置和服务文件"
	case "migrating":
		return "正在迁移 Agent 配置和服务文件，保留设备身份与核心配置"
	case "replacing":
		return "正在用 " + target + " 替换 Agent 主程序"
	case "restarting":
		return "正在重启 Agent 服务以运行 " + target
	case "checking":
		return "正在确认 " + target + " 的运行版本、设备身份和配置迁移结果"
	case "rolling_back":
		return "正在校验并恢复旧版 Agent 程序、配置和服务文件"
	case "succeeded":
		return target + " 已启动，设备身份与配置迁移检查通过"
	case "failed":
		return "Agent 更新失败，尚未替换现有部署；具体原因见错误详情"
	case "rolled_back":
		return "已恢复旧版 Agent 程序与配置，并通过服务检查"
	case "rollback_failed":
		return "Agent 恢复失败，需要处理；具体原因见错误详情"
	}
	return ""
}

func terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rolled_back" || status == "rollback_failed"
}
func rootFor(binary string) string     { return filepath.Join(filepath.Dir(binary), ".configflow-updates") }
func CurrentPath(binary string) string { return filepath.Join(rootFor(binary), "current.json") }
func loadJob(path string) (*Job, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var job Job
	if err = json.Unmarshal(b, &job); err != nil {
		return nil, err
	}
	if filepath.Base(path) == "current.json" && idPattern.MatchString(job.ID) {
		own := filepath.Join(filepath.Dir(path), "job-"+job.ID+".json")
		if _, err := os.Stat(own); err == nil {
			return loadJob(own)
		}
	}
	return &job, nil
}
func (j *Job) save(path, stage string) error {
	j.Status.Status, j.UpdatedAt = stage, time.Now().UTC().Format(time.RFC3339Nano)
	j.Message = j.stageMessage(stage)
	if len(j.Events) == 0 || j.Events[len(j.Events)-1].Status != stage || j.Events[len(j.Events)-1].Message != j.Message {
		j.Events = append(j.Events, Event{At: j.UpdatedAt, Status: stage, Message: j.Message})
		if len(j.Events) > maxEvents {
			j.Events = j.Events[len(j.Events)-maxEvents:]
		}
	}
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	err = AtomicWrite(path, b, 0600)
	if err == nil && path != CurrentPath(j.Binary) {
		// An old supervisor may finish after a subsequent upgrade was reserved.
		// Its journal must never change the current task's identity or result.
		if current, readErr := loadJob(CurrentPath(j.Binary)); readErr == nil && current.ID == j.ID {
			err = AtomicWrite(CurrentPath(j.Binary), b, 0600)
		}
	}
	if err == nil {
		logger.Infof("Agent update %s: %s — %s", j.ID, stage, j.Message)
	}
	return err
}
func ReadStatus(binary, agentID string) (*Status, error) {
	j, err := loadJob(CurrentPath(binary))
	if err != nil {
		return nil, err
	}
	if j.AgentID != agentID {
		return nil, os.ErrNotExist
	}
	if terminal(j.Status.Status) {
		release, lockErr := lock(filepath.Join(rootFor(binary), "update.lock"))
		if lockErr != nil {
			status := j.Status
			status.Status = "checking"
			status.Message = "更新步骤已执行，正在等待更新进程释放操作锁"
			if n := len(status.Events); n > 0 && terminal(status.Events[n-1].Status) {
				status.Events = status.Events[:n-1]
			}
			return &status, nil
		}
		release()
	}
	return &j.Status, nil
}
func Pending(binary string) bool {
	j, err := loadJob(CurrentPath(binary))
	if err != nil {
		return !os.IsNotExist(err)
	}
	return !terminal(j.Status.Status) || j.Status.Status == "rollback_failed"
}
func lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("another Agent update or service operation is active")
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
func waitLock(path string) (func(), error) {
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		release, err := lock(path)
		if err == nil || time.Now().After(deadline) {
			return release, err
		}
	}
}
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyAtomic(from, to string, mode os.FileMode) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return AtomicWrite(to, b, mode)
}
func command(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if name == "supervise-daemon" && len(args) > 0 {
		cmd.Env = append(os.Environ(), "RC_SVCNAME="+args[0])
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		if len(output) > 1024 {
			output = output[:1024]
		}
		return fmt.Errorf("%s operation failed: %s", name, strings.TrimSpace(string(output)))
	}
	return nil
}
func reload(manager string) error {
	if manager == "systemd" {
		return command("systemctl", "daemon-reload")
	}
	return command("rc-update", "-u")
}
func restart(manager, unit string) error {
	if manager == "systemd" {
		_ = command("systemctl", "reset-failed", unit)
		return command("systemctl", "--no-block", "restart", unit)
	}
	return command("rc-service", unit, "restart")
}

func Prepare(configFile, binary string, req Request) (*Status, error) {
	if !idPattern.MatchString(req.ID) || !digestPattern.MatchString(req.SHA256) || req.Size < 4 || req.Size > 128<<20 || req.Version == "" || len(req.Version) > 64 {
		return nil, fmt.Errorf("valid update ID, target version, SHA-256 and bounded size are required")
	}
	c, err := ReadConfig(configFile)
	if err != nil {
		return nil, err
	}
	if err = c.InferLifecycle(); err != nil {
		return nil, err
	}
	if c.Docker() {
		return nil, fmt.Errorf("Docker requires image update")
	}
	if c.String("agent_id") == "" || c.String("token") == "" {
		return nil, fmt.Errorf("Agent registration is required")
	}
	release, err := lock(filepath.Join(rootFor(binary), "update.lock"))
	if err != nil {
		return nil, err
	}
	defer release()
	path := CurrentPath(binary)
	if previous, readErr := loadJob(path); readErr == nil {
		if previous.ID == req.ID {
			if previous.AgentID != c.String("agent_id") || previous.Digest != req.SHA256 || previous.Version != req.Version || previous.Size != req.Size {
				return nil, fmt.Errorf("update ID already belongs to different content")
			}
			return &previous.Status, nil
		}
		if !terminal(previous.Status.Status) || previous.Status.Status == "rollback_failed" {
			return nil, fmt.Errorf("previous Agent update requires completion or recovery")
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("unreadable Agent update journal")
	}
	job := Job{Status: Status{ID: req.ID, Version: req.Version, PreviousVersion: Version, AgentID: c.String("agent_id")},
		ConfigFile: configFile, Binary: binary, Digest: req.SHA256, Size: req.Size,
		Manager: c.String("service_manager"), AgentUnit: c.AgentUnit(), WorkerUnit: "configflow-upgrade-" + req.ID[:16]}
	job.Worker = filepath.Join(rootFor(binary), "worker-"+req.ID)
	job.Backup = filepath.Join(rootFor(binary), "backup-"+req.ID)
	if err = copyAtomic(binary, job.Worker, 0700); err != nil {
		return nil, err
	}
	if err = job.save(path, "queued"); err != nil {
		return nil, err
	}
	jobPath := filepath.Join(rootFor(binary), "job-"+job.ID+".json")
	if err = job.save(jobPath, "queued"); err != nil {
		return nil, err
	}
	if err = startWorker(&job, jobPath); err != nil {
		job.Error = err.Error()
		_ = job.save(jobPath, "failed")
		cleanupWorker(&job)
		return nil, err
	}
	return &job.Status, nil
}

func startWorker(job *Job, path string) error {
	if job.Manager == "systemd" {
		unit := "[Unit]\nDescription=ConfigFlow Agent update\nAfter=network.target\n[Service]\nType=simple\nExecStart=" + systemdQuote(job.Worker) + " -upgrade-worker " + systemdQuote(path) + "\nRestart=on-failure\nRestartSec=2\n[Install]\nWantedBy=multi-user.target\n"
		if err := AtomicWrite("/etc/systemd/system/"+job.WorkerUnit+".service", []byte(unit), 0644); err != nil {
			return err
		}
		if err := reload(job.Manager); err != nil {
			return err
		}
		if err := command("systemctl", "enable", job.WorkerUnit+".service"); err != nil {
			return err
		}
		return command("systemctl", "start", job.WorkerUnit+".service")
	}
	if _, err := exec.LookPath("supervise-daemon"); err != nil {
		return fmt.Errorf("OpenRC supervise-daemon is required for recoverable updates")
	}
	unit := "#!/sbin/openrc-run\nname=\"ConfigFlow Agent update\"\ncommand=" + shellQuote(job.Worker) + "\ncommand_args=" + shellQuote("-upgrade-worker "+shellQuote(path)) + "\nsupervisor=supervise-daemon\nrespawn_delay=2\nrespawn_max=0\noutput_log=" + shellQuote(filepath.Join(rootFor(job.Binary), job.ID+".log")) + "\nerror_log=" + shellQuote(filepath.Join(rootFor(job.Binary), job.ID+".log")) + "\npidfile=/run/" + job.WorkerUnit + ".pid\ndepend() {\n need localmount\n after net\n}\n"
	if err := AtomicWrite("/etc/init.d/"+job.WorkerUnit, []byte(unit), 0755); err != nil {
		return err
	}
	if err := command("rc-update", "add", job.WorkerUnit, "default"); err != nil {
		return err
	}
	return command("rc-service", job.WorkerUnit, "start")
}
func cleanupWorker(job *Job) {
	if job.Manager == "systemd" {
		_ = command("systemctl", "disable", job.WorkerUnit+".service")
		_ = os.Remove("/etc/systemd/system/" + job.WorkerUnit + ".service")
		_ = reload(job.Manager)
	} else {
		_ = command("rc-update", "del", job.WorkerUnit, "default")
		_ = os.Remove("/etc/init.d/" + job.WorkerUnit)
		_ = command("supervise-daemon", job.WorkerUnit, "--stop", "--pidfile", "/run/"+job.WorkerUnit+".pid")
	}
}

func download(c *Config, job *Job, destination string) error {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = "armv7"
	}
	if arch != "amd64" && arch != "arm64" && arch != "armv7" {
		return fmt.Errorf("unsupported Agent architecture")
	}
	url := strings.TrimRight(c.String("server_url"), "/") + "/api/agents/download/configflow-agent-linux-" + arch
	client := &http.Client{Timeout: 3 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("Agent download connection failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Agent download HTTP %d", response.StatusCode)
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, job.Size+1))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("Agent download write failed")
	}
	if n != job.Size || hex.EncodeToString(h.Sum(nil)) != job.Digest {
		return fmt.Errorf("Agent download size or SHA-256 mismatch")
	}
	return nil
}

func verifyCandidate(binary, expected string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "-version").Output()
	if err != nil || strings.TrimSpace(string(out)) != expected {
		return fmt.Errorf("candidate version or executable validation failed")
	}
	return nil
}

func waitReady(c *Config, version string, strict bool) error {
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	host := c.String("agent_host")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		path := "/health"
		if strict {
			path = "/api/upgrade-info"
		}
		req, _ := http.NewRequest("GET", "http://"+net.JoinHostPort(host, strconv.Itoa(c.Port()))+path, nil)
		req.Header.Set("Authorization", "Bearer "+c.String("token"))
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		var value struct {
			Version string `json:"version"`
			AgentID string `json:"agent_id"`
			Ready   bool   `json:"migration_ready"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&value)
		resp.Body.Close()
		if resp.StatusCode == 200 && (!strict || decodeErr == nil && value.Version == version && value.AgentID == c.String("agent_id") && value.Ready) {
			return nil
		}
	}
	return fmt.Errorf("Agent did not confirm expected version, identity and migration readiness")
}

func Run(path string) (result error) {
	job, err := loadJob(path)
	if err != nil {
		return err
	}
	if terminal(job.Status.Status) && job.Status.Status != "rollback_failed" {
		cleanupWorker(job)
		return nil
	}
	release, err := waitLock(filepath.Join(rootFor(job.Binary), "update.lock"))
	if err != nil {
		return err
	}
	defer release()
	c, err := ReadConfig(job.ConfigFile)
	if err != nil {
		return err
	}
	coreLock := filepath.Join(filepath.Dir(c.String("config_path")), ".configflow-deployments/operation.lock")
	coreRelease, err := waitLock(coreLock)
	if err != nil {
		return err
	}
	defer coreRelease()
	defer func() {
		if result != nil {
			job.Error = result.Error()
			if job.Mutating {
				_ = job.save(path, "rolling_back")
				hash, backupErr := hashFile(job.Backup)
				if backupErr == nil && hash != job.BackupDigest {
					backupErr = fmt.Errorf("Agent backup hash mismatch")
				}
				if backupErr == nil {
					backupErr = copyAtomic(job.Backup, job.Binary, job.BinaryMode)
					if backupErr == nil {
						backupErr = os.Chown(job.Binary, job.BinaryUID, job.BinaryGID)
					}
				}
				if backupErr == nil {
					backupErr = Restore(job.Snapshots)
				}
				if backupErr == nil {
					backupErr = reload(job.Manager)
				}
				if backupErr == nil {
					backupErr = restart(job.Manager, job.AgentUnit)
				}
				if backupErr == nil {
					backupErr = waitReady(c, job.PreviousVersion, true)
				}
				if backupErr != nil {
					job.Error += "; rollback: " + backupErr.Error()
					_ = job.save(path, "rollback_failed")
				} else {
					_ = job.save(path, "rolled_back")
				}
			} else {
				_ = job.save(path, "failed")
			}
		}
		if terminal(job.Status.Status) && job.Status.Status != "rollback_failed" {
			cleanupWorker(job)
		}
	}()
	if job.Status.Status != "queued" {
		return fmt.Errorf("interrupted Agent update recovered from durable journal")
	}
	if err = job.save(path, "downloading"); err != nil {
		return err
	}
	candidate := filepath.Join(filepath.Dir(job.Binary), ".configflow-agent-candidate-"+job.ID)
	defer os.Remove(candidate)
	if err = download(c, job, candidate); err != nil {
		return err
	}
	if err = job.save(path, "verifying"); err != nil {
		return err
	}
	if err = verifyCandidate(candidate, job.Version); err != nil {
		return err
	}
	changes, err := MigrationPlan(c, job.Binary, "/")
	if err != nil {
		return err
	}
	if err = job.save(path, "backing_up"); err != nil {
		return err
	}
	for _, change := range changes {
		snapshot, captureErr := captureChange(change)
		if captureErr != nil {
			return captureErr
		}
		job.Snapshots = append(job.Snapshots, snapshot)
	}
	binaryInfo, err := os.Lstat(job.Binary)
	if err != nil {
		return err
	}
	if !binaryInfo.Mode().IsRegular() {
		return fmt.Errorf("Agent executable must be a regular file")
	}
	job.BinaryMode = binaryInfo.Mode().Perm()
	if stat, ok := binaryInfo.Sys().(*syscall.Stat_t); ok {
		job.BinaryUID, job.BinaryGID = int(stat.Uid), int(stat.Gid)
	}
	if err = copyAtomic(job.Binary, job.Backup, 0700); err != nil {
		return err
	}
	job.BackupDigest, err = hashFile(job.Backup)
	if err != nil {
		return err
	}
	job.Mutating = true
	if err = job.save(path, "migrating"); err != nil {
		job.Mutating = false
		return err
	}
	if err = ApplyChanges(changes); err != nil {
		return err
	}
	if err = reload(job.Manager); err != nil {
		return err
	}
	if err = job.save(path, "replacing"); err != nil {
		return err
	}
	if err = os.Chown(candidate, job.BinaryUID, job.BinaryGID); err != nil {
		return err
	}
	if err = os.Chmod(candidate, job.BinaryMode); err != nil {
		return err
	}
	if err = os.Rename(candidate, job.Binary); err != nil {
		return err
	}
	if err = atomicfile.SyncDir(filepath.Dir(job.Binary)); err != nil {
		return err
	}
	if err = job.save(path, "restarting"); err != nil {
		return err
	}
	if err = restart(job.Manager, job.AgentUnit); err != nil {
		return err
	}
	if err = job.save(path, "checking"); err != nil {
		return err
	}
	if err = waitReady(c, job.Version, true); err != nil {
		return err
	}
	return job.save(path, "succeeded")
}
