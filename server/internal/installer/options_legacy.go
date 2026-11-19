package installer

import (
	"math/big"
)

// GoAgentOptions is the compatibility input for AgentInstallOptions.
type GoAgentOptions struct {
	ServerURL               string
	AgentName               string
	ServiceType             string
	AgentPort               any
	AgentIP                 string
	ConfigPath              string
	RestartCommand          string
	BinaryDownloadURL       string
	ServiceManager          string
	ServiceUnit             string
	ServiceBinary           string
	StopCommand             string
	StartCommand            string
	StatusCommand           string
	HealthURL               string
	HealthDNSAddress        string
	HealthDNSName           string
	DeploymentHealthTimeout any
	InstallCore             bool
	ManageIPv4Forwarding    Forwarding
	PrepareDNSHA            bool
	RegistrationToken       string
	RegistrationKey         string
}

// AIOOptions is the compatibility input for AIOInstallOptions.
type AIOOptions struct {
	RegistrationTokens      map[string]string
	RegistrationKey         string
	ServerURL               string
	AgentName               string
	AgentIP                 string
	BinaryDownloadURL       string
	MihomoAgentPort         any // int or str, as Python receives it
	OxidnsAgentPort         any
	MihomoConfigPath        string
	OxidnsConfigPath        string
	MihomoServiceUnit       string
	OxidnsServiceUnit       string
	MihomoServiceBinary     string
	OxidnsServiceBinary     string
	InstallCore             bool
	NonInteractive          bool
	VerifiedAgentPath       string
	InstallationID          string
	InstallationToken       string
	ManageIPv4Forwarding    bool
	ManageTun               bool
	InstallationDescription string
	TunPrepareAssumeEnabled bool
}

// DockerOptions is the compatibility input for DockerInstallOptions.
type DockerOptions struct {
	RegistrationTokens map[string]string
	ServerURL          string
	AgentName          string
	AgentIP            string
	DataDir            string
	NetworkMode        string
	EnableMihomo       bool
	EnableMosdns       bool
	MihomoPort         any
	MosdnsPort         any
	EnableOxidns       bool
	OxidnsPort         any
	EnableDNSHA        bool
	HAParent           string
	HASubnet           string
	HAGateway          string
	HAHostIDPath       string
}

// GatewayOptions is the compatibility input for GatewayInstallOptions.
type GatewayOptions struct {
	ServerURL         string
	AgentName         string
	AgentIP           string
	BinaryDownloadURL string
	MihomoAgentPort   any
	OxidnsAgentPort   any
	EnrollmentURL     string
	EnrollmentToken   string
	AgentChecksums    map[string]string
	InstallationID    string
	FullTun           bool
}

// GenerateGoAgentInstallScript adapts the compatibility options to the native renderer.
func GenerateGoAgentInstallScript(o GoAgentOptions) (string, error) {
	AgentPort, portErr := compatPort(o.AgentPort, 8080)
	DeploymentHealthTimeout, timeoutErr := compatInteger(o.DeploymentHealthTimeout, 30)
	return RenderAgentInstall(AgentInstallOptions{
		ServerURL:               o.ServerURL,
		AgentName:               o.AgentName,
		ServiceType:             o.ServiceType,
		AgentPort:               AgentPort,
		AgentIP:                 o.AgentIP,
		ConfigPath:              o.ConfigPath,
		RestartCommand:          o.RestartCommand,
		BinaryDownloadURL:       o.BinaryDownloadURL,
		ServiceManager:          o.ServiceManager,
		ServiceUnit:             o.ServiceUnit,
		ServiceBinary:           o.ServiceBinary,
		StopCommand:             o.StopCommand,
		StartCommand:            o.StartCommand,
		StatusCommand:           o.StatusCommand,
		HealthURL:               o.HealthURL,
		HealthDNSAddress:        o.HealthDNSAddress,
		HealthDNSName:           o.HealthDNSName,
		DeploymentHealthTimeout: DeploymentHealthTimeout,
		InstallCore:             o.InstallCore,
		ManageIPv4Forwarding:    o.ManageIPv4Forwarding,
		PrepareDNSHA:            o.PrepareDNSHA,
		RegistrationToken:       o.RegistrationToken,
		RegistrationKey:         o.RegistrationKey,
		portError:               portErr, timeoutError: timeoutErr, invalidCorePort: o.AgentPort != nil && !compatDecimalPort(o.AgentPort),
	})
}

// GenerateGoAIOInstallScript adapts the compatibility options to the native renderer.
func GenerateGoAIOInstallScript(o AIOOptions) (string, error) {
	MihomoAgentPort, portErr := compatPort(o.MihomoAgentPort, 8080)
	OxidnsAgentPort, secondErr := compatPort(o.OxidnsAgentPort, 8082)
	if o.MihomoAgentPort != nil && !compatDecimalPort(o.MihomoAgentPort) {
		portErr = valueError("Invalid Agent port")
	}
	if o.OxidnsAgentPort != nil && !compatDecimalPort(o.OxidnsAgentPort) {
		secondErr = valueError("Invalid Agent port")
	}
	if portErr == nil {
		portErr = secondErr
	}
	return RenderAIOInstall(AIOInstallOptions{
		RegistrationTokens:      o.RegistrationTokens,
		RegistrationKey:         o.RegistrationKey,
		ServerURL:               o.ServerURL,
		AgentName:               o.AgentName,
		AgentIP:                 o.AgentIP,
		BinaryDownloadURL:       o.BinaryDownloadURL,
		MihomoAgentPort:         MihomoAgentPort,
		OxidnsAgentPort:         OxidnsAgentPort,
		MihomoConfigPath:        o.MihomoConfigPath,
		OxidnsConfigPath:        o.OxidnsConfigPath,
		MihomoServiceUnit:       o.MihomoServiceUnit,
		OxidnsServiceUnit:       o.OxidnsServiceUnit,
		MihomoServiceBinary:     o.MihomoServiceBinary,
		OxidnsServiceBinary:     o.OxidnsServiceBinary,
		InstallCore:             o.InstallCore,
		NonInteractive:          o.NonInteractive,
		VerifiedAgentPath:       o.VerifiedAgentPath,
		InstallationID:          o.InstallationID,
		InstallationToken:       o.InstallationToken,
		ManageIPv4Forwarding:    o.ManageIPv4Forwarding,
		ManageTun:               o.ManageTun,
		InstallationDescription: o.InstallationDescription,
		TunPrepareAssumeEnabled: o.TunPrepareAssumeEnabled,
		portError:               portErr,
	})
}

// GenerateDockerAgentCompose adapts the compatibility options to the native renderer.
func GenerateDockerAgentCompose(o DockerOptions) (string, error) {
	MihomoPort, err := compatIntegerText(o.MihomoPort, 8080)
	if err != nil {
		return "", err
	}
	MosdnsPort, err := compatIntegerText(o.MosdnsPort, 8081)
	if err != nil {
		return "", err
	}
	OxidnsPort, err := compatIntegerText(o.OxidnsPort, 8082)
	if err != nil {
		return "", err
	}
	return RenderDockerCompose(DockerInstallOptions{
		RegistrationTokens: o.RegistrationTokens,
		ServerURL:          o.ServerURL,
		AgentName:          o.AgentName,
		AgentIP:            o.AgentIP,
		DataDir:            o.DataDir,
		NetworkMode:        o.NetworkMode,
		EnableMihomo:       o.EnableMihomo,
		EnableMosdns:       o.EnableMosdns,
		MihomoPort:         MihomoPort,
		MosdnsPort:         MosdnsPort,
		EnableOxidns:       o.EnableOxidns,
		OxidnsPort:         OxidnsPort,
		EnableDNSHA:        o.EnableDNSHA,
		HAParent:           o.HAParent,
		HASubnet:           o.HASubnet,
		HAGateway:          o.HAGateway,
		HAHostIDPath:       o.HAHostIDPath,
	})
}

// GenerateDockerAgentRun adapts the compatibility options to the native renderer.
func GenerateDockerAgentRun(o DockerOptions) (string, error) {
	MihomoPort, err := compatIntegerText(o.MihomoPort, 8080)
	if err != nil {
		return "", err
	}
	MosdnsPort, err := compatIntegerText(o.MosdnsPort, 8081)
	if err != nil {
		return "", err
	}
	OxidnsPort, err := compatIntegerText(o.OxidnsPort, 8082)
	if err != nil {
		return "", err
	}
	return RenderDockerRun(DockerInstallOptions{
		RegistrationTokens: o.RegistrationTokens,
		ServerURL:          o.ServerURL,
		AgentName:          o.AgentName,
		AgentIP:            o.AgentIP,
		DataDir:            o.DataDir,
		NetworkMode:        o.NetworkMode,
		EnableMihomo:       o.EnableMihomo,
		EnableMosdns:       o.EnableMosdns,
		MihomoPort:         MihomoPort,
		MosdnsPort:         MosdnsPort,
		EnableOxidns:       o.EnableOxidns,
		OxidnsPort:         OxidnsPort,
		EnableDNSHA:        o.EnableDNSHA,
		HAParent:           o.HAParent,
		HASubnet:           o.HASubnet,
		HAGateway:          o.HAGateway,
		HAHostIDPath:       o.HAHostIDPath,
	})
}

// GenerateGatewayInstallScript adapts the compatibility options to the native renderer.
func GenerateGatewayInstallScript(o GatewayOptions) (string, error) {
	MihomoAgentPort, portErr := compatPort(o.MihomoAgentPort, 8080)
	OxidnsAgentPort, secondErr := compatPort(o.OxidnsAgentPort, 8082)
	if o.MihomoAgentPort != nil && !compatDecimalPort(o.MihomoAgentPort) {
		portErr = valueError("Invalid Agent port")
	}
	if o.OxidnsAgentPort != nil && !compatDecimalPort(o.OxidnsAgentPort) {
		secondErr = valueError("Invalid Agent port")
	}
	if portErr == nil {
		portErr = secondErr
	}
	return RenderGatewayInstall(GatewayInstallOptions{
		ServerURL:         o.ServerURL,
		AgentName:         o.AgentName,
		AgentIP:           o.AgentIP,
		BinaryDownloadURL: o.BinaryDownloadURL,
		MihomoAgentPort:   MihomoAgentPort,
		OxidnsAgentPort:   OxidnsAgentPort,
		EnrollmentURL:     o.EnrollmentURL,
		EnrollmentToken:   o.EnrollmentToken,
		AgentChecksums:    o.AgentChecksums,
		InstallationID:    o.InstallationID,
		FullTun:           o.FullTun,
		portError:         portErr,
	})
}

// NewAIOOptions returns the compatibility defaults.
func NewAIOOptions(serverURL, agentName string) AIOOptions {
	n := DefaultAIOOptions(serverURL, agentName)
	return AIOOptions{RegistrationTokens: n.RegistrationTokens, RegistrationKey: n.RegistrationKey, ServerURL: n.ServerURL, AgentName: n.AgentName, AgentIP: n.AgentIP, BinaryDownloadURL: n.BinaryDownloadURL, MihomoAgentPort: n.MihomoAgentPort, OxidnsAgentPort: n.OxidnsAgentPort, MihomoConfigPath: n.MihomoConfigPath, OxidnsConfigPath: n.OxidnsConfigPath, MihomoServiceUnit: n.MihomoServiceUnit, OxidnsServiceUnit: n.OxidnsServiceUnit, MihomoServiceBinary: n.MihomoServiceBinary, OxidnsServiceBinary: n.OxidnsServiceBinary, InstallCore: n.InstallCore, NonInteractive: n.NonInteractive, VerifiedAgentPath: n.VerifiedAgentPath, InstallationID: n.InstallationID, InstallationToken: n.InstallationToken, ManageIPv4Forwarding: n.ManageIPv4Forwarding, ManageTun: n.ManageTun, InstallationDescription: n.InstallationDescription, TunPrepareAssumeEnabled: n.TunPrepareAssumeEnabled}
}

// NewDockerOptions returns the compatibility defaults.
func NewDockerOptions(serverURL string) DockerOptions {
	n := DefaultDockerOptions(serverURL)
	return DockerOptions{RegistrationTokens: n.RegistrationTokens, ServerURL: n.ServerURL, AgentName: n.AgentName, AgentIP: n.AgentIP, DataDir: n.DataDir, NetworkMode: n.NetworkMode, EnableMihomo: n.EnableMihomo, EnableMosdns: n.EnableMosdns, MihomoPort: 8080, MosdnsPort: 8081, EnableOxidns: n.EnableOxidns, OxidnsPort: 8082, EnableDNSHA: n.EnableDNSHA, HAParent: n.HAParent, HASubnet: n.HASubnet, HAGateway: n.HAGateway, HAHostIDPath: n.HAHostIDPath}
}

func compatInteger(value any, fallback int64) (*big.Int, error) {
	if value == nil {
		return big.NewInt(fallback), nil
	}
	switch n := value.(type) {
	case int:
		return big.NewInt(int64(n)), nil
	case int64:
		return big.NewInt(n), nil
	case *big.Int:
		if n != nil {
			return new(big.Int).Set(n), nil
		}
	case bool:
		if n {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case string:
		return parseInstallInteger(n)
	}
	return nil, valueError("Invalid installer integer")
}
func compatPort(value any, fallback int64) (int, error) {
	n, err := compatInteger(value, fallback)
	if err != nil {
		return 0, err
	}
	if !inRange(n, 1, 65535) {
		return 0, valueError("Invalid Agent port")
	}
	return int(n.Int64()), nil
}
func compatIntegerText(value any, fallback int64) (string, error) {
	if boolean, ok := value.(bool); ok {
		if boolean {
			return "True", nil
		}
		return "False", nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	n, err := compatInteger(value, fallback)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}
func compatDecimalPort(value any) bool {
	switch n := value.(type) {
	case string:
		return asciiDigits(n)
	case bool:
		return false
	case int:
		return n >= 0
	case int64:
		return n >= 0
	case *big.Int:
		return n != nil && n.Sign() >= 0
	}
	return false
}
