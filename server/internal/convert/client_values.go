package convert

import (
	"bytes"
	"encoding/json"
	"fmt"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// clientText uses the single shared lossless scalar codec.
type clientText = model.String

// clientTextList is the protocol's scalar-or-list union, not an ordinary model
// slice. Its struct shape preserves this custom wire contract while each value
// uses model.String. A null list is absent; null list members are invalid.
type clientTextList struct{ values []model.String }

func (s *clientTextList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		s.values = nil
		return nil
	}
	if len(data) > 0 && data[0] == '[' {
		var values []*model.String
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		result := make([]model.String, 0, len(values))
		for _, value := range values {
			if value == nil {
				return fmt.Errorf("%s", msgClientText)
			}
			result = append(result, *value)
		}
		s.values = result
		return nil
	}
	var value model.String
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	s.values = []model.String{value}
	return nil
}
func (s clientTextList) MarshalJSON() ([]byte, error) { return json.Marshal(s.values) }
func (s clientTextList) strings() []string {
	result := make([]string, len(s.values))
	for i, value := range s.values {
		result[i] = sanitizeClientField(string(value))
	}
	return result
}

// clientReserved accepts the two documented WireGuard forms without formatting
// arbitrary containers: a preformatted client ID or an ordered numeric list.
type clientReserved struct {
	Text    string
	Numbers []json.Number
}

func (r *clientReserved) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*r = clientReserved{}
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &r.Text)
	}
	return json.Unmarshal(data, &r.Numbers)
}
func (r clientReserved) MarshalJSON() ([]byte, error) {
	if r.Numbers != nil {
		return json.Marshal(r.Numbers)
	}
	return json.Marshal(r.Text)
}

// clientOptions is the protocol schema shared by Surge and Loon. Bool options
// follow R5; a null pointer is absent. The source remains read-only in Node.Params.
type clientOptions struct {
	ALPNRaw json.RawMessage `json:"-"`
	// RealityMembers: reality-opts is an object with any member, read or not.
	RealityMembers      bool                    `json:"-"`
	Cipher              *clientText             `json:"cipher"`
	Password            *clientText             `json:"password"`
	UUID                *clientText             `json:"uuid"`
	Username            *clientText             `json:"username"`
	SNI                 *clientText             `json:"sni"`
	ServerName          *clientText             `json:"servername"`
	Security            *clientText             `json:"security"`
	Network             *clientText             `json:"network"`
	Flow                *clientText             `json:"flow"`
	Plugin              *clientText             `json:"plugin"`
	PSK                 *clientText             `json:"psk"`
	Version             *clientText             `json:"version"`
	AlterID             *clientText             `json:"alterId"`
	Token               *clientText             `json:"token"`
	TestURL             *clientText             `json:"test-url"`
	SectionName         *clientText             `json:"section-name"`
	MTU                 *clientText             `json:"mtu"`
	PrivateKey          *clientText             `json:"private-key"`
	PrivateKeyAlt       *clientText             `json:"privateKey"`
	PublicKey           *clientText             `json:"public-key"`
	PublicKeyAlt        *clientText             `json:"publicKey"`
	PeerPublicKey       *clientText             `json:"peer-public-key"`
	SelfIP              *clientText             `json:"self-ip"`
	IP                  *clientText             `json:"ip"`
	IPv6                *clientText             `json:"ipv6"`
	Address             *clientText             `json:"address"`
	Endpoint            *clientText             `json:"endpoint"`
	DNS                 clientTextList          `json:"dns"`
	DNSServer           clientTextList          `json:"dns-server"`
	ALPN                clientTextList          `json:"alpn"`
	AllowedIPs          clientTextList          `json:"allowed-ips"`
	AllowedIPsAlt       clientTextList          `json:"allowed_ips"`
	Keepalive           *clientText             `json:"keepalive"`
	PersistentKeepalive *clientText             `json:"persistent-keepalive"`
	PresharedKey        *clientText             `json:"preshared-key"`
	PreSharedKey        *clientText             `json:"pre-shared-key"`
	PresharedKeyAlt     *clientText             `json:"presharedKey"`
	ClientID            *clientText             `json:"client-id"`
	Reserved            clientReserved          `json:"reserved"`
	Obfs                *clientText             `json:"obfs"`
	ObfsHost            *clientText             `json:"obfs-host"`
	ObfsParam           *clientText             `json:"obfs-param"`
	ObfsPassword        *clientText             `json:"obfs-password"`
	Protocol            *clientText             `json:"protocol"`
	ProtocolParam       *clientText             `json:"protocol-param"`
	Path                *clientText             `json:"path"`
	Host                *clientText             `json:"host"`
	ServiceName         *clientText             `json:"serviceName"`
	Ports               *clientText             `json:"ports"`
	HopInterval         *clientText             `json:"hop-interval"`
	Down                *clientText             `json:"down"`
	IdleSessionTimeout  *clientText             `json:"idle-session-timeout"`
	UDP                 *bool                   `json:"udp" stored:"gate"`
	UDPRelay            *bool                   `json:"udp-relay" stored:"gate"`
	TLS                 *bool                   `json:"tls" stored:"gate"`
	SkipCertVerify      *bool                   `json:"skip-cert-verify" stored:"gate"`
	Reuse               *bool                   `json:"reuse" stored:"gate"`
	TFO                 *bool                   `json:"tfo" stored:"gate"`
	UDPOverTCP          *bool                   `json:"udp-over-tcp" stored:"gate"`
	WS                  *clientTransportOptions `json:"ws-opts"`
	WSConfig            *clientTransportOptions `json:"ws-config"`
	HTTP                *clientTransportOptions `json:"http-opts"`
	GRPC                *clientTransportOptions `json:"grpc-opts"`
	PluginOptions       *clientPluginOptions    `json:"plugin-opts"`
	ObfsOptions         *clientPluginOptions    `json:"obfs-opts"`
	Reality             *clientReality          `json:"reality-opts"`
}

type clientTransportOptions struct {
	Path        clientTextList `json:"path"`
	Headers     clientHeaders  `json:"headers"`
	ServiceName *clientText    `json:"grpc-service-name"`
}

type clientHeaders struct {
	Host clientTextList `json:"Host"`
}

type clientPluginOptions struct {
	Mode     *clientText `json:"mode"`
	Host     *clientText `json:"host"`
	Password *clientText `json:"password"`
	Version  *clientText `json:"version"`
}

type clientReality struct {
	PublicKey *clientText `json:"public-key"`
	ShortID   *clientText `json:"short-id"`
}

func decodeClientOptions(node *yamlx.Node) (clientOptions, error) {
	var result clientOptions
	if node == nil || nullNode(node) {
		return result, nil
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return result, err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return result, fmt.Errorf("%s: %w", msgClientOptions, model.ErrType)
	}
	// Options of the wrong type read as absent; a switch of another type
	// than a boolean is off (stored:"gate") and reported.
	issues, err := model.DecodeStored(data, &result, "")
	if err != nil {
		return result, fmt.Errorf("%s: %w", msgClientOptions, err)
	}
	result.ALPNRaw = bytes.Clone(members["alpn"])
	if raw := bytes.TrimSpace(members["reality-opts"]); len(raw) > 0 && raw[0] == '{' {
		result.RealityMembers = model.Switch(raw, false)
	}
	for _, issue := range issues {
		switch issue.Path {
		case "udp", "udp-relay", "tls", "skip-cert-verify", "reuse", "tfo", "udp-over-tcp":
			logx.Get("backend.converters.clients").Warningf(msgClientBoolean, issue.Path)
		}
	}
	return result, nil
}
func clientOption(value *clientText, fallback string) string {
	if value == nil {
		return fallback
	}
	return sanitizeClientField(string(*value))
}
func clientFlag(value *bool) bool { return value != nil && *value }
func firstClientText(values ...*clientText) string {
	for _, value := range values {
		if value != nil && *value != "" {
			return sanitizeClientField(string(*value))
		}
	}
	return ""
}

func surgeALPN(options clientOptions, name string) string {
	raw := bytes.TrimSpace(options.ALPNRaw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "h3"
	}
	var scalar string
	if json.Unmarshal(raw, &scalar) == nil {
		return sanitizeClientField(scalar)
	}
	var values []*string
	if json.Unmarshal(raw, &values) == nil {
		if len(values) == 0 {
			return ""
		}
		if len(values) == 1 && values[0] != nil {
			return sanitizeClientField(*values[0])
		}
	}
	logx.Get("backend.converters.surge").Warningf(msgSurgeALPN, name, string(raw))
	return ""
}
