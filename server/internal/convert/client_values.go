package convert

import (
	"bytes"
	"encoding/json"
	"fmt"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// clientText decodes textual protocol options. Numeric YAML/JSON scalars are
// accepted as exact text: passwords such as an unquoted 123 must keep working.
// It never passes a number through float64 and never formats bools/containers.
type clientText string

func (s *clientText) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("%s", msgClientText)
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*s = clientText(value)
		return nil
	}
	if data[0] == '-' || data[0] >= '0' && data[0] <= '9' {
		if !json.Valid(data) {
			return fmt.Errorf("%s", msgClientText)
		}
		*s = clientText(string(data))
		return nil
	}
	return fmt.Errorf("%s", msgClientText)
}
func (s clientText) MarshalJSON() ([]byte, error) { return json.Marshal(string(s)) }

// clientTextList accepts an engine option represented by one text/number or a
// list of text/numbers (ALPN, DNS, HTTP paths). Null has no values.
type clientTextList []clientText

func (s *clientTextList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*s = nil
		return nil
	}
	if len(data) > 0 && data[0] == '[' {
		var result []clientText
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		*s = result
		return nil
	}
	var value clientText
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = []clientText{value}
	return nil
}
func (s clientTextList) MarshalJSON() ([]byte, error) { return json.Marshal([]clientText(s)) }
func (s clientTextList) strings() []string {
	result := make([]string, len(s))
	for i, value := range s {
		result[i] = string(value)
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
	ALPNRaw             json.RawMessage         `json:"-"`
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
	UDP                 *bool                   `json:"udp"`
	UDPRelay            *bool                   `json:"udp-relay"`
	TLS                 *bool                   `json:"tls"`
	SkipCertVerify      *bool                   `json:"skip-cert-verify"`
	Reuse               *bool                   `json:"reuse"`
	TFO                 *bool                   `json:"tfo"`
	UDPOverTCP          *bool                   `json:"udp-over-tcp"`
	WS                  *clientTransportOptions `json:"ws-opts"`
	WSConfig            *clientTransportOptions `json:"ws-config"`
	HTTP                *clientTransportOptions `json:"http-opts"`
	GRPC                *clientTransportOptions `json:"grpc-opts"`
	PluginOptions       *clientPluginOptions    `json:"plugin-opts"`
	ObfsOptions         *clientPluginOptions    `json:"obfs-opts"`
	Reality             *clientReality          `json:"reality-opts"`
	Fields              model.Fields
	Raw                 model.Raw
}

func (o *clientOptions) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, o, &o.Raw) }
func (o clientOptions) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&o, &o.Raw) }

type clientTransportOptions struct {
	Path        clientTextList `json:"path"`
	Headers     clientHeaders  `json:"headers"`
	ServiceName *clientText    `json:"grpc-service-name"`
	Fields      model.Fields
	Raw         model.Raw
}

func (o *clientTransportOptions) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, o, &o.Raw)
}
func (o clientTransportOptions) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&o, &o.Raw) }

type clientHeaders struct {
	Host   clientTextList `json:"Host"`
	Fields model.Fields
	Raw    model.Raw
}

func (o *clientHeaders) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, o, &o.Raw) }
func (o clientHeaders) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&o, &o.Raw) }

type clientPluginOptions struct {
	Mode     *clientText `json:"mode"`
	Host     *clientText `json:"host"`
	Password *clientText `json:"password"`
	Version  *clientText `json:"version"`
	Fields   model.Fields
	Raw      model.Raw
}

func (o *clientPluginOptions) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, o, &o.Raw)
}
func (o clientPluginOptions) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&o, &o.Raw) }

type clientReality struct {
	PublicKey *clientText `json:"public-key"`
	ShortID   *clientText `json:"short-id"`
	Fields    model.Fields
	Raw       model.Raw
}

func (o *clientReality) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, o, &o.Raw) }
func (o clientReality) MarshalJSON() ([]byte, error)     { return model.EncodeRecord(&o, &o.Raw) }

func decodeClientOptions(node *yamlx.Node) (clientOptions, error) {
	var result clientOptions
	if node == nil || nullNode(node) {
		return result, nil
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("%s: %w", msgClientOptions, err)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return result, err
	}
	result.ALPNRaw = bytes.Clone(members["alpn"])
	for _, issue := range model.Issues(result) {
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
	return string(*value)
}
func clientFlag(value *bool) bool { return value != nil && *value }
func firstClientText(values ...*clientText) string {
	for _, value := range values {
		if value != nil && *value != "" {
			return string(*value)
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
		return scalar
	}
	var values []*string
	if json.Unmarshal(raw, &values) == nil {
		if len(values) == 0 {
			return ""
		}
		if len(values) == 1 && values[0] != nil {
			return *values[0]
		}
	}
	logx.Get("backend.converters.surge").Warningf(msgSurgeALPN, name, string(raw))
	return ""
}
