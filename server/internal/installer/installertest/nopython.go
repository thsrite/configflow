package installertest

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

// no-python-legacy.json holds the Python helpers the Docker HA guide and the
// Compose bundle used to run inside the Agent image before Agent 1.9.2-go,
// verbatim, as reference data for comparisons with recorded Python output.
//
//go:embed no-python-legacy.json
var legacyJSON []byte

type legacyTexts struct {
	BundleBootstrap string `json:"bundleBootstrap"`
	EnrollSource    string `json:"enrollSource"`
	EnrollmentProof string `json:"enrollmentProof"`
	MigrationCheck  string `json:"migrationCheck"`
	Inspect         string `json:"inspect"`
}

var legacy = sync.OnceValues(func() (legacyTexts, error) {
	var texts legacyTexts
	if err := json.Unmarshal(legacyJSON, &texts); err != nil {
		return texts, fmt.Errorf("read legacy Docker HA helpers: %w", err)
	}
	return texts, nil
})

// LegacyBundleBootstrap returns bootstrap.py of Compose bundles generated
// before Agent 1.9.2-go.
func LegacyBundleBootstrap() string {
	texts, err := legacy()
	if err != nil {
		panic(err)
	}
	return texts.BundleBootstrap
}

// word is one shlex.quote()d shell word.
const word = `(?:'(?:[^']|'"'"')*'|[A-Za-z0-9_@%+=:,./-]+)`

var (
	inspectRun  = regexp.MustCompile(`docker run --rm --network none --volumes-from (` + word + `) --entrypoint configflow-agent ` + word + ` ha-inspect\n`)
	proofRun    = regexp.MustCompile(`docker run --rm --network none --mount (` + word + `) --entrypoint configflow-agent (` + word + `) ha-enroll-check `)
	enrollRun   = regexp.MustCompile(`docker run --rm -i ((?:` + word + ` )+?)--mount (` + word + `) --entrypoint configflow-agent (` + word + `) ha-enroll <<'CF_DOCKER_HA_ENROLL'\n([^\n]*)\nCF_DOCKER_HA_ENROLL\n`)
	migrationRe = regexp.MustCompile(`(docker image inspect ` + word + ` >/dev/null 2>&1 \|\| docker pull ` + word + `\n)cf_runtime_env=\$\(docker inspect --format '\{\{json \.Config\.Env\}\}' "\$cf_old_id" \| docker run --rm -i --network none --volumes-from "\$cf_old_id:ro" --entrypoint configflow-agent ` + word + ` ha-migration-check ([^\n]*)\)\n`)
	entrypoint  = regexp.MustCompile(`(\n[ ]*)- configflow-agent\n([ ]*)- ha-bootstrap\n`)
)

// bundleFiles are the literal prepare.sh changes of the bundle without
// bootstrap.py, new text first.
var bundleFiles = [][2]string{
	{"for cf_file in compose.yaml prepare.sh enrollment.json README.txt; do", "for cf_file in compose.yaml prepare.sh bootstrap.py enrollment.json README.txt; do"},
	{`case "${cf_entry##*/}" in compose.yaml|prepare.sh|enrollment.json|README.txt) ;;`, `case "${cf_entry##*/}" in compose.yaml|prepare.sh|bootstrap.py|enrollment.json|README.txt) ;;`},
	{"for cf_file in enrollment.json; do", "for cf_file in bootstrap.py enrollment.json; do"},
}

func unquote(w string) string {
	if strings.HasPrefix(w, "'") {
		return strings.ReplaceAll(w[1:len(w)-1], `'"'"'`, "'")
	}
	return w
}

// pythonLiteral is the single-quoted Python string the old enrollment script
// embedded its settings JSON in.
func pythonLiteral(text string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(text, `\`, `\\`), "'", `\'`) + "'"
}

// NoPythonBaseline rewrites Docker HA text generated since Agent 1.9.2-go
// (configflow-agent ha-* subcommands, bundle without bootstrap.py) back into
// the historical form that ran Python helpers in the Agent image. Every other
// byte is unchanged and historical text passes through as is.
func NoPythonBaseline(text string) string {
	if !strings.Contains(text, "configflow-agent") && !strings.Contains(text, "enrollment.json") {
		return text
	}
	texts, err := legacy()
	if err != nil {
		panic(err)
	}
	text = inspectRun.ReplaceAllStringFunc(text, func(match string) string {
		name := strings.TrimSuffix(unquote(inspectRun.FindStringSubmatch(match)[1]), ":ro")
		return "docker exec " + shellWord(name) + " python3 -c " + shellWord(texts.Inspect) + "\n"
	})
	text = proofRun.ReplaceAllString(text, "docker run --rm --network none --entrypoint python3 --mount $1 $2 -c "+
		strings.ReplaceAll(shellWord(texts.EnrollmentProof), "$", "$$")+" ")
	text = enrollRun.ReplaceAllStringFunc(text, func(match string) string {
		m := enrollRun.FindStringSubmatch(match)
		source := strings.ReplaceAll(texts.EnrollSource, "SETTINGS_LITERAL", pythonLiteral(m[4]))
		return "docker run --rm -i " + m[1] + "--entrypoint python3 --mount " + m[2] + " " + m[3] + " - <<'CF_DOCKER_HA_ENROLL'\n" + source + "CF_DOCKER_HA_ENROLL\n"
	})
	text = migrationRe.ReplaceAllStringFunc(text, func(match string) string {
		m := migrationRe.FindStringSubmatch(match)
		return `cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker exec -i "$cf_old_id" python3 -c ` +
			shellWord(texts.MigrationCheck) + " " + m[2] + ")\n" + m[1]
	})
	text = entrypoint.ReplaceAllString(text, "$1- python3\n$2- /run/configflow-compose/bootstrap.py\n")
	for _, change := range bundleFiles {
		text = strings.ReplaceAll(text, change[0], change[1])
	}
	return text
}

// NoPythonBundle is NoPythonBaseline for a base64 Compose bundle: compose.yaml
// and prepare.sh are rewritten and bootstrap.py (0600) is put back after
// prepare.sh. Member data, order and modes are the contract; the compressed
// bytes are not.
func NoPythonBundle(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", err
	}
	for _, file := range reader.File {
		if file.Name == "bootstrap.py" {
			return encoded, nil
		}
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	add := func(header zip.FileHeader, data []byte) error {
		member, err := writer.CreateHeader(&header)
		if err == nil {
			_, err = member.Write(data)
		}
		return err
	}
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			return "", err
		}
		if file.Name == "compose.yaml" || file.Name == "prepare.sh" {
			data = []byte(NoPythonBaseline(string(data)))
		}
		if err := add(file.FileHeader, data); err != nil {
			return "", err
		}
		if file.Name == "prepare.sh" {
			header := file.FileHeader
			header.Name = "bootstrap.py"
			header.ExternalAttrs = (0o600 | 0o100000) << 16
			if err := add(header, []byte(LegacyBundleBootstrap())); err != nil {
				return "", err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(output.Bytes()), nil
}

// noPythonGuide applies NoPythonBaseline to every script field of a guide
// answer and NoPythonBundle to its Compose bundle.
func noPythonGuide(data []byte) ([]byte, error) {
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, err
	}
	changed := false
	rewrite := func(raw json.RawMessage) (json.RawMessage, error) {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return raw, nil
		}
		baseline := NoPythonBaseline(text)
		if baseline == text {
			return raw, nil
		}
		changed = true
		return json.Marshal(baseline)
	}
	for _, key := range []string{"install_command", "inspection_command", "rollback_command", "identity_command", "compose"} {
		if raw, ok := answer[key]; ok {
			value, err := rewrite(raw)
			if err != nil {
				return nil, err
			}
			answer[key] = value
		}
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(answer["command_blocks"], &blocks) == nil && blocks != nil {
		for _, block := range blocks {
			value, err := rewrite(block["command"])
			if err != nil {
				return nil, err
			}
			block["command"] = value
		}
		if changed {
			answer["command_blocks"], _ = json.Marshal(blocks)
		}
	}
	var bundle map[string]json.RawMessage
	if json.Unmarshal(answer["compose_bundle"], &bundle) == nil && bundle != nil {
		var encoded string
		if json.Unmarshal(bundle["content_base64"], &encoded) == nil {
			baseline, err := NoPythonBundle(encoded)
			if err != nil {
				return nil, err
			}
			if baseline != encoded {
				changed = true
				bundle["content_base64"], _ = json.Marshal(baseline)
				answer["compose_bundle"], _ = json.Marshal(bundle)
			}
		}
	}
	if !changed {
		return data, nil
	}
	return json.Marshal(answer)
}
