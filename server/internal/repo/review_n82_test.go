package repo

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestN82ReviewCorruptOpenHasSafeRecovery(t *testing.T) {
	const secret = "synthetic-secret-never-in-startup-diagnostics"
	for _, tt := range []struct{ name, value string }{
		{"JSON", `{"token":"` + secret + `",]`},
		{"UTF8", `"` + secret + "\xff\""},
		{"large", `["` + strings.Repeat("x", 10<<20) + secret + `"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			// Inject an unknown member: no schema validator can hide the bad row.
			rcSQL(t, r.DatabasePath(), `INSERT INTO fields(scope,owner,field,position,value,revision) VALUES('profile','default','corrupt_future',999,CAST(X'`+hex.EncodeToString([]byte(tt.value))+`' AS TEXT),1)`)
			before := must(os.ReadFile(r.DatabasePath()))(t)
			_, err := OpenDefault(r.DataDir())
			if err == nil {
				t.Fatal("corrupt database opened")
			}
			message := err.Error()
			for _, part := range []string{r.DatabasePath(), "fields", "profile/default/corrupt_future", "字节", "请先停止服务并保留原文件", "从可用备份恢复", "最后一个能正常读取此库的版本"} {
				if !strings.Contains(message, part) {
					t.Fatalf("recovery diagnostic omitted %q", part)
				}
			}
			if len(message) > 1500 || strings.Contains(message, secret) {
				t.Fatal("startup diagnostic exposed contents")
			}
			if !bytes.Equal(before, must(os.ReadFile(r.DatabasePath()))(t)) {
				t.Fatal("failed startup changed database bytes")
			}
		})
	}
}
