package autoupdate

// Log lines of the start-up update (the texts of docker/auto_update.py).
const (
	msgPythonManifest     = "ignoring build %s recorded by the Python image; rebuilding for the Go server"
	msgLegacyData         = "检测到数据目录中仅有旧版（SQLite 之前）配置文件（%s），且没有 %s；已跳过自动更新并保持镜像自带版本。请先使用最后一个 Python 版本的 ConfigFlow 完成数据迁移，再升级到此版本。"
	msgSourceUnreachable  = "cannot reach update source (%v); using %s"
	msgAlreadyBuilt       = "%s is at %s, already built"
	msgUpdating           = "updating to %s@%s %s"
	msgRunning            = "running %s (took %.0fs)"
	msgSkippedToolchain   = "skipped %s: %v; use the autoupdate image to build it. Keeping %s so components stay in step"
	msgUpdateFailed       = "update failed, keeping %s: %v"
	msgRestoreFailed      = "restoring the previous build failed (%v); using the image build"
	msgImageRestoreFailed = "restoring the image build failed: %v"
	msgStateDirFailed     = "cannot create the update directory (%v); using the image build"
	msgPreserveFailed     = "cannot keep a copy of the image build: %v"
	msgCommandFailed      = "%s failed (%v):\n%s"
	msgNoGit              = "git is not installed; cannot fetch from git source"
	msgBranchMissing      = "branch %q not found in %s"
	msgArchiveLayout      = "source archive has %d top-level entries, want 1"
	msgNoServerModule     = "source does not contain server/go.mod"
)

// Reasons a changed component cannot be rebuilt in this image.
const (
	msgServerNoGo    = "Go 后端有改动但镜像里没有 Go"
	msgServerArch    = "此 CPU 架构 (%s) 尚未支持自动重建 Go 服务"
	msgFrontendNoNPM = "前端有改动但镜像里没有 npm"
	msgAgentNoGo     = "Agent 有改动但镜像里没有 Go"
	msgProbeNoGo     = "独立测速组件有改动但镜像里没有 Go"
	msgProbeArch     = "此 CPU 架构尚未支持独立测速组件"
)
