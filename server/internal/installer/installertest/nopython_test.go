package installertest

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestNoPythonBaselineRestoresHistoricalHelpers(t *testing.T) {
	texts, err := legacy()
	if err != nil {
		t.Fatal(err)
	}
	enroll := strings.ReplaceAll(texts.EnrollSource, "SETTINGS_LITERAL", pythonLiteral(`{"url": "http://x/it's", "token": "a\\b"}`))
	tests := []struct{ name, text, want string }{
		{"inspect",
			"docker run --rm --network none --volumes-from 'old name:ro' --entrypoint configflow-agent repo/agent:1 ha-inspect\n",
			"docker exec 'old name' python3 -c " + shellWord(texts.Inspect) + "\n"},
		{"proof",
			"  docker run --rm --network none --mount type=bind,src=/d/agent,dst=/opt/configflow-agent,readonly --entrypoint configflow-agent repo/agent:1 ha-enroll-check http://x/e\nfi\n",
			"  docker run --rm --network none --entrypoint python3 --mount type=bind,src=/d/agent,dst=/opt/configflow-agent,readonly repo/agent:1 -c " + shellWord(texts.EnrollmentProof) + " http://x/e\nfi\n"},
		{"enroll",
			"docker run --rm -i --network dns-ha-net --ip 192.0.2.5 --mount 'type=bind,src=/d $x/agent,dst=/opt/configflow-agent' --entrypoint configflow-agent repo/agent:1 ha-enroll <<'CF_DOCKER_HA_ENROLL'\n" +
				`{"url": "http://x/it's", "token": "a\\b"}` + "\nCF_DOCKER_HA_ENROLL\n",
			"docker run --rm -i --network dns-ha-net --ip 192.0.2.5 --entrypoint python3 --mount 'type=bind,src=/d $x/agent,dst=/opt/configflow-agent' repo/agent:1 - <<'CF_DOCKER_HA_ENROLL'\n" + enroll + "CF_DOCKER_HA_ENROLL\n"},
		{"migration",
			"docker image inspect repo/agent:1 >/dev/null 2>&1 || docker pull repo/agent:1\n" +
				`cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker run --rm -i --network none --volumes-from "$cf_old_id:ro" --entrypoint configflow-agent repo/agent:1 ha-migration-check dns-old 'http://x/(a)' 8082)` + "\n",
			`cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker exec -i "$cf_old_id" python3 -c ` + shellWord(texts.MigrationCheck) + ` dns-old 'http://x/(a)' 8082)` + "\n" +
				"docker image inspect repo/agent:1 >/dev/null 2>&1 || docker pull repo/agent:1\n"},
		{"compose entrypoint",
			"    entrypoint:\n      - configflow-agent\n      - ha-bootstrap\n    command: []\n",
			"    entrypoint:\n      - python3\n      - /run/configflow-compose/bootstrap.py\n    command: []\n"},
		{"prepare", "for cf_file in enrollment.json; do\n", "for cf_file in bootstrap.py enrollment.json; do\n"},
		{"probed inspect",
			"if " + probe("repo/agent:1") + "; then\n  docker run --rm --network none --volumes-from 'old name:ro' --entrypoint configflow-agent repo/agent:1 ha-inspect\nelse\n  printf '%s\\n' '旧' >&2\nfi\n",
			"docker exec 'old name' python3 -c " + shellWord(texts.Inspect) + "\n"},
		{"probed migration",
			"docker image inspect repo/agent:1 >/dev/null 2>&1 || docker pull repo/agent:1\n" + probe("repo/agent:1") + ` || { printf "%s\n" '旧' >&2; exit 1; }` + "\n" +
				`cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker run --rm -i --network none --volumes-from "$cf_old_id:ro" --entrypoint configflow-agent repo/agent:1 ha-migration-check dns-old 'http://x/(a)' 8082)` + "\n",
			`cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker exec -i "$cf_old_id" python3 -c ` + shellWord(texts.MigrationCheck) + ` dns-old 'http://x/(a)' 8082)` + "\n" +
				"docker image inspect repo/agent:1 >/dev/null 2>&1 || docker pull repo/agent:1\n"},
		{"unrelated", "docker run --rm --network host --entrypoint configflow-agent img ha-port-check || return 1\n",
			"docker run --rm --network host --entrypoint configflow-agent img ha-port-check || return 1\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NoPythonBaseline(test.text)
			if got != test.want {
				t.Fatalf("got\n%q\nwant\n%q", got, test.want)
			}
			if again := NoPythonBaseline(got); again != got {
				t.Fatalf("not idempotent:\n%q", again)
			}
		})
	}
}

// probe is installer.haAgentReady(image).
func probe(image string) string {
	return `printf '%s\n' "$(docker run --rm --network none --entrypoint configflow-agent ` + image +
		` -version 2>/dev/null)" | awk -F '[.-]' 'NR == 1 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ && ($1 > 1 || $1 == 1 && ($2 > 9 || $2 == 9 && $3 >= 2)) { ok = 1 } END { exit !ok }'`
}

// TestNoPythonBaselineKeepsRealDifferences is the negative control: text that
// differs from the approved new form in a way that matters is not rewritten,
// so the comparison with Python still reports it.
func TestNoPythonBaselineKeepsRealDifferences(t *testing.T) {
	pull := "docker image inspect repo/agent:1 >/dev/null 2>&1 || docker pull repo/agent:1\n"
	check := func(image string) string {
		return `cf_runtime_env=$(docker inspect --format '{{json .Config.Env}}' "$cf_old_id" | docker run --rm -i --network none --volumes-from "$cf_old_id:ro" --entrypoint configflow-agent ` +
			image + " ha-migration-check dns-old http://x 8082)\n"
	}
	tests := []struct{ name, text string }{
		{"writable inspection mount", "docker run --rm --network none --volumes-from old --entrypoint configflow-agent repo/agent:1 ha-inspect\n"},
		{"inspection with network", "docker run --rm --network host --volumes-from old:ro --entrypoint configflow-agent repo/agent:1 ha-inspect\n"},
		{"probe of another image", "if " + probe("repo/agent:0") + "; then\n  docker run --rm --network none --volumes-from old:ro --entrypoint configflow-agent repo/agent:1 ha-inspect\nelse\n  printf '%s\\n' x >&2\nfi\n"},
		{"check runs another image", pull + check("repo/agent:0")},
		{"writable migration mount", pull + strings.Replace(check("repo/agent:1"), `:ro"`, `"`, 1)},
		{"probe not after the pull", probe("repo/agent:1") + ` || { printf "%s\n" x >&2; exit 1; }` + "\n"},
		{"probe after another pull", "docker pull repo/agent:0\n" + probe("repo/agent:1") + ` || { printf "%s\n" x >&2; exit 1; }` + "\n"},
		{"proof with network", "docker run --rm --network host --mount type=bind,src=/d,dst=/opt/configflow-agent,readonly --entrypoint configflow-agent repo/agent:1 ha-enroll-check http://x\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NoPythonBaseline(test.text)
			kept := !strings.Contains(got, "python3")
			if strings.Contains(test.name, "probe") {
				kept = strings.Contains(got, "-version") // the probe text stays and still differs from Python
			}
			if !kept {
				t.Fatalf("real difference rewritten away:\n%q", got)
			}
		})
	}
}

func TestNoPythonBundleRestoresBootstrap(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, member := range []struct{ name, data string }{
		{"compose.yaml", "services:\n  agent:\n    entrypoint:\n      - configflow-agent\n      - ha-bootstrap\n"},
		{"prepare.sh", "for cf_file in enrollment.json; do\n"},
		{"enrollment.json", "{}\n"},
	} {
		header := &zip.FileHeader{Name: member.name, Method: zip.Deflate, ExternalAttrs: (0o700 | 0o100000) << 16}
		w, _ := writer.CreateHeader(header)
		io.WriteString(w, member.data)
	}
	writer.Close()
	encoded := base64.StdEncoding.EncodeToString(archive.Bytes())
	baseline, err := NoPythonBundle(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(baseline)
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	members := map[string]string{}
	for _, file := range reader.File {
		names = append(names, file.Name)
		stream, _ := file.Open()
		data, _ := io.ReadAll(stream)
		members[file.Name] = string(data)
		if file.Name == "bootstrap.py" && file.ExternalAttrs>>16 != 0o100600 {
			t.Fatalf("bootstrap.py mode %o", file.ExternalAttrs>>16)
		}
	}
	if strings.Join(names, ",") != "compose.yaml,prepare.sh,bootstrap.py,enrollment.json" ||
		members["bootstrap.py"] != LegacyBundleBootstrap() || !strings.Contains(members["compose.yaml"], "- /run/configflow-compose/bootstrap.py\n") ||
		members["prepare.sh"] != "for cf_file in bootstrap.py enrollment.json; do\n" {
		t.Fatalf("%v %q", names, members)
	}
	if again, _ := NoPythonBundle(baseline); again != baseline {
		t.Fatal("historical bundle changed")
	}
}
