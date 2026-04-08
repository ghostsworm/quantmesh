package web

import (
	"strings"
	"testing"
)

func TestStripHTMLTags_NoPanicOnScriptStyle(t *testing.T) {
	t.Parallel()
	// 歷史上曾用含 \1 的正則，導致 MustCompile panic、市場情報 API 500
	in := `<div><script>alert(1)</script><p>ok</p><style type="text/css">.a{}</style></div>`
	out := stripHTMLTags(in)
	if strings.Contains(out, "alert") || strings.Contains(out, ".a{}") {
		t.Fatalf("expected script/style stripped, got %q", out)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected text preserved, got %q", out)
	}
}

func TestStripHTMLTags_MultilineScript(t *testing.T) {
	t.Parallel()
	in := "<script>\nconsole.log('x')\n</script>Hello"
	out := stripHTMLTags(in)
	if strings.Contains(out, "console") {
		t.Fatalf("expected multiline script removed, got %q", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Fatalf("expected tail text, got %q", out)
	}
}
