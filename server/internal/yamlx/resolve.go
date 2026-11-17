package yamlx

import (
	"regexp"
	"strings"
)

// Legacy loader tags stay with the loader until N7-3d removes it.
const (
	tagStr       = "tag:yaml.org,2002:str"
	tagNull      = "tag:yaml.org,2002:null"
	tagBool      = "tag:yaml.org,2002:bool"
	tagInt       = "tag:yaml.org,2002:int"
	tagFloat     = "tag:yaml.org,2002:float"
	tagSeq       = "tag:yaml.org,2002:seq"
	tagMap       = "tag:yaml.org,2002:map"
	tagBinary    = "tag:yaml.org,2002:binary"
	tagTimestamp = "tag:yaml.org,2002:timestamp"
	tagMerge     = "tag:yaml.org,2002:merge"
	tagValue     = "tag:yaml.org,2002:value"
	tagSet       = "tag:yaml.org,2002:set"
	tagOmap      = "tag:yaml.org,2002:omap"
	tagPairs     = "tag:yaml.org,2002:pairs"
)

// The implicit scalar resolvers of PyYAML's Resolver (YAML 1.1), in the order
// PyYAML registers them. A value is tested against the resolvers registered for
// its first character; the first match decides the tag.
type implicitResolver struct {
	tag   string
	re    *regexp.Regexp
	first string // characters a value may start with; "" in the list means the empty value
	empty bool   // also applies to the empty value
}

func mustResolver(tag, pattern, first string, empty bool) implicitResolver {
	return implicitResolver{tag: tag, re: regexp.MustCompile(pattern), first: first, empty: empty}
}

var pyResolvers = []implicitResolver{
	mustResolver(tagBool, `^(?:yes|Yes|YES|no|No|NO|true|True|TRUE|false|False|FALSE|on|On|ON|off|Off|OFF)$`, "yYnNtTfFoO", false),
	mustResolver(tagFloat, `^(?:[-+]?(?:[0-9][0-9_]*)\.[0-9_]*(?:[eE][-+][0-9]+)?|\.[0-9][0-9_]*(?:[eE][-+][0-9]+)?|[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`, "-+0123456789.", false),
	mustResolver(tagInt, `^(?:[-+]?0b[0-1_]+|[-+]?0[0-7_]+|[-+]?(?:0|[1-9][0-9_]*)|[-+]?0x[0-9a-fA-F_]+|[-+]?[1-9][0-9_]*(?::[0-5]?[0-9])+)$`, "-+0123456789", false),
	mustResolver(tagMerge, `^(?:<<)$`, "<", false),
	mustResolver(tagNull, `^(?:~|null|Null|NULL|)$`, "~nN", true),
	mustResolver(tagTimestamp, `^(?:[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]|[0-9][0-9][0-9][0-9]-[0-9][0-9]?-[0-9][0-9]?(?:[Tt]|[ \t]+)[0-9][0-9]?:[0-9][0-9]:[0-9][0-9](?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9][0-9]?(?::[0-9][0-9])?))?)$`, "0123456789", false),
	mustResolver(tagValue, `^(?:=)$`, "=", false),
	mustResolver("tag:yaml.org,2002:yaml", `^(?:!|&|\*)$`, "!&*", false),
}

// nativeResolvers are the resolvers of oxidns_native.NativeLoader: SafeLoader's
// without bool, int, float and timestamp, plus YAML 1.2 style bool/int/float.
var nativeResolvers = []implicitResolver{
	mustResolver(tagMerge, `^(?:<<)$`, "<", false),
	mustResolver(tagNull, `^(?:~|null|Null|NULL|)$`, "~nN", true),
	mustResolver(tagValue, `^(?:=)$`, "=", false),
	mustResolver("tag:yaml.org,2002:yaml", `^(?:!|&|\*)$`, "!&*", false),
	mustResolver(tagBool, `^(?:true|false|True|False|TRUE|FALSE)$`, "tTfF", false),
	mustResolver(tagInt, `^[-+]?(?:[0-9][0-9_]*|0o[0-7_]+|0x[0-9a-fA-F_]+|0b[01_]+)$`, "-+0123456789", false),
	mustResolver(tagFloat, `^[-+]?(?:(?:[0-9][0-9_]*\.[0-9_]*|\.[0-9_]+)(?:[eE][-+]?[0-9]+)?|[0-9][0-9_]*[eE][-+]?[0-9]+|\.(?:inf|Inf|INF|nan|NaN|NAN))$`, "-+0123456789.", false),
}

// detectTag is BaseResolver.resolve(ScalarNode, value, (True, False)).
func detectTag(value string) string {
	return detectWith(pyResolvers, value)
}

func detectWith(resolvers []implicitResolver, value string) string {
	// Python's "$" also matches before one trailing newline.
	text := strings.TrimSuffix(value, "\n")
	for _, r := range resolvers {
		if value == "" {
			if !r.empty {
				continue
			}
		} else if !strings.ContainsRune(r.first, firstRune(value)) {
			continue
		}
		if r.re.MatchString(text) {
			return r.tag
		}
	}
	return tagStr
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}
