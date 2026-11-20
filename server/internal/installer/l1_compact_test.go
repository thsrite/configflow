package installer

import (
	"errors"
	"strings"
	"testing"
)

const l1CompactPathMessage = "持久数据根目录须为规范的绝对路径，不能包含逗号、冒号或换行"

const l1OldCompactPathMessage = "持久数据根目录须为规范的绝对路径，不能包含逗号或换行"

func l1CompactGeneration(request map[string]any) bool {
	return request != nil && mapGetOr(request, "action", "generate") == "generate" &&
		mapGetOr(request, "mode", "new") == "new" && mapStr(request, "install_method") == "compose_compact"
}

// L1 D1 is an explicit Python bug fix. Accept only the new exact refusal for
// compact generation with a colon in its data path; never waive other errors.
func l1CompactColonRefusal(request map[string]any, err error) bool {
	var ve *ValueError
	return l1CompactGeneration(request) &&
		strings.Contains(mapStr(request, "data_dir"), ":") && errors.As(err, &ve) && ve.Message == l1CompactPathMessage
}

func l1CompactPathWording(request map[string]any, prior []any, err error) bool {
	var ve *ValueError
	return l1CompactGeneration(request) && len(prior) >= 2 && prior[0] == "ValueError" &&
		prior[1] == l1OldCompactPathMessage && errors.As(err, &ve) && ve.Message == l1CompactPathMessage
}

func TestL1CompactRejectsColonBeforeRendering(t *testing.T) {
	for _, path := range []string{"/srv/cf:compact", "/srv/nested/dir:part"} {
		t.Run(path, func(t *testing.T) {
			request := composeParams(t.TempDir(), "install_method", "compose_compact", "data_dir", path)
			if result, err := GenerateGuide(request, nil); result != nil || !l1CompactColonRefusal(request, err) {
				t.Fatalf("result %v, err %v", result, err)
			}
			params := cloneMap(request)
			params["enable_mihomo"] = false
			params["enable_oxidns"] = true
			params["enable_dns_ha"] = true
			text, info, err := GenerateCompact(params, request, nil)
			if text != "" || info != nil || !l1CompactColonRefusal(request, err) {
				t.Fatalf("partial output: text %q, info %v, err %v", text, info, err)
			}
		})
	}
}
